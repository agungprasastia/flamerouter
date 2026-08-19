//! claude SSE chunk → openai chat.completion.chunk.
//! Port of open-sse/translator/response/claude-to-openai.js

use serde_json::{Map, Value, json};

use crate::translator::schema::{claude_block as CB, openai_block as OB, role as ROLE};

fn created_unix() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

fn build_chunk(state: &Value, delta: Value, finish_reason: Option<&str>) -> Value {
    let msg_id = state
        .get("messageId")
        .and_then(|m| m.as_str())
        .unwrap_or("unknown");
    let model = state.get("model").and_then(|m| m.as_str()).unwrap_or("");
    json!({
        "id": format!("chatcmpl-{}", msg_id),
        "object": "chat.completion.chunk",
        "created": created_unix(),
        "model": model,
        "choices": [{
            "index": 0,
            "delta": delta,
            "finish_reason": finish_reason
        }]
    })
}

fn to_openai_finish_claude(reason: &str) -> &'static str {
    match reason {
        "end_turn" => "stop",
        "max_tokens" => "length",
        "tool_use" => "tool_calls",
        "stop_sequence" => "stop",
        _ => "stop",
    }
}

fn num(v: Option<&Value>) -> u64 {
    v.and_then(|x| x.as_u64()).unwrap_or(0)
}

fn build_claude_usage(input: u64, output: u64, cache_read: u64, cache_creation: u64) -> Value {
    let prompt = input + cache_read + cache_creation;
    let mut usage = Map::new();
    usage.insert("prompt_tokens".into(), json!(prompt));
    usage.insert("completion_tokens".into(), json!(output));
    usage.insert("total_tokens".into(), json!(prompt + output));
    if cache_read > 0 || cache_creation > 0 {
        let mut details = Map::new();
        if cache_read > 0 {
            details.insert("cached_tokens".into(), json!(cache_read));
        }
        if cache_creation > 0 {
            details.insert("cache_creation_tokens".into(), json!(cache_creation));
        }
        usage.insert("prompt_tokens_details".into(), Value::Object(details));
    }
    Value::Object(usage)
}

