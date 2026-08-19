//! Cursor executor — communicates with cursor.sh API.

use anyhow::Result;
use serde_json::Value;

use super::default::{UpstreamBody, UpstreamResponse};
use crate::providers::Provider;

pub async fn execute(
    provider: &Provider,
    _model: &str,
    body: Value,
    _stream: bool,
    credentials: &Value,
) -> Result<UpstreamResponse> {
    let client = reqwest::Client::new();
    let token = credentials
        .get("accessToken")
        .or_else(|| credentials.get("apiKey"))
        .and_then(Value::as_str)
        .unwrap_or("");

    let url = crate::providers::base_url_for(provider);
    let resp = client
        .post(&url)
        .header("authorization", format!("Bearer {token}"))
        .header("content-type", "application/json")
        .json(&body)
        .send()
        .await?;

    let status = resp.status().as_u16();
    let json_body: Value = resp.json().await.unwrap_or_default();

    Ok(UpstreamResponse {
        status,
        body: UpstreamBody::Json(json_body),
        url,
    })
}
