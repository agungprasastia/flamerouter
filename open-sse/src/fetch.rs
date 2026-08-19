//! Web URL content extractor (fetch endpoint).
//! Supports firecrawl, jina-reader, tavily, exa.

use anyhow::{Result, anyhow};
use reqwest::Client;
use serde_json::{Value, json};
use std::time::Duration;

pub const SUPPORTED_PROVIDERS: &[&str] = &["firecrawl", "jina-reader", "tavily", "exa"];

pub fn is_supported(provider: &str) -> bool {
    SUPPORTED_PROVIDERS.contains(&provider)
}

pub fn check_public_url(url_str: &str) -> Result<()> {
    let url = reqwest::Url::parse(url_str).map_err(|_| anyhow!("Invalid URL format"))?;
    let host = url
        .host_str()
        .ok_or_else(|| anyhow!("Missing host"))?
        .to_lowercase();

    if host == "localhost" || host == "ip6-localhost" || host == "ip6-loopback" {
        return Err(anyhow!("Blocked URL: internal host"));
    }
    if host.ends_with(".internal") || host.ends_with(".local") || host.ends_with(".localhost") {
        return Err(anyhow!("Blocked URL: internal host"));
    }
    if let Ok(ip) = host.parse::<std::net::IpAddr>() {
        match ip {
            std::net::IpAddr::V4(ipv4) => {
                if ipv4.is_loopback()
                    || ipv4.is_private()
                    || ipv4.is_link_local()
                    || ipv4.is_unspecified()
                {
                    return Err(anyhow!("Blocked URL: private IP"));
                }
            }
            std::net::IpAddr::V6(ipv6) => {
                if ipv6.is_loopback() || ipv6.is_unspecified() {
                    return Err(anyhow!("Blocked URL: private IP"));
                }
                let seg = ipv6.segments();
                // fe80::/10 (link local) or fc00::/7 (unique local)
                if (seg[0] & 0xffc0) == 0xfe80 || (seg[0] & 0xfe00) == 0xfc00 {
                    return Err(anyhow!("Blocked URL: private IP"));
                }
            }
        }
    }
    Ok(())
}

fn truncate(text: &str, max: Option<usize>) -> String {
    match max {
        Some(m) if m > 0 && text.len() > m => text[..m].to_string(),
        _ => text.to_string(),
    }
}

fn parse_jina_title(text: &str) -> Option<String> {
    for line in text.lines() {
        let trimmed = line.trim();
        if let Some(rest) = trimmed.strip_prefix("Title:") {
            return Some(rest.trim().to_string());
        }
        if let Some(rest) = trimmed.strip_prefix('#')
            && !rest.starts_with('#') {
                return Some(rest.trim().to_string());
            }
    }
    None
}

fn build_response_data(
    provider: &str,
    url: &str,
    title: Option<String>,
    format: &str,
    text: String,
    cost_usd: Option<f64>,
    response_ms: u64,
    upstream_ms: u64,
) -> Value {
    let len = text.len();
    json!({
        "provider": provider,
        "url": url,
        "title": title,
        "content": {
            "format": format,
            "text": text,
            "length": len
        },
        "metadata": {
            "author": null,
            "published_at": null,
            "language": null
        },
        "usage": {
            "fetch_cost_usd": cost_usd
        },
        "metrics": {
            "response_time_ms": response_ms,
            "upstream_latency_ms": upstream_ms
        }
    })
}

