// Embeddings endpoint support (parity with open-sse/handlers/embeddingProviders).
// OpenAI-compatible providers call their dedicated embeddings URL (which differs
// from the chat base URL), gemini uses embedContent/batchEmbedContents with the
// key in the query string, selfhosted/custom nodes take the base from the
// credential (selfhosted-embedding REQUIRES it — never falls back to OpenAI).
use serde_json::{Value, json};

pub const OPENAI_COMPAT_EMBEDDING_URLS: &[(&str, &str)] = &[
    ("openai", "https://api.openai.com/v1/embeddings"),
    ("openrouter", "https://openrouter.ai/api/v1/embeddings"),
    ("mistral", "https://api.mistral.ai/v1/embeddings"),
    ("voyage-ai", "https://api.voyageai.com/v1/embeddings"),
    (
        "fireworks",
        "https://api.fireworks.ai/inference/v1/embeddings",
    ),
    ("together", "https://api.together.xyz/v1/embeddings"),
    (
        "nebius",
        "https://api.tokenfactory.nebius.com/v1/embeddings",
    ),
    ("github", "https://models.github.ai/inference/embeddings"),
    ("nvidia", "https://integrate.api.nvidia.com/v1/embeddings"),
    ("jina-ai", "https://api.jina.ai/v1/embeddings"),
    (
        "vercel-ai-gateway",
        "https://ai-gateway.vercel.sh/v1/embeddings",
    ),
];

const GEMINI_EMBED_BASE: &str = "https://generativelanguage.googleapis.com/v1beta/models";

/// Providers that expose an embeddings endpoint. Prefix providers
/// (openai-compatible-* / custom-embedding-*) read their base from credentials.
pub fn is_supported(provider_id: &str) -> bool {
    OPENAI_COMPAT_EMBEDDING_URLS
        .iter()
        .any(|(id, _)| *id == provider_id)
        || matches!(
            provider_id,
            "gemini" | "google_ai_studio" | "selfhosted-embedding"
        )
        || provider_id.starts_with("openai-compatible-")
        || provider_id.starts_with("custom-embedding-")
}

fn key_from(credentials: &Value) -> Option<&str> {
    credentials
        .get("apiKey")
        .and_then(|v| v.as_str())
        .or_else(|| credentials.get("api_key").and_then(|v| v.as_str()))
        .or_else(|| credentials.get("accessToken").and_then(|v| v.as_str()))
}

/// Percent-encode for a query-string key value (no urlencoding dep needed).
fn encode_key(key: &str) -> String {
    let mut out = String::with_capacity(key.len());
    for b in key.bytes() {
        if b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.' | b'~') {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{:02X}", b));
        }
    }
    out
}

fn normalize_model(model: &str) -> String {
    model
        .strip_prefix("models/")
        .map(|m| m.to_string())
        .unwrap_or_else(|| model.to_string())
}

