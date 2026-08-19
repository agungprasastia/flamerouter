// Images endpoint support (parity with open-sse/handlers/imageProviders).
// OpenAI-compatible providers call their dedicated images/generations URL (which
// differs from the chat base URL); gemini uses generateContent with
// responseModalities TEXT+IMAGE (nano-banana). Per-provider quirks: xai accepts
// only a whitelisted field set; openrouter ships static marketing headers.
//
// Custom-protocol providers (codex SSE, fal-ai polling, stability multipart,
// sdwebui/comfyui/huggingface/nanobanana/runwayml/cloudflare-ai/black-forest-labs,
// antigravity) are NOT implemented here — get_image rejects them the same way
// open-sse rejects unknown providers.
use serde_json::{Value, json};

pub const OPENAI_COMPAT_IMAGE_URLS: &[(&str, &str)] = &[
    ("openai", "https://api.openai.com/v1/images/generations"),
    ("minimax", "https://api.minimaxi.com/v1/images/generations"),
    (
        "openrouter",
        "https://openrouter.ai/api/v1/images/generations",
    ),
    (
        "recraft",
        "https://external.api.recraft.ai/v1/images/generations",
    ),
    ("xai", "https://api.x.ai/v1/images/generations"),
    (
        "vercel-ai-gateway",
        "https://ai-gateway.vercel.sh/v1/images/generations",
    ),
];

/// Static extra request headers, per provider (parity with registry imageConfig.headers).
pub const EXTRA_IMAGE_HEADERS: &[(&str, &[(&str, &str)])] = &[(
    "openrouter",
    &[
        ("HTTP-Referer", "https://endpoint-proxy.local"),
        ("X-Title", "Endpoint Proxy"),
    ],
)];

const GEMINI_IMAGE_BASE: &str = "https://generativelanguage.googleapis.com/v1beta/models";

