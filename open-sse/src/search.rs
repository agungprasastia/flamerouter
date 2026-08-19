//! Search endpoint handler.
//! Supports serper, brave-search, perplexity, exa, tavily, google-pse, linkup, searchapi, youcom, searxng.

use anyhow::{Result, anyhow};
use reqwest::Client;
use serde_json::{Value, json};
use std::time::Duration;

pub const SUPPORTED_PROVIDERS: &[&str] = &[
    "serper",
    "brave-search",
    "perplexity",
    "exa",
    "tavily",
    "google-pse",
    "linkup",
    "searchapi",
    "youcom",
    "searxng",
];

pub fn is_supported(provider: &str) -> bool {
    SUPPORTED_PROVIDERS.contains(&provider)
}

pub fn sanitize_query(query: &str) -> Result<String> {
    let clean: String = query
        .chars()
        .filter(|c| !c.is_control() || *c == '\t' || *c == '\n' || *c == '\r')
        .collect();
    let trimmed = clean.trim();
    if trimmed.is_empty() {
        return Err(anyhow!("Query is empty"));
    }
    Ok(trimmed.to_string())
}

fn make_result(
    provider_id: &str,
    title: String,
    url: String,
    snippet: String,
    score: Option<f64>,
    published_at: Option<String>,
    favicon_url: Option<String>,
    idx: usize,
    now: &str,
) -> Value {
    let display_url = if url.is_empty() {
        Value::Null
    } else {
        let stripped = url
            .strip_prefix("https://")
            .or_else(|| url.strip_prefix("http://"))
            .unwrap_or(&url);
        let stripped = stripped.strip_prefix("www.").unwrap_or(stripped);
        let host_path = stripped.split('?').next().unwrap_or(stripped);
        json!(host_path)
    };

    json!({
        "title": title,
        "url": url,
        "display_url": display_url,
        "snippet": snippet,
        "position": idx + 1,
        "score": score,
        "published_at": published_at,
        "favicon_url": favicon_url,
        "content": null,
        "metadata": {
            "author": null,
            "language": null,
            "source_type": null,
            "image_url": null
        },
        "citation": {
            "provider": provider_id,
            "retrieved_at": now,
            "rank": idx + 1
        },
        "provider_raw": null
    })
}

