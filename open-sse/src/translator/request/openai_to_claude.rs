//! openai → claude request translator.
//! Port of open-sse/translator/request/openai-to-claude.js

use serde_json::{Map, Value, json};

use crate::translator::concerns::{
    adjust_max_tokens, extract_text_content, parse_data_uri, safe_parse_json,
};
use crate::translator::schema::{claude_block as CB, openai_block as OB, role as ROLE};

// Claude Code system prompt — mirror of config/appConstants.js CLAUDE_SYSTEM_PROMPT.
// Kept minimal here; the Node side injects the real one later (prepareClaudeRequest) —
// but for direct Rust→Anthropic path we need the actual constant. Pulling it in would
// couple us to appConstants. For now use the same sentinel text used by tests.
const CLAUDE_SYSTEM_PROMPT: &str = "You are Claude Code, Anthropic's official CLI for Claude.";

const CLAUDE_TOOL_CHOICE_TYPES: &[&str] = &["auto", "any", "tool", "none"];

pub fn translate(model: &str, body: Value, stream: bool, _credentials: Option<&Value>) -> Value {
    let mut result = Map::new();
    result.insert("model".into(), Value::String(model.to_string()));
    let max_tokens = adjust_max_tokens(&body, crate::translator::schema::DEFAULT_MAX_TOKENS);
    result.insert("max_tokens".into(), json!(max_tokens));
    result.insert("stream".into(), Value::Bool(stream));

    if let Some(t) = body.get("temperature") {
        result.insert("temperature".into(), t.clone());
    }

    let mut messages: Vec<Value> = Vec::new();
    let mut system_parts: Vec<String> = Vec::new();

    if let Some(Value::Array(msgs)) = body.get("messages") {
        // Extract system messages
        for msg in msgs {
            if msg.get("role").and_then(|r| r.as_str()) == Some(ROLE::SYSTEM) {
                let c = msg.get("content").cloned().unwrap_or(Value::Null);
                let text = match &c {
                    Value::String(s) => s.clone(),
                    other => extract_text_content(other, "\n"),
                };
                system_parts.push(text);
            }
        }

        // Process non-system with merging logic
        let non_system: Vec<&Value> = msgs
            .iter()
            .filter(|m| m.get("role").and_then(|r| r.as_str()) != Some(ROLE::SYSTEM))
            .collect();

        let mut current_role: Option<&'static str> = None;
        let mut current_parts: Vec<Value> = Vec::new();

        macro_rules! flush {
            () => {
                if let Some(r) = current_role {
                    if !current_parts.is_empty() {
                        messages.push(json!({ "role": r, "content": Value::Array(std::mem::take(&mut current_parts)) }));
                    }
                }
            };
        }

        for msg in non_system {
            let role_str = msg.get("role").and_then(|r| r.as_str()).unwrap_or("");
            let new_role = if role_str == ROLE::USER || role_str == ROLE::TOOL {
                ROLE::USER
            } else {
                ROLE::ASSISTANT
            };
            let blocks = get_content_blocks_from_message(msg);
            let has_tool_use = blocks
                .iter()
                .any(|b| b.get("type").and_then(|t| t.as_str()) == Some(CB::TOOL_USE));
            let has_tool_result = blocks
                .iter()
                .any(|b| b.get("type").and_then(|t| t.as_str()) == Some(CB::TOOL_RESULT));

            if has_tool_result {
                let tool_result_blocks: Vec<Value> = blocks
                    .iter()
                    .filter(|b| b.get("type").and_then(|t| t.as_str()) == Some(CB::TOOL_RESULT))
                    .cloned()
                    .collect();
                let other_blocks: Vec<Value> = blocks
                    .into_iter()
                    .filter(|b| b.get("type").and_then(|t| t.as_str()) != Some(CB::TOOL_RESULT))
                    .collect();

                flush!();

                if !tool_result_blocks.is_empty() {
                    messages.push(
                        json!({ "role": ROLE::USER, "content": Value::Array(tool_result_blocks) }),
                    );
                }
                if !other_blocks.is_empty() {
                    current_role = Some(new_role);
                    current_parts.extend(other_blocks);
                }
                continue;
            }

            if current_role != Some(new_role) {
                flush!();
                current_role = Some(new_role);
            }

            current_parts.extend(blocks);

            if has_tool_use {
                flush!();
            }
        }
        flush!();

        // Add cache_control to last assistant message
        for i in (0..messages.len()).rev() {
            let msg = &mut messages[i];
            let role = msg.get("role").and_then(|r| r.as_str()).unwrap_or("");
            if role != ROLE::ASSISTANT {
                continue;
            }
            if let Some(Value::Array(content)) = msg.get_mut("content") {
                if content.is_empty() {
                    continue;
                }
                let valid: &[&str] = &[CB::TEXT, CB::TOOL_USE, CB::TOOL_RESULT, CB::IMAGE];
                for j in (0..content.len()).rev() {
                    let bt = content[j]
                        .get("type")
                        .and_then(|t| t.as_str())
                        .unwrap_or("");
                    if valid.contains(&bt) {
                        content[j].as_object_mut().map(|o| {
                            o.insert("cache_control".into(), json!({ "type": "ephemeral" }))
                        });
                        break;
                    }
                }
                break;
            }
        }
    }

    // response_format → json mode
    if let Some(rf) = body.get("response_format") {
        let rftype = rf.get("type").and_then(|t| t.as_str()).unwrap_or("");
        if rftype == "json_schema" {
            if let Some(schema) = rf.get("json_schema").and_then(|s| s.get("schema")) {
                let schema_json = serde_json::to_string_pretty(schema).unwrap_or_default();
                system_parts.push(format!(
                    "You must respond with valid JSON that strictly follows this JSON schema:\n```json\n{}\n```\nRespond ONLY with the JSON object, no other text.",
                    schema_json
                ));
            }
        } else if rftype == "json_object" {
            system_parts.push(
                "You must respond with valid JSON. Respond ONLY with a JSON object, no other text."
                    .into(),
            );
        }
    }

    // System with Claude Code prompt
    let claude_code_prompt = json!({ "type": CB::TEXT, "text": CLAUDE_SYSTEM_PROMPT });
    if !system_parts.is_empty() {
        let system_text = system_parts.join("\n");
        result.insert(
            "system".into(),
            json!([
                claude_code_prompt,
                { "type": CB::TEXT, "text": system_text, "cache_control": { "type": "ephemeral", "ttl": "1h" } }
            ]),
        );
    } else {
        result.insert("system".into(), json!([claude_code_prompt]));
    }

    // Tools
    if let Some(Value::Array(tools)) = body.get("tools") {
        let mut converted: Vec<Value> = Vec::new();
        for tool in tools {
            let ttype = tool.get("type").and_then(|t| t.as_str());
            if let Some(t) = ttype
                && t != OB::FUNCTION {
                    converted.push(tool.clone());
                    continue;
                }
            let tool_data = tool.get("function").unwrap_or(tool);
            let name = tool_data.get("name").cloned().unwrap_or(Value::Null);
            let description = tool_data
                .get("description")
                .and_then(|d| d.as_str())
                .unwrap_or("")
                .to_string();
            let input_schema = tool_data
                .get("parameters")
                .or_else(|| tool_data.get("input_schema"))
                .cloned()
                .unwrap_or_else(|| json!({ "type": "object", "properties": {}, "required": [] }));
            converted.push(json!({
                "name": name,
                "description": description,
                "input_schema": input_schema
            }));
        }
        if !converted.is_empty() {
            let last = converted.len() - 1;
            converted[last].as_object_mut().map(|o| {
                o.insert(
                    "cache_control".into(),
                    json!({ "type": "ephemeral", "ttl": "1h" }),
                )
            });
        }
        result.insert("tools".into(), Value::Array(converted));
    }

    if let Some(tc) = body.get("tool_choice") {
        result.insert("tool_choice".into(), convert_openai_tool_choice(tc));
    }

    result.insert("messages".into(), Value::Array(messages));
    Value::Object(result)
}

