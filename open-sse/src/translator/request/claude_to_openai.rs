//! claude → openai request translator.
//! Port of open-sse/translator/request/claude-to-openai.js

use serde_json::{Map, Value, json};

use crate::translator::concerns::{adjust_max_tokens, collapse_text_parts, encode_data_uri};
use crate::translator::schema::{claude_block as CB, openai_block as OB, role as ROLE};

fn strip_anthropic_billing_header(text: &str) -> String {
    // /^x-anthropic-billing-header:[^\n]*(?:\r?\n)?/i
    let lower = text.to_ascii_lowercase();
    if lower.starts_with("x-anthropic-billing-header:") {
        if let Some(nl) = text.find('\n') {
            return text[nl + 1..].to_string();
        }
        return String::new();
    }
    text.to_string()
}

pub fn translate(model: &str, body: Value, stream: bool, _credentials: Option<&Value>) -> Value {
    let mut result = Map::new();
    result.insert("model".into(), Value::String(model.to_string()));
    result.insert("stream".into(), Value::Bool(stream));

    if body.get("max_tokens").is_some() {
        let n = adjust_max_tokens(&body, crate::translator::schema::DEFAULT_MAX_TOKENS);
        result.insert("max_tokens".into(), json!(n));
    }
    if let Some(t) = body.get("temperature") {
        result.insert("temperature".into(), t.clone());
    }

    let mut messages: Vec<Value> = Vec::new();

    // System message
    if let Some(sys) = body.get("system") {
        let system_content = match sys {
            Value::Array(arr) => arr
                .iter()
                .filter_map(|s| s.get("text").and_then(|t| t.as_str()))
                .map(strip_anthropic_billing_header)
                .filter(|s| !s.is_empty())
                .collect::<Vec<_>>()
                .join("\n"),
            Value::String(s) => strip_anthropic_billing_header(s),
            _ => String::new(),
        };
        if !system_content.is_empty() {
            messages.push(json!({ "role": ROLE::SYSTEM, "content": system_content }));
        }
    }

    // Convert messages
    if let Some(Value::Array(msgs)) = body.get("messages") {
        for msg in msgs {
            match convert_claude_message(msg) {
                Some(Converted::One(m)) => messages.push(m),
                Some(Converted::Many(ms)) => messages.extend(ms),
                None => {}
            }
        }
    }

    fix_missing_tool_responses_openai(&mut messages);

    // Tools
    if let Some(Value::Array(tools)) = body.get("tools") {
        let converted: Vec<Value> = tools
            .iter()
            .map(|tool| {
                let name = tool.get("name").cloned().unwrap_or(Value::Null);
                let desc = tool
                    .get("description")
                    .and_then(|d| d.as_str())
                    .map(|s| s.to_string())
                    .unwrap_or_default();
                let params = tool
                    .get("input_schema")
                    .cloned()
                    .unwrap_or_else(|| json!({ "type": "object", "properties": {} }));
                json!({
                    "type": OB::FUNCTION,
                    "function": { "name": name, "description": desc, "parameters": params }
                })
            })
            .collect();
        result.insert("tools".into(), Value::Array(converted));
    }

    if let Some(tc) = body.get("tool_choice") {
        result.insert("tool_choice".into(), convert_tool_choice(tc));
    }

    if let Some(re) = body.get("reasoning_effort") {
        result.insert("reasoning_effort".into(), re.clone());
    } else if let Some(eff) = body.get("reasoning").and_then(|r| r.get("effort")) {
        result.insert("reasoning_effort".into(), eff.clone());
    }
    if let Some(r) = body.get("reasoning") {
        result.insert("reasoning".into(), r.clone());
    }

    result.insert("messages".into(), Value::Array(messages));
    Value::Object(result)
}

enum Converted {
    One(Value),
    Many(Vec<Value>),
}

