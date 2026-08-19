use open_sse::combo;
use open_sse::config;
use open_sse::embeddings;
use open_sse::executors::{DefaultExecutor, UpstreamBody};
use open_sse::fetch;
use open_sse::formats::{Format, detect_format_by_endpoint};
use open_sse::images;
use open_sse::oauth;
use open_sse::ollama;
use open_sse::providers;
use open_sse::rtk;
use open_sse::search;
use open_sse::sse;
use open_sse::stt;
use open_sse::tokens;
use open_sse::translator::{translate_request, translate_response};
use open_sse::tts;
use open_sse::usage;
use open_sse::video;

use axum::{
    Json, Router,
    body::Body,
    extract::{Multipart, Query, State},
    http::{HeaderMap, StatusCode},
    response::{IntoResponse, Response},
    routing::post,
};
use base64::Engine;
use serde_json::{Value, json};
use std::sync::Arc;
use tower_http::cors::CorsLayer;
use tracing_subscriber::EnvFilter;

struct AppState {
    executor: DefaultExecutor,
    config: std::sync::Mutex<config::Config>,
}

#[tokio::main]
async fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if !args.is_empty() {
        if let Err(e) = auth_command(&args).await {
            eprintln!("error: {e}");
            std::process::exit(1);
        }
        return;
    }
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::from_default_env().add_directive("flamerouter=info".parse().unwrap()),
        )
        .init();

    let cfg = config::load();
    tracing::info!(providers = ?cfg.providers.keys().collect::<Vec<_>>(), "config loaded");
    let state = Arc::new(AppState {
        executor: DefaultExecutor::new(),
        config: std::sync::Mutex::new(cfg),
    });

    let app = Router::new()
        .route("/v1/chat/completions", post(chat_completions))
        .route("/v1/messages", post(messages))
        .route("/v1/messages/count_tokens", post(count_tokens))
        .route("/v1/responses", post(responses))
        .route("/v1/responses/compact", post(responses_compact))
        .route("/v1/embeddings", post(embeddings))
        .route("/v1/images", post(images))
        .route("/v1/images/generations", post(images))
        .route("/v1/audio/transcriptions", post(transcriptions))
        .route("/v1/audio/speech", post(speech))
        .route("/v1/audio/voices", axum::routing::get(voices))
        .route("/v1/web/fetch", post(web_fetch))
        .route("/v1/fetch", post(web_fetch))
        .route("/v1/search", post(web_search))
        .route("/v1/videos/generations", post(video_generations))
        .route("/v1/videos/edits", post(video_edits))
        .route("/v1/videos/extensions", post(video_extensions))
        .route("/v1/videos/:id", axum::routing::get(video_get))
        .route("/v1/api/chat", post(ollama_chat))
        .route("/api/chat", post(ollama_chat))
        .route("/v1/models", axum::routing::get(models_list))
        .route("/v1/models/info", axum::routing::get(models_info))
        .route("/v1/models/:kind", axum::routing::get(models_by_kind))
        .route("/healthz", axum::routing::get(|| async { "ok" }))
        .layer(CorsLayer::permissive())
        .with_state(state);

    let port: u16 = std::env::var("PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(20129);
    let addr = std::net::SocketAddr::from(([127, 0, 0, 1], port));
    tracing::info!(%addr, "flamerouter listening");
    let listener = tokio::net::TcpListener::bind(addr).await.unwrap();
    axum::serve(listener, app).await.unwrap();
}

async fn auth_command(args: &[String]) -> anyhow::Result<()> {
    if args.first().map(String::as_str) != Some("auth") {
        anyhow::bail!("usage: flamerouter auth login|logout|status [claude|codex]");
    }
    match args.get(1).map(String::as_str) {
        Some("login") if args.get(2).map(String::as_str) == Some("claude") => {
            auth_login_claude().await
        }
        Some("login") if args.get(2).map(String::as_str) == Some("codex") => {
            auth_login_codex().await
        }
        Some("logout") if args.get(2).map(String::as_str) == Some("claude") => {
            let mut cfg = config::load();
            cfg.logout_oauth("claude");
            config::save(&cfg)?;
            println!("claude logged out");
            Ok(())
        }
        Some("logout") if args.get(2).map(String::as_str) == Some("codex") => {
            let mut cfg = config::load();
            cfg.logout_oauth("codex");
            config::save(&cfg)?;
            println!("codex logged out");
            Ok(())
        }
        Some("status") => {
            let cfg = config::load();
            for prov in ["claude", "codex"] {
                let status = cfg
                    .providers
                    .get(prov)
                    .and_then(|a| a.first())
                    .map(|c| {
                        if c.access_token.is_some() && c.expires_at.unwrap_or(0) > now() {
                            "authenticated"
                        } else {
                            "expired"
                        }
                    })
                    .unwrap_or("not configured");
                println!("{prov}: {status}");
            }
            Ok(())
        }
        _ => anyhow::bail!("usage: flamerouter auth login|logout|status [claude|codex]"),
    }
}

async fn auth_login_codex() -> anyhow::Result<()> {
    let listener = match tokio::net::TcpListener::bind("127.0.0.1:1455").await {
        Ok(l) => l,
        Err(_) => tokio::net::TcpListener::bind("127.0.0.1:0").await?,
    };
    let port = listener.local_addr()?.port();
    let redirect = format!("http://localhost:{port}/auth/callback");
    let pkce = oauth::generate_pkce();
    let url = oauth::codex_authorize_url(&redirect, &pkce);
    println!("Open this URL in your browser:\n{url}");
    let launcher = if cfg!(target_os = "macos") {
        "open"
    } else if cfg!(target_os = "windows") {
        "start"
    } else {
        "xdg-open"
    };
    let _ = std::process::Command::new(launcher).arg(&url).spawn();
    let (mut socket, _) =
        tokio::time::timeout(std::time::Duration::from_secs(300), listener.accept()).await??;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    let mut buf = [0u8; 8192];
    let n = socket.read(&mut buf).await?;
    let request = String::from_utf8_lossy(&buf[..n]);
    let target = request
        .lines()
        .next()
        .and_then(|l| l.split_whitespace().nth(1))
        .ok_or_else(|| anyhow::anyhow!("invalid callback"))?;
    let query = reqwest::Url::parse(&format!("http://localhost{target}"))?;
    if query
        .query_pairs()
        .find(|(k, _)| k == "state")
        .map(|(_, v)| v != pkce.state)
        .unwrap_or(true)
    {
        anyhow::bail!("invalid OAuth state");
    }
    let code = query
        .query_pairs()
        .find(|(k, _)| k == "code")
        .map(|(_, v)| v.into_owned())
        .ok_or_else(|| anyhow::anyhow!("missing authorization code"))?;
    socket.write_all(b"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nAuthentication complete. You can close this window.").await?;
    let tokens = oauth::exchange_codex_code(&code, &redirect, &pkce.verifier)
        .await
        .map_err(|e| anyhow::anyhow!(e))?;
    let expires_at = tokens.expires_in.map(|seconds| now() + seconds);
    let mut cfg = config::load();
    cfg.update_oauth(
        "codex",
        &tokens.access_token,
        &tokens.refresh_token,
        expires_at,
    );
    config::save(&cfg)?;
    println!("codex authenticated");
    Ok(())
}

