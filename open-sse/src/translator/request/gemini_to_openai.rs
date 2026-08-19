//! gemini → openai request translator.
//! Port of open-sse/translator/request/gemini-to-openai.js

use serde_json::{Map, Value, json};

use crate::translator::concerns::{adjust_max_tokens, collapse_text_parts, encode_data_uri};
use crate::translator::schema::{
    DEFAULT_MAX_TOKENS, gemini_role as GROLE, openai_block as OB, role as ROLE,
};

pub fn translate(model: &str, body: Value, stream: bool, _credentials: Option<&Value>) -> Value {
    let mut result = Map::new();
    result.insert("model".into(), json!(model));
    result.insert("stream".into(), json!(stream));
    let mut messages: Vec<Value> = Vec::new();

    // Generation config
    if let Some(cfg) = body.get("generationConfig") {
        if let Some(max_out) = cfg.get("maxOutputTokens").and_then(|v| v.as_u64()) {
            let temp = json!({"max_tokens": max_out, "tools": body.get("tools").cloned().unwrap_or(Value::Null)});
            result.insert(
                "max_tokens".into(),
                json!(adjust_max_tokens(&temp, DEFAULT_MAX_TOKENS)),
            );
        }
        if let Some(t) = cfg.get("temperature") {
            result.insert("temperature".into(), t.clone());
        }
        if let Some(tp) = cfg.get("topP") {
            result.insert("top_p".into(), tp.clone());
        }
    }

    // System instruction
    if let Some(si) = body.get("systemInstruction") {
        let text = extract_gemini_text(si);
        if !text.is_empty() {
            messages.push(json!({"role": ROLE::SYSTEM, "content": text}));
        }
    }

    // Contents → messages
    if let Some(contents) = body.get("contents").and_then(|c| c.as_array()) {
        for content in contents {
            if let Some(m) = convert_gemini_content(content) {
                messages.push(m);
            }
        }
    }

    // Tools
    if let Some(tools) = body.get("tools").and_then(|t| t.as_array()) {
        let mut out_tools = Vec::new();
        for tool in tools {
            if let Some(decls) = tool.get("functionDeclarations").and_then(|f| f.as_array()) {
                for func in decls {
                    out_tools.push(json!({
                        "type": OB::FUNCTION,
                        "function": {
                            "name": func.get("name").cloned().unwrap_or(Value::Null),
                            "description": func.get("description").and_then(|d| d.as_str()).unwrap_or(""),
                            "parameters": func.get("parameters").cloned().unwrap_or(json!({"type":"object","properties":{}}))
                        }
                    }));
                }
            }
        }
        if !out_tools.is_empty() {
            result.insert("tools".into(), Value::Array(out_tools));
        }
    }

    result.insert("messages".into(), Value::Array(messages));
    Value::Object(result)
}

fn convert_gemini_content(content: &Value) -> Option<Value> {
    let role = if content.get("role").and_then(|r| r.as_str()) == Some(GROLE::USER) {
        ROLE::USER
    } else {
        ROLE::ASSISTANT
    };

    let parts_arr = content.get("parts").and_then(|p| p.as_array())?;

    let mut parts: Vec<Value> = Vec::new();
    let mut tool_calls: Vec<Value> = Vec::new();

    for part in parts_arr {
        if let Some(text) = part.get("text").and_then(|t| t.as_str()) {
            parts.push(json!({"type": OB::TEXT, "text": text}));
        }

        if let Some(inline) = part.get("inlineData") {
            let mime = inline
                .get("mimeType")
                .and_then(|m| m.as_str())
                .unwrap_or("application/octet-stream");
            let data = inline.get("data").and_then(|d| d.as_str()).unwrap_or("");
            parts.push(json!({
                "type": OB::IMAGE_URL,
                "image_url": {"url": encode_data_uri(mime, data)}
            }));
        }

        if let Some(fc) = part.get("functionCall") {
            let name = fc.get("name").and_then(|n| n.as_str()).unwrap_or("");
            let id = fc
                .get("id")
                .and_then(|i| i.as_str())
                .map(|s| s.to_string())
                .unwrap_or_else(|| format!("call_{}", name));
            let args = fc.get("args").cloned().unwrap_or(json!({}));
            tool_calls.push(json!({
                "id": id,
                "type": OB::FUNCTION,
                "function": {"name": name, "arguments": serde_json::to_string(&args).unwrap_or_else(|_| "{}".into())}
            }));
        }

        if let Some(fr) = part.get("functionResponse") {
            let name = fr.get("name").and_then(|n| n.as_str()).unwrap_or("");
            let id = fr
                .get("id")
                .and_then(|i| i.as_str())
                .map(|s| s.to_string())
                .unwrap_or_else(|| format!("call_{}", name));
            let resp = fr.get("response").cloned().unwrap_or(json!({}));
            let content_str = if let Some(r) = resp.get("result") {
                serde_json::to_string(r).unwrap_or_else(|_| "{}".into())
            } else {
                serde_json::to_string(&resp).unwrap_or_else(|_| "{}".into())
            };
            return Some(json!({
                "role": ROLE::TOOL,
                "tool_call_id": id,
                "content": content_str
            }));
        }
    }

    if !tool_calls.is_empty() {
        let mut msg = Map::new();
        msg.insert("role".into(), json!(ROLE::ASSISTANT));
        if !parts.is_empty() {
            if parts.len() == 1 {
                if let Some(t) = parts[0].get("text") {
                    msg.insert("content".into(), t.clone());
                } else {
                    msg.insert("content".into(), Value::Array(parts.clone()));
                }
            } else {
                msg.insert("content".into(), Value::Array(parts.clone()));
            }
        }
        msg.insert("tool_calls".into(), Value::Array(tool_calls));
        return Some(Value::Object(msg));
    }

    if !parts.is_empty() {
        return Some(json!({"role": role, "content": collapse_text_parts(parts)}));
    }

    None
}

