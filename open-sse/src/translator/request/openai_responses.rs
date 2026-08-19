//! openai-responses ↔ openai request translator.
//! Port of open-sse/translator/request/openai-responses.js

use serde_json::{Map, Value, json};

use crate::translator::schema::{openai_block as OB, role as ROLE};

const MAX_CALL_ID_LEN: usize = 64;

mod responses_item {
    pub const MESSAGE: &str = "message";
    pub const FUNCTION_CALL: &str = "function_call";
    pub const FUNCTION_CALL_OUTPUT: &str = "function_call_output";
    pub const CUSTOM_TOOL_CALL: &str = "custom_tool_call";
    pub const CUSTOM_TOOL_CALL_OUTPUT: &str = "custom_tool_call_output";
    pub const ADDITIONAL_TOOLS: &str = "additional_tools";
    pub const REASONING: &str = "reasoning";
    pub const OUTPUT_TEXT: &str = "output_text";
    pub const INPUT_TEXT: &str = "input_text";
    pub const INPUT_IMAGE: &str = "input_image";
    pub const SUMMARY_TEXT: &str = "summary_text";
}
use responses_item as RI;

fn clamp_call_id(id: &str) -> String {
    if id.len() > MAX_CALL_ID_LEN {
        id.chars().take(MAX_CALL_ID_LEN).collect()
    } else {
        id.to_string()
    }
}

fn normalize_input(input: &Value) -> Option<Vec<Value>> {
    match input {
        Value::String(s) => {
            let text = if s.trim().is_empty() {
                "..."
            } else {
                s.as_str()
            };
            Some(vec![json!({
                "type": RI::MESSAGE,
                "role": ROLE::USER,
                "content": [{"type": RI::INPUT_TEXT, "text": text}]
            })])
        }
        Value::Array(arr) => {
            if arr.is_empty() {
                Some(vec![json!({
                    "type": RI::MESSAGE,
                    "role": ROLE::USER,
                    "content": [{"type": RI::INPUT_TEXT, "text": "..."}]
                })])
            } else {
                Some(arr.clone())
            }
        }
        _ => None,
    }
}

fn normalize_tool_parameters(params: &Value) -> Value {
    if params.is_null() {
        return json!({"type":"object","properties":{}});
    }
    if params.get("type").and_then(|t| t.as_str()) == Some("object")
        && params.get("properties").is_none()
    {
        let mut p = params.clone();
        p["properties"] = json!({});
        return p;
    }
    params.clone()
}

fn extract_reasoning_text(item: &Value) -> String {
    if let Some(arr) = item.get("summary").and_then(|s| s.as_array()) {
        let txt = arr
            .iter()
            .filter_map(|s| s.get("text").and_then(|t| t.as_str()))
            .collect::<Vec<_>>()
            .join("\n");
        if !txt.is_empty() {
            return txt;
        }
    }
    if let Some(arr) = item.get("content").and_then(|c| c.as_array()) {
        let txt = arr
            .iter()
            .filter_map(|c| c.get("text").and_then(|t| t.as_str()))
            .collect::<Vec<_>>()
            .join("\n");
        if !txt.is_empty() {
            return txt;
        }
    }
    String::new()
}

