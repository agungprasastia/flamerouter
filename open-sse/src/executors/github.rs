//! GitHub Copilot Executor
use anyhow::{Context, Result, anyhow};
use reqwest::Client;
use serde_json::Value;
use std::time::Duration;
use crate::executors::default::{UpstreamBody, UpstreamResponse};
use crate::providers::Provider;

pub async fn execute(
    _provider: &Provider,
    model: &str,
    mut body: Value,
    stream: bool,
    credentials: &Value,
) -> Result<UpstreamResponse> {
    let client = Client::builder()
        .use_rustls_tls()
        .connect_timeout(Duration::from_secs(30))
        .build()?;

    let is_claude = model.to_lowercase().contains("claude");
    let base_url = if is_claude {
        "https://api.githubcopilot.com/v1/messages"
    } else {
        "https://api.githubcopilot.com/chat/completions"
    };

    let token = credentials
        .get("copilotToken")
        .or_else(|| credentials.get("accessToken"))
        .or_else(|| credentials.get("apiKey"))
        .and_then(Value::as_str)
        .unwrap_or("");

    // Modern max_completion_tokens conversion if needed
    if (model.contains("gpt-5") || model.starts_with("o1") || model.starts_with("o3") || model.starts_with("o4"))
        && body.get("max_tokens").is_some() {
        if let Some(tokens) = body.get("max_tokens").cloned() {
            body["max_completion_tokens"] = tokens;
            body.as_object_mut().unwrap().remove("max_tokens");
        }
    }

    if body.get("reasoning_effort").and_then(Value::as_str) == Some("none") {
        body.as_object_mut().unwrap().remove("reasoning_effort");
    }

    let req_id = format!("{}-{}", std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_millis(), rand::random::<u32>());

    let mut req = client
        .post(base_url)
        .json(&body)
        .header("Authorization", format!("Bearer {token}"))
        .header("copilot-integration-id", "vscode-chat")
        .header("editor-version", "vscode/1.97.2")
        .header("editor-plugin-version", "copilot-chat/0.24.1")
        .header("user-agent", "GitHubCopilotChat/0.24.1")
        .header("openai-intent", "conversation-panel")
        .header("x-github-api-version", "2023-07-07")
        .header("x-request-id", req_id)
        .header("x-vscode-user-agent-library-version", "electron-fetch")
        .header("X-Initiator", "user")
        .header("anthropic-version", "2023-06-01");

    if stream {
        req = req.header("Accept", "text/event-stream");
    } else {
        req = req.timeout(Duration::from_secs(300));
    }

    let resp = req.send().await.context("github copilot request")?;
    let status = resp.status().as_u16();
    let url_str = base_url.to_string();

    if stream && status == 200 {
        use futures_util::StreamExt;
        let byte_stream = resp.bytes_stream();
        let mapped = byte_stream.map(|r| r.map_err(|e| anyhow!(e)));
        return Ok(UpstreamResponse {
            status,
            body: UpstreamBody::Sse(Box::pin(mapped)),
            url: url_str,
        });
    }

    let json: Value = resp.json().await.context("parse github json")?;
    Ok(UpstreamResponse {
        status,
        body: UpstreamBody::Json(json),
        url: url_str,
    })
}
