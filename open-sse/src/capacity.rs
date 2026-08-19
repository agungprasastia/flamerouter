//! Capacity Adapter — global fallback pools of models per input-modality capability (vision / pdf / audioInput / videoInput).
//! Port of open-sse/services/capacityAdapter.js.

use serde_json::Value;

pub const CAPABILITY_KEYS: &[&str] = &["vision", "pdf", "audioInput", "videoInput"];
pub const DEFAULT_FALLBACK_MODEL: &str = "openai/gpt-4o-mini";

/// Inspect request body messages to detect required modalities (e.g. image URLs or audio parts).
pub fn detect_required_capabilities(body: &Value) -> Vec<&'static str> {
    let mut caps = Vec::new();
    let messages = body
        .get("messages")
        .or_else(|| body.get("input"))
        .and_then(Value::as_array);

    if let Some(msgs) = messages {
        for m in msgs {
            if let Some(content) = m.get("content").and_then(Value::as_array) {
                for b in content {
                    let b_type = b.get("type").and_then(Value::as_str).unwrap_or("");
                    if b_type == "image_url" || b_type == "image" {
                        if !caps.contains(&"vision") {
                            caps.push("vision");
                        }
                    } else if b_type == "input_audio" || b_type == "audio" {
                        if !caps.contains(&"audioInput") {
                            caps.push("audioInput");
                        }
                    } else if (b_type == "document" || b_type == "pdf")
                        && !caps.contains(&"pdf") {
                            caps.push("pdf");
                        }
                }
            }
        }
    }

    caps
}

/// Strip older conversation history turns to fit into a fallback model's smaller context window.
pub fn strip_history_for_context(body: &mut Value, max_chars: usize) {
    let messages = if let Some(m) = body.get_mut("messages").and_then(Value::as_array_mut) {
        m
    } else if let Some(i) = body.get_mut("input").and_then(Value::as_array_mut) {
        i
    } else {
        return;
    };

    if messages.len() < 3 {
        return;
    }

    let mut total_len: usize = messages
        .iter()
        .map(|m| {
            m.get("content")
                .and_then(Value::as_str)
                .map(|s| s.len())
                .unwrap_or(100)
        })
        .sum();

    if total_len <= max_chars {
        return;
    }

    // Keep system message (first) and latest user turns (tail), drop middle turns
    let is_first_system = messages
        .first()
        .and_then(|m| m.get("role"))
        .and_then(Value::as_str)
        == Some("system");
    let preserve_head = if is_first_system { 1 } else { 0 };

    while messages.len() > preserve_head + 1 && total_len > max_chars {
        let removed = messages.remove(preserve_head);
        let rem_len = removed
            .get("content")
            .and_then(Value::as_str)
            .map(|s| s.len())
            .unwrap_or(100);
        total_len = total_len.saturating_sub(rem_len);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_detect_required_capabilities_vision() {
        let body = json!({
            "messages": [
                {
                    "role": "user",
                    "content": [
                        {"type": "text", "text": "what is this?"},
                        {"type": "image_url", "image_url": {"url": "https://example.com/a.jpg"}}
                    ]
                }
            ]
        });
        let caps = detect_required_capabilities(&body);
        assert_eq!(caps, vec!["vision"]);
    }

    #[test]
    fn test_strip_history_for_context() {
        let mut body = json!({
            "messages": [
                {"role": "system", "content": "system instruction"},
                {"role": "user", "content": "very long message 1 ".repeat(100)},
                {"role": "assistant", "content": "very long message 2 ".repeat(100)},
                {"role": "user", "content": "latest query"}
            ]
        });
        strip_history_for_context(&mut body, 500);
        let msgs = body["messages"].as_array().unwrap();
        assert!(msgs.len() < 4);
        assert_eq!(msgs[0]["role"], "system");
        assert_eq!(msgs.last().unwrap()["content"], "latest query");
    }
}