/// openai-responses → openai chat completions
pub fn translate(_model: &str, body: Value, _stream: bool, _credentials: Option<&Value>) -> Value {
    let Some(input) = body.get("input") else {
        return body;
    };

    let mut result = body.clone();
    let mut messages: Vec<Value> = Vec::new();

    if let Some(instr) = body.get("instructions").and_then(|i| i.as_str()) {
        messages.push(json!({"role": ROLE::SYSTEM, "content": instr}));
    }

    let mut current_assistant: Option<Map<String, Value>> = None;
    let mut pending_reasoning = String::new();
    let mut pending_reasoning_encrypted = String::new();
    let mut additional_tools: Vec<Value> = Vec::new();
    let mut custom_tool_names: Vec<String> = Vec::new();

    let attach_reasoning =
        |msg: &mut Map<String, Value>, reasoning: &mut String, encrypted: &mut String| {
            if !reasoning.is_empty() {
                msg.insert("reasoning_content".into(), json!(reasoning.clone()));
                reasoning.clear();
            }
            if !encrypted.is_empty() {
                msg.insert("encrypted_content".into(), json!(encrypted.clone()));
                encrypted.clear();
            }
        };

    let Some(items) = normalize_input(input) else {
        return body;
    };

    for item in items {
        let item_type =
            item.get("type")
                .and_then(|t| t.as_str())
                .or(if item.get("role").is_some() {
                    Some(RI::MESSAGE)
                } else {
                    None
                });

        match item_type {
            Some(t) if t == RI::MESSAGE => {
                if let Some(am) = current_assistant.take() {
                    messages.push(Value::Object(am));
                }
                let role = item
                    .get("role")
                    .and_then(|r| r.as_str())
                    .unwrap_or(ROLE::USER);
                let content = if let Some(arr) = item.get("content").and_then(|c| c.as_array()) {
                    Value::Array(arr.iter().map(|c| {
                        let ty = c.get("type").and_then(|t| t.as_str()).unwrap_or("");
                        match ty {
                            t if t == RI::INPUT_TEXT || t == RI::OUTPUT_TEXT => {
                                json!({"type": OB::TEXT, "text": c.get("text").cloned().unwrap_or(Value::Null)})
                            }
                            t if t == RI::INPUT_IMAGE => {
                                let url = c.get("image_url").cloned()
                                    .or_else(|| c.get("file_id").cloned())
                                    .unwrap_or(json!(""));
                                json!({"type": OB::IMAGE_URL, "image_url": {"url": url, "detail": c.get("detail").and_then(|d| d.as_str()).unwrap_or("auto")}})
                            }
                            _ => c.clone(),
                        }
                    }).collect())
                } else {
                    item.get("content").cloned().unwrap_or(Value::Null)
                };
                let mut msg = Map::new();
                msg.insert("role".into(), json!(role));
                msg.insert("content".into(), content);
                if role == ROLE::ASSISTANT {
                    attach_reasoning(
                        &mut msg,
                        &mut pending_reasoning,
                        &mut pending_reasoning_encrypted,
                    );
                } else {
                    pending_reasoning.clear();
                    pending_reasoning_encrypted.clear();
                }
                messages.push(Value::Object(msg));
            }
            Some(t) if t == RI::FUNCTION_CALL || t == RI::CUSTOM_TOOL_CALL => {
                if current_assistant.is_none() {
                    let mut m = Map::new();
                    m.insert("role".into(), json!(ROLE::ASSISTANT));
                    m.insert("content".into(), Value::Null);
                    m.insert("tool_calls".into(), json!([]));
                    attach_reasoning(
                        &mut m,
                        &mut pending_reasoning,
                        &mut pending_reasoning_encrypted,
                    );
                    current_assistant = Some(m);
                }
                let name = item.get("name").and_then(|n| n.as_str()).unwrap_or("");
                if name.is_empty() {
                    continue;
                }
                if t == RI::CUSTOM_TOOL_CALL {
                    custom_tool_names.push(name.to_string());
                }
                let tool_input = if t == RI::CUSTOM_TOOL_CALL {
                    let inp = item.get("input").cloned().unwrap_or(Value::Null);
                    if inp.is_string() {
                        inp
                    } else {
                        json!(serde_json::to_string(&inp).unwrap_or_default())
                    }
                } else {
                    item.get("arguments").cloned().unwrap_or(json!("{}"))
                };
                let args_str = if tool_input.is_string() {
                    tool_input.as_str().unwrap_or("{}").to_string()
                } else {
                    serde_json::to_string(&tool_input).unwrap_or_else(|_| "{}".into())
                };
                let call_id = item.get("call_id").and_then(|c| c.as_str()).unwrap_or("");
                if let Some(am) = current_assistant.as_mut()
                    && let Some(tcs) = am.get_mut("tool_calls").and_then(|t| t.as_array_mut()) {
                        tcs.push(json!({
                            "id": call_id,
                            "type": OB::FUNCTION,
                            "function": {"name": name, "arguments": args_str}
                        }));
                    }
            }
            Some(t) if t == RI::FUNCTION_CALL_OUTPUT || t == RI::CUSTOM_TOOL_CALL_OUTPUT => {
                if let Some(am) = current_assistant.take() {
                    messages.push(Value::Object(am));
                }
                let output = item.get("output").cloned().unwrap_or(Value::Null);
                let content = if output.is_string() {
                    output.as_str().unwrap_or("").to_string()
                } else {
                    serde_json::to_string(&output).unwrap_or_default()
                };
                let call_id = item.get("call_id").and_then(|c| c.as_str()).unwrap_or("");
                messages.push(json!({
                    "role": ROLE::TOOL,
                    "tool_call_id": call_id,
                    "content": content
                }));
            }
            Some(t) if t == RI::ADDITIONAL_TOOLS => {
                if let Some(tools) = item.get("tools").and_then(|t| t.as_array()) {
                    additional_tools.extend(tools.iter().cloned());
                }
            }
            Some(t) if t == RI::REASONING => {
                let txt = extract_reasoning_text(&item);
                if !txt.is_empty() {
                    if !pending_reasoning.is_empty() {
                        pending_reasoning.push('\n');
                    }
                    pending_reasoning.push_str(&txt);
                }
                if let Some(enc) = item.get("encrypted_content").and_then(|e| e.as_str())
                    && !enc.is_empty() {
                        pending_reasoning_encrypted = enc.to_string();
                    }
            }
            _ => {}
        }
    }

    if let Some(am) = current_assistant.take() {
        messages.push(Value::Object(am));
    }

    // Convert tools
    let mut response_tools: Vec<Value> = Vec::new();
    if let Some(tools) = body.get("tools").and_then(|t| t.as_array()) {
        response_tools.extend(tools.iter().cloned());
    }
    response_tools.extend(additional_tools);

    if !response_tools.is_empty() {
        let converted: Vec<Value> = response_tools.iter().filter_map(|tool| {
            if tool.get("function").is_some() {
                return Some(tool.clone());
            }
            let name = tool.get("name").and_then(|n| n.as_str())?;
            if name.trim().is_empty() {
                return None;
            }
            if tool.get("type").and_then(|t| t.as_str()) == Some("custom") {
                custom_tool_names.push(name.to_string());
                let format_hint = [
                    tool.get("format").and_then(|f| f.get("syntax")).and_then(|s| s.as_str()),
                    tool.get("format").and_then(|f| f.get("definition")).and_then(|d| d.as_str()),
                ].iter().filter_map(|s| *s).collect::<Vec<_>>().join("\n");
                let desc = [tool.get("description").and_then(|d| d.as_str()).unwrap_or(""), format_hint.as_str()]
                    .iter().filter(|s| !s.is_empty()).map(|s| s.to_string()).collect::<Vec<_>>().join("\n\n");
                return Some(json!({
                    "type": OB::FUNCTION,
                    "function": {
                        "name": name,
                        "description": desc,
                        "parameters": {
                            "type": "object",
                            "properties": {"input": {"type": "string", "description": "Raw freeform input for this custom tool"}},
                            "required": ["input"],
                            "additionalProperties": false
                        }
                    }
                }));
            }
            Some(json!({
                "type": OB::FUNCTION,
                "function": {
                    "name": name,
                    "description": tool.get("description").and_then(|d| d.as_str()).unwrap_or(""),
                    "parameters": normalize_tool_parameters(tool.get("parameters").unwrap_or(&Value::Null)),
                    "strict": tool.get("strict").cloned().unwrap_or(Value::Null)
                }
            }))
        }).collect();
        result["tools"] = Value::Array(converted);
    }

    if !custom_tool_names.is_empty() {
        custom_tool_names.sort();
        custom_tool_names.dedup();
        result["_customToolNames"] = json!(custom_tool_names);
    }

    // Map max_output_tokens → max_tokens
    if let Some(mot) = result.get("max_output_tokens").cloned() {
        if result.get("max_tokens").is_none() {
            result["max_tokens"] = mot;
        }
        result
            .as_object_mut()
            .map(|o| o.remove("max_output_tokens"));
    }

    // Cleanup responses-only fields
    let reasoning_effort = result
        .get("reasoning")
        .and_then(|r| r.get("effort"))
        .and_then(|e| e.as_str())
        .map(|s| s.to_string());
    let obj = result.as_object_mut().unwrap();
    obj.remove("input");
    obj.remove("instructions");
    obj.remove("include");
    obj.remove("prompt_cache_key");
    obj.remove("store");
    if let Some(effort) = reasoning_effort {
        obj.insert("reasoning_effort".into(), json!(effort));
    }
    obj.remove("reasoning");
    obj.remove("client_metadata");

    result["messages"] = Value::Array(messages);
    result
}