fn fix_missing_tool_responses_openai(messages: &mut Vec<Value>) {
    let mut i = 0;
    while i < messages.len() {
        let msg = &messages[i];
        let role = msg.get("role").and_then(|r| r.as_str()).unwrap_or("");
        let tool_call_ids: Vec<String> = if role == ROLE::ASSISTANT {
            msg.get("tool_calls")
                .and_then(|t| t.as_array())
                .map(|arr| {
                    arr.iter()
                        .filter_map(|tc| {
                            tc.get("id")
                                .and_then(|id| id.as_str())
                                .map(|s| s.to_string())
                        })
                        .collect()
                })
                .unwrap_or_default()
        } else {
            Vec::new()
        };
        if tool_call_ids.is_empty() {
            i += 1;
            continue;
        }

        let mut responded: std::collections::HashSet<String> = Default::default();
        let mut insert_pos = i + 1;
        let mut j = i + 1;
        while j < messages.len() {
            let next = &messages[j];
            let r = next.get("role").and_then(|r| r.as_str()).unwrap_or("");
            let tcid = next.get("tool_call_id").and_then(|t| t.as_str());
            if r == ROLE::TOOL && tcid.is_some() {
                responded.insert(tcid.unwrap().to_string());
                insert_pos = j + 1;
                j += 1;
            } else {
                break;
            }
        }

        let missing: Vec<String> = tool_call_ids
            .into_iter()
            .filter(|id| !responded.contains(id))
            .collect();
        if !missing.is_empty() {
            let insertions: Vec<Value> = missing
                .iter()
                .map(|id| {
                    json!({
                        "role": ROLE::TOOL,
                        "tool_call_id": id,
                        "content": "[No response received]"
                    })
                })
                .collect();
            let n = insertions.len();
            for (k, m) in insertions.into_iter().enumerate() {
                messages.insert(insert_pos + k, m);
            }
            i = insert_pos + n - 1;
        }
        i += 1;
    }
}

fn system_reminder_text(content: &Value) -> String {
    let parts: Vec<String> = match content {
        Value::Array(arr) => arr
            .iter()
            .filter(|c| c.get("type").and_then(|t| t.as_str()) == Some(CB::TEXT))
            .filter_map(|c| {
                c.get("text")
                    .and_then(|t| t.as_str())
                    .map(|s| s.to_string())
            })
            .collect(),
        Value::String(s) => vec![s.clone()],
        _ => vec![],
    };
    let text = parts
        .into_iter()
        .filter(|s| !s.is_empty())
        .collect::<Vec<_>>()
        .join("\n");
    if text.trim().is_empty() {
        return String::new();
    }
    format!("<instructions>\n{}\n</instructions>", text)
}

