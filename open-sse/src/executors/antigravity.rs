//! Antigravity & Google Code Assist executor.
//! Port of open-sse/executors/antigravity.js.

use anyhow::{Result, anyhow};
use bytes::Bytes;
use reqwest::Client;
use serde_json::{Value, json};
use std::time::Duration;

use super::default::{UpstreamBody, UpstreamResponse};
use crate::providers::Provider;

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
        .ok_or_else(|| anyhow!("missing Antigravity token"))?;

    let base = crate::providers::base_url_for(provider);
    let url = if base.is_empty() {
        "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateCode".to_string()
    } else {
        base
    };

    let req_body = json!({
        "project": credentials.get("project_id").or_else(|| credentials.get("projectId")).unwrap_or(&json!("")),
        "model": model,
        "request": body
    });

    let resp = client
        .post(&url)
        .header("authorization", format!("Bearer {token}"))
        .header("content-type", "application/json")
        .header("User-Agent", "antigravity/1.0.0 (linux; x64)")
        .json(&req_body)
        .send()
        .await?;

    let status = resp.status().as_u16();

    if !stream {
        let json_body: Value = resp.json().await.unwrap_or_default();
        return Ok(UpstreamResponse {
            status,
            body: UpstreamBody::Json(json_body),
            url,
        });
    }

    let byte_stream = resp.bytes_stream();
    let stream = async_stream::stream! {
        use futures_util::StreamExt;
        tokio::pin!(byte_stream);
        while let Some(chunk) = byte_stream.next().await {
            if let Ok(b) = chunk {
                yield Ok::<Bytes, anyhow::Error>(b);
            }
        }
    };

    Ok(UpstreamResponse {
        status,
        body: UpstreamBody::Sse(Box::pin(stream)),
        url,
    })
}
