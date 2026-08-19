//! Azure executor - Custom deployments and headers
use anyhow::{Context, Result};
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

    let azure_endpoint = credentials
        .pointer("/providerSpecificData/azureEndpoint")
        .and_then(Value::as_str)
        .map(String::from)
        .or_else(|| std::env::var("AZURE_ENDPOINT").ok())
        .unwrap_or_else(|| "https://api.openai.com".to_string());
    let azure_endpoint = azure_endpoint.trim_end_matches('/');

    let api_version = credentials
        .pointer("/providerSpecificData/apiVersion")
        .and_then(Value::as_str)
        .map(String::from)
        .or_else(|| std::env::var("AZURE_API_VERSION").ok())
        .unwrap_or_else(|| "2024-10-01-preview".to_string());

    let deployment = credentials
        .pointer("/providerSpecificData/deployment")
        .and_then(Value::as_str)
        .map(String::from)
        .or_else(|| if !model.is_empty() { Some(model.to_string()) } else { None })
        .or_else(|| std::env::var("AZURE_DEPLOYMENT").ok())
        .unwrap_or_else(|| "gpt-4".to_string());

    let url = format!("{azure_endpoint}/openai/deployments/{deployment}/chat/completions?api-version={api_version}");

    let mut req = client.post(&url).json(&body);
    if !stream {
        req = req.timeout(Duration::from_secs(300));
    }

    let api_key = credentials
        .get("apiKey")
        .or_else(|| credentials.get("accessToken"))
        .or_else(|| credentials.get("api_key"))
        .and_then(Value::as_str)
        .unwrap_or("");

    if !api_key.is_empty() {
        req = req.header("api-key", api_key);
    }

    if let Some(org) = credentials.pointer("/providerSpecificData/organization").and_then(Value::as_str) {
        req = req.header("OpenAI-Organization", org);
    }

    if stream {
        req = req.header("Accept", "text/event-stream");
    }

    let resp = req.send().await.context("azure request")?;
    let status = resp.status().as_u16();

    if stream && status == 200 {
        use futures_util::StreamExt;
        let byte_stream = resp.bytes_stream();
        let mapped = byte_stream.map(|r| r.map_err(|e| anyhow::anyhow!(e)));
        return Ok(UpstreamResponse {
            status,
            body: UpstreamBody::Sse(Box::pin(mapped)),
            url,
        });
    }

    let json: Value = resp.json().await.context("parse azure json")?;
    Ok(UpstreamResponse {
        status,
        body: UpstreamBody::Json(json),
        url,
    })
}