fn extract_gemini_text(content: &Value) -> String {
    match content {
        Value::String(s) => s.clone(),
        Value::Object(_) => content
            .get("parts")
            .and_then(|p| p.as_array())
            .map(|arr| {
                arr.iter()
                    .filter_map(|p| p.get("text").and_then(|t| t.as_str()))
                    .collect::<Vec<_>>()
                    .join("")
            })
            .unwrap_or_default(),
        _ => String::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn basic_user_text() {
        let body = json!({
            "contents": [{"role":"user","parts":[{"text":"hello"}]}]
        });
        let out = translate("gemini-2.0-flash", body, false, None);
        assert_eq!(out["model"], "gemini-2.0-flash");
        assert_eq!(out["messages"][0]["role"], "user");
        assert_eq!(out["messages"][0]["content"], "hello");
    }

    #[test]
    fn system_instruction() {
        let body = json!({
            "systemInstruction": {"parts":[{"text":"be helpful"}]},
            "contents": [{"role":"user","parts":[{"text":"hi"}]}]
        });
        let out = translate("m", body, false, None);
        assert_eq!(out["messages"][0]["role"], "system");
        assert_eq!(out["messages"][0]["content"], "be helpful");
        assert_eq!(out["messages"][1]["role"], "user");
    }

    #[test]
    fn generation_config_maps() {
        let body = json!({
            "generationConfig": {"maxOutputTokens": 100, "temperature": 0.7, "topP": 0.9},
            "contents": [{"role":"user","parts":[{"text":"hi"}]}]
        });
        let out = translate("m", body, false, None);
        assert_eq!(out["max_tokens"], 100);
        assert_eq!(out["temperature"], 0.7);
        assert_eq!(out["top_p"], 0.9);
    }

    #[test]
    fn inline_data_becomes_image_url() {
        let body = json!({
            "contents": [{"role":"user","parts":[
                {"text":"see this"},
                {"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}
            ]}]
        });
        let out = translate("m", body, false, None);
        let content = &out["messages"][0]["content"];
        assert!(content.is_array());
        assert_eq!(content[0]["type"], "text");
        assert_eq!(content[1]["type"], "image_url");
        assert_eq!(
            content[1]["image_url"]["url"],
            "data:image/png;base64,aGVsbG8="
        );
    }

    #[test]
    fn function_call_becomes_tool_calls() {
        let body = json!({
            "contents": [{"role":"model","parts":[
                {"functionCall":{"name":"get_weather","args":{"city":"SF"}}}
            ]}]
        });
        let out = translate("m", body, false, None);
        let msg = &out["messages"][0];
        assert_eq!(msg["role"], "assistant");
        assert_eq!(msg["tool_calls"][0]["id"], "call_get_weather");
        assert_eq!(msg["tool_calls"][0]["function"]["name"], "get_weather");
        assert_eq!(
            msg["tool_calls"][0]["function"]["arguments"],
            "{\"city\":\"SF\"}"
        );
    }

    #[test]
    fn function_response_becomes_tool_message() {
        let body = json!({
            "contents": [{"role":"user","parts":[
                {"functionResponse":{"name":"get_weather","response":{"result":{"temp":72}}}}
            ]}]
        });
        let out = translate("m", body, false, None);
        let msg = &out["messages"][0];
        assert_eq!(msg["role"], "tool");
        assert_eq!(msg["tool_call_id"], "call_get_weather");
        assert_eq!(msg["content"], "{\"temp\":72}");
    }

    #[test]
    fn tools_map_to_openai_format() {
        let body = json!({
            "contents": [{"role":"user","parts":[{"text":"hi"}]}],
            "tools": [{"functionDeclarations":[
                {"name":"search","description":"Search web","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}
            ]}]
        });
        let out = translate("m", body, false, None);
        assert_eq!(out["tools"][0]["type"], "function");
        assert_eq!(out["tools"][0]["function"]["name"], "search");
        assert_eq!(
            out["tools"][0]["function"]["parameters"]["properties"]["q"]["type"],
            "string"
        );
    }

    #[test]
    fn max_tokens_with_tools_bumped() {
        let body = json!({
            "generationConfig": {"maxOutputTokens": 100},
            "contents": [{"role":"user","parts":[{"text":"hi"}]}],
            "tools": [{"functionDeclarations":[{"name":"x","parameters":{}}]}]
        });
        let out = translate("m", body, false, None);
        assert_eq!(out["max_tokens"], 32000);
    }
}
