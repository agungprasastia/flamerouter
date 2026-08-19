//! Vertex AI Executor
use anyhow::{Context, Result, anyhow};
use reqwest::Client;
use serde_json::Value;
use std::time::Duration;
use crate::executors::default::{UpstreamBody, UpstreamResponse};
use crate::providers::Provider;

pub async fn execute(
    _provider: &Provider,
    model: &str,
    body: Value,
    stream: bool,
    credentials: &Value,
) -> Result<UpstreamResponse> {
    let client = Client::builder()
        .use_rustls_tls()
        .connect_timeout(Duration::from_secs(30))
        .build()?;

    let project = credentials.pointer("/providerSpecificData/project")
        .or_else(|| credentials.get("projectId"))
        .or_else(|| credentials.get("project"))
        .and_then(Value::as_str)
        .unwrap_or("default");

    let location = credentials.pointer("/providerSpecificData/location")
        .or_else(|| credentials.get("location"))
        .and_then(Value::as_str)
        .unwrap_or("us-central1");

    let token = credentials.get("accessToken")
        .or_else(|| credentials.get("apiKey"))
        .and_then(Value::as_str)
        .unwrap_or("");

    let action = if stream { "streamGenerateContent?alt=sse" } else { "generateContent" };
    let url = format!("https://{location}-aiplatform.googleapis.com/v1/projects/{project}/locations/{location}/publishers/google/models/{model}:{action}");

    let mut req = client.post(&url)
        .json(&body)
        .header("Authorization", format!("Bearer {token}"))
        .header("Content-Type", "application/json");

    if stream {
        req = req.header("Accept", "text/event-stream");
    } else {
        req = req.timeout(Duration::from_secs(300));
    }

    let resp = req.send().await.context("vertex request")?;
    let status = resp.status().as_u16();

    if stream && status == 200 {
        use futures_util::StreamExt;
        let byte_stream = resp.bytes_stream();
        let mapped = byte_stream.map(|r| r.map_err(|e| anyhow!(e)));
        return Ok(UpstreamResponse {
            status,
            body: UpstreamBody::Sse(Box::pin(mapped)),
            url,
        });
    }

    let json: Value = resp.json().await.context("parse vertex json")?;
    Ok(UpstreamResponse {
        status,
        body: UpstreamBody::Json(json),
        url,
    })
}