/// Build reasoning input item from chat assistant message
fn build_reasoning_input_item(msg: &Value) -> Option<Value> {
    let encrypted = msg
        .get("encrypted_content")
        .and_then(|e| e.as_str())
        .or_else(|| {
            msg.get("reasoning_encrypted_content")
                .and_then(|e| e.as_str())
        })
        .or_else(|| {
            msg.get("reasoning")
                .and_then(|r| r.get("encrypted_content"))
                .and_then(|e| e.as_str())
        })
        .unwrap_or("");

    let mut summary_text = String::new();
    if let Some(rc) = msg.get("reasoning_content").and_then(|r| r.as_str())
        && !rc.trim().is_empty() {
            summary_text = rc.to_string();
        }
    if summary_text.is_empty()
        && let Some(r) = msg.get("reasoning").and_then(|r| r.as_str())
            && !r.trim().is_empty() {
                summary_text = r.to_string();
            }
    if summary_text.is_empty()
        && let Some(details) = msg.get("reasoning_details").and_then(|r| r.as_array()) {
            summary_text = details
                .iter()
                .filter_map(|d| {
                    d.get("text")
                        .and_then(|t| t.as_str())
                        .or_else(|| d.get("content").and_then(|c| c.as_str()))
                })
                .collect::<Vec<_>>()
                .join("\n");
        }

    if encrypted.is_empty() && summary_text.is_empty() {
        return None;
    }

    let mut item = Map::new();
    item.insert("type".into(), json!(RI::REASONING));
    if !summary_text.is_empty() {
        item.insert(
            "summary".into(),
            json!([{"type": RI::SUMMARY_TEXT, "text": summary_text}]),
        );
    }
    if !encrypted.is_empty() {
        item.insert("encrypted_content".into(), json!(encrypted));
    }
    Some(Value::Object(item))
}

