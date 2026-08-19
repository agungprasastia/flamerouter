//! Message token counting estimator (Anthropic mock).
//! Port of src/app/api/v1/messages/count_tokens/route.js.

use serde_json::Value;

fn count_value_chars(value: &Value) -> usize {
    match value {
        Value::Null => 0,
        Value::String(s) => s.len(),
        Value::Number(n) => n.to_string().len(),
        Value::Bool(b) => {
            if *b {
                4
            } else {
                5
            }
        }
        Value::Array(arr) => arr.iter().map(count_value_chars).sum(),
        Value::Object(map) => map
            .iter()
            .map(|(k, v)| k.len() + count_value_chars(v))
            .sum(),
    }
}

fn count_content_block_chars(block: &Value) -> usize {
    if block.is_null() {
        return 0;
    }
    if let Some(s) = block.as_str() {
        return s.len();
    }
    if !block.is_object() {
        return count_value_chars(block);
    }

    match block.get("type").and_then(Value::as_str).unwrap_or("") {
        "text" => block.get("text").map(count_value_chars).unwrap_or(0),
        "tool_use" => {
            block.get("name").map(count_value_chars).unwrap_or(0)
                + block.get("input").map(count_value_chars).unwrap_or(0)
        }
        "tool_result" => block.get("content").map(count_value_chars).unwrap_or(0),
        "thinking" => block.get("thinking").map(count_value_chars).unwrap_or(0),
        _ => count_value_chars(block),
    }
}

fn count_message_chars(message: &Value) -> usize {
    if !message.is_object() {
        return 0;
    }
    let content = match message.get("content") {
        Some(c) => c,
        None => return 0,
    };

    if let Some(s) = content.as_str() {
        return s.len();
    }
    if let Some(arr) = content.as_array() {
        return arr.iter().map(count_content_block_chars).sum();
    }
    count_value_chars(content)
}

pub fn estimate_anthropic_input_tokens(body: &Value) -> u64 {
    let messages = body.get("messages").and_then(Value::as_array);
    let mut total_chars = body.get("system").map(count_value_chars).unwrap_or(0)
        + body.get("tools").map(count_value_chars).unwrap_or(0);

    if let Some(msgs) = messages {
        for msg in msgs {
            total_chars += count_message_chars(msg);
        }
    }

    ((total_chars as f64) / 4.0).ceil() as u64
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_estimate_empty() {
        let body = json!({});
        assert_eq!(estimate_anthropic_input_tokens(&body), 0);
    }

    #[test]
    fn test_estimate_simple_message() {
        let body = json!({
            "messages": [
                {"role": "user", "content": "hello world"}
            ]
        });
        // "hello world" = 11 chars -> ceil(11/4) = 3
        assert_eq!(estimate_anthropic_input_tokens(&body), 3);
    }

    #[test]
    fn test_estimate_blocks_and_system() {
        let body = json!({
            "system": "you are helpful", // 15 chars
            "messages": [
                {
                    "role": "user",
                    "content": [
                        {"type": "text", "text": "test"} // 4 chars
                    ]
                }
            ]
        });
        // 19 chars -> ceil(19/4) = 5
        assert_eq!(estimate_anthropic_input_tokens(&body), 5);
    }
}
