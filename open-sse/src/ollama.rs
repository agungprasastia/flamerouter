//! Ollama API compatibility helpers (`/api/chat`).
//! Transforms OpenAI chat SSE stream or non-stream responses into Ollama NDJSON shape.

use bytes::Bytes;
use serde_json::{Value, json};

pub fn format_ollama_chunk(model: &str, content: &str) -> Bytes {
    let line = json!({
        "model": model,
        "message": {
            "role": "assistant",
            "content": content
        },
        "done": false
    });
    Bytes::from(format!("{}\n", line))
}

pub fn format_ollama_done(model: &str, tool_calls: Option<Vec<Value>>) -> Bytes {
    let mut message = json!({
        "role": "assistant",
        "content": ""
    });
    if let Some(tcs) = tool_calls {
        message["tool_calls"] = Value::Array(tcs);
    }
    let line = json!({
        "model": model,
        "message": message,
        "done": true
    });
    Bytes::from(format!("{}\n", line))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ollama_formats() {
        let chunk = format_ollama_chunk("llama3.2", "hello");
        let parsed: Value = serde_json::from_slice(&chunk).unwrap();
        assert_eq!(parsed["model"], "llama3.2");
        assert_eq!(parsed["done"], false);

        let done = format_ollama_done("llama3.2", None);
        let parsed_done: Value = serde_json::from_slice(&done).unwrap();
        assert_eq!(parsed_done["done"], true);
    }
}
