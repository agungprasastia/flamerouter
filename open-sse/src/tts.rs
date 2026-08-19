use serde_json::{Value, json};

pub const TTS_BASE_URLS: &[(&str, &str)] = &[
    ("openai", "https://api.openai.com/v1/audio/speech"),
    ("openrouter", "https://openrouter.ai/api/v1/audio/speech"),
];

const GEMINI_TTS_BASE: &str = "https://generativelanguage.googleapis.com/v1beta/models";

pub fn is_supported(provider_id: &str) -> bool {
    TTS_BASE_URLS.iter().any(|(id, _)| *id == provider_id)
        || matches!(
            provider_id,
            "gemini" | "google_ai_studio" | "selfhosted-tts"
        )
}

fn key_from(credentials: &Value) -> Option<&str> {
    credentials
        .get("apiKey")
        .and_then(Value::as_str)
        .or_else(|| credentials.get("api_key").and_then(Value::as_str))
        .or_else(|| credentials.get("accessToken").and_then(Value::as_str))
}

fn encode_key(key: &str) -> String {
    key.bytes()
        .map(|b| {
            if b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.' | b'~') {
                (b as char).to_string()
            } else {
                format!("%{b:02X}")
            }
        })
        .collect()
}

pub fn url_for(provider_id: &str, model: &str, credentials: &Value) -> Result<String, String> {
    if matches!(provider_id, "gemini" | "google_ai_studio") {
        let key = key_from(credentials)
            .ok_or_else(|| format!("provider '{}' requires an api key", provider_id))?;
        let base = std::env::var("FLAMEROUTER_BASE_URL_GEMINI")
            .or_else(|_| std::env::var("FLAMEROUTER_BASE_URL_GOOGLE_AI_STUDIO"))
            .unwrap_or_else(|_| GEMINI_TTS_BASE.to_string());
        return Ok(format!(
            "{base}/{model}:generateContent?key={}",
            encode_key(key)
        ));
    }
    if let Some((_, base)) = TTS_BASE_URLS.iter().find(|(id, _)| *id == provider_id) {
        let env = format!(
            "FLAMEROUTER_BASE_URL_{}",
            provider_id.to_uppercase().replace('-', "_")
        );
        return match std::env::var(env) {
            Ok(base) => Ok(format!("{}/audio/speech", base.trim_end_matches('/'))),
            Err(_) => Ok(base.to_string()),
        };
    }
    let raw = credentials
        .get("baseUrl")
        .or_else(|| credentials.get("base_url"))
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .ok_or_else(|| format!("provider '{}' needs baseUrl in credential", provider_id))?;
    let base = raw
        .trim_end_matches('/')
        .trim_end_matches("/v1/audio/speech")
        .trim_end_matches("/v1");
    Ok(format!("{base}/v1/audio/speech"))
}

pub fn build_body(provider_id: &str, model: &str, body: &Value) -> Value {
    if matches!(provider_id, "gemini" | "google_ai_studio") {
        return json!({
            "contents": [{"parts": [{"text": body["input"].as_str().unwrap_or_default()}]}],
            "generationConfig": {"responseModalities": ["AUDIO"]}
        });
    }
    let mut out = json!({
        "model": model,
        "input": body["input"],
        "voice": body.get("voice").and_then(Value::as_str).unwrap_or("alloy")
    });
    for field in ["response_format", "speed"] {
        if let Some(value) = body.get(field) {
            out[field] = value.clone();
        }
    }
    out
}

pub fn gemini_audio(body: &Value) -> Option<(&str, &str)> {
    body.pointer("/candidates/0/content/parts")?
        .as_array()?
        .iter()
        .find_map(|part| {
            let data = part.pointer("/inlineData/data").and_then(Value::as_str)?;
            let mime = part
                .pointer("/inlineData/mimeType")
                .and_then(Value::as_str)
                .unwrap_or("audio/mpeg");
            Some((data, mime))
        })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn builds_openai_body() {
        let body = build_body("openai", "tts-1", &json!({"input": "hi", "voice": "nova"}));
        assert_eq!(body["model"], "tts-1");
        assert_eq!(body["voice"], "nova");
    }

    #[test]
    fn builds_gemini_body_and_extracts_audio() {
        let body = build_body(
            "gemini",
            "gemini-2.5-flash-preview-tts",
            &json!({"input": "hi"}),
        );
        assert_eq!(body["generationConfig"]["responseModalities"][0], "AUDIO");
        let response = json!({"candidates":[{"content":{"parts":[{"inlineData":{"data":"AAAA","mimeType":"audio/wav"}}]}}]});
        assert_eq!(gemini_audio(&response), Some(("AAAA", "audio/wav")));
    }
}