pub fn translate(chunk: Value, state: &mut Value) -> Option<Vec<Value>> {
    let event = chunk.get("type").and_then(|t| t.as_str())?;
    let mut results: Vec<Value> = Vec::new();

    match event {
        "message_start" => {
            let msg = chunk.get("message").cloned().unwrap_or(Value::Null);
            let id = msg
                .get("id")
                .and_then(|i| i.as_str())
                .map(|s| s.to_string())
                .unwrap_or_else(|| format!("msg_{}", created_unix()));
            state["messageId"] = json!(id);
            if let Some(m) = msg.get("model") {
                state["model"] = m.clone();
            }
            state["toolCallIndex"] = json!(0);

            if let Some(usage) = msg.get("usage") {
                let input = num(usage.get("input_tokens"));
                let cache_read = num(usage.get("cache_read_input_tokens"));
                let cache_creation = num(usage.get("cache_creation_input_tokens"));
                let prompt = input + cache_read + cache_creation;
                let mut u = Map::new();
                u.insert("prompt_tokens".into(), json!(prompt));
                u.insert("completion_tokens".into(), json!(0));
                u.insert("total_tokens".into(), json!(prompt));
                u.insert("input_tokens".into(), json!(input));
                u.insert("output_tokens".into(), json!(0));
                if cache_read > 0 {
                    u.insert("cache_read_input_tokens".into(), json!(cache_read));
                }
                if cache_creation > 0 {
                    u.insert("cache_creation_input_tokens".into(), json!(cache_creation));
                }
                state["usage"] = Value::Object(u);
            }

            results.push(build_chunk(state, json!({ "role": ROLE::ASSISTANT }), None));
        }
        "content_block_start" => {
            let block = chunk.get("content_block").cloned().unwrap_or(Value::Null);
            let btype = block.get("type").and_then(|t| t.as_str()).unwrap_or("");
            let idx = chunk.get("index").and_then(|i| i.as_u64()).unwrap_or(0);

            if btype == "server_tool_use" {
                state["serverToolBlockIndex"] = json!(idx);
            } else if btype == CB::TEXT {
                state["textBlockStarted"] = json!(true);
            } else if btype == CB::THINKING {
                state["inThinkingBlock"] = json!(true);
                state["currentBlockIndex"] = json!(idx);
                results.push(build_chunk(state, json!({ "content": "<think>" }), None));
            } else if btype == CB::TOOL_USE {
                let tc_idx = state
                    .get("toolCallIndex")
                    .and_then(|t| t.as_u64())
                    .unwrap_or(0);
                state["toolCallIndex"] = json!(tc_idx + 1);
                let tool_name = block
                    .get("name")
                    .and_then(|n| n.as_str())
                    .unwrap_or("")
                    .to_string();
                let tool_call = json!({
                    "index": tc_idx,
                    "id": block.get("id").cloned().unwrap_or(Value::Null),
                    "type": OB::FUNCTION,
                    "function": { "name": tool_name, "arguments": "" }
                });
                // track in state.toolCalls (object keyed by chunk index)
                if !state.get("toolCalls").is_some() {
                    state["toolCalls"] = json!({});
                }
                state["toolCalls"][idx.to_string()] = tool_call.clone();
                results.push(build_chunk(
                    state,
                    json!({ "tool_calls": [tool_call] }),
                    None,
                ));
            }
        }
        "content_block_delta" => {
            let idx = chunk.get("index").and_then(|i| i.as_u64()).unwrap_or(0);
            let server_idx = state.get("serverToolBlockIndex").and_then(|i| i.as_u64());
            if server_idx == Some(idx) {
                return if results.is_empty() {
                    None
                } else {
                    Some(results)
                };
            }
            let delta = chunk.get("delta").cloned().unwrap_or(Value::Null);
            let dtype = delta.get("type").and_then(|t| t.as_str()).unwrap_or("");

            if dtype == "text_delta" {
                if let Some(text) = delta.get("text").and_then(|t| t.as_str()) {
                    results.push(build_chunk(state, json!({ "content": text }), None));
                }
            } else if dtype == "thinking_delta" {
                if let Some(thinking) = delta.get("thinking").and_then(|t| t.as_str()) {
                    results.push(build_chunk(
                        state,
                        json!({ "reasoning_content": thinking }),
                        None,
                    ));
                }
            } else if dtype == "input_json_delta"
                && let Some(partial) = delta.get("partial_json").and_then(|t| t.as_str())
                    && let Some(tc) = state
                        .get_mut("toolCalls")
                        .and_then(|t| t.get_mut(idx.to_string()))
                    {
                        // Append to tracked arguments
                        let cur = tc["function"]["arguments"]
                            .as_str()
                            .unwrap_or("")
                            .to_string();
                        tc["function"]["arguments"] = json!(format!("{}{}", cur, partial));
                        let tc_clone = tc.clone();
                        results.push(build_chunk(
                            state,
                            json!({
                                "tool_calls": [{
                                    "index": tc_clone["index"],
                                    "id": tc_clone["id"],
                                    "function": { "arguments": partial }
                                }]
                            }),
                            None,
                        ));
                    }
        }
        "content_block_stop" => {
            let idx = chunk.get("index").and_then(|i| i.as_u64()).unwrap_or(0);
            let server_idx = state.get("serverToolBlockIndex").and_then(|i| i.as_u64());
            if server_idx == Some(idx) {
                state["serverToolBlockIndex"] = json!(-1);
                return if results.is_empty() {
                    None
                } else {
                    Some(results)
                };
            }
            let in_thinking = state
                .get("inThinkingBlock")
                .and_then(|b| b.as_bool())
                .unwrap_or(false);
            let current_block = state.get("currentBlockIndex").and_then(|i| i.as_u64());
            if in_thinking && current_block == Some(idx) {
                results.push(build_chunk(state, json!({ "content": "</think>" }), None));
                state["inThinkingBlock"] = json!(false);
            }
            state["textBlockStarted"] = json!(false);
            state["thinkingBlockStarted"] = json!(false);
        }
        "message_delta" => {
            if let Some(usage) = chunk.get("usage") {
                let prev = state.get("usage").cloned().unwrap_or(json!({}));
                let input = usage
                    .get("input_tokens")
                    .and_then(|v| v.as_u64())
                    .unwrap_or_else(|| num(prev.get("input_tokens")));
                let output = num(usage.get("output_tokens"));
                let cache_read = usage
                    .get("cache_read_input_tokens")
                    .and_then(|v| v.as_u64())
                    .unwrap_or_else(|| num(prev.get("cache_read_input_tokens")));
                let cache_creation = usage
                    .get("cache_creation_input_tokens")
                    .and_then(|v| v.as_u64())
                    .unwrap_or_else(|| num(prev.get("cache_creation_input_tokens")));
                let prompt = input + cache_read + cache_creation;

                let mut u = Map::new();
                u.insert("prompt_tokens".into(), json!(prompt));
                u.insert("completion_tokens".into(), json!(output));
                u.insert("total_tokens".into(), json!(prompt + output));
                u.insert("input_tokens".into(), json!(input));
                u.insert("output_tokens".into(), json!(output));
                if cache_read > 0 {
                    u.insert("cache_read_input_tokens".into(), json!(cache_read));
                }
                if cache_creation > 0 {
                    u.insert("cache_creation_input_tokens".into(), json!(cache_creation));
                }
                state["usage"] = Value::Object(u);
            }

            if let Some(sr) = chunk
                .get("delta")
                .and_then(|d| d.get("stop_reason"))
                .and_then(|s| s.as_str())
            {
                let finish = to_openai_finish_claude(sr);
                state["finishReason"] = json!(finish);
                let mut final_chunk = build_chunk(state, json!({}), Some(finish));
                if let Some(u) = state.get("usage") {
                    let input = num(u.get("input_tokens"));
                    let output = num(u.get("output_tokens"));
                    let cache_read = num(u.get("cache_read_input_tokens"));
                    let cache_creation = num(u.get("cache_creation_input_tokens"));
                    final_chunk["usage"] =
                        build_claude_usage(input, output, cache_read, cache_creation);
                }
                results.push(final_chunk);
                state["finishReasonSent"] = json!(true);
            }
        }
        "message_stop" => {
            let already = state
                .get("finishReasonSent")
                .and_then(|b| b.as_bool())
                .unwrap_or(false);
            if !already {
                let tool_calls_count = state
                    .get("toolCalls")
                    .and_then(|t| t.as_object())
                    .map(|o| o.len())
                    .unwrap_or(0);
                let finish = state
                    .get("finishReason")
                    .and_then(|f| f.as_str())
                    .map(|s| s.to_string())
                    .unwrap_or_else(|| {
                        if tool_calls_count > 0 {
                            "tool_calls".to_string()
                        } else {
                            "stop".to_string()
                        }
                    });
                let mut final_chunk = build_chunk(state, json!({}), Some(&finish));
                if let Some(u) = state.get("usage") {
                    let input = num(u.get("input_tokens"));
                    let output = num(u.get("output_tokens"));
                    final_chunk["usage"] = json!({
                        "prompt_tokens": input,
                        "completion_tokens": output,
                        "total_tokens": input + output
                    });
                }
                results.push(final_chunk);
                state["finishReasonSent"] = json!(true);
            }
        }
        _ => {}
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
    fn message_start_emits_role_chunk() {
        let chunk = json!({
            "type": "message_start",
            "message": {"id":"msg_1","model":"claude-opus-4-6","usage":{"input_tokens":10}}
        });
        let mut state = json!({});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out.len(), 1);
        assert_eq!(out[0]["choices"][0]["delta"]["role"], "assistant");
        assert_eq!(out[0]["id"], "chatcmpl-msg_1");
        assert_eq!(state["usage"]["prompt_tokens"], 10);
    }

    #[test]
    fn text_delta_flows_through() {
        let mut state = json!({"messageId":"msg_1","model":"claude"});
        let chunk = json!({"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["delta"]["content"], "hi");
    }

    #[test]
    fn thinking_delta_becomes_reasoning_content() {
        let mut state = json!({"messageId":"msg_1","model":"claude"});
        let chunk = json!({"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["delta"]["reasoning_content"], "hmm");
    }

    #[test]
    fn tool_use_start_then_args() {
        let mut state = json!({"messageId":"msg_1","model":"claude","toolCallIndex":0});
        let start = json!({
            "type":"content_block_start","index":1,
            "content_block":{"type":"tool_use","id":"tu_1","name":"get_weather"}
        });
        let out = translate(start, &mut state).unwrap();
        assert_eq!(
            out[0]["choices"][0]["delta"]["tool_calls"][0]["function"]["name"],
            "get_weather"
        );

        let delta = json!({
            "type":"content_block_delta","index":1,
            "delta":{"type":"input_json_delta","partial_json":"{\"city\":"}
        });
        let out = translate(delta, &mut state).unwrap();
        assert_eq!(
            out[0]["choices"][0]["delta"]["tool_calls"][0]["function"]["arguments"],
            "{\"city\":"
        );
        // accumulated in state
        assert_eq!(
            state["toolCalls"]["1"]["function"]["arguments"],
            "{\"city\":"
        );
    }

    #[test]
    fn message_delta_stop_reason_and_usage() {
        let mut state = json!({
            "messageId":"msg_1","model":"claude",
            "usage":{"input_tokens":10,"output_tokens":0,"prompt_tokens":10,"completion_tokens":0,"total_tokens":10}
        });
        let chunk = json!({
            "type":"message_delta",
            "delta":{"stop_reason":"end_turn"},
            "usage":{"output_tokens":5}
        });
        let out = translate(chunk, &mut state).unwrap();
        let last = &out[out.len() - 1];
        assert_eq!(last["choices"][0]["finish_reason"], "stop");
        assert_eq!(last["usage"]["prompt_tokens"], 10);
        assert_eq!(last["usage"]["completion_tokens"], 5);
        assert_eq!(last["usage"]["total_tokens"], 15);
    }

    #[test]
    fn stop_reason_max_tokens_maps_to_length() {
        let mut state =
            json!({"messageId":"m","model":"c","usage":{"input_tokens":1,"output_tokens":1}});
        let chunk = json!({"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":1}});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["finish_reason"], "length");
    }

    #[test]
    fn stop_reason_tool_use_maps_to_tool_calls() {
        let mut state =
            json!({"messageId":"m","model":"c","usage":{"input_tokens":1,"output_tokens":1}});
        let chunk = json!({"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":1}});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["finish_reason"], "tool_calls");
    }
}
