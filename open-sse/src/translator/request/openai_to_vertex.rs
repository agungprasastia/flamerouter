//! Vertex AI / Antigravity request translator (OpenAI -> Google Vertex generateContent).

use serde_json::Value;

pub fn translate(model: &str, body: Value, stream: bool, _credentials: Option<&Value>) -> Value {
    // Translates OpenAI chat body to Google Cloud Vertex / Antigravity shape
    let mut gemini_body =
        crate::translator::request::openai_to_gemini::translate(model, body, stream, _credentials);

    // Antigravity wraps the contents in a specific request wrapper if needed
    if let Some(obj) = gemini_body.as_object_mut() {
        // Strip blacklisted thinking / unsupported fields for Google endpoints
        obj.remove("output_config");
        obj.remove("thinking");
        obj.remove("reasoning_effort");
        obj.remove("reasoning");
        obj.remove("enable_thinking");
        obj.remove("thinking_budget");
    }

    gemini_body
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_vertex_strips_blacklisted() {
        let body = json!({
            "model": "gemini-2.5-pro",
            "messages": [{"role": "user", "content": "hi"}],
            "reasoning_effort": "high",
            "thinking": {"budget_tokens": 1000}
        });
        let res = translate("gemini-2.5-pro", body, false, None);
        assert!(res.get("reasoning_effort").is_none());
        assert!(res.get("thinking").is_none());
    }
}
