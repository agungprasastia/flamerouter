//! gemini SSE chunk → openai chat.completion.chunk.
//! Port of open-sse/translator/response/gemini-to-openai.js

use serde_json::{Map, Value, json};

use crate::translator::concerns::encode_data_uri;
use crate::translator::schema::{openai_block as OB, role as ROLE};

fn created_unix() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

fn chunk_meta(state: &Value) -> (String, u64, String) {
    let id = state
        .get("messageId")
        .and_then(|m| m.as_str())
        .map(|s| format!("chatcmpl-{}", s))
        .unwrap_or_else(|| "chatcmpl-unknown".into());
    let model = state
        .get("model")
        .and_then(|m| m.as_str())
        .unwrap_or("")
        .to_string();
    (id, created_unix(), model)
}

fn build_chunk(meta: &(String, u64, String), delta: Value, finish_reason: Option<&str>) -> Value {
    json!({
        "id": meta.0,
        "object": "chat.completion.chunk",
        "created": meta.1,
        "model": meta.2,
        "choices": [{
            "index": 0,
            "delta": delta,
            "finish_reason": finish_reason
        }]
    })
}

fn to_openai_finish_gemini(reason: &str) -> &'static str {
    match reason.to_uppercase().as_str() {
        "STOP" => "stop",
        "MAX_TOKENS" => "length",
        "SAFETY" | "RECITATION" | "BLOCKLIST" | "PROHIBITED_CONTENT" => "content_filter",
        _ => "stop",
    }
}

fn gemini_usage_to_openai(raw: &Value) -> Option<Value> {
    if !raw.is_object() {
        return None;
    }
    let n = |k: &str| raw.get(k).and_then(|v| v.as_u64()).unwrap_or(0);
    let cached = n("cachedContentTokenCount");
    let prompt = n("promptTokenCount");
    let thoughts = n("thoughtsTokenCount");
    let total = n("totalTokenCount");
    let mut candidates = n("candidatesTokenCount");
    if candidates == 0 && total > 0 {
        candidates = total.saturating_sub(prompt).saturating_sub(thoughts);
    }

    let mut usage = Map::new();
    usage.insert("prompt_tokens".into(), json!(prompt));
    usage.insert("completion_tokens".into(), json!(candidates + thoughts));
    usage.insert("total_tokens".into(), json!(total));
    if cached > 0 {
        usage.insert(
            "prompt_tokens_details".into(),
            json!({"cached_tokens": cached}),
        );
    }
    if thoughts > 0 {
        usage.insert(
            "completion_tokens_details".into(),
            json!({"reasoning_tokens": thoughts}),
        );
    }
    Some(Value::Object(usage))
}

fn emit_function_call(fc: &Value, state: &mut Value, meta: &(String, u64, String)) -> Value {
    let name = fc.get("name").and_then(|n| n.as_str()).unwrap_or("");
    let args = fc.get("args").cloned().unwrap_or(json!({}));
    let idx = state
        .get("functionIndex")
        .and_then(|i| i.as_u64())
        .unwrap_or(0);
    state["functionIndex"] = json!(idx + 1);
    let tool_call = json!({
        "id": format!("{}-{}-{}", name, created_unix(), idx),
        "index": idx,
        "type": OB::FUNCTION,
        "function": {"name": name, "arguments": serde_json::to_string(&args).unwrap_or_else(|_| "{}".into())}
    });
    let count = state
        .get("geminiToolCallCount")
        .and_then(|c| c.as_u64())
        .unwrap_or(0);
    state["geminiToolCallCount"] = json!(count + 1);
    build_chunk(meta, json!({"tool_calls": [tool_call]}), None)
}