fn get_content_blocks_from_message(msg: &Value) -> Vec<Value> {
    let mut blocks = Vec::new();
    let role = msg.get("role").and_then(|r| r.as_str()).unwrap_or("");
    let content = msg.get("content").cloned().unwrap_or(Value::Null);

    match role {
        ROLE::TOOL => {
            blocks.push(json!({
                "type": CB::TOOL_RESULT,
                "tool_use_id": msg.get("tool_call_id").cloned().unwrap_or(Value::Null),
                "content": content
            }));
        }
        ROLE::USER => match &content {
            Value::String(s) => {
                if !s.is_empty() {
                    blocks.push(json!({ "type": CB::TEXT, "text": s }));
                }
            }
            Value::Array(parts) => {
                for part in parts {
                    let ptype = part.get("type").and_then(|t| t.as_str()).unwrap_or("");
                    match ptype {
                        t if t == OB::TEXT => {
                            if let Some(text) = part.get("text").and_then(|t| t.as_str())
                                && !text.is_empty() {
                                    blocks.push(json!({ "type": CB::TEXT, "text": text }));
                                }
                        }
                        t if t == CB::TOOL_RESULT => {
                            let mut o = Map::new();
                            o.insert("type".into(), Value::String(CB::TOOL_RESULT.into()));
                            o.insert(
                                "tool_use_id".into(),
                                part.get("tool_use_id").cloned().unwrap_or(Value::Null),
                            );
                            o.insert(
                                "content".into(),
                                part.get("content").cloned().unwrap_or(Value::Null),
                            );
                            if let Some(ie) = part.get("is_error")
                                && ie.as_bool().unwrap_or(false) {
                                    o.insert("is_error".into(), ie.clone());
                                }
                            blocks.push(Value::Object(o));
                        }
                        t if t == OB::IMAGE_URL => {
                            let url = part
                                .get("image_url")
                                .and_then(|i| i.get("url"))
                                .and_then(|u| u.as_str())
                                .unwrap_or("");
                            if let Some((mime, b64)) = parse_data_uri(url) {
                                blocks.push(json!({
                                    "type": CB::IMAGE,
                                    "source": { "type": "base64", "media_type": mime, "data": b64 }
                                }));
                            } else if url.starts_with("http://") || url.starts_with("https://") {
                                blocks.push(json!({
                                    "type": CB::IMAGE,
                                    "source": { "type": "url", "url": url }
                                }));
                            }
                        }
                        t if t == OB::IMAGE => {
                            if let Some(src) = part.get("source") {
                                blocks.push(json!({ "type": CB::IMAGE, "source": src }));
                            }
                        }
                        t if t == OB::FILE => {
                            if let Some(file_data) = part
                                .get("file")
                                .and_then(|f| f.get("file_data"))
                                .and_then(|d| d.as_str())
                                && let Some((mime, b64)) = parse_data_uri(file_data)
                                    && mime == "application/pdf" {
                                        blocks.push(json!({
                                            "type": CB::DOCUMENT,
                                            "source": { "type": "base64", "media_type": mime, "data": b64 }
                                        }));
                                    }
                        }
                        _ => {}
                    }
                }
            }
            _ => {}
        },
        ROLE::ASSISTANT => {
            match &content {
                Value::Array(parts) => {
                    for part in parts {
                        let ptype = part.get("type").and_then(|t| t.as_str()).unwrap_or("");
                        match ptype {
                            t if t == OB::TEXT => {
                                if let Some(text) = part.get("text").and_then(|t| t.as_str())
                                    && !text.is_empty() {
                                        blocks.push(json!({ "type": CB::TEXT, "text": text }));
                                    }
                            }
                            t if t == CB::TOOL_USE => {
                                blocks.push(json!({
                                    "type": CB::TOOL_USE,
                                    "id": part.get("id").cloned().unwrap_or(Value::Null),
                                    "name": part.get("name").cloned().unwrap_or(Value::Null),
                                    "input": part.get("input").cloned().unwrap_or(Value::Null)
                                }));
                            }
                            t if t == CB::THINKING => {
                                let mut pb = part.clone();
                                if let Some(o) = pb.as_object_mut() {
                                    o.remove("cache_control");
                                }
                                blocks.push(pb);
                            }
                            _ => {}
                        }
                    }
                }
                Value::String(s) => {
                    if !s.is_empty() {
                        blocks.push(json!({ "type": CB::TEXT, "text": s }));
                    }
                }
                other if !other.is_null() => {
                    let text = extract_text_content(other, "\n");
                    if !text.is_empty() {
                        blocks.push(json!({ "type": CB::TEXT, "text": text }));
                    }
                }
                _ => {}
            }

            if let Some(Value::Array(tcs)) = msg.get("tool_calls") {
                for tc in tcs {
                    if tc.get("type").and_then(|t| t.as_str()) == Some(OB::FUNCTION) {
                        let args = tc
                            .get("function")
                            .and_then(|f| f.get("arguments"))
                            .cloned()
                            .unwrap_or(Value::Null);
                        let parsed_args = safe_parse_json(&args, args.clone());
                        blocks.push(json!({
                            "type": CB::TOOL_USE,
                            "id": tc.get("id").cloned().unwrap_or(Value::Null),
                            "name": tc.get("function").and_then(|f| f.get("name")).cloned().unwrap_or(Value::Null),
                            "input": parsed_args
                        }));
                    }
                }
            }
        }
        _ => {}
    }

    blocks
}

