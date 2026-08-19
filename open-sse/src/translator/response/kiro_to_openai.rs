//! Kiro / AWS CodeWhisperer response translator (Kiro -> OpenAI SSE chunks).

use serde_json::{Value, json};

pub fn translate(chunk: Value, state: &mut Value) -> Option<Vec<Value>> {
    if chunk.get("object").and_then(Value::as_str) == Some("chat.completion.chunk") {
        return Some(vec![chunk]);
    }

    if state.get("id").is_none() {
        state["id"] = json!(format!(
            "chatcmpl-kiro-{}",
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_millis()
        ));
    }
    let id = state["id"].as_str().unwrap_or("chatcmpl-kiro");

    let text = chunk
        .pointer("/assistantResponseEvent/content")
        .or_else(|| chunk.get("content"))
        .or_else(|| chunk.get("text"))
        .and_then(Value::as_str);

    if let Some(t) = text
        && !t.is_empty() {
            return Some(vec![json!({
                "id": id,
                "object": "chat.completion.chunk",
                "choices": [{
                    "index": 0,
                    "delta": { "content": t },
                    "finish_reason": null
                }]
            })]);
        }

    None
}