/// Resolve the upstream embeddings URL for a provider/model/input shape.
/// Err = configuration error surfaced as a 400 by the caller.
pub fn url_for(
    provider_id: &str,
    model: &str,
    input_is_array: bool,
    credentials: &Value,
) -> Result<String, String> {
    if matches!(provider_id, "gemini" | "google_ai_studio") {
        let key = key_from(credentials).ok_or_else(|| {
            format!(
                "provider '{}' requires an api key in the credential",
                provider_id
            )
        })?;
        let op = if input_is_array {
            "batchEmbedContents"
        } else {
            "embedContent"
        };
        // env override for tests/dev, like providers::base_url_for (base ends in "/models")
        let base = std::env::var("FLAMEROUTER_BASE_URL_GEMINI")
            .or_else(|_| std::env::var("FLAMEROUTER_BASE_URL_GOOGLE_AI_STUDIO"))
            .unwrap_or_else(|_| GEMINI_EMBED_BASE.to_string());
        let model = normalize_model(model);
        let key = encode_key(key);
        return Ok(format!("{base}/{model}:{op}?key={key}"));
    }
    if let Some((_, base)) = OPENAI_COMPAT_EMBEDDING_URLS
        .iter()
        .find(|(id, _)| *id == provider_id)
    {
        // env override for tests/dev (like providers::base_url_for): {base}/embeddings
        let key = format!(
            "FLAMEROUTER_BASE_URL_{}",
            provider_id.to_uppercase().replace('-', "_")
        );
        return match std::env::var(&key) {
            Ok(b) => Ok(format!("{}/embeddings", b.trim_end_matches('/'))),
            Err(_) => Ok(base.to_string()),
        };
    }
    // openai-compatible-* / custom-embedding-* / selfhosted-embedding — base from
    // the credential; selfhosted-embedding has NO fallback by design.
    let raw = credentials
        .get("baseUrl")
        .or_else(|| credentials.get("base_url"))
        .and_then(|v| v.as_str())
        .map(|s| s.trim())
        .filter(|s| !s.is_empty())
        .ok_or_else(|| {
            format!(
                "provider '{}' needs an endpoint: set the credential's baseUrl \
                 (e.g. http://host:8080/v1 — '/embeddings' is appended to it). \
                 Refusing to fall back to api.openai.com, which would send your \
                 input and API key there.",
                provider_id
            )
        })?;
    let trimmed = raw.trim_end_matches('/').trim_end_matches("/embeddings");
    Ok(format!("{}/embeddings", trimmed))
}

/// Parse the optional dimensions field: number, numeric string, '' / non-numeric
/// / <=0 → None (parity with the JS adapter's Number(dimensions) guard).
pub fn parse_dimensions(dimensions: Option<&Value>) -> Option<f64> {
    let Some(d) = dimensions else { return None };
    let n: f64 = match d {
        Value::Number(n) => n.as_f64()?,
        Value::String(s) => s.trim().parse().ok()?,
        _ => return None,
    };
    if n.is_finite() && n > 0.0 {
        Some(n)
    } else {
        None
    }
}

/// Build the upstream request body (OpenAI-compatible shape for every provider
/// except gemini, which uses embedContent / batchEmbedContents).
pub fn build_body(
    provider_id: &str,
    model: &str,
    input: &Value,
    encoding_format: Option<&str>,
    dimensions: Option<&Value>,
) -> Value {
    if matches!(provider_id, "gemini" | "google_ai_studio") {
        return gemini_body(model, input, parse_dimensions(dimensions));
    }
    let mut body = json!({ "model": model, "input": input });
    let obj = body.as_object_mut().unwrap();
    if let Some(ef) = encoding_format.filter(|s| !s.is_empty()) {
        obj.insert("encoding_format".into(), json!(ef));
    }
    if let Some(d) = parse_dimensions(dimensions) {
        obj.insert("dimensions".into(), json!(d));
    }
    body
}

fn gemini_body(model: &str, input: &Value, dimensions: Option<f64>) -> Value {
    let m = format!("models/{}", normalize_model(model));
    let with_dims = |mut r: Value| {
        if let Some(d) = dimensions {
            r.as_object_mut()
                .unwrap()
                .insert("outputDimensionality".into(), json!(d));
        }
        r
    };
    if let Some(items) = input.as_array() {
        json!({
            "requests": items.iter().map(|t| {
                with_dims(json!({
                    "model": m,
                    "content": { "parts": [{ "text": t.as_str().unwrap_or_default() }] },
                }))
            }).collect::<Vec<_>>(),
        })
    } else {
        with_dims(json!({
            "model": m,
            "content": { "parts": [{ "text": input.as_str().unwrap_or_default() }] },
        }))
    }
}

