//! Video generation proxy (xAI Grok Imagine shape).
//! Supports /v1/videos/generations, /v1/videos/edits, /v1/videos/extensions, and GET /v1/videos/{id}.

use anyhow::{Result, anyhow};
use reqwest::Client;
use serde_json::Value;
use std::time::Duration;

pub const SUPPORTED_PROVIDERS: &[&str] = &["xai", "grok-web"];

pub fn is_supported(provider: &str) -> bool {
    SUPPORTED_PROVIDERS.contains(&provider)
}

fn base_url(provider: &str) -> String {
    let key = format!(
        "FLAMEROUTER_BASE_URL_{}",
        provider.to_uppercase().replace('-', "_")
    );
    if let Ok(v) = std::env::var(&key) {
        return v;
    }
    match provider {
        "xai" => "https://api.x.ai/v1/videos".to_string(),
        _ => "https://api.x.ai/v1/videos".to_string(),
    }
}

pub async fn create_video_job(
    provider: &str,
    action: &str,
    body: Value,
    credentials: &Value,
) -> Result<Value> {
    let client = Client::builder()
        .connect_timeout(Duration::from_secs(30))
        .timeout(Duration::from_secs(120))
        .build()?;

    let token = credentials
        .get("apiKey")
        .or_else(|| credentials.get("api_key"))
        .or_else(|| credentials.get("accessToken"))
        .and_then(Value::as_str)
        .unwrap_or("");

    let url = format!("{}/{}", base_url(provider).trim_end_matches('/'), action);
    let mut req = client.post(&url).header("content-type", "application/json");
    if !token.is_empty() {
        req = req.header("authorization", format!("Bearer {token}"));
    }

    let resp = req.json(&body).send().await?;
    let status = resp.status();
    let json_body: Value = resp.json().await.unwrap_or_default();
    if !status.is_success() {
        return Err(anyhow!("Video create error {}: {}", status, json_body));
    }
    Ok(json_body)
}

pub async fn get_video_job(provider: &str, request_id: &str, credentials: &Value) -> Result<Value> {
    let client = Client::builder()
        .connect_timeout(Duration::from_secs(30))
        .timeout(Duration::from_secs(120))
        .build()?;

    let token = credentials
        .get("apiKey")
        .or_else(|| credentials.get("api_key"))
        .or_else(|| credentials.get("accessToken"))
        .and_then(Value::as_str)
        .unwrap_or("");

    let url = format!(
        "{}/{}",
        base_url(provider).trim_end_matches('/'),
        request_id
    );
    let mut req = client.get(&url).header("accept", "application/json");
    if !token.is_empty() {
        req = req.header("authorization", format!("Bearer {token}"));
    }

    let resp = req.send().await?;
    let status = resp.status();
    let json_body: Value = resp.json().await.unwrap_or_default();
    if !status.is_success() {
        return Err(anyhow!("Video poll error {}: {}", status, json_body));
    }
    Ok(json_body)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_supported() {
        assert!(is_supported("xai"));
        assert!(!is_supported("openai"));
    }
}