pub fn translate(chunk: Value, state: &mut Value) -> Option<Vec<Value>> {
    // Antigravity wrapper unwrap
    let response = chunk.get("response").cloned().unwrap_or(chunk.clone());
    let candidate = response.get("candidates")?.as_array()?.first()?.clone();

    let mut results: Vec<Value> = Vec::new();

    // Initialize state
    if state.get("messageId").is_none() {
        let id = response
            .get("responseId")
            .and_then(|r| r.as_str())
            .map(|s| s.to_string())
            .unwrap_or_else(|| format!("msg_{}", created_unix()));
        state["messageId"] = json!(id);
        state["model"] = response
            .get("modelVersion")
            .cloned()
            .unwrap_or(json!("gemini"));
        state["functionIndex"] = json!(0);
        state["geminiToolCallCount"] = json!(0);
        let meta = chunk_meta(state);
        results.push(build_chunk(&meta, json!({"role": ROLE::ASSISTANT}), None));
    }

    let meta = chunk_meta(state);

    // Process parts
    if let Some(parts) = candidate
        .get("content")
        .and_then(|c| c.get("parts"))
        .and_then(|p| p.as_array())
    {
        for part in parts {
            let has_thought_sig =
                part.get("thoughtSignature").is_some() || part.get("thought_signature").is_some();
            let is_thought = part
                .get("thought")
                .and_then(|t| t.as_bool())
                .unwrap_or(false);

            if has_thought_sig {
                if let Some(text) = part.get("text").and_then(|t| t.as_str())
                    && !text.is_empty() {
                        let delta = if is_thought {
                            json!({"reasoning_content": text})
                        } else {
                            json!({"content": text})
                        };
                        results.push(build_chunk(&meta, delta, None));
                    }
                if let Some(fc) = part.get("functionCall") {
                    results.push(emit_function_call(fc, state, &meta));
                }
                continue;
            }

            if let Some(text) = part.get("text").and_then(|t| t.as_str())
                && !text.is_empty() {
                    let delta = if is_thought {
                        json!({"reasoning_content": text})
                    } else {
                        json!({"content": text})
                    };
                    results.push(build_chunk(&meta, delta, None));
                }

            if let Some(fc) = part.get("functionCall") {
                results.push(emit_function_call(fc, state, &meta));
            }

            let inline = part.get("inlineData").or_else(|| part.get("inline_data"));
            if let Some(inline) = inline
                && let Some(data) = inline.get("data").and_then(|d| d.as_str()) {
                    let mime = inline
                        .get("mimeType")
                        .or_else(|| inline.get("mime_type"))
                        .and_then(|m| m.as_str())
                        .unwrap_or("image/png");
                    results.push(build_chunk(
                        &meta,
                        json!({
                            "images": [{
                                "type": OB::IMAGE_URL,
                                "image_url": {"url": encode_data_uri(mime, data)}
                            }]
                        }),
                        None,
                    ));
                }
        }
    }

    // Usage
    let usage_meta = response
        .get("usageMetadata")
        .or_else(|| chunk.get("usageMetadata"));
    if let Some(u) = usage_meta.and_then(gemini_usage_to_openai) {
        state["usage"] = u;
    }

    // Finish reason
    if let Some(finish) = candidate.get("finishReason").and_then(|f| f.as_str()) {
        let mut openai_finish = to_openai_finish_gemini(finish);
        let tool_count = state
            .get("geminiToolCallCount")
            .and_then(|c| c.as_u64())
            .unwrap_or(0);
        if openai_finish == "stop" && tool_count > 0 {
            openai_finish = "tool_calls";
        }
        let mut final_chunk = build_chunk(&meta, json!({}), Some(openai_finish));
        if let Some(u) = state.get("usage") {
            final_chunk["usage"] = u.clone();
        }
        results.push(final_chunk);
        state["finishReason"] = json!(openai_finish);
    }

    if results.is_empty() {
        None
    } else {
        Some(results)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn first_chunk_emits_role() {
        let chunk = json!({
            "responseId": "resp-1",
            "modelVersion": "gemini-2.0-flash",
            "candidates": [{"content": {"parts": [{"text": "hi"}]}}]
        });
        let mut state = json!({});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["delta"]["role"], "assistant");
        assert_eq!(out[0]["id"], "chatcmpl-resp-1");
        assert_eq!(out[0]["model"], "gemini-2.0-flash");
        assert_eq!(out[1]["choices"][0]["delta"]["content"], "hi");
    }

    #[test]
    fn text_flows_through() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":0,"geminiToolCallCount":0});
        let chunk = json!({"candidates":[{"content":{"parts":[{"text":"hello"}]}}]});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["delta"]["content"], "hello");
    }

    #[test]
    fn thought_becomes_reasoning_content() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":0,"geminiToolCallCount":0});
        let chunk = json!({"candidates":[{"content":{"parts":[{"text":"hmm","thought":true}]}}]});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["delta"]["reasoning_content"], "hmm");
    }

    #[test]
    fn function_call_becomes_tool_call() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":0,"geminiToolCallCount":0});
        let chunk = json!({"candidates":[{"content":{"parts":[
            {"functionCall":{"name":"get_weather","args":{"city":"SF"}}}
        ]}}]});
        let out = translate(chunk, &mut state).unwrap();
        let tc = &out[0]["choices"][0]["delta"]["tool_calls"][0];
        assert_eq!(tc["function"]["name"], "get_weather");
        assert_eq!(tc["function"]["arguments"], "{\"city\":\"SF\"}");
        assert_eq!(state["geminiToolCallCount"], 1);
    }

    #[test]
    fn inline_data_becomes_image() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":0,"geminiToolCallCount":0});
        let chunk = json!({"candidates":[{"content":{"parts":[
            {"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}
        ]}}]});
        let out = translate(chunk, &mut state).unwrap();
        let img = &out[0]["choices"][0]["delta"]["images"][0];
        assert_eq!(img["image_url"]["url"], "data:image/png;base64,aGVsbG8=");
    }

    #[test]
    fn finish_stop() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":0,"geminiToolCallCount":0});
        let chunk = json!({"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}]});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["finish_reason"], "stop");
    }

    #[test]
    fn finish_max_tokens() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":0,"geminiToolCallCount":0});
        let chunk = json!({"candidates":[{"content":{"parts":[]},"finishReason":"MAX_TOKENS"}]});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["finish_reason"], "length");
    }

    #[test]
    fn finish_safety_is_content_filter() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":0,"geminiToolCallCount":0});
        let chunk = json!({"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["finish_reason"], "content_filter");
    }

    #[test]
    fn finish_stop_with_tools_becomes_tool_calls() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":1,"geminiToolCallCount":1});
        let chunk = json!({"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}]});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["finish_reason"], "tool_calls");
    }

    #[test]
    fn usage_included_in_final_chunk() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":0,"geminiToolCallCount":0});
        let chunk = json!({
            "candidates":[{"content":{"parts":[]},"finishReason":"STOP"}],
            "usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}
        });
        let out = translate(chunk, &mut state).unwrap();
        let final_chunk = out.last().unwrap();
        assert_eq!(final_chunk["usage"]["prompt_tokens"], 10);
        assert_eq!(final_chunk["usage"]["completion_tokens"], 5);
        assert_eq!(final_chunk["usage"]["total_tokens"], 15);
    }

    #[test]
    fn thoughts_counted_as_reasoning() {
        let mut state =
            json!({"messageId":"r","model":"g","functionIndex":0,"geminiToolCallCount":0});
        let chunk = json!({
            "candidates":[{"content":{"parts":[]},"finishReason":"STOP"}],
            "usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3,"thoughtsTokenCount":2,"totalTokenCount":15}
        });
        let out = translate(chunk, &mut state).unwrap();
        let final_chunk = out.last().unwrap();
        assert_eq!(final_chunk["usage"]["completion_tokens"], 5);
        assert_eq!(
            final_chunk["usage"]["completion_tokens_details"]["reasoning_tokens"],
            2
        );
    }

    #[test]
    fn antigravity_wrapper_unwrapped() {
        let chunk = json!({
            "response": {
                "responseId": "r1",
                "modelVersion": "gemini-2.0",
                "candidates": [{"content": {"parts": [{"text": "hi"}]}}]
            }
        });
        let mut state = json!({});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[1]["choices"][0]["delta"]["content"], "hi");
    }
}