async fn auth_login_claude() -> anyhow::Result<()> {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await?;
    let port = listener.local_addr()?.port();
    let redirect = format!("http://127.0.0.1:{port}/callback");
    let pkce = oauth::generate_pkce();
    let url = oauth::claude_authorize_url(&redirect, &pkce);
    println!("Open this URL in your browser:\n{url}");
    let launcher = if cfg!(target_os = "macos") {
        "open"
    } else if cfg!(target_os = "windows") {
        "start"
    } else {
        "xdg-open"
    };
    let _ = std::process::Command::new(launcher).arg(&url).spawn();
    let (mut socket, _) =
        tokio::time::timeout(std::time::Duration::from_secs(300), listener.accept()).await??;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    let mut buf = [0u8; 8192];
    let n = socket.read(&mut buf).await?;
    let request = String::from_utf8_lossy(&buf[..n]);
    let target = request
        .lines()
        .next()
        .and_then(|l| l.split_whitespace().nth(1))
        .ok_or_else(|| anyhow::anyhow!("invalid callback"))?;
    let query = reqwest::Url::parse(&format!("http://localhost{target}"))?;
    if query
        .query_pairs()
        .find(|(k, _)| k == "state")
        .map(|(_, v)| v != pkce.state)
        .unwrap_or(true)
    {
        anyhow::bail!("invalid OAuth state");
    }
    let code = query
        .query_pairs()
        .find(|(k, _)| k == "code")
        .map(|(_, v)| v.into_owned())
        .ok_or_else(|| anyhow::anyhow!("missing authorization code"))?;
    socket.write_all(b"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nAuthentication complete. You can close this window.").await?;
    let tokens = oauth::exchange_claude_code(&code, &redirect, &pkce.verifier)
        .await
        .map_err(|e| anyhow::anyhow!(e))?;
    let expires_at = tokens.expires_in.map(|seconds| now() + seconds);
    let mut cfg = config::load();
    cfg.update_oauth(
        "claude",
        &tokens.access_token,
        &tokens.refresh_token,
        expires_at,
    );
    config::save(&cfg)?;
    println!("claude authenticated");
    Ok(())
}

fn now() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs()
}

/// Credentials injected by Next.js via header `X-Provider-Credential: <json>`.
fn extract_credentials(headers: &HeaderMap) -> Option<Value> {
    headers
        .get("x-provider-credential")
        .and_then(|v| v.to_str().ok())
        .and_then(|s| serde_json::from_str(s).ok())
}

fn extract_provider_model(headers: &HeaderMap, body: &Value) -> Option<(String, String)> {
    // Provider can come from header, else infer from body.model as "provider/model"
    if let Some(p) = headers.get("x-provider").and_then(|v| v.to_str().ok()) {
        let model = body
            .get("model")
            .and_then(|m| m.as_str())
            .unwrap_or("")
            .to_string();
        return Some((p.to_string(), model));
    }
    let model = body.get("model").and_then(|m| m.as_str())?;
    let (p, m) = model.split_once('/')?;
    Some((p.to_string(), m.to_string()))
}

async fn chat_completions(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    let start = std::time::Instant::now();
    let (provider, model) = extract_provider_model(&headers, &body)
        .map(|(p, m)| (p.clone(), m))
        .unwrap_or_default();
    let resp = handle_chat(state, headers, body, "/v1/chat/completions").await;
    usage::record(
        "/v1/chat/completions",
        &provider,
        &model,
        resp.status().as_u16(),
        start.elapsed().as_millis() as u64,
    );
    resp
}

async fn messages(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    let start = std::time::Instant::now();
    let (provider, model) = extract_provider_model(&headers, &body)
        .map(|(p, m)| (p.clone(), m))
        .unwrap_or_default();
    let resp = handle_chat(state, headers, body, "/v1/messages").await;
    usage::record(
        "/v1/messages",
        &provider,
        &model,
        resp.status().as_u16(),
        start.elapsed().as_millis() as u64,
    );
    resp
}

async fn count_tokens(Json(body): Json<Value>) -> Response {
    let count = tokens::estimate_anthropic_input_tokens(&body);
    Json(json!({ "input_tokens": count })).into_response()
}

async fn responses(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    let start = std::time::Instant::now();
    let (provider, model) = extract_provider_model(&headers, &body)
        .map(|(p, m)| (p.clone(), m))
        .unwrap_or_default();
    let resp = handle_chat(state, headers, body, "/v1/responses").await;
    usage::record(
        "/v1/responses",
        &provider,
        &model,
        resp.status().as_u16(),
        start.elapsed().as_millis() as u64,
    );
    resp
}

async fn responses_compact(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(mut body): Json<Value>,
) -> Response {
    body["_compact"] = json!(true);
    let start = std::time::Instant::now();
    let (provider, model) = extract_provider_model(&headers, &body)
        .map(|(p, m)| (p.clone(), m))
        .unwrap_or_default();
    let resp = handle_chat(state, headers, body, "/v1/responses").await;
    usage::record(
        "/v1/responses/compact",
        &provider,
        &model,
        resp.status().as_u16(),
        start.elapsed().as_millis() as u64,
    );
    resp
}

async fn embeddings(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    let start = std::time::Instant::now();
    let (provider, model) = extract_provider_model(&headers, &body)
        .map(|(p, m)| (p.clone(), m))
        .unwrap_or_default();
    let resp = handle_embeddings(state, headers, body).await;
    usage::record(
        "/v1/embeddings",
        &provider,
        &model,
        resp.status().as_u16(),
        start.elapsed().as_millis() as u64,
    );
    resp
}

async fn handle_embeddings(state: Arc<AppState>, headers: HeaderMap, body: Value) -> Response {
    // Resolve provider + model (same contract as the chat routes: "provider/model")
    let Some((provider_id, model_raw)) = extract_provider_model(&headers, &body) else {
        return err(StatusCode::BAD_REQUEST, "missing provider or model");
    };
    let model = model_raw
        .strip_prefix(&format!("{}/", provider_id))
        .map(|s| s.to_string())
        .unwrap_or(model_raw);
    if providers::get(&provider_id).is_none() {
        return err(
            StatusCode::BAD_REQUEST,
            &format!("unknown provider: {}", provider_id),
        );
    }
    if !embeddings::is_supported(&provider_id) {
        return err(
            StatusCode::BAD_REQUEST,
            &format!("Provider '{}' does not support embeddings.", provider_id),
        );
    }

    // Validate input (parity with embeddingsCore)
    let input = match body.get("input") {
        Some(v @ (Value::String(_) | Value::Array(_))) => v,
        Some(_) => {
            return err(
                StatusCode::BAD_REQUEST,
                "input must be a string or array of strings",
            );
        }
        None => return err(StatusCode::BAD_REQUEST, "Missing required field: input"),
    };
    let input_is_array = input.is_array();

    // Credential: explicit header first, else first config credential for the
    // provider (embeddings has no multi-account fallback — parity with open-sse).
    let credentials: Value = match extract_credentials(&headers) {
        Some(c) => c,
        None => state
            .config
            .lock()
            .unwrap()
            .credentials_for(&provider_id)
            .into_iter()
            .next()
            .unwrap_or(Value::Null),
    };
    if credentials.is_null() {
        return err(
            StatusCode::UNAUTHORIZED,
            &format!(
                "no credential for provider '{}' (header or config)",
                provider_id
            ),
        );
    }

    // Build URL + upstream body (config errors surface as 400)
    let url = match embeddings::url_for(&provider_id, &model, input_is_array, &credentials) {
        Ok(u) => u,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e),
    };
    let encoding_format = body.get("encoding_format").and_then(|v| v.as_str());
    let dimensions = body.get("dimensions");
    let request_body =
        embeddings::build_body(&provider_id, &model, input, encoding_format, dimensions);

    let client = reqwest::Client::builder()
        .connect_timeout(std::time::Duration::from_secs(30))
        .build()
        .unwrap();

    // Run request (single attempt + optional OAuth refresh on 401).
    // gemini carries the key in the query string — no Authorization header.
    let mut req = client.post(&url).header("content-type", "application/json");
    if !matches!(provider_id.as_str(), "gemini" | "google_ai_studio") {
        req = req.header(
            "authorization",
            format!("Bearer {}", credential_key(&credentials)),
        );
    }
    let mut resp = match req
        .json(&request_body)
        .timeout(std::time::Duration::from_secs(300))
        .send()
        .await
    {
        Ok(r) => r,
        Err(_) => {
            return err(
                StatusCode::BAD_GATEWAY,
                &format!("[{}/{}] fetch failed", provider_id, model),
            );
        }
    };

    if resp.status() == StatusCode::UNAUTHORIZED
        && let Some(refresh_token) = credentials.get("refreshToken").and_then(|t| t.as_str())
            && let Some(Ok(tokens)) = oauth::refresh_for_provider(&provider_id, refresh_token).await
            {
                tracing::info!(provider = %provider_id, "refreshed OAuth token for embeddings");
                {
                    let mut cfg = state.config.lock().unwrap();
                    cfg.update_tokens(&provider_id, &tokens.access_token, &tokens.refresh_token);
                    let _ = config::save(&cfg);
                }
                let mut retried = credentials.clone();
                retried["accessToken"] = json!(tokens.access_token);
                retried["refreshToken"] = json!(tokens.refresh_token);
                if let Ok(r) = client
                    .post(&url)
                    .header("content-type", "application/json")
                    .header(
                        "authorization",
                        format!("Bearer {}", credential_key(&retried)),
                    )
                    .json(&request_body)
                    .timeout(std::time::Duration::from_secs(300))
                    .send()
                    .await
                {
                    resp = r;
                }
            }

    let status = resp.status();
    if !status.is_success() {
        // Relay the upstream error body verbatim (same as the chat fallback path)
        if let Ok(text) = resp.text().await {
            if let Ok(json_body) = serde_json::from_str::<Value>(&text) {
                return Response::builder()
                    .status(status)
                    .header("content-type", "application/json")
                    .body(Body::from(json_body.to_string()))
                    .unwrap();
            }
            return err(status, &text);
        }
        return err(status, "upstream error");
    }

    let parsed: Value = match resp.json().await {
        Ok(v) => v,
        Err(_) => {
            return err(
                StatusCode::BAD_GATEWAY,
                &format!("Invalid JSON response from {}", provider_id),
            );
        }
    };
    Json(embeddings::normalize(
        &provider_id,
        &model,
        parsed,
        input_is_array,
    ))
    .into_response()
}

