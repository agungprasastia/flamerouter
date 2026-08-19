//! Kiro / AWS CodeWhisperer request translator (OpenAI -> Kiro generateAssistantResponse).

use serde_json::{Value, json};

pub fn translate(model: &str, body: Value, _stream: bool, credentials: Option<&Value>) -> Value {
    let messages = body
        .get("messages")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();

    let mut history = Vec::new();
    let mut last_user_message = String::new();

    for m in &messages {
        let role = m.get("role").and_then(Value::as_str).unwrap_or("user");
        let content = m.get("content").and_then(Value::as_str).unwrap_or("");

        if role == "user" {
            last_user_message = content.to_string();
            history.push(json!({
                "userInputMessage": {
                    "content": content
                }
            }));
        } else if role == "assistant" {
            history.push(json!({
                "assistantResponseMessage": {
                    "content": content
                }
            }));
        }
    }

    let profile_arn = credentials
        .and_then(|c| c.get("profileArn"))
        .or_else(|| credentials.and_then(|c| c.get("profile_arn")))
        .and_then(Value::as_str);

    let mut out = json!({
        "conversationState": {
            "conversationId": format!("conv-{}", std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_millis()),
            "currentMessage": {
                "userInputMessage": {
                    "content": last_user_message,
                    "modelId": model
                }
            },
            "history": history
        }
    });

    if let Some(arn) = profile_arn {
        out["profileArn"] = json!(arn);
    }

    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_kiro_request_translation() {
        let body = json!({
            "model": "claude-3-5-sonnet",
            "messages": [
                {"role": "user", "content": "hello kiro"}
            ]
        });
        let res = translate("claude-3-5-sonnet", body, true, None);
        assert_eq!(
            res.pointer("/conversationState/currentMessage/userInputMessage/content")
                .unwrap(),
            "hello kiro"
        );
    }
}
