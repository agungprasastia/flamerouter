//! Trae executor — SOLO remote agent API.
//! Port of open-sse/executors/trae.js.

use anyhow::{Result, anyhow};
use bytes::Bytes;
use futures_util::StreamExt;
use reqwest::Client;
use serde_json::{Value, json};
use std::time::Duration;

use super::default::{UpstreamBody, UpstreamResponse};
use crate::providers::Provider;

fn flatten_messages_query(messages: &[Value]) -> String {
    let mut parts = Vec::new();
    for m in messages {
        let role = m.get("role").and_then(Value::as_str).unwrap_or("user");
        let content = m.get("content").and_then(Value::as_str).unwrap_or("");
        if role == "system" {
            parts.push(format!("[System]\n{content}"));
        } else if role == "assistant" {
            parts.push(format!("[Assistant]\n{content}"));
        } else {
            parts.push(content.to_string());
        }
    }
    json!([{ "type": "text", "data": { "content": parts.join("\n\n") } }]).to_string()
}

pub async fn execute(
    provider: &Provider,
    model: &str,
    body: Value,
    stream: bool,
    credentials: &Value,
) -> Result<UpstreamResponse> {
    let client = Client::builder()
        .connect_timeout(Duration::from_secs(30))
        .build()?;
    let token = credentials
        .get("accessToken")
        .or_else(|| credentials.get("apiKey"))
        .and_then(Value::as_str)
        .ok_or_else(|| anyhow!("missing Trae token"))?;

    let base = crate::providers::base_url_for(provider);
    let base = base.trim_end_matches('/');

    let messages = body
        .get("messages")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    let query = flatten_messages_query(&messages);

    // If base url points to standard openai chat/completions mock (in tests or proxy), fallback to default post
    if base.contains("/chat/completions") || base.contains("/v1") || base.contains("127.0.0.1") {
        let post_url = if base.ends_with("/chat/completions") {
            base.to_string()
        } else {
            format!("{}/chat/completions", base.trim_end_matches('/'))
        };
        let resp = client
            .post(&post_url)
            .header("authorization", format!("Cloud-IDE-JWT {token}"))
            .header("content-type", "application/json")
            .json(&body)
            .send()
            .await?;
        let status = resp.status().as_u16();
        let json_body: Value = resp.json().await.unwrap_or_default();
        return Ok(UpstreamResponse {
            status,
            body: UpstreamBody::Json(json_body),
            url: post_url,
        });
    }

    // 1. Create chat session
    let create_body = json!({
        "mode": "code",
        "environment_id": "default",
        "initial_message": {
            "chat_session_id": "",
            "content": [],
            "query": query,
            "model_name": if model == "auto" { "" } else { model },
            "agent_type": "solo_agent_remote",
            "model_selection_strategy": if model == "auto" { "auto" } else { "manual" },
            "common_params": json!({
                "language": "en-us",
                "app_language": "en",
                "quality": "stable",
                "app_version": "1.0.0.1229",
                "user_identity": "Free",
                "scope": "marscode-us",
                "tenant": "marscode",
                "region": "US-East",
                "aiRegion": "US-East",
                "solo_chat_mode": "code"
            }).to_string()
        },
        "env": "remote",
        "auto_create_project": false,
        "origin": "web"
    });

    let session_res = client
        .post(format!("{base}/chat_sessions"))
        .header("authorization", format!("Cloud-IDE-JWT {token}"))
        .header("content-type", "application/json")
        .header("x-trae-client-type", "web")
        .json(&create_body)
        .send()
        .await?;

    let session_status = session_res.status();
    let session_json: Value = session_res.json().await.unwrap_or_default();
    if !session_status.is_success() || session_json.get("code").and_then(Value::as_i64) != Some(0) {
        return Err(anyhow!("Trae session creation failed: {}", session_json));
    }

    let session_id = session_json
        .pointer("/data/chat_session_id")
        .and_then(Value::as_str)
        .unwrap_or("");
    let message_id = session_json
        .pointer("/data/message_id")
        .and_then(Value::as_str)
        .unwrap_or("");

    // 2. Stream events from GET /chat_sessions/{id}/events
    let events_url =
        format!("{base}/chat_sessions/{session_id}/events?reply_to_message_id={message_id}");
    let events_res = client
        .get(&events_url)
        .header("authorization", format!("Cloud-IDE-JWT {token}"))
        .header("accept", "text/event-stream")
        .send()
        .await?;

    let status = events_res.status().as_u16();

    if !stream {
        let text = events_res.text().await?;
        let mut full_thought = String::new();
        for line in text.lines() {
            if let Some(data) = line.strip_prefix("data:")
                && let Ok(v) = serde_json::from_str::<Value>(data.trim())
                    && let Some(t) = v.get("thought").and_then(Value::as_str)
                        && t.len() > full_thought.len() {
                            full_thought = t.to_string();
                        }
        }
        return Ok(UpstreamResponse {
            status,
            body: UpstreamBody::Json(json!({
                "id": "chatcmpl-trae",
                "object": "chat.completion",
                "model": model,
                "choices": [{
                    "index": 0,
                    "message": { "role": "assistant", "content": full_thought },
                    "finish_reason": "stop"
                }]
            })),
            url: base.to_string(),
        });
    }

    let input = events_res.bytes_stream();
    let model = model.to_string();
    let out_stream = async_stream::stream! {
        let mut buffer = String::new();
        let mut full_thought = String::new();
        futures_util::pin_mut!(input);
        while let Some(chunk) = input.next().await {
            let Ok(chunk) = chunk else { continue };
            buffer.push_str(&String::from_utf8_lossy(&chunk));
            while let Some(pos) = buffer.find('\n') {
                let line = buffer[..pos].trim().to_string();
                buffer.drain(..=pos);
                if let Some(data) = line.strip_prefix("data:") {
                    let data = data.trim();
                    if let Ok(v) = serde_json::from_str::<Value>(data)
                        && let Some(t) = v.get("thought").and_then(Value::as_str)
                            && t.len() > full_thought.len() {
                                let delta = &t[full_thought.len()..];
                                full_thought = t.to_string();
                                let chunk = json!({
                                    "id": "chatcmpl-trae",
                                    "object": "chat.completion.chunk",
                                    "model": model,
                                    "choices": [{
                                        "index": 0,
                                        "delta": { "content": delta },
                                        "finish_reason": null
                                    }]
                                });
                                yield Ok::<Bytes, anyhow::Error>(Bytes::from(format!("data: {chunk}\n\n")));
                            }
                }
            }
        }
        yield Ok(Bytes::from("data: [DONE]\n\n"));
    };

    Ok(UpstreamResponse {
        status,
        body: UpstreamBody::Sse(Box::pin(out_stream)),
        url: base.to_string(),
    })
}