fn convert_claude_message(msg: &Value) -> Option<Converted> {
    let role_str = msg.get("role").and_then(|r| r.as_str()).unwrap_or("");

    // Mid-conversation system -> user
    if role_str == ROLE::SYSTEM {
        let text = system_reminder_text(msg.get("content")?);
        if text.is_empty() {
            return None;
        }
        return Some(Converted::One(
            json!({ "role": ROLE::USER, "content": text }),
        ));
    }

    let role = if role_str == ROLE::USER || role_str == ROLE::TOOL {
        ROLE::USER
    } else {
        ROLE::ASSISTANT
    };

    let content = msg.get("content")?;

    // String content
    if let Value::String(s) = content {
        return Some(Converted::One(json!({ "role": role, "content": s })));
    }

    // Array content
    if let Value::Array(blocks) = content {
        let mut parts: Vec<Value> = Vec::new();
        let mut tool_calls: Vec<Value> = Vec::new();
        let mut tool_results: Vec<Value> = Vec::new();

        for block in blocks {
            let btype = block.get("type").and_then(|t| t.as_str()).unwrap_or("");
            match btype {
                t if t == CB::TEXT => {
                    parts.push(json!({ "type": OB::TEXT, "text": block.get("text").cloned().unwrap_or(Value::Null) }));
                }
                t if t == CB::IMAGE => {
                    if let Some(src) = block.get("source")
                        && src.get("type").and_then(|t| t.as_str()) == Some("base64") {
                            let mime = src.get("media_type").and_then(|m| m.as_str()).unwrap_or("");
                            let data = src.get("data").and_then(|d| d.as_str()).unwrap_or("");
                            parts.push(json!({
                                "type": OB::IMAGE_URL,
                                "image_url": { "url": encode_data_uri(mime, data) }
                            }));
                        }
                }
                t if t == CB::TOOL_USE => {
                    let input = block.get("input").cloned().unwrap_or_else(|| json!({}));
                    tool_calls.push(json!({
                        "id": block.get("id").cloned().unwrap_or(Value::Null),
                        "type": OB::FUNCTION,
                        "function": {
                            "name": block.get("name").cloned().unwrap_or(Value::Null),
                            "arguments": serde_json::to_string(&input).unwrap_or_else(|_| "{}".into())
                        }
                    }));
                }
                t if t == CB::TOOL_RESULT => {
                    let result_content = match block.get("content") {
                        Some(Value::String(s)) => s.clone(),
                        Some(Value::Array(arr)) => {
                            let texts: Vec<String> = arr
                                .iter()
                                .filter(|c| {
                                    c.get("type").and_then(|t| t.as_str()) == Some(CB::TEXT)
                                })
                                .filter_map(|c| {
                                    c.get("text")
                                        .and_then(|t| t.as_str())
                                        .map(|s| s.to_string())
                                })
                                .collect();
                            if texts.is_empty() {
                                serde_json::to_string(&Value::Array(arr.clone()))
                                    .unwrap_or_default()
                            } else {
                                texts.join("\n")
                            }
                        }
                        Some(other) => serde_json::to_string(other).unwrap_or_default(),
                        None => String::new(),
                    };
                    tool_results.push(json!({
                        "role": ROLE::TOOL,
                        "tool_call_id": block.get("tool_use_id").cloned().unwrap_or(Value::Null),
                        "content": result_content
                    }));
                }
                _ => {}
            }
        }

        if !tool_results.is_empty() {
            let mut out = tool_results;
            if !parts.is_empty() {
                out.push(json!({ "role": ROLE::USER, "content": collapse_text_parts(parts) }));
            }
            return Some(Converted::Many(out));
        }

        if !tool_calls.is_empty() {
            let mut obj = Map::new();
            obj.insert("role".into(), Value::String(ROLE::ASSISTANT.into()));
            if !parts.is_empty() {
                obj.insert("content".into(), collapse_text_parts(parts));
            }
            obj.insert("tool_calls".into(), Value::Array(tool_calls));
            return Some(Converted::One(Value::Object(obj)));
        }

        if !parts.is_empty() {
            return Some(Converted::One(json!({
                "role": role,
                "content": collapse_text_parts(parts)
            })));
        }

        if blocks.is_empty() {
            return Some(Converted::One(json!({ "role": role, "content": "" })));
        }
    }

    None
}

