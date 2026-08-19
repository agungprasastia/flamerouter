//! Black-box E2E: spawns the real flamerouter binary against mock upstreams.
//! Covers: openai passthrough (stream + non-stream), combo fallback, multi-account
//! fallback, and openai→claude translation on both streaming paths.

use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::routing::post;
use axum::{Json, Router};
use serde_json::{Value, json};
use std::net::TcpListener;
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

// ─── mock upstream helpers ────────────────────────────────────────────────

#[derive(Clone, Default)]
struct ReqLog {
    hits: Arc<AtomicUsize>,
    last_body: Arc<Mutex<Option<Value>>>,
    last_auth: Arc<Mutex<Option<String>>>,
    last_key: Arc<Mutex<Option<String>>>,
}

impl ReqLog {
    fn record(&self, body: &Value, headers: &HeaderMap) {
        self.hits.fetch_add(1, Ordering::SeqCst);
        *self.last_body.lock().unwrap() = Some(body.clone());
        *self.last_auth.lock().unwrap() = headers
            .get("authorization")
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_string());
        *self.last_key.lock().unwrap() = headers
            .get("x-api-key")
            .or_else(|| headers.get("x-goog-api-key"))
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_string());
    }
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .unwrap()
        .local_addr()
        .unwrap()
        .port()
}

async fn spawn_mock(router: Router) -> u16 {
    // Port 0 = kernel allocates atomically at bind time — no drop-then-rebind
    // gap, so parallel tests can never race each other for a port.
    let listener = tokio::net::TcpListener::bind(("127.0.0.1", 0))
        .await
        .unwrap();
    let port = listener.local_addr().unwrap().port();
    tokio::spawn(async move { axum::serve(listener, router).await.unwrap() });
    port
}

async fn openai_mock(always_503: bool) -> (u16, ReqLog) {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new().route(
        "/v1/chat/completions",
        post(move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
            log.record(&body, &headers);
            if always_503 {
                return (StatusCode::SERVICE_UNAVAILABLE, Json(json!({"error": "mock down"})));
            }
            let msg = format!("Hi from openai {}", body["model"].as_str().unwrap_or("?"));
            (
                StatusCode::OK,
                Json(json!({
                    "id": "chatcmpl-e2e",
                    "object": "chat.completion",
                    "model": body["model"],
                    "choices": [{"index": 0, "message": {"role": "assistant", "content": msg}, "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": 2, "completion_tokens": 2, "total_tokens": 4}
                })),
            )
        }),
    )
    .with_state(l);
    let port = spawn_mock(app).await;
    (port, log)
}

async fn openai_stream_mock() -> (u16, ReqLog) {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new().route(
        "/v1/chat/completions",
        post(move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
            log.record(&body, &headers);
            let model = body["model"].as_str().unwrap_or("?").to_string();
            let stream = async_stream::stream! {
                for t in ["Hello", " there"] {
                    let chunk = json!({
                        "id": "chatcmpl-s",
                        "object": "chat.completion.chunk",
                        "model": model,
                        "choices": [{"index": 0, "delta": {"content": t}, "finish_reason": null}]
                    });
                    yield Ok::<_, std::io::Error>(axum::body::Bytes::from(format!("data: {}\n\n", chunk)));
                }
                yield Ok(axum::body::Bytes::from("data: [DONE]\n\n"));
            };
            (
                StatusCode::OK,
                axum::response::Response::builder()
                    .header("content-type", "text/event-stream")
                    .body(axum::body::Body::from_stream(stream))
                    .unwrap(),
            )
        }),
    )
    .with_state(l);
    let port = spawn_mock(app).await;
    (port, log)
}

