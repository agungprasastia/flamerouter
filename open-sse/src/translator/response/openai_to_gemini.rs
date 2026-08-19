//! openai SSE chunk → gemini SSE JSON structure.

use serde_json::{Value, json};

pub fn translate(chunk: Value, _state: &mut Value) -> Option<Vec<Value>> {
    let choices = chunk.get("choices")?.as_array()?;
    if choices.is_empty() {
        return None;
    }

    let choice = &choices[0];
    let delta = choice.get("delta")?;

    let mut parts = Vec::new();

    if let Some(text) = delta.get("content").and_then(|c| c.as_str())
        && !text.is_empty() {
            parts.push(json!({ "text": text }));
        }

    if let Some(tcs) = delta.get("tool_calls").and_then(|t| t.as_array()) {
        for tc in tcs {
            if let Some(func) = tc.get("function") {
                let name = func.get("name").and_then(|n| n.as_str()).unwrap_or("");
                let args_str = func
                    .get("arguments")
                    .and_then(|a| a.as_str())
                    .unwrap_or("{}");
                let args: Value = serde_json::from_str(args_str).unwrap_or(json!({}));
                parts.push(json!({
                    "functionCall": {
                        "name": name,
                        "args": args
                    }
                }));
            }
        }
    }

    if parts.is_empty() && choice.get("finish_reason").is_none() {
        return None;
    }

    let finish_reason = match choice.get("finish_reason").and_then(|f| f.as_str()) {
        Some("stop") => Some("STOP"),
        Some("length") => Some("MAX_TOKENS"),
        Some("tool_calls") => Some("STOP"),
        _ => None,
    };

    let mut candidate = json!({
        "content": {
            "role": "model",
            "parts": parts
        }
    });

    if let Some(fr) = finish_reason {
        candidate["finishReason"] = json!(fr);
    }

    Some(vec![json!({
        "candidates": [candidate]
    })])
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_text_delta() {
        let chunk = json!({
            "choices": [{
                "delta": { "content": "Hello" }
            }]
        });
        let mut state = json!({});
        let res = translate(chunk, &mut state).unwrap();
        assert_eq!(res.len(), 1);
        assert_eq!(
            res[0]["candidates"][0]["content"]["parts"][0]["text"],
            "Hello"
        );
        assert_eq!(res[0]["candidates"][0]["content"]["role"], "model");
    }

    #[test]
    fn test_finish_reason() {
        let chunk = json!({
            "choices": [{
                "delta": {},
                "finish_reason": "stop"
            }]
        });
        let mut state = json!({});
        let res = translate(chunk, &mut state).unwrap();
        assert_eq!(res[0]["candidates"][0]["finishReason"], "STOP");
    }
}
