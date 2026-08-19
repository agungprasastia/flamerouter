use anyhow::{Result, anyhow};
use bytes::Bytes;
use futures_util::StreamExt;
use reqwest::Client;
use serde_json::{Value, json};
use std::time::Duration;

use super::default::{UpstreamBody, UpstreamResponse};
use crate::providers::Provider;

fn base_url(provider: &Provider) -> String {
    let env = "FLAMEROUTER_BASE_URL_ZED";
    std::env::var(env).unwrap_or_else(|_| provider.base_url.to_string())
}

fn token_url() -> String {
    std::env::var("FLAMEROUTER_ZED_TOKEN_URL")
        .unwrap_or_else(|_| "https://cloud.zed.dev/client/llm_tokens".to_string())
}

fn user_auth(credentials: &Value) -> Result<String> {
    let user = credentials
        .get("userId")
        .or_else(|| credentials.get("user_id"))
        .and_then(Value::as_str)
        .ok_or_else(|| anyhow!("Zed credential missing userId"))?;
    let token = credentials
        .get("accessToken")
        .or_else(|| credentials.get("access_token"))
        .or_else(|| credentials.get("apiKey"))
        .and_then(Value::as_str)
        .ok_or_else(|| anyhow!("Zed credential missing accessToken"))?;
    Ok(format!("{user} {token}"))
}

async fn fetch_token(client: &Client, credentials: &Value) -> Result<String> {
    let auth = user_auth(credentials)?;
    let organization = credentials
        .pointer("/providerSpecificData/organizationId")
        .or_else(|| credentials.get("organizationId"));
    let mut body = json!({});
    if let Some(org) = organization {
        body["organization_id"] = org.clone();
    }
    let response = client
        .post(token_url())
        .header("authorization", auth)
        .header("content-type", "application/json")
        .json(&body)
        .send()
        .await?;
    let status = response.status();
    let value: Value = response.json().await.unwrap_or_default();
    if !status.is_success() {
        return Err(anyhow!("Zed token request failed: {}", status));
    }
    value
        .get("token")
        .and_then(Value::as_str)
        .or_else(|| value.pointer("/token/value").and_then(Value::as_str))
        .map(str::to_string)
        .ok_or_else(|| anyhow!("Zed did not return an LLM token"))
}

fn provider_request(model: &str, body: &Value, stream: bool) -> Value {
    let mut request = body.clone();
    request["model"] = json!(model);
    request["stream"] = json!(stream);
    request
}

fn envelope(model: &str, body: &Value, stream: bool, credentials: &Value, provider: &str) -> Value {
    json!({
        "thread_id": body.get("thread_id").or_else(|| credentials.get("_clientSessionId")),
        "prompt_id": body.get("prompt_id"),
        "provider": provider,
        "model": model,
        "provider_request": provider_request(model, body, stream)
    })
}

fn event_line(line: &str, model: &str) -> Option<String> {
    let value: Value = serde_json::from_str(line.trim()).ok()?;
    if value.get("status").is_some() {
        return None;
    }
    let event = value.get("event").cloned().unwrap_or(value);
    if event.get("choices").is_some() {
        return Some(event.to_string());
    }
    if let Some(text) = event.pointer("/delta/text").and_then(Value::as_str) {
        return Some(json!({"id":"chatcmpl-zed","object":"chat.completion.chunk","model":model,"choices":[{"index":0,"delta":{"content":text},"finish_reason":null}]}).to_string());
    }
    None
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
    let token = fetch_token(&client, credentials).await?;
    let url = base_url(provider);
    let url = if url.ends_with("/completions") {
        url
    } else {
        format!("{}/completions", url.trim_end_matches('/'))
    };
    let response = client
        .post(&url)
        .header("authorization", format!("Bearer {token}"))
        .header("content-type", "application/json")
        .header("accept", "application/x-ndjson, text/event-stream, */*")
        .json(&envelope(model, &body, true, credentials, "OpenAi"))
        .send()
        .await?;
    let status = response.status().as_u16();
    if !stream {
        let raw = response.text().await?;
        let content = raw
            .lines()
            .filter_map(|line| event_line(line, model))
            .filter_map(|line| serde_json::from_str::<Value>(&line).ok())
            .filter_map(|v| {
                v.pointer("/choices/0/delta/content")
                    .and_then(Value::as_str)
                    .map(str::to_string)
            })
            .collect::<String>();
        return Ok(UpstreamResponse {
            status,
            body: UpstreamBody::Json(
                json!({"id":"chatcmpl-zed","object":"chat.completion","model":model,"choices":[{"index":0,"message":{"role":"assistant","content":content},"finish_reason":"stop"}]}),
            ),
            url,
        });
    }
    let input = response.bytes_stream();
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
                if let Some(event) = event_line(&line, &model) { yield Ok::<Bytes, anyhow::Error>(Bytes::from(format!("data: {event}\n\n"))); }
            }
        }
        if let Some(event) = event_line(&buffer, &model) { yield Ok::<Bytes, anyhow::Error>(Bytes::from(format!("data: {event}\n\n"))); }
        yield Ok(Bytes::from("data: [DONE]\n\n"));
    };
    Ok(UpstreamResponse {
        status,
        body: UpstreamBody::Sse(Box::pin(stream)),
        url,
    })
}