async fn claude_mock() -> (u16, ReqLog) {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new().route(
        "/v1/messages",
        post(move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
            log.record(&body, &headers);
            let stream = body["stream"].as_bool().unwrap_or(false);
            let model = body["model"].as_str().unwrap_or("?").to_string();
            if stream {
                let events = [
                    json!({"type":"message_start","message":{"id":"msg_01","role":"assistant","model":model,"content":[],"usage":{"input_tokens":5,"output_tokens":1}}}),
                    json!({"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}),
                    json!({"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}),
                    json!({"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}),
                    json!({"type":"content_block_stop","index":0}),
                    json!({"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}),
                    json!({"type":"message_stop"}),
                ];
                let stream = async_stream::stream! {
                    for e in events {
                        yield Ok::<_, std::io::Error>(axum::body::Bytes::from(format!("event: {}\ndata: {}\n\n", e["type"].as_str().unwrap(), e)));
                    }
                };
                return (
                    StatusCode::OK,
                    axum::response::Response::builder()
                        .header("content-type", "text/event-stream")
                        .body(axum::body::Body::from_stream(stream))
                        .unwrap(),
                )
                .into_response();
            }
            (
                StatusCode::OK,
                Json(json!({
                    "id": "msg_01",
                    "type": "message",
                    "role": "assistant",
                    "model": model,
                    "content": [{"type": "text", "text": "Hello there"}],
                    "stop_reason": "end_turn",
                    "stop_sequence": null,
                    "usage": {"input_tokens": 5, "output_tokens": 2}
                })),
            )
                .into_response()
        }),
    )
    .with_state(l);
    let port = spawn_mock(app).await;
    (port, log)
}

// ─── flamerouter binary harness ───────────────────────────────────────────

fn config_path(name: &str) -> std::path::PathBuf {
    std::path::Path::new(env!("CARGO_TARGET_TMPDIR")).join(format!("{name}.json"))
}

fn write_config(name: &str, providers: Value, combos: Option<Value>) -> std::path::PathBuf {
    write_config_full(name, providers, combos, "fallback", 1)
}

fn write_config_full(
    name: &str,
    providers: Value,
    combos: Option<Value>,
    strategy: &str,
    sticky_limit: usize,
) -> std::path::PathBuf {
    let mut root = json!({"providers": providers});
    if let Some(c) = combos {
        root["combos"] = c;
        root["combo_strategy"] = json!(strategy);
        root["combo_sticky_limit"] = json!(sticky_limit);
    }
    let p = config_path(name);
    std::fs::write(&p, serde_json::to_string_pretty(&root).unwrap()).unwrap();
    p
}

struct Server {
    child: Child,
    port: u16,
}

impl Drop for Server {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

async fn spawn_router(
    name: &str,
    cfg: std::path::PathBuf,
    base_urls: &[(&str, u16)],
    extra_envs: &[(&str, String)],
) -> (Server, std::path::PathBuf) {
    let urls: Vec<(String, String)> = base_urls
        .iter()
        .map(|(p, port)| (p.to_string(), format!("http://127.0.0.1:{port}/v1")))
        .collect();
    spawn_router_with_raw_bases(name, cfg, &urls, extra_envs).await
}

async fn spawn_router_with_raw_bases(
    name: &str,
    cfg: std::path::PathBuf,
    base_urls: &[(String, String)],
    extra_envs: &[(&str, String)],
) -> (Server, std::path::PathBuf) {
    let data_dir =
        std::path::Path::new(env!("CARGO_TARGET_TMPDIR")).join(format!("e2e-data-{name}"));
    // The router is a separate process: free_port has a drop-then-bind gap, so a
    // concurrent test can steal the port — retry with a fresh port when the child dies.
    for _ in 0..3 {
        let _ = std::fs::remove_dir_all(&data_dir);
        let port = free_port();
        let mut cmd = Command::new(env!("CARGO_BIN_EXE_open-sse"));
        cmd.env("FLAMEROUTER_CONFIG", &cfg)
            .env("FLAMEROUTER_DATA_DIR", &data_dir)
            .env("PORT", port.to_string())
            .env("RUST_LOG", "off")
            .stdout(Stdio::null())
            .stderr(Stdio::null());
        for (provider, url) in base_urls {
            cmd.env(
                format!(
                    "FLAMEROUTER_BASE_URL_{}",
                    provider.to_uppercase().replace('-', "_")
                ),
                url,
            );
        }
        for (k, v) in extra_envs {
            cmd.env(k, v);
        }
        let child = cmd.spawn().expect("spawn flamerouter binary");
        let mut server = Server { child, port };
        let url = format!("http://127.0.0.1:{}/healthz", server.port);
        for _ in 0..100 {
            if reqwest::get(&url).await.is_ok() {
                return (server, data_dir);
            }
            tokio::time::sleep(Duration::from_millis(50)).await;
        }
        // Port stolen (or child crashed) — kill and retry with a fresh port.
        let _ = server.child.kill();
        let _ = server.child.wait();
        drop(server);
    }
    panic!("flamerouter did not become healthy (3 spawn attempts)");
}

async fn post_json(server: &Server, path: &str, body: Value) -> reqwest::Response {
    post_json_headers(server, path, body, &[]).await
}

async fn post_json_headers(
    server: &Server,
    path: &str,
    body: Value,
    extra_headers: &[(&str, &str)],
) -> reqwest::Response {
    let mut req = reqwest::Client::new()
        .post(format!("http://127.0.0.1:{}{}", server.port, path))
        .json(&body);
    for (k, v) in extra_headers {
        req = req.header(*k, *v);
    }
    req.send().await.expect("request to flamerouter")
}

async fn collect_sse_text(mut resp: reqwest::Response) -> String {
    let mut text = String::new();
    let mut buf: Vec<u8> = Vec::new(); // not needed; we buffer into Vec<u8> per chunk line
    let _ = &mut buf;
    while let Some(chunk) = resp.chunk().await.unwrap() {
        for line in chunk.split(|&b| b == b'\n') {
            let line = String::from_utf8_lossy(line);
            if let Some(data) = line.strip_prefix("data: ") {
                if data == "[DONE]" {
                    return text;
                }
                if let Ok(v) = serde_json::from_str::<Value>(data) {
                    if let Some(d) = v.pointer("/choices/0/delta/content") {
                        text.push_str(d.as_str().unwrap_or(""));
                    }
                }
            }
        }
    }
    text
}

/// Collect typed SSE events (`event: X` + `data: json` pairs) from a response.
async fn collect_typed_events(resp: reqwest::Response) -> Vec<(String, Value)> {
    collect_typed_events_from_text(&resp.text().await.unwrap())
}

/// Parse typed SSE events from raw body text (keeps the raw bytes inspectable by callers).
fn collect_typed_events_from_text(text: &str) -> Vec<(String, Value)> {
    let mut out = Vec::new();
    let mut cur_event = String::new();
    for line in text.lines() {
        if let Some(e) = line.strip_prefix("event: ") {
            cur_event = e.to_string();
        } else if let Some(d) = line.strip_prefix("data: ") {
            if let Ok(v) = serde_json::from_str::<Value>(d) {
                out.push((cur_event.clone(), v));
            }
            cur_event.clear();
        }
    }
    out
}

// ─── tests ────────────────────────────────────────────────────────────────

#[tokio::test]
async fn openai_to_openai_nonstream() {
    let (offer_port, log) = openai_mock(false).await;
    let cfg = write_config("e2e-oai-ns", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, _data) = spawn_router("e2e-oai-ns", cfg, &[("openai", offer_port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "openai/gpt-4o-mini", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert!(
        body["choices"][0]["message"]["content"]
            .as_str()
            .unwrap()
            .starts_with("Hi from openai")
    );
    assert_eq!(log.hits.load(Ordering::SeqCst), 1);
    assert_eq!(
        log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer sk-1")
    );
}

#[tokio::test]
async fn openai_to_openai_stream() {
    let (sport, _) = openai_stream_mock().await;
    let cfg = write_config("e2e-oai-s", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, _data) = spawn_router("e2e-oai-s", cfg, &[("openai", sport)], &[]).await;

    let resp = post_json(&server, "/v1/chat/completions", json!({"model": "openai/gpt-4o-mini", "stream": true, "messages": [{"role": "user", "content": "hi"}]}))
        .await;
    assert_eq!(resp.status(), 200);
    assert_eq!(collect_sse_text(resp).await, "Hello there");
}

#[tokio::test]
async fn combo_fallback_503_to_claude_stream() {
    // openai upstream always 503 → combo must fall through to claude, translated to OpenAI SSE.
    let (oai_port, oai_log) = openai_mock(true).await;
    let (cl_port, cl_log) = claude_mock().await;
    let cfg = write_config(
        "e2e-combo",
        json!({
            "openai": {"api_key": "sk-1"},
            "claude": {"api_key": "sk-ant-1"}
        }),
        Some(json!({"duo": ["openai/gpt-4o-mini", "claude/claude-3-5-sonnet"]})),
    );
    let (server, _data) = spawn_router(
        "e2e-combo",
        cfg,
        &[("openai", oai_port), ("claude", cl_port)],
        &[],
    )
    .await;

    let resp = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "duo", "stream": true, "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    assert_eq!(collect_sse_text(resp).await, "Hello there");

    assert_eq!(oai_log.hits.load(Ordering::SeqCst), 1);
    assert_eq!(cl_log.hits.load(Ordering::SeqCst), 1);
    // claude upstream must have received a TRANSLATED request: array blocks + x-api-key.
    let cl_body = cl_log.last_body.lock().unwrap().clone().unwrap();
    assert!(
        cl_body["messages"][0]["content"].is_array(),
        "expected translated block content, got {:?}",
        cl_body
    );
    assert_eq!(cl_body["messages"][0]["content"][0]["text"], "hi");
    assert_eq!(cl_log.last_key.lock().unwrap().as_deref(), Some("sk-ant-1"));
}

#[tokio::test]
async fn multi_account_fallback_bad_key_then_good() {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new().route(
        "/v1/chat/completions",
        post(move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
            log.record(&body, &headers);
            let auth = headers
                .get("authorization")
                .and_then(|v| v.to_str().ok())
                .unwrap_or("")
                .to_string();
            if auth == "Bearer sk-bad" {
                return (StatusCode::UNAUTHORIZED, Json(json!({"error": "bad key"})));
            }
            (
                StatusCode::OK,
                Json(json!({
                    "id": "chatcmpl-m",
                    "object": "chat.completion",
                    "model": body["model"],
                    "choices": [{"index": 0, "message": {"role": "assistant", "content": "multi-account ok"}, "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": 2, "completion_tokens": 2, "total_tokens": 4}
                })),
            )
        }),
    )
    .with_state(l);
    let mport = spawn_mock(app).await;

    let cfg = write_config(
        "e2e-macc",
        json!({
            "openai": [{"api_key": "sk-bad"}, {"api_key": "sk-good"}]
        }),
        None,
    );
    let (server, _data) = spawn_router("e2e-macc", cfg, &[("openai", mport)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "openai/gpt-4o-mini", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    assert_eq!(
        resp.json::<Value>().await.unwrap()["choices"][0]["message"]["content"],
        "multi-account ok"
    );
    assert_eq!(
        log.hits.load(Ordering::SeqCst),
        2,
        "bad key attempt first, then good key"
    );
}

#[tokio::test]
async fn openai_to_claude_nonstream_translated_back() {
    let (cl_port, cl_log) = claude_mock().await;
    let cfg = write_config(
        "e2e-cl-ns",
        json!({"claude": {"api_key": "sk-ant-1"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-cl-ns", cfg, &[("claude", cl_port)], &[]).await;

    let resp = post_json(&server, "/v1/chat/completions", json!({"model": "claude/claude-3-5-sonnet", "messages": [{"role": "user", "content": "hi"}]}))
        .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    // Client speaks OpenAI — response must be OpenAI chat.completion shape.
    assert!(
        body.get("choices").is_some(),
        "expected OpenAI-shaped response to OpenAI client, got: {body}"
    );
    assert_eq!(body["choices"][0]["message"]["content"], "Hello there");
    let cl_body = cl_log.last_body.lock().unwrap().clone().unwrap();
    assert!(
        cl_body["messages"][0]["content"].is_array(),
        "request must be translated to Claude blocks: {cl_body}"
    );
}

#[tokio::test]
async fn models_listing_lists_configured_providers_and_combos() {
    let (oai_port, _) = openai_mock(false).await;
    let cfg = write_config(
        "e2e-models",
        json!({
            "openai": {"api_key": "sk-1"},
            "claude": {"api_key": "sk-ant-1"}
        }),
        Some(json!({"duo": ["openai/gpt-4o-mini", "claude/claude-3-5-sonnet"]})),
    );
    let (server, _data) = spawn_router("e2e-models", cfg, &[("openai", oai_port)], &[]).await;

    let resp = reqwest::get(format!("http://127.0.0.1:{}/v1/models", server.port))
        .await
        .unwrap();
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["object"], "list");
    let ids: Vec<String> = body["data"]
        .as_array()
        .unwrap()
        .iter()
        .map(|m| m["id"].as_str().unwrap().to_string())
        .collect();
    assert!(ids.contains(&"openai/*".to_string()), "ids: {ids:?}");
    assert!(ids.contains(&"claude/*".to_string()), "ids: {ids:?}");
    assert!(ids.contains(&"duo".to_string()), "ids: {ids:?}");
}

#[tokio::test]
async fn usage_log_and_usage_json_recorded() {
    let (oai_port, _) = openai_mock(false).await;
    let cfg = write_config("e2e-usage", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, data_dir) = spawn_router("e2e-usage", cfg, &[("openai", oai_port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "openai/gpt-4o-mini", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    tokio::time::sleep(Duration::from_millis(100)).await;

    let usage: Value =
        serde_json::from_str(&std::fs::read_to_string(data_dir.join("usage.json")).unwrap())
            .unwrap();
    assert_eq!(usage["openai"]["requests"], 1);
    let log = std::fs::read_to_string(data_dir.join("log.txt")).unwrap();
    assert!(
        log.contains("/v1/chat/completions openai gpt-4o-mini 200"),
        "log: {log}"
    );
}

#[tokio::test]
async fn oauth_401_triggers_refresh_and_retries_with_new_token() {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new().route(
        "/v1/chat/completions",
        post(move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
            log.record(&body, &headers);
            let auth = headers.get("authorization").and_then(|v| v.to_str().ok()).unwrap_or("").to_string();
            if auth == "Bearer tok-expired" {
                return (StatusCode::UNAUTHORIZED, Json(json!({"error": "expired token"})));
            }
            (
                StatusCode::OK,
                Json(json!({
                    "id": "chatcmpl-r",
                    "object": "chat.completion",
                    "model": body["model"],
                    "choices": [{"index": 0, "message": {"role": "assistant", "content": "refreshed ok"}, "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
                })),
            )
        }),
    )
    .with_state(l);
    let xai_port = spawn_mock(app).await;

    let token_hits = Arc::new(AtomicUsize::new(0));
    let th = token_hits.clone();
    let token_app = Router::new().route(
        "/oauth/token",
        post(move || async move {
            th.fetch_add(1, Ordering::SeqCst);
            Json(json!({"access_token": "tok-new", "refresh_token": "rt-2", "expires_in": 3600}))
        }),
    );
    let token_port = spawn_mock(token_app).await;

    let cfg = write_config(
        "e2e-oauth",
        json!({"xai": {"access_token": "tok-expired", "refresh_token": "rt-1"}}),
        None,
    );
    let (server, _data) = spawn_router(
        "e2e-oauth",
        cfg,
        &[("xai", xai_port)],
        &[(
            "FLAMEROUTER_OAUTH_TOKEN_URL_XAI",
            format!("http://127.0.0.1:{token_port}/oauth/token"),
        )],
    )
    .await;

    let resp = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "xai/grok-4", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    assert_eq!(
        resp.json::<Value>().await.unwrap()["choices"][0]["message"]["content"],
        "refreshed ok"
    );
    // upstream hit twice: expired token attempt + refreshed retry
    assert_eq!(log.hits.load(Ordering::SeqCst), 2);
    assert_eq!(
        token_hits.load(Ordering::SeqCst),
        1,
        "token endpoint called exactly once"
    );
}

/// Gemini upstream mock: `:streamGenerateContent` (SSE) + `:generateContent` (JSON).
/// Both serve "Hi from gemini" so stream/nonstream tests share one upstream.
async fn gemini_mock() -> (u16, ReqLog) {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new()
        .route(
            "/v1beta/models/:rest",
            post(move |State(log): State<ReqLog>, headers: HeaderMap, Path(rest): Path<String>, Json(body): Json<Value>| async move {
                log.record(&body, &headers);
                if rest.ends_with(":streamGenerateContent") {
                    let stream = async_stream::stream! {
                        let chunk = json!({
                            "responseId": "resp-1",
                            "modelVersion": "gemini-2.5-flash",
                            "candidates": [{"content": {"parts": [{"text": "Hi from gemini"}]}}],
                            "usageMetadata": {"promptTokenCount": 2, "candidatesTokenCount": 3, "totalTokenCount": 5}
                        });
                        yield Ok::<_, std::io::Error>(axum::body::Bytes::from(format!("data: {}\n\n", chunk)));
                        let done = json!({});
                        yield Ok(axum::body::Bytes::from(format!("data: {}\n\n", done)));
                    };
                    return (
                        StatusCode::OK,
                        axum::response::Response::builder()
                            .header("content-type", "text/event-stream")
                            .body(axum::body::Body::from_stream(stream))
                            .unwrap(),
                    )
                        .into_response();
                }
                (
                    StatusCode::OK,
                    Json(json!({
                        "candidates": [{
                            "content": {"parts": [{"text": "Hi from gemini nonstream"}]},
                            "finishReason": "STOP"
                        }],
                        "usageMetadata": {"promptTokenCount": 2, "candidatesTokenCount": 4, "totalTokenCount": 6},
                        "modelVersion": "gemini-2.5-flash"
                    })),
                )
                    .into_response()
            }),
        )
        .with_state(l);
    let port = spawn_mock(app).await;
    (port, log)
}

#[tokio::test]
async fn gemini_stream_translated_to_openai_sse() {
    let (g_port, log) = gemini_mock().await;
    let cfg = write_config("e2e-gem", json!({"gemini": {"api_key": "gkey-1"}}), None);
    // gemini base_url must be the /models root (build_url appends {model}:streamGenerateContent),
    // so pass the raw base instead of the default /v1-suffixed one.
    let (server, _data) = spawn_router_with_raw_bases(
        "e2e-gem",
        cfg,
        &[(
            "gemini".to_string(),
            format!("http://127.0.0.1:{g_port}/v1beta/models"),
        )],
        &[],
    )
    .await;

    let resp = post_json(&server, "/v1/chat/completions", json!({"model": "gemini/gemini-2.5-flash", "stream": true, "messages": [{"role": "user", "content": "hi"}]}))
        .await;
    assert_eq!(resp.status(), 200);
    assert_eq!(collect_sse_text(resp).await, "Hi from gemini");

    let g_body = log.last_body.lock().unwrap().clone().unwrap();
    assert_eq!(g_body["contents"][0]["role"], "user");
    assert_eq!(g_body["contents"][0]["parts"][0]["text"], "hi");
    assert_eq!(log.last_key.lock().unwrap().as_deref(), Some("gkey-1"));
}

#[tokio::test]
async fn gemini_nonstream_translated_to_openai_json() {
    let (g_port, _log) = gemini_mock().await;
    let cfg = write_config("e2e-gem-ns", json!({"gemini": {"api_key": "gkey-1"}}), None);
    let (server, _data) = spawn_router_with_raw_bases(
        "e2e-gem-ns",
        cfg,
        &[(
            "gemini".to_string(),
            format!("http://127.0.0.1:{g_port}/v1beta/models"),
        )],
        &[],
    )
    .await;

    let resp = post_json(&server, "/v1/chat/completions", json!({ "model": "gemini/gemini-2.5-flash", "messages": [{"role": "user", "content": "hi"}]}))
        .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    // Gemini candidates body must be converted to OpenAI chat.completion shape, not passed through.
    assert_eq!(
        body["choices"][0]["message"]["content"],
        "Hi from gemini nonstream"
    );
    assert_eq!(body["choices"][0]["finish_reason"], "stop");
    assert_eq!(body["usage"]["prompt_tokens"], 2);
    assert_eq!(body["usage"]["completion_tokens"], 4);
    assert_eq!(body["usage"]["total_tokens"], 6);
}

#[tokio::test]
async fn responses_upstream_nonstream_translated_to_openai_json() {
    let log = ReqLog::default();
    let l = log.clone();
    let app =
        Router::new()
            .route(
                "/v1/responses",
                post(
                    move |State(log): State<ReqLog>,
                          headers: HeaderMap,
                          Json(body): Json<Value>| async move {
                        log.record(&body, &headers);
                        (
                StatusCode::OK,
                Json(json!({
                    "id": "resp_mock_1",
                    "object": "response",
                    "model": "grok-codex-1",
                    "output": [{
                        "type": "message",
                        "role": "assistant",
                        "status": "completed",
                        "content": [{"type": "output_text", "text": "Hi from responses"}]
                    }],
                    "usage": {"input_tokens": 3, "output_tokens": 5, "total_tokens": 8}
                })),
            )
                .into_response()
                    },
                ),
            )
            .with_state(l);
    let rport = spawn_mock(app).await;

    let cfg = write_config("e2e-rsp-ns", json!({"codex": {"api_key": "cx-1"}}), None);
    let (server, _data) = spawn_router("e2e-rsp-ns", cfg, &[("codex", rport)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/chat/completions",
        json!({ "model": "codex/grok-codex-1", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    // Responses body must be converted to OpenAI chat.completion shape, not passed through.
    assert_eq!(
        body["choices"][0]["message"]["content"],
        "Hi from responses"
    );
    assert_eq!(body["choices"][0]["finish_reason"], "stop");
    assert_eq!(body["usage"]["prompt_tokens"], 3);
    assert_eq!(body["usage"]["completion_tokens"], 5);
}

#[tokio::test]
async fn messages_client_nonstream_to_gemini_upstream_translated_to_claude_message() {
    let (g_port, _log) = gemini_mock().await;
    let cfg = write_config(
        "e2e-msg-gem-ns",
        json!({"gemini": {"api_key": "gkey-1"}}),
        None,
    );
    let (server, _data) = spawn_router_with_raw_bases(
        "e2e-msg-gem-ns",
        cfg,
        &[(
            "gemini".to_string(),
            format!("http://127.0.0.1:{g_port}/v1beta/models"),
        )],
        &[],
    )
    .await;

    let resp = post_json(
        &server,
        "/v1/messages",
        json!({"model": "gemini/gemini-2.5-flash", "max_tokens": 16, "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    // Claude client must receive an Anthropic message shape, converted from gemini candidates.
    assert_eq!(body["type"], "message");
    assert_eq!(body["content"][0]["type"], "text");
    assert_eq!(body["content"][0]["text"], "Hi from gemini nonstream");
    assert_eq!(body["stop_reason"], "end_turn");
    assert_eq!(body["usage"]["input_tokens"], 2);
    assert_eq!(body["usage"]["output_tokens"], 4);
}

#[tokio::test]
async fn messages_client_stream_to_openai_upstream_translated_to_claude_sse() {
    // Client speaks Anthropic /v1/messages; upstream is an OpenAI provider.
    // Request must be translated claude→openai, response openai SSE → claude typed SSE.
    let (sport, oai_log) = openai_stream_mock().await;
    let cfg = write_config("e2e-msg", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, _data) = spawn_router("e2e-msg", cfg, &[("openai", sport)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/messages",
        json!({"model": "openai/gpt-4o-mini", "max_tokens": 16, "stream": true, "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let raw = resp.text().await.unwrap();
    // An Anthropic client must never see OpenAI's `data: [DONE]` terminator.
    assert!(
        !raw.contains("[DONE]"),
        "claude client got an OpenAI [DONE] marker:\n{raw}"
    );
    let events = collect_typed_events_from_text(&raw);
    let types: Vec<&str> = events.iter().map(|(t, _)| t.as_str()).collect();
    assert!(
        types.contains(&"message_start"),
        "missing message_start: {types:?}"
    );
    assert!(
        types.contains(&"content_block_delta"),
        "missing content_block_delta: {types:?}"
    );
    let text: String = events
        .iter()
        .filter(|(t, _)| t == "content_block_delta")
        .map(|(_, v)| v["delta"]["text"].as_str().unwrap_or("").to_string())
        .collect();
    assert_eq!(text, "Hello there");

    // Upstream must have received a translated OpenAI request (string content, Bearer key).
    let oai_body = oai_log.last_body.lock().unwrap().clone().unwrap();
    assert_eq!(oai_body["messages"][0]["content"], "hi");
    assert_eq!(
        oai_log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer sk-1")
    );
}

#[tokio::test]
async fn responses_client_stream_to_claude_upstream_translated_to_responses_sse() {
    // Client speaks OpenAI Responses API /v1/responses; upstream is Anthropic.
    // Response must come back as typed response.* events.
    let (cl_port, cl_log) = claude_mock().await;
    let cfg = write_config("e2e-rsp", json!({"claude": {"api_key": "sk-ant-1"}}), None);
    let (server, _data) = spawn_router("e2e-rsp", cfg, &[("claude", cl_port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/responses",
        json!({"model": "claude/claude-3-5-sonnet", "input": "hi", "stream": true}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let events = collect_typed_events(resp).await;
    let types: Vec<&str> = events.iter().map(|(t, _)| t.as_str()).collect();
    assert!(
        types.contains(&"response.created"),
        "missing response.created: {types:?}"
    );
    let text: String = events
        .iter()
        .filter(|(t, _)| t == "response.output_text.delta")
        .map(|(_, v)| v["delta"].as_str().unwrap_or("").to_string())
        .collect();
    assert_eq!(text, "Hello there");

    // Upstream must have received a translated Anthropic request (block content, x-api-key).
    let cl_body = cl_log.last_body.lock().unwrap().clone().unwrap();
    assert!(
        cl_body["messages"][0]["content"].is_array(),
        "expected translated block content, got {:?}",
        cl_body
    );
    assert_eq!(cl_body["messages"][0]["content"][0]["text"], "hi");
    assert_eq!(cl_log.last_key.lock().unwrap().as_deref(), Some("sk-ant-1"));
}

/// Non-streaming OpenAI mock returning a tag-specific message so callers can
/// tell which upstream served a request.
async fn openai_tagged_mock(tag: &'static str) -> (u16, ReqLog) {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new().route(
        "/v1/chat/completions",
        post(move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
            log.record(&body, &headers);
            (
                StatusCode::OK,
                Json(json!({
                    "id": "chatcmpl-tag",
                    "object": "chat.completion",
                    "model": body["model"],
                    "choices": [{"index": 0, "message": {"role": "assistant", "content": format!("Hello from {tag}")}, "finish_reason": "stop"}]
                })),
            )
                .into_response()
        }),
    )
    .with_state(l);
    let port = spawn_mock(app).await;
    (port, log)
}

#[tokio::test]
async fn combo_round_robin_rotates_across_requests() {
    let (a_port, a_log) = openai_tagged_mock("A").await;
    let (b_port, b_log) = openai_tagged_mock("B").await;
    let cfg = write_config_full(
        "e2e-rr",
        json!({"openai": {"api_key": "sk-1"}, "deepseek": {"api_key": "sk-2"}}),
        Some(json!({"rr": ["openai/gpt-4o-mini", "deepseek/deepseek-chat"]})),
        "round-robin",
        1,
    );
    let (server, data_dir) = spawn_router(
        "e2e-rr",
        cfg,
        &[("openai", a_port), ("deepseek", b_port)],
        &[],
    )
    .await;

    let resp1 = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "rr", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp1.status(), 200);
    let body1: Value = resp1.json().await.unwrap();
    assert_eq!(body1["choices"][0]["message"]["content"], "Hello from A");

    let resp2 = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "rr", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp2.status(), 200);
    let body2: Value = resp2.json().await.unwrap();
    assert_eq!(body2["choices"][0]["message"]["content"], "Hello from B");

    assert_eq!(a_log.hits.load(Ordering::SeqCst), 1);
    assert_eq!(b_log.hits.load(Ordering::SeqCst), 1);

    // Combo traffic must be recorded under the combo name, not dropped.
    let usage: Value =
        serde_json::from_str(&std::fs::read_to_string(data_dir.join("usage.json")).unwrap())
            .unwrap();
    assert_eq!(
        usage["rr"]["requests"], 2,
        "combo requests must be counted: {usage}"
    );
    assert_eq!(usage.get(""), None, "no empty-provider rows");
    assert_eq!(
        usage.get("openai"),
        None,
        "per-model provider rows not expected for combo"
    );
}

#[tokio::test]
async fn provider_credential_header_used_when_config_has_none() {
    let (o_port, o_log) = openai_mock(false).await;
    // Empty provider config — credentials must come from X-Provider-Credential.
    let cfg = write_config("e2e-hdr", json!({}), None);
    let (server, _data) = spawn_router("e2e-hdr", cfg, &[("openai", o_port)], &[]).await;

    let resp = post_json_headers(
        &server,
        "/v1/chat/completions",
        json!({"model": "openai/gpt-4o-mini", "messages": [{"role": "user", "content": "hi"}]}),
        &[("X-Provider-Credential", r#"{"api_key": "sk-hdr"}"#)],
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert!(
        body["choices"][0]["message"]["content"]
            .as_str()
            .unwrap()
            .starts_with("Hi from openai")
    );
    assert_eq!(
        o_log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer sk-hdr")
    );
}

#[tokio::test]
async fn messages_client_nonstream_to_claude_upstream_passthrough() {
    let (cl_port, cl_log) = claude_mock().await;
    let cfg = write_config(
        "e2e-msg-cl-ns",
        json!({"claude": {"api_key": "sk-ant-1"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-msg-cl-ns", cfg, &[("claude", cl_port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/messages",
        json!({"model": "claude/claude-3-5-sonnet", "max_tokens": 16, "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    // Same-format client/upstream: claude message shape passes through untouched.
    assert_eq!(body["type"], "message");
    assert_eq!(body["content"][0]["type"], "text");
    assert_eq!(body["content"][0]["text"], "Hello there");
    assert_eq!(body["stop_reason"], "end_turn");
    assert_eq!(body["usage"]["input_tokens"], 5);
    assert_eq!(cl_log.last_key.lock().unwrap().as_deref(), Some("sk-ant-1"));
}

#[tokio::test]
async fn x_provider_header_selects_provider_for_bare_model() {
    let (o_port, o_log) = openai_mock(false).await;
    let cfg = write_config("e2e-xprov", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, _data) = spawn_router("e2e-xprov", cfg, &[("openai", o_port)], &[]).await;

    // Bare model (no "provider/" prefix) — provider comes from X-Provider header.
    let resp = post_json_headers(
        &server,
        "/v1/chat/completions",
        json!({"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "hi"}]}),
        &[("X-Provider", "openai")],
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert!(
        body["choices"][0]["message"]["content"]
            .as_str()
            .unwrap()
            .starts_with("Hi from openai")
    );

    // Same bare model WITHOUT the header must be rejected (no provider resolvable).
    let resp = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 400);
    assert_eq!(
        o_log.hits.load(Ordering::SeqCst),
        1,
        "upstream must only see the header-mediated request"
    );
}

#[tokio::test]
async fn trae_auth_prefix_on_authorization_header() {
    // Registry: trae auth_scheme = "Cloud-IDE-JWT" → header must be "Cloud-IDE-JWT <jwt>",
    // not the bare token (open-sse trae.js: `Authorization: Cloud-IDE-JWT ${token}`).
    let (o_port, o_log) = openai_mock(false).await;
    let cfg = write_config("e2e-trae", json!({"trae": {"api_key": "jwt-9"}}), None);
    let (server, _data) = spawn_router("e2e-trae", cfg, &[("trae", o_port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "trae/gpt-oss-120b", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    assert_eq!(
        o_log.last_auth.lock().unwrap().as_deref(),
        Some("Cloud-IDE-JWT jwt-9")
    );
}

/// OpenAI Responses upstream mock (streaming): emits response.* typed SSE chunks.
async fn responses_stream_mock() -> (u16, ReqLog) {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new().route(
        "/v1/responses",
        post(move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
            log.record(&body, &headers);
            let stream = async_stream::stream! {
                for t in ["Hello", " there"] {
                    let chunk = json!({
                        "type": "response.output_text.delta",
                        "item_id": "msg_1",
                        "output_index": 0,
                        "content_index": 0,
                        "delta": t
                    });
                    yield Ok::<_, std::io::Error>(axum::body::Bytes::from(format!("data: {}\n\n", chunk)));
                }
                let done = json!({"type": "response.completed", "response": {"id": "resp_1"}});
                yield Ok(axum::body::Bytes::from(format!("data: {}\n\n", done)));
            };
            (
                StatusCode::OK,
                axum::response::Response::builder()
                    .header("content-type", "text/event-stream")
                    .body(axum::body::Body::from_stream(stream))
                    .unwrap(),
            )
                .into_response()
        }),
    )
    .with_state(l);
    let port = spawn_mock(app).await;
    (port, log)
}

#[tokio::test]
async fn openai_client_stream_to_openai_responses_upstream_translated() {
    let (rport, r_log) = responses_stream_mock().await;
    let cfg = write_config("e2e-rsp-str", json!({"codex": {"api_key": "cx-1"}}), None);
    let (server, _data) = spawn_router("e2e-rsp-str", cfg, &[("codex", rport)], &[]).await;

    let resp = post_json(&server, "/v1/chat/completions", json!({"model": "codex/grok-codex-1", "stream": true, "messages": [{"role": "user", "content": "hi"}]}))
        .await;
    assert_eq!(resp.status(), 200);
    assert_eq!(collect_sse_text(resp).await, "Hello there");
    // Upstream must have received a translated responses-format request.
    let r_body = r_log.last_body.lock().unwrap().clone().unwrap();
    assert_eq!(r_body["input"][0]["content"][0]["text"], "hi");
    assert_eq!(
        r_log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer cx-1")
    );
}

#[tokio::test]
async fn all_accounts_failed_relays_last_upstream_error() {
    // Two bad keys — every account 401s; the client must get the upstream error
    // (status + body), not a synthetic 502.
    let log = ReqLog::default();
    let l = log.clone();
    let app =
        Router::new()
            .route(
                "/v1/chat/completions",
                post(
                    move |State(log): State<ReqLog>,
                          headers: HeaderMap,
                          Json(body): Json<Value>| async move {
                        log.record(&body, &headers);
                        (
                            StatusCode::UNAUTHORIZED,
                            Json(json!({"error": {"message": "bad key buddy"}})),
                        )
                    },
                ),
            )
            .with_state(l);
    let mport = spawn_mock(app).await;

    let cfg = write_config(
        "e2e-all-bad",
        json!({"openai": [{"api_key": "sk-bad1"}, {"api_key": "sk-bad2"}]}),
        None,
    );
    let (server, _data) = spawn_router("e2e-all-bad", cfg, &[("openai", mport)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/chat/completions",
        json!({"model": "openai/gpt-4o-mini", "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 401);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["error"]["message"], "bad key buddy");
    assert_eq!(
        log.hits.load(Ordering::SeqCst),
        2,
        "both accounts tried before relay"
    );
}

#[tokio::test]
async fn messages_client_nonstream_to_openai_upstream_translated_to_claude_message() {
    let (o_port, _o_log) = openai_mock(false).await;
    let cfg = write_config("e2e-msg-ns", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, _data) = spawn_router("e2e-msg-ns", cfg, &[("openai", o_port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/messages",
        json!({"model": "openai/gpt-4o-mini", "max_tokens": 16, "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["type"], "message");
    assert_eq!(body["content"][0]["type"], "text");
    assert!(
        body["content"][0]["text"]
            .as_str()
            .unwrap()
            .starts_with("Hi from openai")
    );
    assert_eq!(body["stop_reason"], "end_turn");
}

#[tokio::test]
async fn messages_client_stream_to_claude_upstream_passthrough_sse() {
    let (cl_port, _cl_log) = claude_mock().await;
    let cfg = write_config(
        "e2e-msg-cl-s",
        json!({"claude": {"api_key": "sk-ant-1"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-msg-cl-s", cfg, &[("claude", cl_port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/messages",
        json!({"model": "claude/claude-3-5-sonnet", "max_tokens": 16, "stream": true, "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let events = collect_typed_events(resp).await;
    let text: String = events
        .iter()
        .filter(|(t, _)| t == "content_block_delta")
        .map(|(_, v)| v["delta"]["text"].as_str().unwrap_or("").to_string())
        .collect();
    assert_eq!(text, "Hello there");
}

#[tokio::test]
async fn messages_client_stream_to_gemini_upstream_translated_to_claude_sse() {
    let (g_port, _g_log) = gemini_mock().await;
    let cfg = write_config(
        "e2e-msg-gem-s",
        json!({"gemini": {"api_key": "gkey-1"}}),
        None,
    );
    let (server, _data) = spawn_router_with_raw_bases(
        "e2e-msg-gem-s",
        cfg,
        &[(
            "gemini".to_string(),
            format!("http://127.0.0.1:{g_port}/v1beta/models"),
        )],
        &[],
    )
    .await;

    let resp = post_json(
        &server,
        "/v1/messages",
        json!({"model": "gemini/gemini-2.5-flash", "max_tokens": 16, "stream": true, "messages": [{"role": "user", "content": "hi"}]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let events = collect_typed_events(resp).await;
    let text: String = events
        .iter()
        .filter(|(t, _)| t == "content_block_delta")
        .map(|(_, v)| v["delta"]["text"].as_str().unwrap_or("").to_string())
        .collect();
    assert_eq!(text, "Hi from gemini");
}

#[tokio::test]
async fn responses_client_nonstream_to_claude_upstream_translated_to_responses() {
    let (cl_port, _cl_log) = claude_mock().await;
    let cfg = write_config(
        "e2e-rsp-cl-ns",
        json!({"claude": {"api_key": "sk-ant-1"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-rsp-cl-ns", cfg, &[("claude", cl_port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/responses",
        json!({"model": "claude/claude-3-5-sonnet", "input": "hi"}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["object"], "response");
    assert_eq!(body["output"][0]["type"], "message");
    assert_eq!(body["output"][0]["content"][0]["type"], "output_text");
    assert_eq!(body["output"][0]["content"][0]["text"], "Hello there");
    assert_eq!(body["usage"]["input_tokens"], 5);
}

// ─── embeddings ────────────────────────────────────────────────────────────

async fn embeddings_mock() -> (u16, ReqLog) {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new().route(
        "/v1/embeddings",
        post(move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
            log.record(&body, &headers);
            (
                StatusCode::OK,
                Json(json!({
                    "object": "list",
                    "data": [{"object": "embedding", "index": 0, "embedding": [0.1, 0.2, 0.3]}],
                    "model": body["model"],
                    "usage": {"prompt_tokens": 1, "total_tokens": 1}
                })),
            )
        }),
    )
    .with_state(l);
    let port = spawn_mock(app).await;
    (port, log)
}

#[tokio::test]
async fn embeddings_openai_compat_passthrough() {
    let (port, log) = embeddings_mock().await;
    let cfg = write_config("e2e-emb-oai", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, _data) = spawn_router("e2e-emb-oai", cfg, &[("openai", port)], &[]).await;

    // In production the URL is the hardcoded api.openai.com/v1/embeddings; the
    // harness env-overrides FLAMEROUTER_BASE_URL_OPENAI to point at the mock.
    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "openai/text-embedding-3-small", "input": "hello", "dimensions": 256}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["object"], "list");
    assert_eq!(body["data"][0]["embedding"], json!([0.1, 0.2, 0.3]));
    assert_eq!(body["model"], "text-embedding-3-small");
    assert_eq!(log.hits.load(Ordering::SeqCst), 1);
    assert_eq!(
        log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer sk-1")
    );
    let req = log.last_body.lock().unwrap().clone().unwrap();
    assert_eq!(req["input"], "hello");
    assert_eq!(req["model"], "text-embedding-3-small");
    assert_eq!(req["dimensions"].as_f64(), Some(256.0));
}

#[tokio::test]
async fn embeddings_input_validation() {
    let (port, _log) = embeddings_mock().await;
    let cfg = write_config("e2e-emb-val", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, _data) = spawn_router("e2e-emb-val", cfg, &[("openai", port)], &[]).await;

    // missing input → 400
    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "openai/text-embedding-3-small"}),
    )
    .await;
    assert_eq!(resp.status(), 400);
    // non-string/array input → 400
    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "openai/text-embedding-3-small", "input": 42}),
    )
    .await;
    assert_eq!(resp.status(), 400);
    let body: Value = resp.json().await.unwrap();
    assert!(
        body["error"]["message"]
            .as_str()
            .unwrap()
            .contains("string or array")
    );
    // provider without embedding support → 400
    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "claude/claude-3-5-sonnet", "input": "hi"}),
    )
    .await;
    assert_eq!(resp.status(), 400);
    let body: Value = resp.json().await.unwrap();
    assert!(
        body["error"]["message"]
            .as_str()
            .unwrap()
            .contains("does not support embeddings")
    );
    // unknown provider → 400
    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "nosuch/x", "input": "hi"}),
    )
    .await;
    assert_eq!(resp.status(), 400);
    // no credential → 401
    let cfg = write_config("e2e-emb-val2", json!({}), None);
    let (server, _data) = spawn_router("e2e-emb-val2", cfg, &[("openai", port)], &[]).await;
    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "openai/t", "input": "hi"}),
    )
    .await;
    assert_eq!(resp.status(), 401);
}

#[tokio::test]
async fn embeddings_gemini_embed_content_and_batch() {
    let last_op = Arc::new(Mutex::new(None::<String>));
    let last_key = Arc::new(Mutex::new(None::<String>));
    let app = Router::new().route(
        "/v1beta/models/:rest",
        post({
            let last_op = last_op.clone();
            let last_key = last_key.clone();
            move |axum::extract::Query(q): axum::extract::Query<
                std::collections::HashMap<String, String>,
            >,
                  Path(rest): Path<String>,
                  Json(_body): Json<Value>| async move {
                *last_op.lock().unwrap() = Some(rest.clone());
                *last_key.lock().unwrap() = q.get("key").cloned();
                if rest.ends_with(":embedContent") {
                    (
                        StatusCode::OK,
                        Json(json!({"embedding": {"values": [0.5]}})),
                    )
                } else {
                    (
                        StatusCode::OK,
                        Json(json!({"embeddings": [{"values": [1.0]}, {"values": [2.0]}]})),
                    )
                }
            }
        }),
    );
    let g_port = spawn_mock(app).await;
    let cfg = write_config("e2e-emb-gem", json!({"gemini": {"api_key": "gk-9"}}), None);
    let (server, _data) = spawn_router_with_raw_bases(
        "e2e-emb-gem",
        cfg,
        &[(
            "gemini".to_string(),
            format!("http://127.0.0.1:{g_port}/v1beta/models"),
        )],
        &[],
    )
    .await;

    // single string input → embedContent, normalized to OpenAI shape, key in query
    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "gemini/gemini-embedding-001", "input": "hi"}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["object"], "list");
    assert_eq!(body["data"][0]["index"], 0);
    assert_eq!(body["data"][0]["embedding"], json!([0.5]));
    assert_eq!(body["model"], "gemini-embedding-001");
    assert_eq!(body["usage"]["prompt_tokens"], 0);
    assert_eq!(
        last_op.lock().unwrap().as_deref(),
        Some("gemini-embedding-001:embedContent")
    );
    assert_eq!(last_key.lock().unwrap().as_deref(), Some("gk-9"));

    // array input → batchEmbedContents with requests[]
    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "gemini/gemini-embedding-001", "input": ["a", "b"]}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    let data = body["data"].as_array().unwrap();
    assert_eq!(data.len(), 2);
    assert_eq!(data[1]["index"], 1);
    assert_eq!(data[1]["embedding"], json!([2.0]));
    assert_eq!(
        last_op.lock().unwrap().as_deref(),
        Some("gemini-embedding-001:batchEmbedContents")
    );
}

#[tokio::test]
async fn embeddings_selfhosted_uses_credential_base_url() {
    let (port, log) = embeddings_mock().await;
    let cfg = write_config_full(
        "e2e-emb-self",
        json!({"selfhosted-embedding": {"api_key": "sk-self", "baseUrl": format!("http://127.0.0.1:{port}/v1")}}),
        None,
        "fallback",
        1,
    );
    let (server, _data) = spawn_router("e2e-emb-self", cfg, &[], &[]).await;

    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "selfhosted-embedding/emb-1", "input": "data"}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["data"][0]["embedding"], json!([0.1, 0.2, 0.3]));
    assert_eq!(log.hits.load(Ordering::SeqCst), 1);
    assert_eq!(
        log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer sk-self")
    );
    let req = log.last_body.lock().unwrap().clone().unwrap();
    assert_eq!(req["input"], "data");

    // credential WITHOUT baseUrl must refuse (no OpenAI fallback), not 500
    let cfg = write_config(
        "e2e-emb-self2",
        json!({"selfhosted-embedding": {"api_key": "sk-self"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-emb-self2", cfg, &[], &[]).await;
    let resp = post_json(
        &server,
        "/v1/embeddings",
        json!({"model": "selfhosted-embedding/emb-1", "input": "data"}),
    )
    .await;
    assert_eq!(resp.status(), 400);
    let body: Value = resp.json().await.unwrap();
    assert!(
        body["error"]["message"]
            .as_str()
            .unwrap()
            .contains("baseUrl")
    );
}

// ─── images ────────────────────────────────────────────────────────────────

async fn images_mock() -> (u16, ReqLog) {
    let log = ReqLog::default();
    let l = log.clone();
    let app =
        Router::new()
            .route(
                "/v1/images/generations",
                post(
                    move |State(log): State<ReqLog>,
                          headers: HeaderMap,
                          Json(body): Json<Value>| async move {
                        log.record(&body, &headers);
                        (
                            StatusCode::OK,
                            Json(json!({
                                "created": 1700000000,
                                "data": [{"b64_json": "QUJD", "revised_prompt": body["prompt"]}]
                            })),
                        )
                    },
                ),
            )
            .with_state(l);
    let port = spawn_mock(app).await;
    (port, log)
}

#[tokio::test]
async fn images_openai_compat_passthrough_with_defaults() {
    let (port, log) = images_mock().await;
    let cfg = write_config("e2e-img-oai", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, _data) = spawn_router("e2e-img-oai", cfg, &[("openai", port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/images",
        json!({"model": "openai/gpt-image-1", "prompt": "a cat"}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["created"], 1700000000);
    assert_eq!(body["data"][0]["b64_json"], "QUJD");
    assert_eq!(log.hits.load(Ordering::SeqCst), 1);
    assert_eq!(
        log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer sk-1")
    );
    let req = log.last_body.lock().unwrap().clone().unwrap();
    assert_eq!(req["model"], "gpt-image-1");
    assert_eq!(req["prompt"], "a cat");
    assert_eq!(req["n"], 1, "n must default to 1");
    assert_eq!(req["size"], "1024x1024", "size must default to 1024x1024");
}

#[tokio::test]
async fn images_openrouter_sends_static_headers_and_options_passthrough() {
    let log = ReqLog::default();
    let l = log.clone();
    let referer = Arc::new(Mutex::new(None::<String>));
    let title = Arc::new(Mutex::new(None::<String>));
    let app = Router::new().route(
        "/v1/images/generations",
        post({
            let referer = referer.clone();
            let title = title.clone();
            move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
                log.record(&body, &headers);
                *referer.lock().unwrap() = headers.get("http-referer").and_then(|v| v.to_str().ok()).map(|s| s.to_string());
                *title.lock().unwrap() = headers.get("x-title").and_then(|v| v.to_str().ok()).map(|s| s.to_string());
                (StatusCode::OK, Json(json!({"created": 1, "data": [{"url": "https://x/i.png"}]})))
            }
        }),
    )
    .with_state(l);
    let port = spawn_mock(app).await;
    let cfg = write_config(
        "e2e-img-or",
        json!({"openrouter": {"api_key": "sk-or"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-img-or", cfg, &[("openrouter", port)], &[]).await;

    let body = json!({"model": "openrouter/openai/gpt-image-1", "prompt": "p", "n": 2, "quality": "hd", "style": "vivid", "response_format": "url"});
    let resp = post_json(&server, "/v1/images", body).await;
    assert_eq!(resp.status(), 200);
    assert_eq!(
        referer.lock().unwrap().as_deref(),
        Some("https://endpoint-proxy.local")
    );
    assert_eq!(title.lock().unwrap().as_deref(), Some("Endpoint Proxy"));
    assert_eq!(
        log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer sk-or")
    );
    let req = log.last_body.lock().unwrap().clone().unwrap();
    assert_eq!(req["n"], 2);
    assert_eq!(req["quality"], "hd");
    assert_eq!(req["style"], "vivid");
    assert_eq!(req["response_format"], "url");
}

#[tokio::test]
async fn images_xai_bodyfields_whitelist() {
    let (port, log) = images_mock().await;
    let cfg = write_config("e2e-img-xai", json!({"xai": {"api_key": "sk-xai"}}), None);
    let (server, _data) = spawn_router("e2e-img-xai", cfg, &[("xai", port)], &[]).await;

    let body = json!({"model": "xai/grok-2-image", "prompt": "p", "n": 3, "size": "512x512", "response_format": "b64_json"});
    let resp = post_json(&server, "/v1/images", body).await;
    assert_eq!(resp.status(), 200);
    let req = log.last_body.lock().unwrap().clone().unwrap();
    assert_eq!(req["n"], 3);
    assert_eq!(req["response_format"], "b64_json");
    assert!(
        req.get("size").is_none(),
        "xai must not receive size: {req}"
    );
}

#[tokio::test]
async fn images_gemini_nano_banana() {
    let last_op = Arc::new(Mutex::new(None::<String>));
    let last_key = Arc::new(Mutex::new(None::<String>));
    let app = Router::new().route(
        "/v1beta/models/:rest",
        post({
            let last_op = last_op.clone();
            let last_key = last_key.clone();
            move |axum::extract::Query(q): axum::extract::Query<std::collections::HashMap<String, String>>,
                  Path(rest): Path<String>,
                  Json(body): Json<Value>| async move {
                *last_op.lock().unwrap() = Some(rest.clone());
                *last_key.lock().unwrap() = q.get("key").cloned();
                let model = body["contents"][0]["parts"][0]["text"].as_str().unwrap_or("");
                assert_eq!(body["generationConfig"]["responseModalities"], json!(["TEXT", "IMAGE"]));
                (
                    StatusCode::OK,
                    Json(json!({
                        "candidates": [{"content": {"parts": [{"inlineData": {"data": format!("IMG:{}", model)}}]}}]
                    })),
                )
            }
        }),
    );
    let g_port = spawn_mock(app).await;
    let cfg = write_config("e2e-img-gem", json!({"gemini": {"api_key": "gk-9"}}), None);
    let (server, _data) = spawn_router_with_raw_bases(
        "e2e-img-gem",
        cfg,
        &[(
            "gemini".to_string(),
            format!("http://127.0.0.1:{g_port}/v1beta/models"),
        )],
        &[],
    )
    .await;

    let resp = post_json(
        &server,
        "/v1/images",
        json!({"model": "gemini/nano-banana", "prompt": "a cat"}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert!(body["created"].is_u64());
    let data = body["data"].as_array().unwrap();
    assert_eq!(data.len(), 1);
    assert_eq!(data[0]["b64_json"], "IMG:a cat");
    assert_eq!(
        last_op.lock().unwrap().as_deref(),
        Some("nano-banana:generateContent")
    );
    assert_eq!(last_key.lock().unwrap().as_deref(), Some("gk-9"));
}

#[tokio::test]
async fn images_validation_and_unsupported() {
    let (port, _log) = images_mock().await;
    let cfg = write_config("e2e-img-val", json!({"openai": {"api_key": "sk-1"}}), None);
    let (server, _data) = spawn_router("e2e-img-val", cfg, &[("openai", port)], &[]).await;

    // missing prompt → 400
    let resp = post_json(
        &server,
        "/v1/images",
        json!({"model": "openai/gpt-image-1"}),
    )
    .await;
    assert_eq!(resp.status(), 400);
    let body: Value = resp.json().await.unwrap();
    assert!(
        body["error"]["message"]
            .as_str()
            .unwrap()
            .contains("prompt")
    );
    // empty prompt → 400
    let resp = post_json(
        &server,
        "/v1/images",
        json!({"model": "openai/gpt-image-1", "prompt": ""}),
    )
    .await;
    assert_eq!(resp.status(), 400);
    // provider without image support → 400 (chat-only provider AND custom-protocol providers)
    for model in ["claude/claude-3-5-sonnet", "fal-ai/flux", "codex/gpt-5"] {
        let resp = post_json(
            &server,
            "/v1/images",
            json!({"model": model, "prompt": "p"}),
        )
        .await;
        assert_eq!(resp.status(), 400);
        let body: Value = resp.json().await.unwrap();
        assert!(
            body["error"]["message"]
                .as_str()
                .unwrap()
                .contains("does not support image generation"),
            "expected rejection for {model}"
        );
    }
    // unknown provider → 400
    let resp = post_json(
        &server,
        "/v1/images",
        json!({"model": "nosuch/x", "prompt": "p"}),
    )
    .await;
    assert_eq!(resp.status(), 400);
}

// ─── audio ──────────────────────────────────────────────────────────────────

async fn audio_mock() -> (u16, ReqLog) {
    let log = ReqLog::default();
    let l = log.clone();
    let app =
        Router::new()
            .route(
                "/v1/audio/transcriptions",
                post(
                    move |State(log): State<ReqLog>,
                          headers: HeaderMap,
                          body: axum::body::Bytes| async move {
                        log.record(&json!({"bytes": body.len()}), &headers);
                        (StatusCode::OK, Json(json!({"text": "hello audio"}))).into_response()
                    },
                ),
            )
            .route(
                "/v1/audio/speech",
                post(
                    move |State(log): State<ReqLog>,
                          headers: HeaderMap,
                          Json(body): Json<Value>| async move {
                        log.record(&body, &headers);
                        Response::builder()
                            .status(StatusCode::OK)
                            .header("content-type", "audio/mpeg")
                            .body(axum::body::Body::from("MP3-MOCK"))
                            .unwrap()
                    },
                ),
            )
            .with_state(l);
    let port = spawn_mock(app).await;
    (port, log)
}

#[tokio::test]
async fn audio_openai_stt_and_tts() {
    let (port, log) = audio_mock().await;
    let cfg = write_config(
        "e2e-audio-oai",
        json!({"openai": {"api_key": "sk-audio"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-audio-oai", cfg, &[("openai", port)], &[]).await;

    let form = reqwest::multipart::Form::new()
        .text("model", "openai/whisper-1")
        .part(
            "file",
            reqwest::multipart::Part::bytes(b"WAV".to_vec())
                .file_name("sample.wav")
                .mime_str("audio/wav")
                .unwrap(),
        );
    let resp = reqwest::Client::new()
        .post(format!(
            "http://127.0.0.1:{}/v1/audio/transcriptions",
            server.port
        ))
        .multipart(form)
        .send()
        .await
        .unwrap();
    assert_eq!(resp.status(), 200);
    assert_eq!(resp.json::<Value>().await.unwrap()["text"], "hello audio");

    let resp = post_json(
        &server,
        "/v1/audio/speech",
        json!({"model": "openai/tts-1", "input": "hello", "voice": "alloy"}),
    )
    .await;
    assert_eq!(resp.status(), 200);
    assert_eq!(resp.bytes().await.unwrap().as_ref(), b"MP3-MOCK");
    assert_eq!(
        log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer sk-audio")
    );
    assert_eq!(
        log.last_body.lock().unwrap().as_ref().unwrap()["voice"],
        "alloy"
    );
}

#[tokio::test]
async fn audio_validation_and_voices() {
    let (port, _) = audio_mock().await;
    let cfg = write_config(
        "e2e-audio-val",
        json!({"openai": {"api_key": "sk-audio"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-audio-val", cfg, &[("openai", port)], &[]).await;

    let resp = post_json(
        &server,
        "/v1/audio/speech",
        json!({"model": "openai/tts-1"}),
    )
    .await;
    assert_eq!(resp.status(), 400);
    let resp = reqwest::Client::new()
        .post(format!(
            "http://127.0.0.1:{}/v1/audio/transcriptions",
            server.port
        ))
        .send()
        .await
        .unwrap();
    assert_eq!(resp.status(), 400);

    let resp = reqwest::get(format!(
        "http://127.0.0.1:{}/v1/audio/voices?provider=openai",
        server.port
    ))
    .await
    .unwrap();
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["object"], "list");
    assert_eq!(body["data"][0]["id"], "alloy");
}

#[tokio::test]
async fn zed_executor_exchanges_token_and_converts_ndjson() {
    let log = ReqLog::default();
    let l = log.clone();
    let app = Router::new()
        .route("/client/llm_tokens", post(|| async { Json(json!({"token": "llm-token"})) }))
        .route("/completions", post(move |State(log): State<ReqLog>, headers: HeaderMap, Json(body): Json<Value>| async move {
            log.record(&body, &headers);
            let stream = async_stream::stream! {
                yield Ok::<_, std::io::Error>(axum::body::Bytes::from("{\"event\":{\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}}\n"));
                yield Ok(axum::body::Bytes::from("{\"status\":\"stream_ended\"}\n"));
            };
            Response::builder().status(StatusCode::OK).header("content-type", "application/x-ndjson").body(axum::body::Body::from_stream(stream)).unwrap()
        }))
        .with_state(l);
    let port = spawn_mock(app).await;
    let token_url = format!("http://127.0.0.1:{port}/client/llm_tokens");
    let cfg = write_config(
        "e2e-zed",
        json!({"zed": {"user_id": "user-1", "access_token": "access-1"}}),
        None,
    );
    let (server, _data) = spawn_router_with_raw_bases(
        "e2e-zed",
        cfg,
        &[("zed".to_string(), format!("http://127.0.0.1:{port}"))],
        &[("FLAMEROUTER_ZED_TOKEN_URL", token_url)],
    )
    .await;

    let resp = post_json(&server, "/v1/chat/completions", json!({"model": "zed/gpt-4o", "stream": true, "messages": [{"role": "user", "content": "hi"}]})).await;
    assert_eq!(resp.status(), 200);
    assert_eq!(collect_sse_text(resp).await, "Hello");
    let envelope = log.last_body.lock().unwrap().clone().unwrap();
    assert_eq!(envelope["provider"], "OpenAi");
    assert_eq!(envelope["model"], "gpt-4o");
    assert_eq!(
        log.last_auth.lock().unwrap().as_deref(),
        Some("Bearer llm-token")
    );
}

#[tokio::test]
async fn messages_count_tokens_and_model_endpoints() {
    let cfg = write_config(
        "e2e-tokens",
        json!({"openai": {"api_key": "sk-test"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-tokens", cfg, &[], &[]).await;

    // 1. /v1/messages/count_tokens
    let resp = post_json(
        &server,
        "/v1/messages/count_tokens",
        json!({
            "system": "you are an assistant",
            "messages": [{"role": "user", "content": "hello world"}]
        }),
    )
    .await;
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["input_tokens"], 8); // (20 + 11)/4 = ceil(31/4) = 8

    // 2. /v1/models/info
    let resp = reqwest::get(format!(
        "http://127.0.0.1:{}/v1/models/info?id=openai/gpt-4o",
        server.port
    ))
    .await
    .unwrap();
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["id"], "openai/gpt-4o");
    assert_eq!(body["name"], "gpt-4o");
    assert_eq!(body["kind"], "llm");

    // 3. /v1/models/image (kind filtering)
    let resp = reqwest::get(format!("http://127.0.0.1:{}/v1/models/image", server.port))
        .await
        .unwrap();
    assert_eq!(resp.status(), 200);
    let body: Value = resp.json().await.unwrap();
    assert_eq!(body["object"], "list");
}

#[tokio::test]
async fn web_fetch_and_search_endpoints() {
    let cfg = write_config(
        "e2e-fetch-search",
        json!({"jina-reader": {"api_key": "test-key"}, "brave-search": {"api_key": "brave-key"}}),
        None,
    );
    let (server, _data) = spawn_router("e2e-fetch-search", cfg, &[], &[]).await;

    // 1. /v1/web/fetch SSRF guard check
    let resp = post_json(
        &server,
        "/v1/web/fetch",
        json!({
            "url": "http://127.0.0.1:8080/secret"
        }),
    )
    .await;
    assert_eq!(resp.status(), 400);

    // 2. /v1/search empty query check
    let resp = post_json(
        &server,
        "/v1/search",
        json!({
            "query": ""
        }),
    )
    .await;
    assert_eq!(resp.status(), 400);

    // 3. /v1/videos/generations unsupported provider
    let resp = post_json(
        &server,
        "/v1/videos/generations",
        json!({
            "model": "openai/sora",
            "prompt": "flying cat"
        }),
    )
    .await;
    assert_eq!(resp.status(), 400);
}
