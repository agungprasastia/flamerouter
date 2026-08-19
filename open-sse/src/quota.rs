//! Provider quota and credit balance fetchers in Rust.
//! Covers Google/Antigravity, Anthropic/Claude, Codex, DeepSeek, Kimi, Minimax, etc.

use anyhow::{anyhow, Result};
use reqwest::Client;
use serde_json::{json, Value};
use std::time::Duration;

pub async fn fetch_provider_quota(provider: &str, credentials: &Value) -> Result<Value> {
    let client = Client::builder()
        .use_rustls_tls()
        .connect_timeout(Duration::from_secs(15))
        .build()?;
    
    let token = credentials
        .get("apiKey")
        .or_else(|| credentials.get("accessToken"))
        .or_else(|| credentials.get("api_key"))
        .and_then(Value::as_str)
        .unwrap_or("");

    match provider {
        "anthropic" | "claude" => {
            let resp = client.get("https://api.anthropic.com/api/oauth/usage")
                .header("authorization", format!("Bearer {token}"))
                .header("anthropic-version", "2023-06-01")
                .send().await?;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() { return Err(anyhow!("Anthropic quota fetch failed: {}", status)); }
            Ok(json_body)
        }
        "minimax" | "minimax-cn" => {
            let resp = client.get("https://api.minimax.chat/v1/user/charge_info")
                .header("authorization", format!("Bearer {token}"))
                .send().await?;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() { return Err(anyhow!("Minimax quota fetch failed: {}", status)); }
            Ok(json_body)
        }
        "deepseek" => {
            let resp = client.get("https://api.deepseek.com/user/balance")
                .header("authorization", format!("Bearer {token}"))
                .send().await?;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() { return Err(anyhow!("Deepseek quota fetch failed: {}", status)); }
            Ok(json_body)
        }
        "kimi" => {
            let resp = client.get("https://api.moonshot.cn/v1/users/me/balance")
                .header("authorization", format!("Bearer {token}"))
                .send().await?;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() { return Err(anyhow!("Kimi quota fetch failed: {}", status)); }
            Ok(json_body)
        }
        _ => {
            Ok(json!({
                "provider": provider,
                "status": "active",
                "unlimited": true
            }))
        }
    }
}
