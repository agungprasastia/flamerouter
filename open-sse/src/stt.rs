use serde_json::{Value, json};

pub const STT_BASE_URLS: &[(&str, &str)] = &[
    ("openai", "https://api.openai.com/v1/audio/transcriptions"),
    (
        "groq",
        "https://api.groq.com/openai/v1/audio/transcriptions",
    ),
];

const GEMINI_STT_BASE: &str = "https://generativelanguage.googleapis.com/v1beta/models";

pub fn is_supported(provider_id: &str) -> bool {
    STT_BASE_URLS.iter().any(|(id, _)| *id == provider_id)
        || matches!(
            provider_id,
            "gemini" | "google_ai_studio" | "selfhosted-stt"
        )
}

fn key_from(credentials: &Value) -> Option<&str> {
    credentials
        .get("apiKey")
        .and_then(|v| v.as_str())
        .or_else(|| credentials.get("api_key").and_then(|v| v.as_str()))
        .or_else(|| credentials.get("accessToken").and_then(|v| v.as_str()))
}

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

pub fn url_for(provider_id: &str, model: &str, credentials: &Value) -> Result<String, String> {
    if matches!(provider_id, "gemini" | "google_ai_studio") {
        let key = key_from(credentials)
            .ok_or_else(|| format!("provider '{}' requires an api key", provider_id))?;
        let model_id = model.strip_prefix("models/").unwrap_or(model);
        let base = std::env::var("FLAMEROUTER_BASE_URL_GEMINI")
            .or_else(|_| std::env::var("FLAMEROUTER_BASE_URL_GOOGLE_AI_STUDIO"))
            .unwrap_or_else(|_| GEMINI_STT_BASE.to_string());
        return Ok(format!(
            "{base}/{model_id}:generateContent?key={}",
            encode_key(key)
        ));
    }
    if let Some((_, base)) = STT_BASE_URLS.iter().find(|(id, _)| *id == provider_id) {
        let key = format!(
            "FLAMEROUTER_BASE_URL_{}",
            provider_id.to_uppercase().replace('-', "_")
        );
        return match std::env::var(&key) {
            Ok(b) => Ok(format!("{}/audio/transcriptions", b.trim_end_matches('/'))),
            Err(_) => Ok(base.to_string()),
        };
    }
    let raw = credentials
        .get("baseUrl")
        .or_else(|| credentials.get("base_url"))
        .and_then(|v| v.as_str())
        .map(|s| s.trim())
        .filter(|s| !s.is_empty())
        .ok_or_else(|| format!("provider '{}' needs baseUrl in credential", provider_id))?;
    let trimmed = raw
        .trim_end_matches('/')
        .trim_end_matches("/v1")
        .trim_end_matches('/');
    Ok(format!("{}/v1/audio/transcriptions", trimmed))
}

pub fn gemini_body(_model: &str, audio_base64: &str, mime_type: &str) -> Value {
    json!({
        "contents": [{
            "parts": [
                {"text": "Generate a transcript of the speech. Return only the transcribed text, no commentary."},
                {"inline_data": {"mime_type": mime_type, "data": audio_base64}}
            ]
        }]
    })
}

pub fn normalize_stt(provider_id: &str, body: Value) -> Value {
    if !matches!(provider_id, "gemini" | "google_ai_studio") {
        return body;
    }
    let text = body
        .get("candidates")
        .and_then(|c| c.get(0))
        .and_then(|c| c.get("content"))
        .and_then(|c| c.get("parts"))
        .and_then(|p| p.as_array())
        .map(|parts| {
            parts
                .iter()
                .filter_map(|p| p.get("text").and_then(|t| t.as_str()))
                .collect::<Vec<_>>()
                .join("")
        })
        .unwrap_or_default();
    json!({"text": text})
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn is_supported_list() {
        for id in [
            "openai",
            "groq",
            "gemini",
            "google_ai_studio",
            "selfhosted-stt",
        ] {
            assert!(is_supported(id), "expected {id} supported");
        }
        assert!(!is_supported("claude"));
        assert!(!is_supported("openai-compatible-box"));
    }

    #[test]
    fn url_compat() {
        let creds = json!({"apiKey": "k"});
        let u = url_for("openai", "whisper-1", &creds).unwrap();
        assert_eq!(u, "https://api.openai.com/v1/audio/transcriptions");
        let u = url_for("groq", "whisper-large-v3", &creds).unwrap();
        assert_eq!(u, "https://api.groq.com/openai/v1/audio/transcriptions");
    }

    #[test]
    fn url_gemini() {
        let creds = json!({"apiKey": "a b"});
        let u = url_for("gemini", "gemini-2.0-flash-001", &creds).unwrap();
        assert!(u.contains(":generateContent?key=a%20b"));
        assert!(u.contains("gemini-2.0-flash-001"));
        assert!(url_for("gemini", "m", &json!({})).is_err());
    }

    #[test]
    fn url_selfhosted() {
        let creds = json!({"apiKey": "k", "baseUrl": "http://h:8080/v1/"});
        let u = url_for("selfhosted-stt", "m", &creds).unwrap();
        assert_eq!(u, "http://h:8080/v1/audio/transcriptions");
        assert!(url_for("selfhosted-stt", "m", &json!({"apiKey": "k"})).is_err());
    }

    #[test]
    fn gemini_body_contains_audio() {
        let b = gemini_body("gemini-2.0-flash-001", "AAAA", "audio/wav");
        assert_eq!(b["contents"][0]["parts"][1]["inline_data"]["data"], "AAAA");
        assert_eq!(
            b["contents"][0]["parts"][1]["inline_data"]["mime_type"],
            "audio/wav"
        );
    }

    #[test]
    fn normalize_compat_passthrough() {
        let body = json!({"text": "hello"});
        assert_eq!(normalize_stt("openai", body.clone()), body);
    }

    #[test]
    fn normalize_gemini() {
        let body = json!({
            "candidates": [{
                "content": {
                    "parts": [{"text": "hello world"}]
                }
            }]
        });
        let out = normalize_stt("gemini", body);
        assert_eq!(out["text"], "hello world");
        let out = normalize_stt("gemini", json!({}));
        assert_eq!(out["text"], "");
    }
}