async fn images(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    let start = std::time::Instant::now();
    let (provider, model) = extract_provider_model(&headers, &body)
        .map(|(p, m)| (p.clone(), m))
        .unwrap_or_default();
    let resp = handle_images(state, headers, body).await;
    usage::record(
        "/v1/images",
        &provider,
        &model,
        resp.status().as_u16(),
        start.elapsed().as_millis() as u64,
    );
    resp
}

async fn transcriptions(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    multipart: Multipart,
) -> Response {
    let start = std::time::Instant::now();
    let result = handle_transcriptions(state, headers, multipart).await;
    usage::record(
        "/v1/audio/transcriptions",
        "audio",
        "",
        result.status().as_u16(),
        start.elapsed().as_millis() as u64,
    );
    result
}

async fn handle_transcriptions(
    state: Arc<AppState>,
    headers: HeaderMap,
    mut multipart: Multipart,
) -> Response {
    let mut file = None;
    let mut model = None;
    let mut language = None;
    let mut prompt = None;
    while let Ok(Some(field)) = multipart.next_field().await {
        let name = field.name().unwrap_or("").to_string();
        if name == "file" {
            let filename = field.file_name().unwrap_or("audio.wav").to_string();
            let mime = field.content_type().unwrap_or("audio/wav").to_string();
            match field.bytes().await {
                Ok(bytes) => file = Some((bytes, filename, mime)),
                Err(_) => return err(StatusCode::BAD_REQUEST, "invalid audio file"),
            }
        } else {
            let value = match field.text().await {
                Ok(v) => v,
                Err(_) => return err(StatusCode::BAD_REQUEST, "invalid multipart field"),
            };
            match name.as_str() {
                "model" => model = Some(value),
                "language" => language = Some(value),
                "prompt" => prompt = Some(value),
                _ => {}
            }
        }
    }
    let model_raw = model.filter(|m| !m.is_empty());
    let Some(model_raw) = model_raw else {
        return err(StatusCode::BAD_REQUEST, "Missing model");
    };
    let Some((provider_id, model)) = extract_provider_model(&headers, &json!({"model": model_raw}))
    else {
        return err(StatusCode::BAD_REQUEST, "missing provider or model");
    };
    let model = model
        .strip_prefix(&format!("{provider_id}/"))
        .unwrap_or(&model)
        .to_string();
    if providers::get(&provider_id).is_none() || !stt::is_supported(&provider_id) {
        return err(
            StatusCode::BAD_REQUEST,
            &format!("Provider '{}' does not support STT", provider_id),
        );
    }
    let Some((audio, filename, mime)) = file else {
        return err(StatusCode::BAD_REQUEST, "Missing required field: file");
    };
    let credentials = extract_credentials(&headers)
        .or_else(|| {
            state
                .config
                .lock()
                .unwrap()
                .credentials_for(&provider_id)
                .into_iter()
                .next()
        })
        .unwrap_or(Value::Null);
    if credentials.is_null() {
        return err(
            StatusCode::UNAUTHORIZED,
            &format!("no credential for provider '{provider_id}'"),
        );
    }
    let url = match stt::url_for(&provider_id, &model, &credentials) {
        Ok(v) => v,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e),
    };
    let client = reqwest::Client::new();
    let response = if matches!(provider_id.as_str(), "gemini" | "google_ai_studio") {
        let body = stt::gemini_body(
            &model,
            &base64::engine::general_purpose::STANDARD.encode(&audio),
            &mime,
        );
        client
            .post(url)
            .header("content-type", "application/json")
            .json(&body)
            .send()
            .await
    } else {
        let part = reqwest::multipart::Part::bytes(audio.to_vec()).file_name(filename);
        let part = match part.mime_str(&mime) {
            Ok(part) => part,
            Err(_) => reqwest::multipart::Part::bytes(audio.to_vec()).file_name("audio.wav"),
        };
        let mut form = reqwest::multipart::Form::new()
            .part("file", part)
            .text("model", model.clone());
        if let Some(value) = language {
            form = form.text("language", value);
        }
        if let Some(value) = prompt {
            form = form.text("prompt", value);
        }
        client
            .post(url)
            .header(
                "authorization",
                format!("Bearer {}", credential_key(&credentials)),
            )
            .multipart(form)
            .send()
            .await
    };
    let Ok(response) = response else {
        return err(StatusCode::BAD_GATEWAY, "STT upstream request failed");
    };
    let status = response.status();
    let text = response.text().await.unwrap_or_default();
    if !status.is_success() {
        return err(status, &text);
    }
    let body = serde_json::from_str::<Value>(&text).unwrap_or_else(|_| json!({"text": text}));
    Json(stt::normalize_stt(&provider_id, body)).into_response()
}

async fn speech(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    let start = std::time::Instant::now();
    let result = handle_speech(state, headers, body).await;
    usage::record(
        "/v1/audio/speech",
        "audio",
        "",
        result.status().as_u16(),
        start.elapsed().as_millis() as u64,
    );
    result
}