/// Normalize the upstream response into the OpenAI embeddinsg shape.
/// Compat providers pass through (already OpenAI-shaped); gemini maps
/// embedding{.values} / embeddings[] → data[].
pub fn normalize(provider_id: &str, model: &str, body: Value, input_is_array: bool) -> Value {
    if !matches!(provider_id, "gemini" | "google_ai_studio") {
        return body;
    }
    // Gemni responses are struct  {... "embedding": ...} for single, {... "embeddings": [...]} for batch.
    if body.get("object").and_then(|o| o.as_str()) == Some("list") && body.get("data").is_some() {
        return body;
    }
    // Batch: {"embeddings": [{"values": [...]}, ...]} — also handle single-win8 {"embedding.h"}.
    let items: Vec<Value> =
        if input_is_array {
            body.get("embeddings")
                .and_then(|e| e.as_array())
                .map(|arr| {
                    arr.iter()
                    .enumerate()
                    .map(|(idx, emb)| json!({
                        "object": "embedding",
                        "index": idx,
                        "embedding": emb.get("values").cloned().unwrap_or_else(|| json!([])),
                    }))
                    .collect()
                })
                .unwrap_or_default()
        } else if let Some(values) = body.get("embedding").and_then(|e| e.get("values")) {
            vec![json!({ "object": "embedding", "index": 0, "embedding": values })]
        } else {
            vec![]
        };
    json!({
        "object": "list",
        "data": items,
        "model": model,
        "usage": { "prompt_tokens": 0, "total_tokens": 0 },
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn is_supported_list() {
        assert!(is_supported("openai"));
        assert!(is_supported("voyage-ai"));
        assert!(is_supported("jina-ai"));
        assert!(is_supported("vercel-ai-gateway"));
        assert!(is_supported("gemini"));
        assert!(is_supported("google_ai_studio"));
        assert!(is_supported("selfhosted-embedding"));
        assert!(is_supported("openai-compatible-mybox"));
        assert!(is_supported("custom-embedding-xyz"));
        assert!(!is_supported("claude"));
        assert!(!is_supported("amazon-bedrock"));
        assert!(!is_supported("openai-compat")); // prefix must match exactly
    }

    #[test]
    fn url_compat_providers() {
        let creds = json!({ "apiKey": "k" });
        assert_eq!(
            url_for("openai", "text-embedding-3-small", false, &creds).unwrap(),
            "https://api.openai.com/v1/embeddings"
        );
        assert_eq!(
            url_for("jina-ai", "jina-embeddings-v3", false, &creds).unwrap(),
            "https://api.jina.ai/v1/embeddings"
        );
    }

    #[test]
    fn url_gemini() {
        let creds = json!({ "apiKey": "a b/c" });
        assert_eq!(
            url_for("gemini", "gemini-embedding-001", false, &creds).unwrap(),
            "https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:embedContent?key=a%20b%2Fc"
        );
        assert_eq!(
            url_for(
                "google_ai_studio",
                "models/gemini-embedding-001",
                true,
                &creds
            )
            .unwrap(),
            "https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:batchEmbedContents?key=a%20b%2Fc"
        );

        // key from accessToken works as fallback
        let token = json!({ "accessToken": "t" });
        assert!(
            url_for("gemini", "m", false, &token)
                .unwrap()
                .ends_with("?key=t")
        );
        // no key → error
        assert!(url_for("gemini", "m", false, &json!({})).is_err());
    }

    #[test]
    fn url_selfhosted() {
        let creds = json!({ "apiKey": "k", "baseUrl": "http://h:8080/v1/" });
        assert_eq!(
            url_for("selfhosted-embedding", "emb", false, &creds).unwrap(),
            "http://h:8080/v1/embeddings"
        );
        // full embeddings URL pasted in is normalized too
        let full = json!({ "apiKey": "k", "baseUrl": "http://h:8080/v1/embeddings" });
        assert_eq!(
            url_for("selfhosted-embedding", "emb", false, &full).unwrap(),
            "http://h:8080/v1/embeddings"
        );
        // custom node
        assert_eq!(
            url_for(
                "openai-compatible-box",
                "emb",
                false,
                &json!({ "baseUrl": "http://h/v1" })
            )
            .unwrap(),
            "http://h/v1/embeddings"
        );
        // missing baseUrl → error, never OpenAI fallback
        assert!(
            url_for(
                "selfhosted-embedding",
                "emb",
                false,
                &json!({ "apiKey": "k" })
            )
            .is_err()
        );
    }

    #[test]
    fn build_body_openai_shape() {
        let input = json!("hello");
        let b = build_body(
            "openai",
            "text-embedding-3-small",
            &input,
            Some("base64"),
            Some(&json!("256")),
        );
        assert_eq!(b["model"], "text-embedding-3-small");
        assert_eq!(b["input"], "hello");
        assert_eq!(b["encoding_format"], "base64");
        assert_eq!(b["dimensions"].as_f64(), Some(256.0));

        // empty/invalid dimensions are dropped
        let b = build_body("openai", "m", &input, None, Some(&json!("")));
        assert!(b.get("dimensions").is_none());
        let b = build_body("openai", "m", &input, None, Some(&json!("abc")));
        assert!(b.get("dimensions").is_none());
        // no extra keys for arrays either
        let b = build_body("openai", "m", &json!(["a", "b"]), None, None);
        assert_eq!(b["input"].as_array().unwrap().len(), 2);
    }

    #[test]
    fn build_body_gemini() {
        let b = build_body(
            "gemini",
            "models/gemini-embedding-001",
            &json!("hi"),
            None,
            None,
        );
        assert_eq!(b["model"], "models/gemini-embedding-001");
        assert_eq!(b["content"]["parts"][0]["text"], "hi");
        assert!(b.get("requests").is_none());

        let b = build_body(
            "gemini",
            "gemini-embedding-001",
            &json!(["a", "b"]),
            None,
            Some(&json!("128")),
        );
        let reqs = b["requests"].as_array().unwrap();
        assert_eq!(reqs.len(), 2);
        assert_eq!(reqs[0]["model"], "models/gemini-embedding-001");
        assert_eq!(reqs[0]["content"]["parts"][0]["text"], "a");
        assert_eq!(reqs[0]["outputDimensionality"].as_f64(), Some(128.0));
        assert_eq!(reqs[1]["content"]["parts"][0]["text"], "b");
    }

    #[test]
    fn normalize_compat_passthrough() {
        let body = json!({ "object": "list", "data": [{"embedding": [1.0]}], "model": "m" });
        assert_eq!(normalize("openai", "m", body.clone(), true), body);
    }

    #[test]
    fn normalize_gemini() {
        // single embedContent
        let out = normalize(
            "gemini",
            "gemini-embedding-001",
            json!({ "embedding": { "values": [0.1, 0.2] } }),
            false,
        );
        assert_eq!(out["object"], "list");
        assert_eq!(out["data"][0]["index"], 0);
        assert_eq!(out["data"][0]["embedding"], json!([0.1, 0.2]));
        assert_eq!(out["model"], "gemini-embedding-001");
        assert_eq!(out["usage"]["prompt_tokens"], 0);

        // batch
        let out = normalize(
            "gemini",
            "m",
            json!({ "embeddings": [{ "values": [1.0] }, { "values": [2.0] }] }),
            true,
        );
        assert_eq!(out["data"].as_array().unwrap().len(), 2);
        assert_eq!(out["data"][1]["index"], 1);
        assert_eq!(out["data"][1]["embedding"], json!([2.0]));

        // already-openai-shaped body passes through
        let body = json!({ "object": "list", "data": [], "model": "m" });
        assert_eq!(normalize("gemini", "m", body.clone(), true), body);

        // empty batch → empty data
        let out = normalize("gemini", "m", json!({}), false);
        assert_eq!(out["data"].as_array().unwrap().len(), 0);
    }
}
