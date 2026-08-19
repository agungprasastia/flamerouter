//! Shared translator concerns — port of open-sse/translator/concerns/

use serde_json::{Value, json};

/// encodeDataUri(mime, base64) → data:mime;base64,<payload>
pub fn encode_data_uri(mime_type: &str, base64: &str) -> String {
    format!("data:{};base64,{}", mime_type, base64)
}

/// parseDataUri(url) → Option<(mime, base64)>
pub fn parse_data_uri(url: &str) -> Option<(String, String)> {
    let rest = url.strip_prefix("data:")?;
    let (mime, payload) = rest.split_once(";base64,")?;
    Some((mime.to_string(), payload.to_string()))
}

/// safeParseJSON(str, fallback) — non-string passthrough; parse error → fallback.
pub fn safe_parse_json(v: &Value, fallback: Value) -> Value {
    match v {
        Value::String(s) => serde_json::from_str(s).unwrap_or(fallback),
        other => other.clone(),
    }
}

/// collapseTextParts(parts) — lone text part becomes plain string, else array as-is.
pub fn collapse_text_parts(parts: Vec<Value>) -> Value {
    if parts.len() == 1
        && parts[0].get("type").and_then(|t| t.as_str())
            == Some(crate::translator::schema::openai_block::TEXT)
    {
        return parts
            .into_iter()
            .next()
            .unwrap()
            .get("text")
            .cloned()
            .unwrap_or(Value::Null);
    }
    Value::Array(parts)
}

/// adjustMaxTokens(body, ceiling) — mirror formats/maxTokens.js
pub fn adjust_max_tokens(body: &Value, ceiling: u64) -> u64 {
    let mut max_tokens = body
        .get("max_tokens")
        .and_then(|v| v.as_u64())
        .unwrap_or(crate::translator::schema::DEFAULT_MAX_TOKENS);

    let has_tools = body
        .get("tools")
        .and_then(|t| t.as_array())
        .map(|a| !a.is_empty())
        .unwrap_or(false);
    if has_tools && max_tokens < crate::translator::schema::DEFAULT_MIN_TOKENS {
        max_tokens = crate::translator::schema::DEFAULT_MIN_TOKENS;
    }

    if let Some(budget) = body
        .get("thinking")
        .and_then(|t| t.get("budget_tokens"))
        .and_then(|b| b.as_u64())
        && max_tokens <= budget {
            max_tokens = budget + 1024;
        }

    if max_tokens > ceiling {
        max_tokens = ceiling;
    }
    max_tokens
}

/// extractTextContent(content, joiner) — flatten content parts (array or string) to text.
/// Mirror of open-sse/translator/formats/gemini.js extractTextContent
pub fn extract_text_content(content: &Value, joiner: &str) -> String {
    match content {
        Value::String(s) => s.clone(),
        Value::Array(arr) => arr
            .iter()
            .filter_map(|p| {
                if p.get("type").and_then(|t| t.as_str()) == Some("text") {
                    p.get("text")
                        .and_then(|t| t.as_str())
                        .map(|s| s.to_string())
                } else {
                    p.get("text")
                        .and_then(|t| t.as_str())
                        .map(|s| s.to_string())
                }
            })
            .collect::<Vec<_>>()
            .join(joiner),
        _ => String::new(),
    }
}

/// Helper: build json object inline.
#[macro_export]
macro_rules! jobj {
    ($($k:expr => $v:expr),* $(,)?) => {
        {
            let mut m = serde_json::Map::new();
            $( m.insert($k.to_string(), serde_json::json!($v)); )*
            serde_json::Value::Object(m)
        }
    };
}

pub use crate::jobj;

pub fn json_text(s: impl Into<String>) -> Value {
    json!({ "type": "text", "text": s.into() })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn data_uri_roundtrip() {
        let uri = encode_data_uri("image/png", "aGVsbG8=");
        assert_eq!(uri, "data:image/png;base64,aGVsbG8=");
        let (m, b) = parse_data_uri(&uri).unwrap();
        assert_eq!(m, "image/png");
        assert_eq!(b, "aGVsbG8=");
        assert!(parse_data_uri("https://example.com/x.png").is_none());
    }

    #[test]
    fn collapse_text() {
        let v = collapse_text_parts(vec![json!({"type":"text","text":"hi"})]);
        assert_eq!(v, json!("hi"));
        let v = collapse_text_parts(vec![
            json!({"type":"text","text":"a"}),
            json!({"type":"text","text":"b"}),
        ]);
        assert!(v.is_array());
    }

    #[test]
    fn max_tokens_rules() {
        // default
        assert_eq!(adjust_max_tokens(&json!({}), 64000), 64000);
        // explicit
        assert_eq!(adjust_max_tokens(&json!({"max_tokens": 100}), 64000), 100);
        // tools bump min
        assert_eq!(
            adjust_max_tokens(&json!({"max_tokens": 100, "tools": [{}]}), 64000),
            32000
        );
        // ceiling cap
        assert_eq!(
            adjust_max_tokens(&json!({"max_tokens": 99999}), 64000),
            64000
        );
        // thinking budget bump
        assert_eq!(
            adjust_max_tokens(
                &json!({"max_tokens": 1000, "thinking": {"budget_tokens": 5000}}),
                64000
            ),
            6024
        );
    }
}