/// openai chat completions → openai-responses
pub fn translate_to_responses(
    model: &str,
    body: Value,
    _stream: bool,
    _credentials: Option<&Value>,
) -> Value {
    // Already in Responses API format
    if body.get("input").is_some() {
        let mut b = body;
        b["model"] = json!(model);
        b["stream"] = json!(true);
        return b;
    }

    let mut result = Map::new();
    result.insert("model".into(), json!(model));
    result.insert("stream".into(), json!(true));
    result.insert("store".into(), json!(false));
    let mut input: Vec<Value> = Vec::new();

    let mut has_system = false;
    if let Some(msgs) = body.get("messages").and_then(|m| m.as_array()) {
        for msg in msgs {
            let role = msg.get("role").and_then(|r| r.as_str()).unwrap_or("");
            let content = msg.get("content").cloned().unwrap_or(Value::Null);

            if role == ROLE::SYSTEM || role == ROLE::DEVELOPER {
                if !has_system {
                    let text = if content.is_string() {
                        content.as_str().unwrap_or("").to_string()
                    } else {
                        String::new()
                    };
                    result.insert("instructions".into(), json!(text));
                    has_system = true;
                }
                continue;
            }

            if role == ROLE::USER || role == ROLE::ASSISTANT {
                if role == ROLE::ASSISTANT
                    && let Some(ri) = build_reasoning_input_item(msg) {
                        input.push(ri);
                    }
                let content_type = if role == ROLE::USER {
                    RI::INPUT_TEXT
                } else {
                    RI::OUTPUT_TEXT
                };
                let parts: Vec<Value> = if content.is_string() {
                    vec![json!({"type": content_type, "text": content})]
                } else if let Some(arr) = content.as_array() {
                    arr.iter().map(|c| {
                        let ty = c.get("type").and_then(|t| t.as_str()).unwrap_or("");
                        match ty {
                            t if t == OB::TEXT => json!({"type": content_type, "text": c.get("text").cloned().unwrap_or(Value::Null)}),
                            t if t == OB::IMAGE_URL => {
                                let url = c.get("image_url").and_then(|i| {
                                    if i.is_string() { i.as_str().map(|s| s.to_string()) }
                                    else { i.get("url").and_then(|u| u.as_str()).map(|s| s.to_string()) }
                                }).unwrap_or_default();
                                json!({"type": RI::INPUT_IMAGE, "image_url": url, "detail": c.get("image_url").and_then(|i| i.get("detail")).and_then(|d| d.as_str()).unwrap_or("auto")})
                            }
                            t if t == RI::INPUT_IMAGE => c.clone(),
                            _ => {
                                let text = c.get("text").or_else(|| c.get("content")).cloned().unwrap_or_else(|| json!(serde_json::to_string(c).unwrap_or_default()));
                                json!({"type": content_type, "text": text})
                            }
                        }
                    }).collect()
                } else {
                    Vec::new()
                };
                if !parts.is_empty() {
                    input.push(json!({
                        "type": RI::MESSAGE,
                        "role": role,
                        "content": parts
                    }));
                }
            }

            if role == ROLE::ASSISTANT
                && let Some(tcs) = msg.get("tool_calls").and_then(|t| t.as_array()) {
                    for tc in tcs {
                        let id = tc.get("id").and_then(|i| i.as_str()).unwrap_or("");
                        let name = tc
                            .get("function")
                            .and_then(|f| f.get("name"))
                            .and_then(|n| n.as_str())
                            .unwrap_or("_unknown");
                        let args = tc
                            .get("function")
                            .and_then(|f| f.get("arguments"))
                            .and_then(|a| a.as_str())
                            .unwrap_or("{}");
                        input.push(json!({
                            "type": RI::FUNCTION_CALL,
                            "call_id": clamp_call_id(id),
                            "name": name,
                            "arguments": args
                        }));
                    }
                }

            if role == ROLE::TOOL {
                let output = if content.is_string() {
                    content.as_str().unwrap_or("").to_string()
                } else if let Some(arr) = content.as_array() {
                    arr.iter()
                        .map(|c| {
                            c.get("text")
                                .and_then(|t| t.as_str())
                                .map(|s| s.to_string())
                                .unwrap_or_else(|| serde_json::to_string(c).unwrap_or_default())
                        })
                        .collect::<Vec<_>>()
                        .join("")
                } else {
                    serde_json::to_string(&content).unwrap_or_default()
                };
                let call_id = msg
                    .get("tool_call_id")
                    .and_then(|c| c.as_str())
                    .unwrap_or("");
                input.push(json!({
                    "type": RI::FUNCTION_CALL_OUTPUT,
                    "call_id": clamp_call_id(call_id),
                    "output": output
                }));
            }
        }
    }

    if !has_system {
        result.insert("instructions".into(), json!(""));
    }

    // Convert tools
    if let Some(tools) = body.get("tools").and_then(|t| t.as_array()) {
        let converted: Vec<Value> = tools.iter().map(|tool| {
            if tool.get("type").and_then(|t| t.as_str()) == Some(OB::FUNCTION)
                && let Some(f) = tool.get("function") {
                    return json!({
                        "type": OB::FUNCTION,
                        "name": f.get("name").cloned().unwrap_or(Value::Null),
                        "description": f.get("description").and_then(|d| d.as_str()).unwrap_or(""),
                        "parameters": normalize_tool_parameters(f.get("parameters").unwrap_or(&Value::Null)),
                        "strict": f.get("strict").cloned().unwrap_or(Value::Null)
                    });
                }
            tool.clone()
        }).collect();
        result.insert("tools".into(), Value::Array(converted));
    }

    // Pass through fields
    for k in &["temperature", "max_tokens", "top_p", "service_tier"] {
        if let Some(v) = body.get(*k) {
            result.insert(k.to_string(), v.clone());
        }
    }
    if let Some(r) = body.get("reasoning") {
        result.insert("reasoning".into(), r.clone());
    } else if let Some(effort) = body.get("reasoning_effort").and_then(|e| e.as_str()) {
        result.insert(
            "reasoning".into(),
            json!({"effort": effort, "summary": "auto"}),
        );
    }

    result.insert("input".into(), Value::Array(input));
    Value::Object(result)
}

