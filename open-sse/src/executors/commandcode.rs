//! CommandCode executor — talks to CommandCode AI SDK v5 NDJSON upstream.

use anyhow::{Result, anyhow};
use bytes::Bytes;
use futures_util::StreamExt;
use reqwest::Client;
use serde_json::{Value, json};
use std::time::Duration;

use super::default::{UpstreamBody, UpstreamResponse};
use crate::providers::Provider;

pub async fn execute(
    provider: &Provider,
    model: &str,
    mut body: Value,
    stream: bool,
    credentials: &Value,
) -> Result<UpstreamResponse> {
    let client = Client::builder()
        .connect_timeout(Duration::from_secs(30))
        .build()?;
    let token = credentials
        .get("apiKey")
        .or_else(|| credentials.get("api_key"))
        .or_else(|| credentials.get("accessToken"))
        .and_then(Value::as_str)
        .ok_or_else(|| anyhow!("missing CommandCode token"))?;

    let url = crate::providers::base_url_for(provider);
    body["stream"] = json!(true);

    let session_id = format!(
        "sess-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_millis()
    );

    let mut req = client
        .post(&url)
        .header("content-type", "application/json")
        .header("authorization", format!("Bearer {token}"))
        .header("x-session-id", session_id)
        .header("accept", "text/event-stream, application/x-ndjson, */*")
        .json(&body);

    if !stream {
        req = req.timeout(Duration::from_secs(300));
    }

    let resp = req.send().await?;
    let status = resp.status().as_u16();

    if !stream {
        let text = resp.text().await?;
        let mut content = String::new();
        for line in text.lines() {
            let line = line.trim();
            if line.is_empty() {
                continue;
            }
            if let Ok(v) = serde_json::from_str::<Value>(line)
                && let Some(t) = v.get("text").and_then(Value::as_str) {
                    content.push_str(t);
                }
        }
        return Ok(UpstreamResponse {
            status,
            body: UpstreamBody::Json(json!({
                "id": "chatcmpl-cmdcode",
                "object": "chat.completion",
                "model": model,
                "choices": [{
                    "index": 0,
                    "message": { "role": "assistant", "content": content },
                    "finish_reason": "stop"
                }]
            })),
            url,
        });
    }

    let input = resp.bytes_stream();
    let model = model.to_string();
    let stream = async_stream::stream! {
        let mut buffer = String::new();
        futures_util::pin_mut!(input);
        while let Some(chunk) = input.next().await {
            let Ok(chunk) = chunk else { continue };
            buffer.push_str(&String::from_utf8_lossy(&chunk));
            while let Some(pos) = buffer.find('\n') {
                let line = buffer[..pos].trim().to_string();
                buffer.drain(..=pos);
                if line.is_empty() { continue; }
                if let Ok(v) = serde_json::from_str::<Value>(&line)
                    && let Some(text) = v.get("text").and_then(Value::as_str) {
                        let chunk = json!({
                            "id": "chatcmpl-cmdcode",
                            "object": "chat.completion.chunk",
                            "model": model,
                            "choices": [{
                                "index": 0,
                                "delta": { "content": text },
                                "finish_reason": null
                            }]
                        });
                        yield Ok::<Bytes, anyhow::Error>(Bytes::from(format!("data: {chunk}\n\n")));
                    }
            }
        }
        yield Ok(Bytes::from("data: [DONE]\n\n"));
    };

    Ok(UpstreamResponse {
        status,
        body: UpstreamBody::Sse(Box::pin(stream)),
        url,
    })
}