async fn handle_speech(state: Arc<AppState>, headers: HeaderMap, body: Value) -> Response {
    let Some((provider_id, model_raw)) = extract_provider_model(&headers, &body) else {
        return err(StatusCode::BAD_REQUEST, "missing provider or model");
    };
    let model = model_raw
        .strip_prefix(&format!("{provider_id}/"))
        .unwrap_or(&model_raw)
        .to_string();
    let input = body.get("input").and_then(Value::as_str).unwrap_or("");
    if input.trim().is_empty() {
        return err(StatusCode::BAD_REQUEST, "Missing required field: input");
    }
    if providers::get(&provider_id).is_none() || !tts::is_supported(&provider_id) {
        return err(
            StatusCode::BAD_REQUEST,
            &format!("Provider '{}' does not support TTS", provider_id),
        );
    }
    let credentials = extract_credentials(&headers)
        .or_else(|| {
            state
                .config
                .lock()
                .unwrap()
                .credentials_for(&provider_id)
                .into_iter()
                .next()
        })
        .unwrap_or(Value::Null);
    if credentials.is_null() {
        return err(
            StatusCode::UNAUTHORIZED,
            &format!("no credential for provider '{provider_id}'"),
        );
    }
    let url = match tts::url_for(&provider_id, &model, &credentials) {
        Ok(v) => v,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e),
    };
    let request_body = tts::build_body(&provider_id, &model, &body);
    let mut request = reqwest::Client::new()
        .post(url)
        .header("content-type", "application/json");
    if !matches!(provider_id.as_str(), "gemini" | "google_ai_studio") {
        request = request.header(
            "authorization",
            format!("Bearer {}", credential_key(&credentials)),
        );
    }
    let Ok(response) = request.json(&request_body).send().await else {
        return err(StatusCode::BAD_GATEWAY, "TTS upstream request failed");
    };
    let status = response.status();
    let content_type = response
        .headers()
        .get("content-type")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("audio/mpeg")
        .to_string();
    let bytes = response.bytes().await.unwrap_or_default();
    if !status.is_success() {
        return err(status, &String::from_utf8_lossy(&bytes));
    }
    if matches!(provider_id.as_str(), "gemini" | "google_ai_studio") {
        let parsed: Value = serde_json::from_slice(&bytes).unwrap_or_default();
        let Some((data, mime)) = tts::gemini_audio(&parsed) else {
            return err(StatusCode::BAD_GATEWAY, "Gemini TTS returned no audio");
        };
        let audio = match base64::Engine::decode(&base64::engine::general_purpose::STANDARD, data) {
            Ok(v) => v,
            Err(_) => return err(StatusCode::BAD_GATEWAY, "invalid Gemini audio"),
        };
        return audio_response(
            audio,
            mime,
            body.get("response_format").and_then(Value::as_str),
        );
    }
    audio_response(
        bytes.to_vec(),
        &content_type,
        body.get("response_format").and_then(Value::as_str),
    )
}

fn audio_response(bytes: Vec<u8>, content_type: &str, response_format: Option<&str>) -> Response {
    if response_format == Some("json") {
        let audio = base64::Engine::encode(&base64::engine::general_purpose::STANDARD, &bytes);
        return Json(
            json!({"audio": audio, "format": content_type.strip_prefix("audio/").unwrap_or("mp3")}),
        )
        .into_response();
    }
    Response::builder()
        .status(StatusCode::OK)
        .header("content-type", content_type)
        .header("content-length", bytes.len())
        .body(Body::from(bytes))
        .unwrap()
}

async fn voices(Query(params): Query<std::collections::HashMap<String, String>>) -> Response {
    let provider = params.get("provider").map(String::as_str).unwrap_or("");
    let ids: &[&str] = match provider {
        "gemini" => &["Zephyr", "Puck", "Kore", "Charon"],
        _ => &[
            "alloy", "ash", "coral", "echo", "fable", "nova", "onyx", "sage", "shimmer",
        ],
    };
    Json(json!({"object": "list", "data": ids.iter().map(|id| json!({"id": id, "name": id, "model": format!("{provider}/{id}")})).collect::<Vec<_>>() })).into_response()
}

async fn handle_images(state: Arc<AppState>, headers: HeaderMap, body: Value) -> Response {
    let Some((provider_id, model_raw)) = extract_provider_model(&headers, &body) else {
        return err(StatusCode::BAD_REQUEST, "missing provider or model");
    };
    let model = model_raw
        .strip_prefix(&format!("{}/", provider_id))
        .map(|s| s.to_string())
        .unwrap_or(model_raw);
    if providers::get(&provider_id).is_none() {
        return err(
            StatusCode::BAD_REQUEST,
            &format!("unknown provider: {}", provider_id),
        );
    }
    if !images::is_supported(&provider_id) {
        return err(
            StatusCode::BAD_REQUEST,
            &format!(
                "Provider '{}' does not support image generation",
                provider_id
            ),
        );
    }

    // Validate prompt (parity with imageGenerationCore)
    if body
        .get("prompt")
        .and_then(|p| p.as_str())
        .unwrap_or("")
        .is_empty()
    {
        return err(StatusCode::BAD_REQUEST, "Missing required field: prompt");
    }

    // Credential: explicit header first, else first config credential (no
    // multi-account fallback for images — parity with open-sse).
    let credentials: Value = match extract_credentials(&headers) {
        Some(c) => c,
        None => state
            .config
            .lock()
            .unwrap()
            .credentials_for(&provider_id)
            .into_iter()
            .next()
            .unwrap_or(Value::Null),
    };
    if credentials.is_null() {
        return err(
            StatusCode::UNAUTHORIZED,
            &format!(
                "no credential for provider '{}' (header or config)",
                provider_id
            ),
        );
    }

    // Build URL + upstream body (config errors surface as 400)
    let url = match images::url_for(&provider_id, &model, &credentials) {
        Ok(u) => u,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e),
    };
    let request_body = images::build_body(&provider_id, &model, &body);

    let client = reqwest::Client::builder()
        .connect_timeout(std::time::Duration::from_secs(30))
        .build()
        .unwrap();

    // Run request (single attempt + optional OAuth refresh on 401).
    // gemini carries the key in the query string — no Authorization header.
    let mut req = client.post(&url).header("content-type", "application/json");
    if !matches!(provider_id.as_str(), "gemini" | "google_ai_studio") {
        req = req.header(
            "authorization",
            format!("Bearer {}", credential_key(&credentials)),
        );
    }
    // static per-provider headers (e.g. openrouter marketing headers)
    if let Some((_, headers)) = images::EXTRA_IMAGE_HEADERS
        .iter()
        .find(|(id, _)| *id == provider_id)
    {
        for (name, value) in *headers {
            req = req.header(*name, *value);
        }
    }

    let mut resp = match req
        .json(&request_body)
        .timeout(std::time::Duration::from_secs(300))
        .send()
        .await
    {
        Ok(r) => r,
        Err(_) => {
            return err(
                StatusCode::BAD_GATEWAY,
                &format!("[{}/{}] fetch failed", provider_id, model),
            );
        }
    };

    if resp.status() == StatusCode::UNAUTHORIZED
        && let Some(refresh_token) = credentials.get("refreshToken").and_then(|t| t.as_str())
            && let Some(Ok(tokens)) = oauth::refresh_for_provider(&provider_id, refresh_token).await
            {
                tracing::info!(provider = %provider_id, "refreshed OAuth token for images");
                {
                    let mut cfg = state.config.lock().unwrap();
                    cfg.update_tokens(&provider_id, &tokens.access_token, &tokens.refresh_token);
                    let _ = config::save(&cfg);
                }
                let mut retried = credentials.clone();
                retried["accessToken"] = json!(tokens.access_token);
                retried["refreshToken"] = json!(tokens.refresh_token);
                let mut retry_req = client.post(&url).header("content-type", "application/json");
                if !matches!(provider_id.as_str(), "gemini" | "google_ai_studio") {
                    retry_req = retry_req.header(
                        "authorization",
                        format!("Bearer {}", credential_key(&retried)),
                    );
                }
                if let Ok(r) = retry_req
                    .json(&request_body)
                    .timeout(std::time::Duration::from_secs(300))
                    .send()
                    .await
                {
                    resp = r;
                }
            }

    let status = resp.status();
    if !status.is_success() {
        // Relay the upstream error body verbatim (same as the chat fallback path)
        if let Ok(text) = resp.text().await {
            if let Ok(json_body) = serde_json::from_str::<Value>(&text) {
                return Response::builder()
                    .status(status)
                    .header("content-type", "application/json")
                    .body(Body::from(json_body.to_string()))
                    .unwrap();
            }
            return err(status, &text);
        }
        return err(status, "upstream error");
    }

    let parsed: Value = match resp.json().await {
        Ok(v) => v,
        Err(_) => {
            return err(
                StatusCode::BAD_GATEWAY,
                &format!("Invalid JSON response from {}", provider_id),
            );
        }
    };
    let prompt = body.get("prompt").and_then(|p| p.as_str()).unwrap_or("");
    Json(images::normalize(&provider_id, parsed, prompt, &model)).into_response()
}