pub async fn execute_fetch(
    provider: &str,
    url: &str,
    format: Option<&str>,
    max_characters: Option<usize>,
    api_key: &str,
) -> Result<Value> {
    let client = Client::builder()
        .connect_timeout(Duration::from_secs(15))
        .timeout(Duration::from_secs(30))
        .build()?;

    let fmt = format.unwrap_or("markdown");
    let start = std::time::Instant::now();

    match provider {
        "firecrawl" => {
            let upstream_start = std::time::Instant::now();
            let mut req = client
                .post("https://api.firecrawl.dev/v1/scrape")
                .header("content-type", "application/json")
                .json(&json!({
                    "url": url,
                    "formats": [fmt]
                }));
            if !api_key.is_empty() {
                req = req.header("authorization", format!("Bearer {api_key}"));
            }
            let resp = req.send().await?;
            let upstream_ms = upstream_start.elapsed().as_millis() as u64;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() {
                return Err(anyhow!(
                    "Firecrawl error {}: {}",
                    status,
                    json_body.get("error").unwrap_or(&json_body)
                ));
            }
            let d = json_body.get("data").cloned().unwrap_or(json!({}));
            let text_raw = d
                .get("markdown")
                .or_else(|| d.get("html"))
                .or_else(|| d.get("text"))
                .and_then(Value::as_str)
                .unwrap_or("");
            let text = truncate(text_raw, max_characters);
            let title = d
                .pointer("/metadata/title")
                .and_then(Value::as_str)
                .map(str::to_string);
            Ok(build_response_data(
                provider,
                url,
                title,
                fmt,
                text,
                None,
                start.elapsed().as_millis() as u64,
                upstream_ms,
            ))
        }
        "jina-reader" => {
            let upstream_start = std::time::Instant::now();
            let mut req = client
                .post("https://r.jina.ai/")
                .header("content-type", "application/json")
                .json(&json!({ "url": url }));
            if !api_key.is_empty() {
                req = req.header("authorization", format!("Bearer {api_key}"));
            }
            let resp = req.send().await?;
            let upstream_ms = upstream_start.elapsed().as_millis() as u64;
            let status = resp.status();
            let body = resp.text().await?;
            if !status.is_success() {
                return Err(anyhow!(
                    "Jina error {}: {}",
                    status,
                    &body[..body.len().min(300)]
                ));
            }
            let title = parse_jina_title(&body);
            let text = truncate(&body, max_characters);
            Ok(build_response_data(
                provider,
                url,
                title,
                fmt,
                text,
                None,
                start.elapsed().as_millis() as u64,
                upstream_ms,
            ))
        }
        "tavily" => {
            let upstream_start = std::time::Instant::now();
            let mut req = client
                .post("https://api.tavily.com/extract")
                .header("content-type", "application/json")
                .json(&json!({
                    "urls": [url],
                    "extract_depth": "basic"
                }));
            if !api_key.is_empty() {
                req = req.header("authorization", format!("Bearer {api_key}"));
            }
            let resp = req.send().await?;
            let upstream_ms = upstream_start.elapsed().as_millis() as u64;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() {
                return Err(anyhow!(
                    "Tavily error {}: {}",
                    status,
                    json_body.get("error").unwrap_or(&json_body)
                ));
            }
            let first = json_body
                .pointer("/results/0")
                .cloned()
                .unwrap_or(json!({}));
            let text_raw = first
                .get("raw_content")
                .and_then(Value::as_str)
                .unwrap_or("");
            let text = truncate(text_raw, max_characters);
            Ok(build_response_data(
                provider,
                url,
                None,
                fmt,
                text,
                None,
                start.elapsed().as_millis() as u64,
                upstream_ms,
            ))
        }
        "exa" => {
            let upstream_start = std::time::Instant::now();
            let mut req = client
                .post("https://api.exa.ai/contents")
                .header("content-type", "application/json")
                .json(&json!({
                    "ids": [url],
                    "text": true
                }));
            if !api_key.is_empty() {
                req = req.header("x-api-key", api_key);
            }
            let resp = req.send().await?;
            let upstream_ms = upstream_start.elapsed().as_millis() as u64;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() {
                return Err(anyhow!(
                    "Exa error {}: {}",
                    status,
                    json_body.get("error").unwrap_or(&json_body)
                ));
            }
            let first = json_body
                .pointer("/results/0")
                .cloned()
                .unwrap_or(json!({}));
            let text_raw = first.get("text").and_then(Value::as_str).unwrap_or("");
            let title = first
                .get("title")
                .and_then(Value::as_str)
                .map(str::to_string);
            let text = truncate(text_raw, max_characters);
            Ok(build_response_data(
                provider,
                url,
                title,
                fmt,
                text,
                None,
                start.elapsed().as_millis() as u64,
                upstream_ms,
            ))
        }
        _ => Err(anyhow!("Unsupported fetch provider: {}", provider)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_supported() {
        assert!(is_supported("firecrawl"));
        assert!(is_supported("jina-reader"));
        assert!(is_supported("tavily"));
        assert!(is_supported("exa"));
        assert!(!is_supported("openai"));
    }

    #[test]
    fn test_check_public_url() {
        assert!(check_public_url("https://example.com/test").is_ok());
        assert!(check_public_url("http://localhost:3000").is_err());
        assert!(check_public_url("http://127.0.0.1/secret").is_err());
        assert!(check_public_url("http://192.168.1.1/admin").is_err());
        assert!(check_public_url("http://service.internal/status").is_err());
    }

    #[test]
    fn test_parse_jina_title() {
        let doc = "Title: My Cool Page\n\nContent here";
        assert_eq!(parse_jina_title(doc), Some("My Cool Page".to_string()));

        let doc2 = "# Heading 1\nSome text";
        assert_eq!(parse_jina_title(doc2), Some("Heading 1".to_string()));
    }
}