#[cfg(test)]
mod tests {
    use super::*;

    // --- responses → openai ---
    #[test]
    fn string_input_becomes_user_message() {
        let body = json!({"input": "hello"});
        let out = translate("gpt-4", body, false, None);
        assert_eq!(out["messages"][0]["role"], "user");
        assert_eq!(out["messages"][0]["content"][0]["text"], "hello");
        assert!(out.get("input").is_none());
    }

    #[test]
    fn empty_string_input_uses_placeholder() {
        let body = json!({"input": ""});
        let out = translate("gpt-4", body, false, None);
        assert_eq!(out["messages"][0]["content"][0]["text"], "...");
    }

    #[test]
    fn instructions_become_system() {
        let body = json!({"input": "hi", "instructions": "be terse"});
        let out = translate("gpt-4", body, false, None);
        assert_eq!(out["messages"][0]["role"], "system");
        assert_eq!(out["messages"][0]["content"], "be terse");
        assert!(out.get("instructions").is_none());
    }

    #[test]
    fn function_call_becomes_tool_calls() {
        let body = json!({"input":[
            {"type":"function_call","call_id":"c1","name":"search","arguments":"{\"q\":\"x\"}"}
        ]});
        let out = translate("m", body, false, None);
        let msg = &out["messages"][0];
        assert_eq!(msg["role"], "assistant");
        assert_eq!(msg["tool_calls"][0]["id"], "c1");
        assert_eq!(msg["tool_calls"][0]["function"]["name"], "search");
    }