async fn web_fetch(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    let start = std::time::Instant::now();
    let url = body.get("url").and_then(Value::as_str).unwrap_or("");
    if url.is_empty() {
        return err(StatusCode::BAD_REQUEST, "Missing required field: url");
    }
    if let Err(e) = fetch::check_public_url(url) {
        return err(StatusCode::BAD_REQUEST, &e.to_string());
    }

    let provider = body
        .get("provider")
        .or_else(|| body.get("model"))
        .and_then(Value::as_str)
        .unwrap_or("jina-reader");
    if !fetch::is_supported(provider) {
        return err(
            StatusCode::BAD_REQUEST,
            &format!("Unsupported fetch provider: {provider}"),
        );
    }

    let credentials = extract_credentials(&headers)
        .or_else(|| {
            state
                .config
                .lock()
                .unwrap()
                .credentials_for(provider)
                .into_iter()
                .next()
        })
        .unwrap_or(Value::Null);
    let key = credential_key(&credentials);

    let format = body.get("format").and_then(Value::as_str);
    let max_chars = body
        .get("max_characters")
        .and_then(Value::as_u64)
        .map(|n| n as usize);

    match fetch::execute_fetch(provider, url, format, max_chars, &key).await {
        Ok(data) => {
            usage::record(
                "/v1/web/fetch",
                provider,
                url,
                200,
                start.elapsed().as_millis() as u64,
            );
            Json(data).into_response()
        }
        Err(e) => {
            usage::record(
                "/v1/web/fetch",
                provider,
                url,
                502,
                start.elapsed().as_millis() as u64,
            );
            err(StatusCode::BAD_GATEWAY, &e.to_string())
        }
    }
}

async fn web_search(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    let start = std::time::Instant::now();
    let query = body.get("query").and_then(Value::as_str).unwrap_or("");
    if query.trim().is_empty() {
        return err(StatusCode::BAD_REQUEST, "Missing required field: query");
    }

    let provider = body
        .get("provider")
        .or_else(|| body.get("model"))
        .and_then(Value::as_str)
        .unwrap_or("brave-search");
    if !search::is_supported(provider) {
        return err(
            StatusCode::BAD_REQUEST,
            &format!("Provider {provider} does not support web search"),
        );
    }

    let credentials = extract_credentials(&headers)
        .or_else(|| {
            state
                .config
                .lock()
                .unwrap()
                .credentials_for(provider)
                .into_iter()
                .next()
        })
        .unwrap_or(Value::Null);

    let search_type = body.get("search_type").and_then(Value::as_str);
    let max_results = body
        .get("max_results")
        .and_then(Value::as_u64)
        .map(|n| n as usize);
    let country = body.get("country").and_then(Value::as_str);
    let language = body.get("language").and_then(Value::as_str);

    match search::execute_search(
        provider,
        query,
        search_type,
        max_results,
        country,
        language,
        &credentials,
    )
    .await
    {
        Ok(data) => {
            usage::record(
                "/v1/search",
                provider,
                query,
                200,
                start.elapsed().as_millis() as u64,
            );
            Json(data).into_response()
        }
        Err(e) => {
            usage::record(
                "/v1/search",
                provider,
                query,
                502,
                start.elapsed().as_millis() as u64,
            );
            err(StatusCode::BAD_GATEWAY, &e.to_string())
        }
    }
}

async fn video_generations(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    handle_video_action(state, headers, "generations", body).await
}

async fn video_edits(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    handle_video_action(state, headers, "edits", body).await
}

async fn video_extensions(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    handle_video_action(state, headers, "extensions", body).await
}

async fn handle_video_action(
    state: Arc<AppState>,
    headers: HeaderMap,
    action: &str,
    body: Value,
) -> Response {
    let start = std::time::Instant::now();
    let provider_str = body
        .get("provider")
        .or_else(|| body.get("model"))
        .and_then(Value::as_str)
        .unwrap_or("xai");
    let provider = provider_str
        .split('/')
        .next()
        .unwrap_or(provider_str)
        .to_string();
    if !video::is_supported(&provider) {
        return err(
            StatusCode::BAD_REQUEST,
            &format!("Provider '{provider}' does not support video generation"),
        );
    }

    let credentials = extract_credentials(&headers)
        .or_else(|| {
            state
                .config
                .lock()
                .unwrap()
                .credentials_for(&provider)
                .into_iter()
                .next()
        })
        .unwrap_or(Value::Null);

    match video::create_video_job(&provider, action, body, &credentials).await {
        Ok(data) => {
            usage::record(
                &format!("/v1/videos/{action}"),
                &provider,
                "video",
                200,
                start.elapsed().as_millis() as u64,
            );
            Json(data).into_response()
        }
        Err(e) => {
            usage::record(
                &format!("/v1/videos/{action}"),
                &provider,
                "video",
                502,
                start.elapsed().as_millis() as u64,
            );
            err(StatusCode::BAD_GATEWAY, &e.to_string())
        }
    }
}

async fn video_get(
    axum::extract::Path(id): axum::extract::Path<String>,
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
) -> Response {
    let start = std::time::Instant::now();
    let provider = "xai";
    let credentials = extract_credentials(&headers)
        .or_else(|| {
            state
                .config
                .lock()
                .unwrap()
                .credentials_for(provider)
                .into_iter()
                .next()
        })
        .unwrap_or(Value::Null);

    match video::get_video_job(provider, &id, &credentials).await {
        Ok(data) => {
            usage::record(
                "/v1/videos/[id]",
                provider,
                "video",
                200,
                start.elapsed().as_millis() as u64,
            );
            Json(data).into_response()
        }
        Err(e) => {
            usage::record(
                "/v1/videos/[id]",
                provider,
                "video",
                502,
                start.elapsed().as_millis() as u64,
            );
            err(StatusCode::BAD_GATEWAY, &e.to_string())
        }
    }
}

