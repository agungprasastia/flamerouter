//! openai → gemini request translator (core only — no antigravity/gemini_cli envelope).
//! Port of open-sse/translator/request/openai-to-gemini.js (openaiToGeminiBase path).

use serde_json::{Map, Value, json};
use std::collections::HashMap;

use crate::translator::concerns::extract_text_content;
use crate::translator::schema::{gemini_role as GROLE, openai_block as OB, role as ROLE};

const DEFAULT_SAFETY_SETTINGS: &[(&str, &str)] = &[
    ("HARM_CATEGORY_HATE_SPEECH", "OFF"),
    ("HARM_CATEGORY_DANGEROUS_CONTENT", "OFF"),
    ("HARM_CATEGORY_SEXUALLY_EXPLICIT", "OFF"),
    ("HARM_CATEGORY_HARASSMENT", "OFF"),
    ("HARM_CATEGORY_CIVIC_INTEGRITY", "OFF"),
];

const DEFAULT_THINKING_SIGNATURE: &str = "EUAqAUEQSwgBSUxoR05UUUdT";

fn sanitize_function_name(name: &str) -> String {
    if name.is_empty() {
        return "_unknown".into();
    }
    let mut out: String = name
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '_' || c == '.' || c == ':' || c == '-' {
                c
            } else {
                '_'
            }
        })
        .collect();
    if !out
        .chars()
        .next()
        .map(|c| c.is_ascii_alphabetic() || c == '_')
        .unwrap_or(false)
    {
        out.insert(0, '_');
    }
    out.chars().take(64).collect()
}

fn try_parse_json(s: &Value) -> Value {
    match s {
        Value::String(t) => serde_json::from_str(t).unwrap_or(Value::Null),
        other => other.clone(),
    }
}

fn convert_openai_content_to_parts(content: &Value) -> Vec<Value> {
    let mut parts = Vec::new();
    match content {
        Value::String(s) => parts.push(json!({"text": s})),
        Value::Array(arr) => {
            for item in arr {
                let ty = item.get("type").and_then(|t| t.as_str()).unwrap_or("");
                match ty {
                    t if t == OB::TEXT => {
                        if let Some(txt) = item.get("text") {
                            parts.push(json!({"text": txt}));
                        }
                    }
                    t if t == OB::IMAGE_URL => {
                        if let Some(url) = item
                            .get("image_url")
                            .and_then(|i| i.get("url"))
                            .and_then(|u| u.as_str())
                        {
                            if let Some((mime, data)) = url.strip_prefix("data:").and_then(|rest| {
                                rest.split_once(";base64,")
                                    .map(|(m, d)| (m.to_string(), d.to_string()))
                            }) {
                                parts
                                    .push(json!({"inlineData": {"mime_type": mime, "data": data}}));
                            } else if url.starts_with("http://") || url.starts_with("https://") {
                                parts.push(
                                    json!({"fileData": {"fileUri": url, "mimeType": "image/*"}}),
                                );
                            }
                        }
                    }
                    t if t == OB::INPUT_AUDIO => {
                        if let Some(data) = item
                            .get("input_audio")
                            .and_then(|a| a.get("data"))
                            .and_then(|d| d.as_str())
                        {
                            let format = item
                                .get("input_audio")
                                .and_then(|a| a.get("format"))
                                .and_then(|f| f.as_str())
                                .unwrap_or("wav");
                            let mime = if format == "mp3" {
                                "audio/mpeg".to_string()
                            } else {
                                format!("audio/{}", format)
                            };
                            parts.push(json!({"inlineData": {"mime_type": mime, "data": data}}));
                        }
                    }
                    _ => {}
                }
            }
        }
        _ => {}
    }
    parts
}

fn normalize_contents(contents: Vec<Value>) -> Vec<Value> {
    let mut out: Vec<Value> = Vec::new();
    for c in contents {
        let role = c.get("role").and_then(|r| r.as_str()).unwrap_or("");
        let parts = c
            .get("parts")
            .and_then(|p| p.as_array())
            .cloned()
            .unwrap_or_default();
        if role.is_empty() || parts.is_empty() {
            continue;
        }
        if let Some(last) = out.last_mut()
            && last.get("role").and_then(|r| r.as_str()) == Some(role)
                && let Some(lp) = last.get_mut("parts").and_then(|p| p.as_array_mut()) {
                    lp.extend(parts);
                    continue;
                }
        out.push(c);
    }
    out
}