fn convert_tool_choice(choice: &Value) -> Value {
    match choice {
        Value::String(s) => Value::String(s.clone()),
        Value::Object(_) => {
            let t = choice.get("type").and_then(|t| t.as_str()).unwrap_or("");
            match t {
                "auto" => json!("auto"),
                "any" => json!("required"),
                "tool" => json!({
                    "type": OB::FUNCTION,
                    "function": { "name": choice.get("name").cloned().unwrap_or(Value::Null) }
                }),
                _ => json!("auto"),
            }
        }
        _ => json!("auto"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn simple_string_messages() {
        let body = json!({
            "max_tokens": 100,
            "messages": [
                {"role":"user","content":"hello"},
                {"role":"assistant","content":"hi"},
                {"role":"user","content":"how"}
            ]
        });
        let out = translate("gpt-4", body, false, None);
        assert_eq!(out["model"], "gpt-4");
        assert_eq!(out["stream"], false);
        assert_eq!(out["max_tokens"], 100);
        let msgs = out["messages"].as_array().unwrap();
        assert_eq!(msgs.len(), 3);
        assert_eq!(msgs[0]["role"], "user");
        assert_eq!(msgs[0]["content"], "hello");
    }

    #[test]
    fn system_string() {
        let body = json!({
            "max_tokens": 100,
            "system": "You are helpful.",
            "messages": [{"role":"user","content":"hi"}]
        });
        let out = translate("gpt-4", body, false, None);
        let msgs = out["messages"].as_array().unwrap();
        assert_eq!(msgs[0]["role"], "system");
        assert_eq!(msgs[0]["content"], "You are helpful.");
        assert_eq!(msgs[1]["role"], "user");
    }

    #[test]
    fn system_array_joined() {
        let body = json!({
            "max_tokens": 100,
            "system": [{"type":"text","text":"A"},{"type":"text","text":"B"}],
            "messages": [{"role":"user","content":"hi"}]
        });
        let out = translate("gpt-4", body, false, None);
        assert_eq!(out["messages"][0]["content"], "A\nB");
    }

    #[test]
    fn billing_header_stripped() {
        let body = json!({
            "max_tokens": 100,
            "system": "x-anthropic-billing-header: abc\nReal prompt",
            "messages": [{"role":"user","content":"hi"}]
        });
        let out = translate("gpt-4", body, false, None);
        assert_eq!(out["messages"][0]["content"], "Real prompt");
    }

    #[test]
    fn image_base64_to_image_url() {
        let body = json!({
            "max_tokens": 100,
            "messages": [{
                "role":"user",
                "content":[
                    {"type":"text","text":"what"},
                    {"type":"image","source":{"type":"base64","media_type":"image/png","data":"PNGDATA"}}
                ]
            }]
        });
        let out = translate("gpt-4", body, false, None);
        let parts = out["messages"][0]["content"].as_array().unwrap();
        assert_eq!(parts[0]["type"], "text");
        assert_eq!(parts[1]["type"], "image_url");
        assert_eq!(
            parts[1]["image_url"]["url"],
            "data:image/png;base64,PNGDATA"
        );
    }

    #[test]
    fn tool_use_to_tool_calls() {
        let body = json!({
            "max_tokens": 100,
            "messages": [
                {"role":"user","content":"call"},
                {"role":"assistant","content":[
                    {"type":"tool_use","id":"tu_1","name":"get_weather","input":{"city":"NYC"}}
                ]}
            ]
        });
        let out = translate("gpt-4", body, false, None);
        let asst = &out["messages"][1];
        assert_eq!(asst["role"], "assistant");
        let tcs = asst["tool_calls"].as_array().unwrap();
        assert_eq!(tcs[0]["id"], "tu_1");
        assert_eq!(tcs[0]["type"], "function");
        assert_eq!(tcs[0]["function"]["name"], "get_weather");
        // arguments is stringified JSON
        let args: Value =
            serde_json::from_str(tcs[0]["function"]["arguments"].as_str().unwrap()).unwrap();
        assert_eq!(args["city"], "NYC");
    }

    #[test]
    fn tool_result_to_tool_message() {
        let body = json!({
            "max_tokens": 100,
            "messages": [
                {"role":"user","content":"call"},
                {"role":"assistant","content":[
                    {"type":"tool_use","id":"tu_1","name":"get_weather","input":{}}
                ]},
                {"role":"user","content":[
                    {"type":"tool_result","tool_use_id":"tu_1","content":"sunny"}
                ]}
            ]
        });
        let out = translate("gpt-4", body, false, None);
        let msgs = out["messages"].as_array().unwrap();
        // user, assistant (tool_call), tool response
        assert_eq!(msgs[2]["role"], "tool");
        assert_eq!(msgs[2]["tool_call_id"], "tu_1");
        assert_eq!(msgs[2]["content"], "sunny");
    }

    #[test]
    fn missing_tool_response_inserted() {
        // Assistant has 2 tool_calls, only 1 response follows → 1 missing inserted
        let body = json!({
            "max_tokens": 100,
            "messages": [
                {"role":"user","content":"call both"},
                {"role":"assistant","content":[
                    {"type":"tool_use","id":"tu_1","name":"a","input":{}},
                    {"type":"tool_use","id":"tu_2","name":"b","input":{}}
                ]},
                {"role":"user","content":[
                    {"type":"tool_result","tool_use_id":"tu_1","content":"ok"}
                ]},
                {"role":"user","content":"next"}
            ]
        });
        let out = translate("gpt-4", body, false, None);
        let msgs = out["messages"].as_array().unwrap();
        // user, assistant, tool(tu_1), tool(tu_2 missing inserted), user "next"
        let tool_msgs: Vec<&Value> = msgs.iter().filter(|m| m["role"] == "tool").collect();
        assert_eq!(tool_msgs.len(), 2);
        let ids: Vec<&str> = tool_msgs
            .iter()
            .map(|m| m["tool_call_id"].as_str().unwrap())
            .collect();
        assert!(ids.contains(&"tu_1"));
        assert!(ids.contains(&"tu_2"));
        let tu2 = tool_msgs
            .iter()
            .find(|m| m["tool_call_id"] == "tu_2")
            .unwrap();
        assert_eq!(tu2["content"], "[No response received]");
    }

    #[test]
    fn tools_converted_to_openai_shape() {
        let body = json!({
            "max_tokens": 100,
            "messages": [{"role":"user","content":"hi"}],
            "tools": [{
                "name":"get_weather",
                "description":"Get weather",
                "input_schema":{"type":"object","properties":{"city":{"type":"string"}}}
            }]
        });
        let out = translate("gpt-4", body, false, None);
        let tools = out["tools"].as_array().unwrap();
        assert_eq!(tools[0]["type"], "function");
        assert_eq!(tools[0]["function"]["name"], "get_weather");
        assert_eq!(tools[0]["function"]["parameters"]["type"], "object");
    }

    #[test]
    fn tool_choice_variants() {
        let body_auto = json!({"max_tokens":100,"tool_choice":{"type":"auto"},"messages":[{"role":"user","content":"hi"}]});
        let out = translate("gpt-4", body_auto, false, None);
        assert_eq!(out["tool_choice"], "auto");

        let body_any = json!({"max_tokens":100,"tool_choice":{"type":"any"},"messages":[{"role":"user","content":"hi"}]});
        let out = translate("gpt-4", body_any, false, None);
        assert_eq!(out["tool_choice"], "required");

        let body_tool = json!({"max_tokens":100,"tool_choice":{"type":"tool","name":"get_weather"},"messages":[{"role":"user","content":"hi"}]});
        let out = translate("gpt-4", body_tool, false, None);
        assert_eq!(out["tool_choice"]["type"], "function");
        assert_eq!(out["tool_choice"]["function"]["name"], "get_weather");
    }

    #[test]
    fn mid_conversation_system_wraps_instructions() {
        let body = json!({
            "max_tokens": 100,
            "messages": [
                {"role":"user","content":"first"},
                {"role":"system","content":"new directive"},
                {"role":"user","content":"second"}
            ]
        });
        let out = translate("gpt-4", body, false, None);
        let msgs = out["messages"].as_array().unwrap();
        // system becomes user with <instructions>
        assert_eq!(msgs[1]["role"], "user");
        let c = msgs[1]["content"].as_str().unwrap();
        assert!(c.contains("<instructions>"));
        assert!(c.contains("new directive"));
    }

    #[test]
    fn reasoning_effort_passthrough() {
        let body = json!({
            "max_tokens": 100,
            "reasoning_effort": "high",
            "messages": [{"role":"user","content":"hi"}]
        });
        let out = translate("gpt-4", body, false, None);
        assert_eq!(out["reasoning_effort"], "high");
    }

    #[test]
    fn reasoning_object_effort_mapped() {
        let body = json!({
            "max_tokens": 100,
            "reasoning": {"effort":"medium"},
            "messages": [{"role":"user","content":"hi"}]
        });
        let out = translate("gpt-4", body, false, None);
        assert_eq!(out["reasoning_effort"], "medium");
        assert_eq!(out["reasoning"]["effort"], "medium");
    }
}