async fn ollama_chat(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> Response {
    let model = body
        .get("model")
        .and_then(Value::as_str)
        .unwrap_or("llama3.2")
        .to_string();
    let stream = body.get("stream").and_then(Value::as_bool).unwrap_or(true);

    let chat_resp = handle_chat(state, headers, body, "/v1/chat/completions").await;
    let status = chat_resp.status();
    if !status.is_success() {
        return chat_resp;
    }

    if !stream {
        // Non-stream OpenAI JSON -> Ollama NDJSON single line
        return Response::builder()
            .status(StatusCode::OK)
            .header("content-type", "application/x-ndjson")
            .body(Body::from(ollama::format_ollama_done(&model, None)))
            .unwrap();
    }

    // Stream SSE -> Ollama NDJSON
    let byte_stream = chat_resp.into_body().into_data_stream();
    let mapped_stream = async_stream::stream! {
        use futures_util::StreamExt;
        let mut buffer = String::new();
        tokio::pin!(byte_stream);
        while let Some(chunk_res) = byte_stream.next().await {
            let Ok(chunk) = chunk_res else { continue };
            buffer.push_str(&String::from_utf8_lossy(&chunk));
            while let Some(pos) = buffer.find("\n\n") {
                let sse_block = buffer[..pos].trim().to_string();
                buffer.drain(..pos + 2);
                for line in sse_block.lines() {
                    if let Some(data) = line.strip_prefix("data:") {
                        let data = data.trim();
                        if data == "[DONE]" {
                            yield Ok::<_, std::io::Error>(ollama::format_ollama_done(&model, None));
                            return;
                        }
                        if let Ok(v) = serde_json::from_str::<Value>(data)
                            && let Some(text) = v.pointer("/choices/0/delta/content").and_then(Value::as_str)
                                && !text.is_empty() {
                                    yield Ok::<_, std::io::Error>(ollama::format_ollama_chunk(&model, text));
                                }
                    }
                }
            }
        }
        yield Ok::<_, std::io::Error>(ollama::format_ollama_done(&model, None));
    };

    Response::builder()
        .status(StatusCode::OK)
        .header("content-type", "application/x-ndjson")
        .body(Body::from_stream(mapped_stream))
        .unwrap()
}

/// First usable key from a credential for the Authorization header.
fn credential_key(credentials: &Value) -> String {
    credentials
        .get("apiKey")
        .or_else(|| credentials.get("api_key"))
        .or_else(|| credentials.get("accessToken"))
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string()
}

async fn models_list(State(state): State<Arc<AppState>>) -> Response {
    let cfg = state.config.lock().unwrap();
    let mut data: Vec<Value> = Vec::new();
    for pid in cfg.providers.keys() {
        if providers::get(pid).is_none() {
            continue;
        }
        data.push(json!({
            "id": format!("{pid}/*"),
            "object": "model",
            "owned_by": pid
        }));
    }
    for name in cfg.combos.keys() {
        data.push(json!({
            "id": name,
            "object": "model",
            "owned_by": "combo"
        }));
    }
    Json(json!({ "object": "list", "data": data })).into_response()
}

async fn models_by_kind(
    axum::extract::Path(kind): axum::extract::Path<String>,
    State(state): State<Arc<AppState>>,
) -> Response {
    let supported_kinds = ["image", "tts", "stt", "embedding", "image-to-text", "web"];
    if !supported_kinds.contains(&kind.as_str()) {
        return (
            StatusCode::NOT_FOUND,
            Json(json!({
                "error": {
                    "message": format!("Unknown model kind: {kind}. Supported: {}", supported_kinds.join(", ")),
                    "type": "invalid_request_error"
                }
            }))
        ).into_response();
    }
    models_list(State(state)).await
}

async fn models_info(Query(params): Query<std::collections::HashMap<String, String>>) -> Response {
    let Some(id) = params.get("id") else {
        return err(
            StatusCode::BAD_REQUEST,
            "Missing required query param: id (e.g. ?id=openai/gpt-4o)",
        );
    };
    let Some((provider, model_name)) = id.split_once('/') else {
        return (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": { "message": format!("Model not found: {id}"), "type": "not_found" } }))
        ).into_response();
    };

    Json(json!({
        "id": id,
        "name": model_name,
        "kind": "llm",
        "owned_by": provider,
        "endpoint": "/v1/chat/completions"
    }))
    .into_response()
}

/// Encode a translated chunk as SSE bytes using the appropriate format.
/// - OpenAI Responses API: `event: <type>\ndata: <json>\n\n` (chunks have {"event","data"})
/// - Claude: `event: <type>\ndata: <json>\n\n` (chunks have {"type",...})
/// - OpenAI chat: `data: <json>\n\n`
fn encode_output_sse(source: Format, chunk: &Value) -> bytes::Bytes {
    // Responses API format: translator returns {"event": "...", "data": {...}}
    if let (Some(event), Some(data)) = (
        chunk.get("event").and_then(|e| e.as_str()),
        chunk.get("data"),
    ) {
        let data_str = serde_json::to_string(data).unwrap_or_default();
        return sse::encode_sse_event_typed(event, &data_str);
    }
    // Claude format: chunk has "type" field → use type as event name
    if source == Format::Claude
        && let Some(event_type) = chunk.get("type").and_then(|t| t.as_str()) {
            let data_str = serde_json::to_string(chunk).unwrap_or_default();
            return sse::encode_sse_event_typed(event_type, &data_str);
        }
    // Default OpenAI format
    let s = serde_json::to_string(chunk).unwrap_or_default();
    sse::encode_sse_event(&s)
}

async fn handle_chat(
    state: Arc<AppState>,
    headers: HeaderMap,
    body: Value,
    pathname: &str,
) -> Response {
    // Check if model is a combo (no "/" in model string → might be combo name)
    let model_str = body.get("model").and_then(|m| m.as_str()).unwrap_or("");
    if !model_str.contains('/') && !model_str.is_empty() {
        let combo_info = {
            let cfg = state.config.lock().unwrap();
            cfg.combo_models(model_str).map(|models| {
                let strategy = cfg.combo_strategy_str().to_string();
                let sticky = cfg.sticky_limit();
                (models.clone(), strategy, sticky)
            })
        };
        if let Some((models, strategy, sticky)) = combo_info {
            let ordered = combo::get_ordered_models(&models, model_str, &strategy, sticky);
            tracing::info!(combo = model_str, strategy = %strategy, models = ?ordered, "combo routing");
            return handle_combo(state, headers, body, pathname, &ordered).await;
        }
    }

    // Single model path
    handle_single_model(state, headers, body, pathname).await
}

/// Try each combo model in order; 5xx or error → next model.
async fn handle_combo(
    state: Arc<AppState>,
    headers: HeaderMap,
    body: Value,
    pathname: &str,
    models: &[String],
) -> Response {
    let mut last_error = String::new();
    let start = std::time::Instant::now();
    let combo_name = body
        .get("model")
        .and_then(|m| m.as_str())
        .unwrap_or("")
        .to_string();

    for (i, model_str) in models.iter().enumerate() {
        tracing::info!(model = %model_str, attempt = i + 1, total = models.len(), "combo try");
        // Replace model in body
        let mut body_copy = body.clone();
        body_copy["model"] = Value::String(model_str.clone());
        let resp = handle_single_model(state.clone(), headers.clone(), body_copy, pathname).await;
        let status = resp.status().as_u16();
        // Success or client error (4xx) — return immediately
        if status < 500 {
            // Record under the combo name (as provider) so combo traffic isn't lost
            // to the empty-provider skip in usage::record.
            usage::record(
                pathname,
                &combo_name,
                &combo_name,
                status,
                start.elapsed().as_millis() as u64,
            );
            return resp;
        }
        // 5xx — log and try next
        last_error = format!("{}: status {}", model_str, status);
        tracing::warn!(model = %model_str, status, "combo model failed, trying next");
    }

    tracing::warn!("all combo models failed");
    usage::record(
        pathname,
        &combo_name,
        &combo_name,
        503,
        start.elapsed().as_millis() as u64,
    );
    err(
        StatusCode::SERVICE_UNAVAILABLE,
        &format!("all combo models failed: {}", last_error),
    )
}

async fn handle_single_model(
    state: Arc<AppState>,
    headers: HeaderMap,
    body: Value,
    pathname: &str,
) -> Response {
    // Resolve provider + model
    let Some((provider_id, model_raw)) = extract_provider_model(&headers, &body) else {
        return err(StatusCode::BAD_REQUEST, "missing provider or model");
    };
    // Strip provider prefix from model id when caller passed "provider/model"
    let model = model_raw
        .strip_prefix(&format!("{}/", provider_id))
        .map(|s| s.to_string())
        .unwrap_or(model_raw);
    let Some(provider) = providers::get(&provider_id) else {
        return err(
            StatusCode::BAD_REQUEST,
            &format!("unknown provider: {}", provider_id),
        );
    };
    // Credential resolution: explicit header first (single attempt), else all config credentials
    let cred_list: Vec<Value> = match extract_credentials(&headers) {
        Some(c) => vec![c],
        None => state.config.lock().unwrap().credentials_for(&provider_id),
    };
    if cred_list.is_empty() {
        return err(
            StatusCode::UNAUTHORIZED,
            &format!(
                "no credential for provider '{}' (header or config)",
                provider_id
            ),
        );
    }

    // Detect source format
    let source = detect_format_by_endpoint(pathname, &body).unwrap_or(Format::Openai);
    let target = match provider.target_format {
        "claude" => Format::Claude,
        "openai" => Format::Openai,
        "openai-responses" => Format::OpenaiResponses,
        "gemini" => Format::Gemini,
        _ => Format::Openai,
    };

    let stream = body
        .get("stream")
        .and_then(|s| s.as_bool())
        .unwrap_or(false);

    // Apply RTK token saver (pre-translate compression)
    let mut body = body;
    rtk::compress_messages(&mut body);

    // Multi-account fallback loop
    let mut last_upstream = None;
    for (acc_idx, mut credentials) in cred_list.into_iter().enumerate() {
        let expired = credentials
            .get("expiresAt")
            .and_then(Value::as_u64)
            .map(|at| at <= now() + 60)
            .unwrap_or(false);
        if expired
            && let Some(refresh_token) = credentials.get("refreshToken").and_then(Value::as_str) {
                match oauth::refresh_for_provider(&provider_id, refresh_token).await {
                    Some(Ok(tokens)) => {
                        let expires_at = tokens.expires_in.map(|seconds| now() + seconds);
                        {
                            let mut cfg = state.config.lock().unwrap();
                            cfg.update_oauth(
                                &provider_id,
                                &tokens.access_token,
                                &tokens.refresh_token,
                                expires_at,
                            );
                            let _ = config::save(&cfg);
                        }
                        credentials["accessToken"] = json!(tokens.access_token);
                        credentials["refreshToken"] = json!(tokens.refresh_token);
                        credentials["expiresAt"] = json!(expires_at);
                    }
                    Some(Err(oauth::RefreshError::Permanent(_))) => {
                        return err(
                            StatusCode::UNAUTHORIZED,
                            &format!(
                                "OAuth credentials expired for {}; please re-login",
                                provider_id
                            ),
                        );
                    }
                    Some(Err(oauth::RefreshError::Transient(_))) | None => {}
                }
            }
        // Translate request
        let Some(translated) = translate_request(
            source,
            target,
            &model,
            body.clone(),
            stream,
            Some(&credentials),
        ) else {
            return err(
                StatusCode::BAD_REQUEST,
                &format!(
                    "no translator for {} → {}",
                    source.as_str(),
                    target.as_str()
                ),
            );
        };

        // Execute upstream
        let result = state
            .executor
            .execute(provider, &model, translated.clone(), stream, &credentials)
            .await;

        let mut upstream = match result {
            Ok(r) => r,
            Err(e) => {
                tracing::warn!(provider = %provider_id, account = acc_idx, error = %e, "account request error");
                continue;
            }
        };

        // OAuth refresh on 401 — try once with refreshed token for this account
        if upstream.status == 401
            && let Some(refresh_token) = credentials.get("refreshToken").and_then(|t| t.as_str())
                && let Some(Ok(tokens)) =
                    oauth::refresh_for_provider(&provider_id, refresh_token).await
                {
                    tracing::info!(provider = %provider_id, account = acc_idx, "refreshed OAuth token");
                    {
                        let mut cfg = state.config.lock().unwrap();
                        let expires_at = tokens.expires_in.map(|seconds| now() + seconds);
                        cfg.update_oauth(
                            &provider_id,
                            &tokens.access_token,
                            &tokens.refresh_token,
                            expires_at,
                        );
                        let _ = config::save(&cfg);
                    }
                    credentials["accessToken"] = json!(tokens.access_token);
                    credentials["refreshToken"] = json!(tokens.refresh_token);
                    credentials["expiresAt"] =
                        json!(tokens.expires_in.map(|seconds| now() + seconds));
                    if let Ok(r) = state
                        .executor
                        .execute(provider, &model, translated, stream, &credentials)
                        .await
                    {
                        upstream = r;
                    }
                }

        if upstream.status == 200 {
            // Success! Send back response
            if !stream {
                let UpstreamBody::Json(json_body) = upstream.body else {
                    return err(StatusCode::INTERNAL_SERVER_ERROR, "expected json");
                };
                let out = nonstream_response_convert(source, target, json_body);
                return Json(out).into_response();
            }

            let UpstreamBody::Sse(byte_stream) = upstream.body else {
                return err(StatusCode::INTERNAL_SERVER_ERROR, "expected sse");
            };

            let event_stream = sse::parse_sse(byte_stream);
            let out_stream = async_stream::stream! {
                use futures_util::StreamExt;
                let mut st = json!({});
                tokio::pin!(event_stream);
                while let Some(ev) = event_stream.next().await {
                    if ev.data == "[DONE]" {
                        // Upstream termination marker — only OpenAI-format clients
                        // expect `data: [DONE]`; claude/responses clients must not see it.
                        if source == Format::Openai {
                            yield Ok::<_, std::io::Error>(sse::encode_sse_event("[DONE]"));
                        }
                        break;
                    }
                    let Ok(chunk) = serde_json::from_str::<Value>(&ev.data) else {
                        continue;
                    };
                    let out = translate_response(target, source, chunk, &mut st);
                    for c in out {
                        yield Ok(encode_output_sse(source, &c));
                    }
                }
            };

            return Response::builder()
                .status(200)
                .header("content-type", "text/event-stream")
                .header("cache-control", "no-cache")
                .header("connection", "keep-alive")
                .body(Body::from_stream(out_stream))
                .unwrap();
        }

        // Error status (401, 429, 5xx) → log and try next account
        tracing::warn!(provider = %provider_id, account = acc_idx, status = upstream.status, "account failed, trying next");
        last_upstream = Some(upstream);
    }

    // All accounts failed — return last upstream error response
    if let Some(upstream) = last_upstream
        && let UpstreamBody::Json(j) = upstream.body {
            return Response::builder()
                .status(upstream.status)
                .header("content-type", "application/json")
                .body(Body::from(j.to_string()))
                .unwrap();
        }
    err(StatusCode::BAD_GATEWAY, "all accounts failed for provider")
}

/// Convert a non-streaming upstream JSON response into the client's format.
/// Pivots through the OpenAI chat.completion shape: upstream body → openai → client format.
fn nonstream_response_convert(source: Format, target: Format, body: Value) -> Value {
    if source == target {
        return body;
    }
    let openai = match target {
        Format::Claude => openai_json_from_claude_message(body),
        Format::Gemini | Format::Antigravity | Format::Vertex | Format::GeminiCli => {
            openai_json_from_gemini_body(body)
        }
        Format::OpenaiResponses | Format::OpenaiResponse | Format::Codex => {
            openai_json_from_responses_body(body)
        }
        // Binary formats (kiro/cursor/commandcode) carry no JSON body — passthrough;
        // openai/ollama are already OpenAI-shaped.
        _ => body,
    };
    match source {
        Format::Claude => openai_json_to_claude_message(openai),
        Format::OpenaiResponses => openai_json_to_responses(openai),
        _ => openai,
    }
}

/// Convert Gemini generateContent JSON → OpenAI chat.completion JSON (non-streaming).
fn openai_json_from_gemini_body(body: Value) -> Value {
    // Antigravity wraps the gemini body in a "response" key (mirrors the stream translator).
    let gemini = body
        .get("response")
        .cloned()
        .unwrap_or_else(|| body.clone());
    // If it doesn't look like gemini shape, return as-is
    let Some(candidates) = gemini.get("candidates").and_then(|c| c.as_array()) else {
        return body;
    };
    let candidate = &candidates[0];
    let parts = candidate
        .get("content")
        .and_then(|c| c.get("parts"))
        .and_then(|p| p.as_array())
        .cloned()
        .unwrap_or_default();
    let text: String = parts
        .iter()
        .filter_map(|p| {
            p.get("text")
                .and_then(|t| t.as_str())
                .map(|s| s.to_string())
        })
        .collect::<Vec<_>>()
        .join("");
    let finish_reason = match candidate
        .get("finishReason")
        .and_then(|f| f.as_str())
        .unwrap_or("STOP")
    {
        "MAX_TOKENS" => "length",
        "STOP" => "stop",
        _ => "stop",
    };
    let usage = gemini.get("usageMetadata").cloned().unwrap_or(json!({}));
    json!({
        "id": "chatcmpl-gemini",
        "object": "chat.completion",
        "created": 0,
        "model": gemini.get("modelVersion").cloned().unwrap_or(json!("gemini")),
        "choices": [{
            "index": 0,
            "message": { "role": "assistant", "content": text },
            "finish_reason": finish_reason
        }],
        "usage": {
            "prompt_tokens": usage.get("promptTokenCount").cloned().unwrap_or(json!(0)),
            "completion_tokens": usage.get("candidatesTokenCount").cloned().unwrap_or(json!(0)),
            "total_tokens": usage.get("totalTokenCount").cloned().unwrap_or(json!(0))
        }
    })
}

/// Convert OpenAI Responses API JSON → OpenAI chat.completion JSON (non-streaming).
fn openai_json_from_responses_body(resp_body: Value) -> Value {
    // If it doesn't look like a responses body, return as-is
    let Some(output) = resp_body.get("output").and_then(|o| o.as_array()) else {
        return resp_body;
    };
    let text: Vec<String> = output
        .iter()
        .filter(|o| o.get("type").and_then(|t| t.as_str()) == Some("message"))
        .filter_map(|o| o.get("content").and_then(|c| c.as_array()))
        .flatten()
        .filter(|p| p.get("type").and_then(|t| t.as_str()) == Some("output_text"))
        .filter_map(|p| {
            p.get("text")
                .and_then(|t| t.as_str())
                .map(|s| s.to_string())
        })
        .collect();
    let tool_calls: Vec<Value> = output
        .iter()
        .filter(|o| o.get("type").and_then(|t| t.as_str()) == Some("function_call"))
        .map(|fc| {
            json!({
                "id": fc.get("call_id").cloned().unwrap_or(json!("call_unknown")),
                "type": "function",
                "function": {
                    "name": fc.get("name").cloned().unwrap_or(json!("")),
                    "arguments": fc.get("arguments").cloned().unwrap_or(json!("{}"))
                }
            })
        })
        .collect();
    let has_tool_calls = !tool_calls.is_empty();
    let mut message = json!({ "role": "assistant", "content": text.join("\n") });
    if has_tool_calls {
        message["tool_calls"] = Value::Array(tool_calls);
    }
    let finish_reason = if has_tool_calls { "tool_calls" } else { "stop" };
    let usage = resp_body.get("usage").cloned().unwrap_or(json!({}));
    json!({
        "id": resp_body.get("id").cloned().unwrap_or(json!("resp_unknown")),
        "object": "chat.completion",
        "created": 0,
        "model": resp_body.get("model").cloned().unwrap_or(json!("")),
        "choices": [{
            "index": 0,
            "message": message,
            "finish_reason": finish_reason
        }],
        "usage": {
            "prompt_tokens": usage.get("input_tokens").cloned().unwrap_or(json!(0)),
            "completion_tokens": usage.get("output_tokens").cloned().unwrap_or(json!(0)),
            "total_tokens": usage.get("total_tokens").cloned().unwrap_or(json!(0))
        }
    })
}

/// Convert Anthropic message JSON → OpenAI chat.completion JSON (non-streaming).
fn openai_json_from_claude_message(anthropic: Value) -> Value {
    if anthropic.get("type").and_then(|t| t.as_str()) != Some("message") {
        return anthropic;
    }
    let blocks: &Vec<Value> = &anthropic
        .get("content")
        .and_then(|c| c.as_array())
        .cloned()
        .unwrap_or_default();
    let text: Vec<String> = blocks
        .iter()
        .filter(|b| b.get("type").and_then(|t| t.as_str()) == Some("text"))
        .filter_map(|b| {
            b.get("text")
                .and_then(|t| t.as_str())
                .map(|s| s.to_string())
        })
        .collect();
    let tool_calls: Vec<Value> = blocks
        .iter()
        .filter(|b| b.get("type").and_then(|t| t.as_str()) == Some("tool_use"))
        .map(|b| {
            json!({
                "id": b["id"],
                "type": "function",
                "function": {
                    "name": b["name"],
                    "arguments": serde_json::to_string(&b["input"]).unwrap_or_else(|_| "{}".into())
                }
            })
        })
        .collect();
    let mut message = json!({ "role": "assistant", "content": text.join("\n") });
    if !tool_calls.is_empty() {
        message["tool_calls"] = Value::Array(tool_calls);
    }
    let usage = anthropic.get("usage").cloned().unwrap_or(json!({}));
    json!({
        "id": anthropic.get("id").cloned().unwrap_or(json!("msg_unknown")),
        "object": "chat.completion",
        "created": 0,
        "model": anthropic.get("model").cloned().unwrap_or(json!("")),
        "choices": [{
            "index": 0,
            "message": message,
            "finish_reason": "stop"
        }],
        "usage": {
            "prompt_tokens": usage.get("input_tokens").cloned().unwrap_or(json!(0)),
            "completion_tokens": usage.get("output_tokens").cloned().unwrap_or(json!(0)),
            "total_tokens": json!(0)
        }
    })
}

/// Convert OpenAI chat.completion JSON → Claude message JSON (non-streaming).
fn openai_json_to_claude_message(openai: Value) -> Value {
    // If it doesn't look like OpenAI shape, return as-is
    if openai.get("choices").is_none() {
        return openai;
    }
    let choice = &openai["choices"][0];
    let msg = &choice["message"];
    let mut content: Vec<Value> = Vec::new();
    if let Some(text) = msg.get("content").and_then(|c| c.as_str())
        && !text.is_empty() {
            content.push(json!({"type":"text","text": text}));
        }
    if let Some(tcs) = msg.get("tool_calls").and_then(|t| t.as_array()) {
        for tc in tcs {
            let args_str = tc["function"]["arguments"].as_str().unwrap_or("{}");
            let input: Value = serde_json::from_str(args_str).unwrap_or(json!({}));
            content.push(json!({
                "type": "tool_use",
                "id": tc["id"],
                "name": tc["function"]["name"],
                "input": input
            }));
        }
    }
    let stop_reason = match choice["finish_reason"].as_str().unwrap_or("stop") {
        "stop" => "end_turn",
        "length" => "max_tokens",
        "tool_calls" => "tool_use",
        _ => "end_turn",
    };
    let usage = openai.get("usage").cloned().unwrap_or(json!({}));
    json!({
        "id": openai.get("id").cloned().unwrap_or(json!("msg_unknown")),
        "type": "message",
        "role": "assistant",
        "model": openai.get("model").cloned().unwrap_or(json!("")),
        "content": content,
        "stop_reason": stop_reason,
        "stop_sequence": null,
        "usage": {
            "input_tokens": usage.get("prompt_tokens").cloned().unwrap_or(json!(0)),
            "output_tokens": usage.get("completion_tokens").cloned().unwrap_or(json!(0))
        }
    })
}

fn err(status: StatusCode, msg: &str) -> Response {
    (
        status,
        Json(json!({ "error": { "message": msg, "type": "invalid_request_error" } })),
    )
        .into_response()
}

/// Convert OpenAI chat.completion JSON → Responses API response JSON (non-streaming).
fn openai_json_to_responses(openai: Value) -> Value {
    if openai.get("choices").is_none() {
        return openai;
    }
    let choice = &openai["choices"][0];
    let msg = &choice["message"];
    let mut output: Vec<Value> = Vec::new();

    // Text content → message output item
    if let Some(text) = msg.get("content").and_then(|c| c.as_str())
        && !text.is_empty() {
            output.push(json!({
                "type": "message",
                "role": "assistant",
                "content": [{"type": "output_text", "text": text}]
            }));
        }

    // Tool calls → function_call output items
    if let Some(tcs) = msg.get("tool_calls").and_then(|t| t.as_array()) {
        for tc in tcs {
            let name = tc["function"]["name"].as_str().unwrap_or("");
            let args = tc["function"]["arguments"].as_str().unwrap_or("{}");
            let call_id = tc["id"].as_str().unwrap_or("");
            output.push(json!({
                "type": "function_call",
                "call_id": call_id,
                "name": name,
                "arguments": args
            }));
        }
    }

    let usage = openai.get("usage").cloned().unwrap_or(json!({}));
    let input_tokens = usage
        .get("prompt_tokens")
        .and_then(|v| v.as_u64())
        .unwrap_or(0);
    let output_tokens = usage
        .get("completion_tokens")
        .and_then(|v| v.as_u64())
        .unwrap_or(0);

    json!({
        "id": format!("resp_{}", openai.get("id").and_then(|i| i.as_str()).unwrap_or("unknown")),
        "object": "response",
        "status": "completed",
        "output": output,
        "usage": {
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
            "total_tokens": input_tokens + output_tokens
        }
    })
}