pub fn translate(model: &str, body: Value, _stream: bool, _credentials: Option<&Value>) -> Value {
    let mut generation_config = Map::new();
    if let Some(t) = body.get("temperature") {
        generation_config.insert("temperature".into(), t.clone());
    }
    if let Some(tp) = body.get("top_p") {
        generation_config.insert("topP".into(), tp.clone());
    }
    if let Some(tk) = body.get("top_k") {
        generation_config.insert("topK".into(), tk.clone());
    }
    if let Some(mt) = body.get("max_tokens") {
        generation_config.insert("maxOutputTokens".into(), mt.clone());
    }

    let safety: Vec<Value> = DEFAULT_SAFETY_SETTINGS
        .iter()
        .map(|(cat, thr)| json!({"category": cat, "threshold": thr}))
        .collect();

    let mut contents: Vec<Value> = Vec::new();
    let mut system_instruction: Option<Value> = None;

    // Build tool_call_id → name map and tool responses cache
    let mut tc_id2name: HashMap<String, String> = HashMap::new();
    let mut tool_responses: HashMap<String, Value> = HashMap::new();

    if let Some(msgs) = body.get("messages").and_then(|m| m.as_array()) {
        for msg in msgs {
            let role = msg.get("role").and_then(|r| r.as_str()).unwrap_or("");
            if role == ROLE::ASSISTANT {
                if let Some(tcs) = msg.get("tool_calls").and_then(|t| t.as_array()) {
                    for tc in tcs {
                        if tc.get("type").and_then(|t| t.as_str()) == Some(OB::FUNCTION)
                            && let (Some(id), Some(name)) = (
                                tc.get("id").and_then(|i| i.as_str()),
                                tc.get("function")
                                    .and_then(|f| f.get("name"))
                                    .and_then(|n| n.as_str()),
                            ) {
                                tc_id2name.insert(id.to_string(), name.to_string());
                            }
                    }
                }
            } else if role == ROLE::TOOL
                && let Some(id) = msg.get("tool_call_id").and_then(|i| i.as_str()) {
                    tool_responses.insert(
                        id.to_string(),
                        msg.get("content").cloned().unwrap_or(Value::Null),
                    );
                }
        }
    }

    // Convert messages
    if let Some(msgs) = body.get("messages").and_then(|m| m.as_array()) {
        let msg_count = msgs.len();
        for msg in msgs {
            let role = msg.get("role").and_then(|r| r.as_str()).unwrap_or("");
            let content = msg.get("content").cloned().unwrap_or(Value::Null);

            if role == ROLE::SYSTEM && msg_count > 1 {
                let text = if content.is_string() {
                    content.as_str().unwrap_or("").to_string()
                } else {
                    extract_text_content(&content, "")
                };
                system_instruction = Some(json!({
                    "role": GROLE::USER,
                    "parts": [{"text": text}]
                }));
            } else if role == ROLE::USER || (role == ROLE::SYSTEM && msg_count == 1) {
                let parts = convert_openai_content_to_parts(&content);
                if !parts.is_empty() {
                    contents.push(json!({"role": GROLE::USER, "parts": parts}));
                }
            } else if role == ROLE::ASSISTANT {
                let mut parts: Vec<Value> = Vec::new();

                if let Some(reasoning) = msg.get("reasoning_content").and_then(|r| r.as_str()) {
                    parts.push(json!({"thought": true, "text": reasoning}));
                    parts.push(json!({"thoughtSignature": DEFAULT_THINKING_SIGNATURE, "text": ""}));
                }

                if !content.is_null() {
                    let text = if content.is_string() {
                        content.as_str().unwrap_or("").to_string()
                    } else {
                        extract_text_content(&content, "")
                    };
                    if !text.is_empty() {
                        parts.push(json!({"text": text}));
                    }
                }

                let mut tool_call_ids: Vec<String> = Vec::new();
                if let Some(tcs) = msg.get("tool_calls").and_then(|t| t.as_array()) {
                    for tc in tcs {
                        if tc.get("type").and_then(|t| t.as_str()) != Some(OB::FUNCTION) {
                            continue;
                        }
                        let args = try_parse_json(
                            tc.get("function")
                                .and_then(|f| f.get("arguments"))
                                .unwrap_or(&json!("{}")),
                        );
                        let name = tc
                            .get("function")
                            .and_then(|f| f.get("name"))
                            .and_then(|n| n.as_str())
                            .unwrap_or("");
                        let id = tc.get("id").and_then(|i| i.as_str()).unwrap_or("");
                        parts.push(json!({
                            "thoughtSignature": DEFAULT_THINKING_SIGNATURE,
                            "functionCall": {
                                "id": id,
                                "name": sanitize_function_name(name),
                                "args": args
                            }
                        }));
                        tool_call_ids.push(id.to_string());
                    }

                    if !parts.is_empty() {
                        contents.push(json!({"role": GROLE::MODEL, "parts": parts}));
                    }

                    // Tool responses in subsequent messages
                    let has_responses = tool_call_ids
                        .iter()
                        .any(|id| tool_responses.contains_key(id));
                    if has_responses {
                        let mut tool_parts = Vec::new();
                        for fid in &tool_call_ids {
                            let Some(resp) = tool_responses.get(fid) else {
                                continue;
                            };
                            let name = tc_id2name.get(fid).cloned().unwrap_or_else(|| {
                                let parts: Vec<&str> = fid.split('-').collect();
                                if parts.len() > 2 {
                                    parts[..parts.len() - 2].join("-")
                                } else {
                                    fid.clone()
                                }
                            });
                            let parsed = try_parse_json(resp);
                            let result = if parsed.is_null() || !parsed.is_object() {
                                json!({"result": if parsed.is_null() { resp.clone() } else { parsed }})
                            } else {
                                json!({"result": parsed})
                            };
                            tool_parts.push(json!({
                                "functionResponse": {
                                    "id": fid,
                                    "name": sanitize_function_name(&name),
                                    "response": result
                                }
                            }));
                        }
                        if !tool_parts.is_empty() {
                            contents.push(json!({"role": GROLE::USER, "parts": tool_parts}));
                        }
                    }
                } else if !parts.is_empty() {
                    contents.push(json!({"role": GROLE::MODEL, "parts": parts}));
                }
            }
        }
    }

    // Tools
    let mut function_declarations = Vec::new();
    if let Some(tools) = body.get("tools").and_then(|t| t.as_array()) {
        for t in tools {
            // Anthropic format
            if t.get("name").is_some() && t.get("input_schema").is_some() {
                function_declarations.push(json!({
                    "name": sanitize_function_name(t.get("name").and_then(|n| n.as_str()).unwrap_or("")),
                    "description": t.get("description").and_then(|d| d.as_str()).unwrap_or(""),
                    "parameters": t.get("input_schema").cloned().unwrap_or(json!({"type":"object","properties":{}}))
                }));
            }
            // OpenAI format
            else if t.get("type").and_then(|ty| ty.as_str()) == Some(OB::FUNCTION)
                && let Some(f) = t.get("function") {
                    function_declarations.push(json!({
                        "name": sanitize_function_name(f.get("name").and_then(|n| n.as_str()).unwrap_or("")),
                        "description": f.get("description").and_then(|d| d.as_str()).unwrap_or(""),
                        "parameters": f.get("parameters").cloned().unwrap_or(json!({"type":"object","properties":{}}))
                    }));
                }
        }
    }

    let mut result = Map::new();
    result.insert("model".into(), json!(model));
    result.insert(
        "contents".into(),
        Value::Array(normalize_contents(contents)),
    );
    result.insert("generationConfig".into(), Value::Object(generation_config));
    result.insert("safetySettings".into(), Value::Array(safety));
    if let Some(si) = system_instruction {
        result.insert("systemInstruction".into(), si);
    }
    if !function_declarations.is_empty() {
        result.insert(
            "tools".into(),
            json!([{"functionDeclarations": function_declarations}]),
        );
    }
    Value::Object(result)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitize_names() {
        assert_eq!(sanitize_function_name("get_weather"), "get_weather");
        assert_eq!(sanitize_function_name("9lives"), "_9lives");
        assert_eq!(sanitize_function_name("hello world"), "hello_world");
        assert_eq!(sanitize_function_name(""), "_unknown");
        assert_eq!(sanitize_function_name("a".repeat(100).as_str()).len(), 64);
    }

    #[test]
    fn basic_user_message() {
        let body = json!({"messages":[{"role":"user","content":"hi"}]});
        let out = translate("gemini-2.0-flash", body, false, None);
        assert_eq!(out["model"], "gemini-2.0-flash");
        assert_eq!(out["contents"][0]["role"], "user");
        assert_eq!(out["contents"][0]["parts"][0]["text"], "hi");
    }

    #[test]
    fn system_becomes_system_instruction() {
        let body = json!({"messages":[
            {"role":"system","content":"be terse"},
            {"role":"user","content":"hi"}
        ]});
        let out = translate("m", body, false, None);
        assert_eq!(out["systemInstruction"]["parts"][0]["text"], "be terse");
        assert_eq!(out["contents"].as_array().unwrap().len(), 1);
    }

    #[test]
    fn generation_config_mapping() {
        let body = json!({
            "messages":[{"role":"user","content":"hi"}],
            "temperature": 0.5, "top_p": 0.9, "top_k": 40, "max_tokens": 256
        });
        let out = translate("m", body, false, None);
        assert_eq!(out["generationConfig"]["temperature"], 0.5);
        assert_eq!(out["generationConfig"]["topP"], 0.9);
        assert_eq!(out["generationConfig"]["topK"], 40);
        assert_eq!(out["generationConfig"]["maxOutputTokens"], 256);
    }

    #[test]
    fn image_data_uri_becomes_inline_data() {
        let body = json!({"messages":[{"role":"user","content":[
            {"type":"text","text":"see"},
            {"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}
        ]}]});
        let out = translate("m", body, false, None);
        let parts = &out["contents"][0]["parts"];
        assert_eq!(parts[0]["text"], "see");
        assert_eq!(parts[1]["inlineData"]["mime_type"], "image/png");
        assert_eq!(parts[1]["inlineData"]["data"], "aGVsbG8=");
    }

    #[test]
    fn http_image_becomes_file_data() {
        let body = json!({"messages":[{"role":"user","content":[
            {"type":"image_url","image_url":{"url":"https://example.com/x.png"}}
        ]}]});
        let out = translate("m", body, false, None);
        assert_eq!(
            out["contents"][0]["parts"][0]["fileData"]["fileUri"],
            "https://example.com/x.png"
        );
    }

    #[test]
    fn tool_calls_become_function_call() {
        let body = json!({"messages":[
            {"role":"assistant","content":null,"tool_calls":[
                {"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}}
            ]}
        ]});
        let out = translate("m", body, false, None);
        assert_eq!(out["contents"][0]["role"], "model");
        let fc = &out["contents"][0]["parts"][0]["functionCall"];
        assert_eq!(fc["id"], "call_1");
        assert_eq!(fc["name"], "get_weather");
        assert_eq!(fc["args"]["city"], "SF");
    }

    #[test]
    fn tool_responses_become_function_response() {
        let body = json!({"messages":[
            {"role":"assistant","content":null,"tool_calls":[
                {"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}
            ]},
            {"role":"tool","tool_call_id":"call_1","content":"{\"temp\":72}"}
        ]});
        let out = translate("m", body, false, None);
        let user_with_resp = out["contents"]
            .as_array()
            .unwrap()
            .iter()
            .find(|c| c["role"] == "user" && c["parts"][0].get("functionResponse").is_some())
            .expect("should have functionResponse content");
        let fr = &user_with_resp["parts"][0]["functionResponse"];
        assert_eq!(fr["id"], "call_1");
        assert_eq!(fr["name"], "get_weather");
        assert_eq!(fr["response"]["result"]["temp"], 72);
    }

    #[test]
    fn tools_openai_format() {
        let body = json!({
            "messages":[{"role":"user","content":"hi"}],
            "tools":[{"type":"function","function":{"name":"search","description":"s","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}]
        });
        let out = translate("m", body, false, None);
        let decls = &out["tools"][0]["functionDeclarations"];
        assert_eq!(decls[0]["name"], "search");
        assert_eq!(decls[0]["parameters"]["properties"]["q"]["type"], "string");
    }

    #[test]
    fn reasoning_content_becomes_thought() {
        let body = json!({"messages":[
            {"role":"assistant","content":"answer","reasoning_content":"hmm"}
        ]});
        let out = translate("m", body, false, None);
        let parts = &out["contents"][0]["parts"];
        assert_eq!(parts[0]["thought"], true);
        assert_eq!(parts[0]["text"], "hmm");
        assert_eq!(parts[1]["thoughtSignature"], DEFAULT_THINKING_SIGNATURE);
        assert_eq!(parts[2]["text"], "answer");
    }

    #[test]
    fn same_role_contents_merged() {
        let body = json!({"messages":[
            {"role":"user","content":"a"},
            {"role":"user","content":"b"}
        ]});
        let out = translate("m", body, false, None);
        let contents = out["contents"].as_array().unwrap();
        assert_eq!(contents.len(), 1);
        assert_eq!(contents[0]["parts"].as_array().unwrap().len(), 2);
    }
}