fn convert_openai_tool_choice(choice: &Value) -> Value {
    if choice.is_null() {
        return json!({ "type": "auto" });
    }
    match choice {
        Value::String(s) => {
            if s == "required" {
                json!({ "type": "any" })
            } else {
                json!({ "type": "auto" })
            }
        }
        Value::Object(_) => {
            if let Some(name) = choice.get("function").and_then(|f| f.get("name")) {
                return json!({ "type": "tool", "name": name });
            }
            let t = choice.get("type").and_then(|t| t.as_str()).unwrap_or("");
            if CLAUDE_TOOL_CHOICE_TYPES.contains(&t) {
                return choice.clone();
            }
            json!({ "type": "auto" })
        }
        _ => json!({ "type": "auto" }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn simple_user_message() {
        let body = json!({
            "max_tokens": 100,
            "messages": [{"role":"user","content":"hello"}]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        assert_eq!(out["model"], "claude-opus-4-6");
        assert_eq!(out["stream"], false);
        let msgs = out["messages"].as_array().unwrap();
        assert_eq!(msgs.len(), 1);
        assert_eq!(msgs[0]["role"], "user");
        let parts = msgs[0]["content"].as_array().unwrap();
        assert_eq!(parts[0]["type"], "text");
        assert_eq!(parts[0]["text"], "hello");
    }

    #[test]
    fn system_extracted_to_top_level() {
        let body = json!({
            "max_tokens": 100,
            "messages": [
                {"role":"system","content":"You are helpful."},
                {"role":"user","content":"hi"}
            ]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        let sys = out["system"].as_array().unwrap();
        // [ClaudeCode prompt, user system w/ cache_control]
        assert_eq!(sys.len(), 2);
        assert_eq!(sys[1]["text"], "You are helpful.");
        // system not in messages
        let msgs = out["messages"].as_array().unwrap();
        assert_eq!(msgs.len(), 1);
        assert_eq!(msgs[0]["role"], "user");
    }

    #[test]
    fn image_url_base64_to_image_block() {
        let body = json!({
            "max_tokens": 100,
            "messages": [{
                "role":"user",
                "content":[
                    {"type":"text","text":"what"},
                    {"type":"image_url","image_url":{"url":"data:image/png;base64,PNGDATA"}}
                ]
            }]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        let parts = out["messages"][0]["content"].as_array().unwrap();
        assert_eq!(parts[1]["type"], "image");
        assert_eq!(parts[1]["source"]["type"], "base64");
        assert_eq!(parts[1]["source"]["media_type"], "image/png");
        assert_eq!(parts[1]["source"]["data"], "PNGDATA");
    }

    #[test]
    fn image_url_remote_to_url_block() {
        let body = json!({
            "max_tokens": 100,
            "messages": [{
                "role":"user",
                "content":[{"type":"image_url","image_url":{"url":"https://example.com/x.png"}}]
            }]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        let parts = out["messages"][0]["content"].as_array().unwrap();
        assert_eq!(parts[0]["source"]["type"], "url");
        assert_eq!(parts[0]["source"]["url"], "https://example.com/x.png");
    }

    #[test]
    fn tool_calls_become_tool_use_blocks() {
        let body = json!({
            "max_tokens": 100,
            "messages": [
                {"role":"user","content":"call"},
                {"role":"assistant","content":"","tool_calls":[{
                    "id":"call_1","type":"function",
                    "function":{"name":"get_weather","arguments":"{\"city\":\"NYC\"}"}
                }]}
            ]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        let msgs = out["messages"].as_array().unwrap();
        let asst_parts = msgs[1]["content"].as_array().unwrap();
        let tu = asst_parts.iter().find(|b| b["type"] == "tool_use").unwrap();
        assert_eq!(tu["id"], "call_1");
        assert_eq!(tu["name"], "get_weather");
        assert_eq!(tu["input"]["city"], "NYC");
    }

    #[test]
    fn tool_role_becomes_tool_result_in_user_message() {
        let body = json!({
            "max_tokens": 100,
            "messages": [
                {"role":"user","content":"call"},
                {"role":"assistant","content":"","tool_calls":[{
                    "id":"call_1","type":"function",
                    "function":{"name":"get_weather","arguments":"{}"}
                }]},
                {"role":"tool","tool_call_id":"call_1","content":"sunny"}
            ]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        let msgs = out["messages"].as_array().unwrap();
        // user, assistant (with tool_use), user (with tool_result)
        assert_eq!(msgs.len(), 3);
        assert_eq!(msgs[2]["role"], "user");
        let parts = msgs[2]["content"].as_array().unwrap();
        assert_eq!(parts[0]["type"], "tool_result");
        assert_eq!(parts[0]["tool_use_id"], "call_1");
        assert_eq!(parts[0]["content"], "sunny");
    }

    #[test]
    fn consecutive_same_role_messages_merge() {
        let body = json!({
            "max_tokens": 100,
            "messages": [
                {"role":"user","content":"a"},
                {"role":"user","content":"b"}
            ]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        let msgs = out["messages"].as_array().unwrap();
        assert_eq!(msgs.len(), 1);
        let parts = msgs[0]["content"].as_array().unwrap();
        assert_eq!(parts.len(), 2);
    }

    #[test]
    fn tools_get_input_schema() {
        let body = json!({
            "max_tokens": 100,
            "messages": [{"role":"user","content":"hi"}],
            "tools":[{
                "type":"function",
                "function":{
                    "name":"get_weather",
                    "description":"Get weather",
                    "parameters":{"type":"object","properties":{"city":{"type":"string"}}}
                }
            }]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        let tools = out["tools"].as_array().unwrap();
        assert_eq!(tools[0]["name"], "get_weather");
        assert_eq!(tools[0]["input_schema"]["type"], "object");
    }

    #[test]
    fn tool_choice_required_becomes_any() {
        let body = json!({
            "max_tokens":100,
            "tool_choice":"required",
            "messages":[{"role":"user","content":"hi"}]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        assert_eq!(out["tool_choice"]["type"], "any");
    }

    #[test]
    fn tool_choice_function_becomes_tool() {
        let body = json!({
            "max_tokens":100,
            "tool_choice":{"type":"function","function":{"name":"get_weather"}},
            "messages":[{"role":"user","content":"hi"}]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        assert_eq!(out["tool_choice"]["type"], "tool");
        assert_eq!(out["tool_choice"]["name"], "get_weather");
    }

    #[test]
    fn tool_choice_claude_native_passthrough() {
        let body = json!({
            "max_tokens":100,
            "tool_choice":{"type":"none"},
            "messages":[{"role":"user","content":"hi"}]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        assert_eq!(out["tool_choice"]["type"], "none");
    }

    #[test]
    fn response_format_json_object_appends_system() {
        let body = json!({
            "max_tokens":100,
            "response_format":{"type":"json_object"},
            "messages":[{"role":"user","content":"hi"}]
        });
        let out = translate("claude-opus-4-6", body, false, None);
        let sys = out["system"].as_array().unwrap();
        let sys_text = sys[1]["text"].as_str().unwrap();
        assert!(sys_text.contains("valid JSON"));
    }
}