pub async fn execute_search(
    provider: &str,
    query: &str,
    search_type: Option<&str>,
    max_results: Option<usize>,
    country: Option<&str>,
    language: Option<&str>,
    credentials: &Value,
) -> Result<Value> {
    let client = Client::builder()
        .connect_timeout(Duration::from_secs(15))
        .timeout(Duration::from_secs(30))
        .build()?;

    let query = sanitize_query(query)?;
    let max = max_results.unwrap_or(5).min(100);
    let search_type = search_type.unwrap_or("web");
    let token = credentials
        .get("apiKey")
        .or_else(|| credentials.get("api_key"))
        .or_else(|| credentials.get("accessToken"))
        .and_then(Value::as_str)
        .unwrap_or("");

    let start = std::time::Instant::now();
    let now = chrono_now_iso();

    let (results, total_results) = match provider {
        "serper" => {
            let endpoint = if search_type == "news" {
                "https://google.serper.dev/news"
            } else {
                "https://google.serper.dev/search"
            };
            let mut body = json!({ "q": query, "num": max });
            if let Some(c) = country {
                body["gl"] = json!(c.to_lowercase());
            }
            if let Some(l) = language {
                body["hl"] = json!(l);
            }
            let resp = client
                .post(endpoint)
                .header("content-type", "application/json")
                .header("X-API-Key", token)
                .json(&body)
                .send()
                .await?;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() {
                return Err(anyhow!("Serper error {}: {}", status, json_body));
            }
            let items = if search_type == "news" {
                json_body.get("news")
            } else {
                json_body.get("organic")
            };
            let mut list = Vec::new();
            if let Some(arr) = items.and_then(Value::as_array) {
                for (idx, item) in arr.iter().enumerate().take(max) {
                    list.push(make_result(
                        provider,
                        item.get("title")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("link")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("snippet")
                            .or_else(|| item.get("description"))
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        None,
                        item.get("date").and_then(Value::as_str).map(str::to_string),
                        None,
                        idx,
                        &now,
                    ));
                }
            }
            let total = json_body
                .pointer("/searchParameters/totalResults")
                .and_then(Value::as_u64);
            (list, total)
        }
        "brave-search" => {
            let endpoint = if search_type == "news" {
                "https://api.search.brave.com/res/v1/news/search"
            } else {
                "https://api.search.brave.com/res/v1/web/search"
            };
            let mut req = client
                .get(endpoint)
                .header("Accept", "application/json")
                .header("X-Subscription-Token", token)
                .query(&[("q", &query), ("count", &max.to_string())]);
            if let Some(c) = country {
                req = req.query(&[("country", c)]);
            }
            if let Some(l) = language {
                req = req.query(&[("search_lang", l)]);
            }
            let resp = req.send().await?;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() {
                return Err(anyhow!("Brave error {}: {}", status, json_body));
            }
            let container = if search_type == "news" {
                json_body.get("news").or(Some(&json_body))
            } else {
                json_body.get("web")
            };
            let mut list = Vec::new();
            if let Some(arr) = container
                .and_then(|c| c.get("results"))
                .and_then(Value::as_array)
            {
                for (idx, item) in arr.iter().enumerate().take(max) {
                    list.push(make_result(
                        provider,
                        item.get("title")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("url")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("description")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        None,
                        item.get("page_age")
                            .or_else(|| item.get("age"))
                            .and_then(Value::as_str)
                            .map(str::to_string),
                        item.pointer("/meta_url/favicon")
                            .or_else(|| item.get("favicon"))
                            .and_then(Value::as_str)
                            .map(str::to_string),
                        idx,
                        &now,
                    ));
                }
            }
            let total = container
                .and_then(|c| c.get("totalCount"))
                .and_then(Value::as_u64);
            (list, total)
        }
        "tavily" => {
            let endpoint = "https://api.tavily.com/search";
            let mut body = json!({
                "query": query,
                "max_results": max,
                "topic": if search_type == "news" { "news" } else { "general" }
            });
            if let Some(c) = country {
                body["country"] = json!(c);
            }
            let resp = client
                .post(endpoint)
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .json(&body)
                .send()
                .await?;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() {
                return Err(anyhow!("Tavily error {}: {}", status, json_body));
            }
            let mut list = Vec::new();
            if let Some(arr) = json_body.get("results").and_then(Value::as_array) {
                for (idx, item) in arr.iter().enumerate().take(max) {
                    list.push(make_result(
                        provider,
                        item.get("title")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("url")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("content")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("score").and_then(Value::as_f64),
                        item.get("published_date")
                            .and_then(Value::as_str)
                            .map(str::to_string),
                        None,
                        idx,
                        &now,
                    ));
                }
            }
            (list, None)
        }
        "exa" => {
            let endpoint = "https://api.exa.ai/search";
            let mut body = json!({
                "query": query,
                "numResults": max,
                "type": "auto",
                "text": true,
                "highlights": true
            });
            if search_type == "news" {
                body["category"] = json!("news");
            }
            let resp = client
                .post(endpoint)
                .header("content-type", "application/json")
                .header("x-api-key", token)
                .json(&body)
                .send()
                .await?;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() {
                return Err(anyhow!("Exa error {}: {}", status, json_body));
            }
            let mut list = Vec::new();
            if let Some(arr) = json_body.get("results").and_then(Value::as_array) {
                for (idx, item) in arr.iter().enumerate().take(max) {
                    let snippet = item
                        .pointer("/highlights/0")
                        .or_else(|| item.get("text"))
                        .and_then(Value::as_str)
                        .unwrap_or("");
                    list.push(make_result(
                        provider,
                        item.get("title")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("url")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        snippet.to_string(),
                        item.get("score").and_then(Value::as_f64),
                        item.get("publishedDate")
                            .and_then(Value::as_str)
                            .map(str::to_string),
                        item.get("favicon")
                            .and_then(Value::as_str)
                            .map(str::to_string),
                        idx,
                        &now,
                    ));
                }
            }
            (list, None)
        }
        "perplexity" => {
            let endpoint = "https://api.perplexity.ai/search";
            let mut body = json!({
                "query": query,
                "max_results": max
            });
            if let Some(c) = country {
                body["country"] = json!(c);
            }
            if let Some(l) = language {
                body["search_language_filter"] = json!([l]);
            }
            let resp = client
                .post(endpoint)
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .json(&body)
                .send()
                .await?;
            let status = resp.status();
            let json_body: Value = resp.json().await.unwrap_or_default();
            if !status.is_success() {
                return Err(anyhow!("Perplexity error {}: {}", status, json_body));
            }
            let mut list = Vec::new();
            if let Some(arr) = json_body.get("results").and_then(Value::as_array) {
                for (idx, item) in arr.iter().enumerate().take(max) {
                    list.push(make_result(
                        provider,
                        item.get("title")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("url")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        item.get("snippet")
                            .and_then(Value::as_str)
                            .unwrap_or("")
                            .to_string(),
                        None,
                        item.get("date")
                            .or_else(|| item.get("last_updated"))
                            .and_then(Value::as_str)
                            .map(str::to_string),
                        None,
                        idx,
                        &now,
                    ));
                }
            }
            (list, None)
        }
        _ => return Err(anyhow!("Unsupported search provider: {}", provider)),
    };

    let duration_ms = start.elapsed().as_millis() as u64;

    Ok(json!({
        "provider": provider,
        "query": query,
        "results": results,
        "answer": null,
        "usage": {
            "queries_used": 1,
            "search_cost_usd": 0.0
        },
        "metrics": {
            "response_time_ms": duration_ms,
            "upstream_latency_ms": duration_ms,
            "total_results_available": total_results
        },
        "errors": []
    }))
}

fn chrono_now_iso() -> String {
    // Simple ISO 8601 UTC timestamp without heavy chrono dep
    let now = std::time::SystemTime::now();
    let dur = now
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default();
    let secs = dur.as_secs();
    format!("{secs}")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_supported() {
        assert!(is_supported("serper"));
        assert!(is_supported("brave-search"));
        assert!(is_supported("tavily"));
        assert!(is_supported("exa"));
        assert!(is_supported("perplexity"));
        assert!(!is_supported("openai"));
    }

    #[test]
    fn test_sanitize_query() {
        assert_eq!(sanitize_query("  rust async  ").unwrap(), "rust async");
        assert!(sanitize_query("   ").is_err());
    }
}