    #[test]
    fn function_call_output_becomes_tool_message() {
        let body = json!({"input":[
            {"type":"function_call","call_id":"c1","name":"search","arguments":"{}"},
            {"type":"function_call_output","call_id":"c1","output":"result"}
        ]});
        let out = translate("m", body, false, None);
        assert_eq!(out["messages"][0]["role"], "assistant");
        assert_eq!(out["messages"][1]["role"], "tool");
        assert_eq!(out["messages"][1]["tool_call_id"], "c1");
        assert_eq!(out["messages"][1]["content"], "result");
    }

    #[test]
    fn reasoning_buffered_then_attached() {
        let body = json!({"input":[
            {"type":"reasoning","summary":[{"type":"summary_text","text":"hmm"}],"encrypted_content":"enc123"},
            {"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}
        ]});
        let out = translate("m", body, false, None);
        let msg = &out["messages"][0];
        assert_eq!(msg["role"], "assistant");
        assert_eq!(msg["reasoning_content"], "hmm");
        assert_eq!(msg["encrypted_content"], "enc123");
    }

    #[test]
    fn responses_tools_to_openai() {
        let body = json!({
            "input":"hi",
            "tools":[{"type":"function","name":"search","description":"s","parameters":{"type":"object"}}]
        });
        let out = translate("m", body, false, None);
        assert_eq!(out["tools"][0]["type"], "function");
        assert_eq!(out["tools"][0]["function"]["name"], "search");
        assert_eq!(
            out["tools"][0]["function"]["parameters"]["properties"],
            json!({})
        );
    }

    #[test]
    fn max_output_tokens_mapped() {
        let body = json!({"input":"hi","max_output_tokens":100});
        let out = translate("m", body, false, None);
        assert_eq!(out["max_tokens"], 100);
        assert!(out.get("max_output_tokens").is_none());
    }

    #[test]
    fn reasoning_effort_extracted() {
        let body = json!({"input":"hi","reasoning":{"effort":"high"}});
        let out = translate("m", body, false, None);
        assert_eq!(out["reasoning_effort"], "high");
        assert!(out.get("reasoning").is_none());
    }

    // --- openai → responses ---
    #[test]
    fn user_message_to_input() {
        let body = json!({"messages":[{"role":"user","content":"hello"}]});
        let out = translate_to_responses("gpt-4", body, false, None);
        assert_eq!(out["model"], "gpt-4");
        assert_eq!(out["stream"], true);
        assert_eq!(out["store"], false);
        assert_eq!(out["input"][0]["type"], "message");
        assert_eq!(out["input"][0]["role"], "user");
        assert_eq!(out["input"][0]["content"][0]["type"], "input_text");
        assert_eq!(out["input"][0]["content"][0]["text"], "hello");
    }

    #[test]
    fn system_to_instructions() {
        let body = json!({"messages":[
            {"role":"system","content":"be terse"},
            {"role":"user","content":"hi"}
        ]});
        let out = translate_to_responses("m", body, false, None);
        assert_eq!(out["instructions"], "be terse");
        assert_eq!(out["input"].as_array().unwrap().len(), 1);
    }

    #[test]
    fn tool_calls_become_function_call() {
        let body = json!({"messages":[
            {"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"search","arguments":"{}"}}]}
        ]});
        let out = translate_to_responses("m", body, false, None);
        let fc = out["input"]
            .as_array()
            .unwrap()
            .iter()
            .find(|i| i["type"] == "function_call")
            .unwrap();
        assert_eq!(fc["call_id"], "c1");
        assert_eq!(fc["name"], "search");
    }

    #[test]
    fn tool_message_becomes_function_call_output() {
        let body = json!({"messages":[
            {"role":"tool","tool_call_id":"c1","content":"result"}
        ]});
        let out = translate_to_responses("m", body, false, None);
        let fco = &out["input"][0];
        assert_eq!(fco["type"], "function_call_output");
        assert_eq!(fco["call_id"], "c1");
        assert_eq!(fco["output"], "result");
    }

    #[test]
    fn long_call_id_clamped() {
        let long_id = "a".repeat(100);
        let body = json!({"messages":[
            {"role":"tool","tool_call_id":long_id,"content":"r"}
        ]});
        let out = translate_to_responses("m", body, false, None);
        assert_eq!(out["input"][0]["call_id"].as_str().unwrap().len(), 64);
    }

    #[test]
    fn reasoning_content_becomes_reasoning_item() {
        let body = json!({"messages":[
            {"role":"assistant","content":"answer","reasoning_content":"hmm","encrypted_content":"enc"}
        ]});
        let out = translate_to_responses("m", body, false, None);
        let reasoning = out["input"]
            .as_array()
            .unwrap()
            .iter()
            .find(|i| i["type"] == "reasoning")
            .unwrap();
        assert_eq!(reasoning["summary"][0]["text"], "hmm");
        assert_eq!(reasoning["encrypted_content"], "enc");
    }

    #[test]
    fn openai_tools_to_responses() {
        let body = json!({
            "messages":[{"role":"user","content":"hi"}],
            "tools":[{"type":"function","function":{"name":"search","description":"s","parameters":{"type":"object"}}}]
        });
        let out = translate_to_responses("m", body, false, None);
        assert_eq!(out["tools"][0]["name"], "search");
        assert_eq!(out["tools"][0]["parameters"]["properties"], json!({}));
    }

    #[test]
    fn already_responses_format_passthrough() {
        let body = json!({"input":[{"type":"message","role":"user","content":[]}]});
        let out = translate_to_responses("gpt-4", body, false, None);
        assert!(out.get("input").is_some());
        assert_eq!(out["model"], "gpt-4");
        assert_eq!(out["stream"], true);
    }
}