pub fn is_supported(provider_id: &str) -> bool {
    OPENAI_COMPAT_IMAGE_URLS
        .iter()
        .any(|(id, _)| *id == provider_id)
        || matches!(provider_id, "gemini" | "google_ai_studio")
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

/// Resolve the upstream image-generation URL. Err = configuration error
/// surfaced as a 400 by the caller.
pub fn url_for(provider_id: &str, model: &str, credentials: &Value) -> Result<String, String> {
    if matches!(provider_id, "gemini" | "google_ai_studio") {
        let key = key_from(credentials).ok_or_else(|| {
            format!(
                "provider '{}' requires an api key in the credential",
                provider_id
            )
        })?;
        let model_id = model
            .strip_prefix("models/")
            .map(|m| m.to_string())
            .unwrap_or_else(|| model.to_string());
        // env override for tests/dev, like providers::base_url_for (base ends in "/models")
        let base = std::env::var("FLAMEROUTER_BASE_URL_GEMINI")
            .or_else(|_| std::env::var("FLAMEROUTER_BASE_URL_GOOGLE_AI_STUDIO"))
            .unwrap_or_else(|_| GEMINI_IMAGE_BASE.to_string());
        return Ok(format!(
            "{base}/{model_id}:generateContent?key={}",
            encode_key(key)
        ));
    }
    let (_, base) = OPENAI_COMPAT_IMAGE_URLS
        .iter()
        .find(|(id, _)| *id == provider_id)
        .ok_or_else(|| {
            format!(
                "Provider '{}' does not support image generation",
                provider_id
            )
        })?;
    // env override for tests/dev (like providers::base_url_for): {base}/images/generations
    let key = format!(
        "FLAMEROUTER_BASE_URL_{}",
        provider_id.to_uppercase().replace('-', "_")
    );
    match std::env::var(&key) {
        Ok(b) => Ok(format!("{}/images/generations", b.trim_end_matches('/'))),
        Err(_) => Ok(base.to_string()),
    }
}

/// Build the upstream request body (parity with the openai adapter's buildBody).
/// Defaults: n = 1, size = "1024x1024". xai only accepts model/prompt/n/response_format.
pub fn build_body(provider_id: &str, model: &str, body: &Value) -> Value {
    if matches!(provider_id, "gemini" | "google_ai_studio") {
        return json!({
            "contents": [{ "parts": [{ "text": body["prompt"].as_str().unwrap_or_default() }] }],
            "generationConfig": { "responseModalities": ["TEXT", "IMAGE"] },
        });
    }
    let prompt = body["prompt"].as_str().unwrap_or_default();
    let n = body.get("n").cloned().unwrap_or_else(|| json!(1));
    let size = body
        .get("size")
        .and_then(|s| s.as_str())
        .unwrap_or("1024x1024");
    let mut full = json!({ "model": model, "prompt": prompt, "n": n, "size": size });
    for field in ["quality", "style", "response_format"] {
        if let Some(v) = body.get(field) {
            full[field] = v.clone();
        }
    }
    if provider_id == "xai" {
        // bodyFields whitelist: xAI rejects unknown fields
        let mut whitelisted = json!({});
        for f in ["model", "prompt", "n", "response_format"] {
            if let Some(v) = full.get(f) {
                whitelisted[f] = v.clone();
            }
        }
        return whitelisted;
    }
    full
}

/// Normalize the upstream response into the OpenAI images shape.
/// Compat providers pass through (already OpenAI-shaped); gemini maps inlineData
/// parts → data[].b64_json.
pub fn normalize(provider_id: &str, body: Value, prompt: &str, input_model: &str) -> Value {
    let _ = input_model;
    if !matches!(provider_id, "gemini" | "google_ai_studio") {
        return body;
    }
    let parts = body
        .get("candidates")
        .and_then(|c| c.get(0))
        .and_then(|c| c.get("content"))
        .and_then(|c| c.get("parts"))
        .and_then(|p| p.as_array())
        .cloned()
        .unwrap_or_default();
    let images: Vec<Value> = parts
        .iter()
        .filter_map(|p| p.get("inlineData").and_then(|d| d.get("data")))
        .map(|d| json!({ "b64_json": d }))
        .collect();
    json!({
        "created": std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0),
        "data": if images.is_empty() {
            vec![json!({ "b64_json": "", "revised_prompt": prompt })]
        } else {
            images
        },
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn is_supported_list() {
        for id in [
            "openai",
            "minimax",
            "openrouter",
            "recraft",
            "xai",
            "vercel-ai-gateway",
            "gemini",
            "google_ai_studio",
        ] {
            assert!(is_supported(id), "expected {id} supported");
        }
        assert!(!is_supported("claude"));
        assert!(!is_supported("fal-ai"));
        assert!(!is_supported("codex"));
    }

    #[test]
    fn url_compat() {
        let creds = json!({ "apiKey": "k" });
        assert_eq!(
            url_for("openai", "gpt-image-1", &creds).unwrap(),
            "https://api.openai.com/v1/images/generations"
        );
        assert_eq!(
            url_for("xai", "grok-2-image", &creds).unwrap(),
            "https://api.x.ai/v1/images/generations"
        );
    }

    #[test]
    fn url_gemini() {
        let creds = json!({ "apiKey": "a b" });
        assert_eq!(
            url_for("gemini", "nano-banana", &creds).unwrap(),
            "https://generativelanguage.googleapis.com/v1beta/models/nano-banana:generateContent?key=a%20b"
        );
        // models/ prefix is stripped once
        assert_eq!(
            url_for("gemini", "models/nano-banana", &creds).unwrap(),
            "https://generativelanguage.googleapis.com/v1beta/models/nano-banana:generateContent?key=a%20b"
        );
        assert!(url_for("gemini", "m", &json!({})).is_err());
        assert!(url_for("claude", "m", &creds).is_err());
    }

    #[test]
    fn build_body_defaults_and_passthrough() {
        let body = json!({ "prompt": "a cat" });
        let b = build_body("openai", "gpt-image-1", &body);
        assert_eq!(b["model"], "gpt-image-1");
        assert_eq!(b["prompt"], "a cat");
        assert_eq!(b["n"], 1);
        assert_eq!(b["size"], "1024x1024");

        let body = json!({ "prompt": "a cat", "n": 2, "size": "512x512", "quality": "hd", "style": "vivid", "response_format": "b64_json" });
        let b = build_body("recraft", "recraft-v3", &body);
        assert_eq!(b["n"], 2);
        assert_eq!(b["size"], "512x512");
        assert_eq!(b["quality"], "hd");
        assert_eq!(b["style"], "vivid");
        assert_eq!(b["response_format"], "b64_json");
    }

    #[test]
    fn build_body_xai_whitelist() {
        let body =
            json!({ "prompt": "x", "n": 3, "size": "1024x1024", "response_format": "b64_json" });
        let b = build_body("xai", "grok-2-image", &body);
        assert_eq!(b["n"], 3);
        assert_eq!(b["response_format"], "b64_json");
        assert!(b.get("size").is_none(), "xai must not receive size: {b}");
    }

    #[test]
    fn build_body_gemini() {
        let b = build_body("gemini", "nano-banana", &json!({ "prompt": "a cat" }));
        assert_eq!(b["contents"][0]["parts"][0]["text"], "a cat");
        assert_eq!(
            b["generationConfig"]["responseModalities"],
            json!(["TEXT", "IMAGE"])
        );
    }

    #[test]
    fn normalize_compat_passthrough() {
        let body = json!({ "created": 1, "data": [{ "url": "https://x/i.png" }] });
        assert_eq!(normalize("openai", body.clone(), "p", "m"), body);
    }

    #[test]
    fn normalize_gemini() {
        let out = normalize(
            "gemini",
            json!({
                "candidates": [{
                    "content": {
                        "parts": [
                            { "inlineData": { "data": "AAAA" } },
                            { "inlineData": { "data": "BBBB" } }
                        ]
                    }
                }]
            }),
            "a cat",
            "nano-banana",
        );
        assert!(out["created"].is_u64());
        assert_eq!(out["data"][0]["b64_json"], "AAAA");
        assert_eq!(out["data"][1]["b64_json"], "BBBB");

        // no images → fallback placeholder entry with revised_prompt
        let out = normalize(
            "gemini",
            json!({ "candidates": [] }),
            "a cat",
            "nano-banana",
        );
        assert_eq!(out["data"][0]["b64_json"], "");
        assert_eq!(out["data"][0]["revised_prompt"], "a cat");
    }
}
