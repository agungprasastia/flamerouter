//! openai SSE chunk → claude SSE chunk.
//! Port of open-sse/translator/response/openai-to-claude.js

use serde_json::{Value, json};

fn msg_id(state: &Value) -> String {
    state
        .get("messageId")
        .and_then(|m| m.as_str())
        .map(|s| s.to_string())
        .unwrap_or_else(|| {
            format!(
                "msg_{}",
                std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .map(|d| d.as_millis())
                    .unwrap_or(0)
            )
        })
}

fn model_str(state: &Value) -> String {
    state
        .get("model")
        .and_then(|m| m.as_str())
        .unwrap_or("")
        .to_string()
}

fn ensure_started(state: &mut Value, chunk: &Value) -> Vec<Value> {
    let mut out = Vec::new();
    let started = state
        .get("started")
        .and_then(|b| b.as_bool())
        .unwrap_or(false);
    if started {
        return out;
    }
    // capture id/model from first chunk
    if let Some(id) = chunk.get("id").and_then(|i| i.as_str()) {
        // openai gives "chatcmpl-..." — keep as-is (strip prefix optional)
        state["messageId"] = json!(id);
    }
    if let Some(m) = chunk.get("model") {
        state["model"] = m.clone();
    }
    out.push(json!({
        "type": "message_start",
        "message": {
            "id": msg_id(state),
            "type": "message",
            "role": "assistant",
            "model": model_str(state),
            "content": [],
            "stop_reason": null,
            "stop_sequence": null,
            "usage": {"input_tokens": 0, "output_tokens": 0}
        }
    }));
    state["started"] = json!(true);
    state["contentBlockIndex"] = json!(0);
    state["textBlockStarted"] = json!(false);
    out
}

fn from_openai_finish_claude(reason: &str) -> &'static str {
    match reason {
        "stop" => "end_turn",
        "length" => "max_tokens",
        "tool_calls" => "tool_use",
        _ => "end_turn",
    }
}

pub fn translate(chunk: Value, state: &mut Value) -> Option<Vec<Value>> {
    let mut results = Vec::new();
    results.extend(ensure_started(state, &chunk));

    let choices = chunk.get("choices").and_then(|c| c.as_array())?;
    let choice = choices.first()?;
    let delta = choice.get("delta").cloned().unwrap_or(json!({}));
    let finish_reason = choice.get("finish_reason").and_then(|f| f.as_str());

    // text content delta
    if let Some(content) = delta.get("content").and_then(|c| c.as_str())
        && !content.is_empty() {
            let started = state
                .get("textBlockStarted")
                .and_then(|b| b.as_bool())
                .unwrap_or(false);
            if !started {
                let idx = state
                    .get("contentBlockIndex")
                    .and_then(|i| i.as_u64())
                    .unwrap_or(0);
                results.push(json!({
                    "type": "content_block_start",
                    "index": idx,
                    "content_block": {"type":"text","text":""}
                }));
                state["textBlockStarted"] = json!(true);
            }
            let idx = state
                .get("contentBlockIndex")
                .and_then(|i| i.as_u64())
                .unwrap_or(0);
            results.push(json!({
                "type": "content_block_delta",
                "index": idx,
                "delta": {"type":"text_delta","text": content}
            }));
        }

    // reasoning_content → thinking block
    if let Some(reasoning) = delta.get("reasoning_content").and_then(|c| c.as_str())
        && !reasoning.is_empty() {
            let thinking_started = state
                .get("thinkingBlockStarted")
                .and_then(|b| b.as_bool())
                .unwrap_or(false);
            if !thinking_started {
                let idx = state
                    .get("contentBlockIndex")
                    .and_then(|i| i.as_u64())
                    .unwrap_or(0);
                results.push(json!({
                    "type": "content_block_start",
                    "index": idx,
                    "content_block": {"type":"thinking","thinking":""}
                }));
                state["thinkingBlockStarted"] = json!(true);
            }
            let idx = state
                .get("contentBlockIndex")
                .and_then(|i| i.as_u64())
                .unwrap_or(0);
            results.push(json!({
                "type": "content_block_delta",
                "index": idx,
                "delta": {"type":"thinking_delta","thinking": reasoning}
            }));
        }

    // tool_calls
    if let Some(tcs) = delta.get("tool_calls").and_then(|t| t.as_array()) {
        for tc in tcs {
            let idx = tc.get("index").and_then(|i| i.as_u64()).unwrap_or(0);
            let fn_delta = tc.get("function").cloned().unwrap_or(json!({}));
            // start of new tool call (has name)
            if let Some(name) = fn_delta.get("name").and_then(|n| n.as_str()) {
                let block_idx = state
                    .get("contentBlockIndex")
                    .and_then(|i| i.as_u64())
                    .unwrap_or(0)
                    + 1
                    + idx;
                let id = tc.get("id").and_then(|i| i.as_str()).unwrap_or("");
                results.push(json!({
                    "type":"content_block_start",
                    "index": block_idx,
                    "content_block": {
                        "type":"tool_use",
                        "id": id,
                        "name": name,
                        "input": {}
                    }
                }));
                if !state.get("toolCalls").is_some() {
                    state["toolCalls"] = json!({});
                }
                state["toolCalls"][idx.to_string()] =
                    json!({"blockIndex": block_idx, "id": id, "name": name});
            }
            if let Some(args) = fn_delta.get("arguments").and_then(|a| a.as_str())
                && !args.is_empty() {
                    let block_idx = state
                        .get("toolCalls")
                        .and_then(|t| t.get(idx.to_string()))
                        .and_then(|t| t.get("blockIndex"))
                        .and_then(|b| b.as_u64())
                        .unwrap_or(0);
                    results.push(json!({
                        "type":"content_block_delta",
                        "index": block_idx,
                        "delta": {"type":"input_json_delta","partial_json": args}
                    }));
                }
        }
    }

    // finish_reason
    if let Some(fr) = finish_reason {
        // close any open text/thinking blocks
        let text_open = state
            .get("textBlockStarted")
            .and_then(|b| b.as_bool())
            .unwrap_or(false);
        let thinking_open = state
            .get("thinkingBlockStarted")
            .and_then(|b| b.as_bool())
            .unwrap_or(false);
        let idx = state
            .get("contentBlockIndex")
            .and_then(|i| i.as_u64())
            .unwrap_or(0);
        if text_open || thinking_open {
            results.push(json!({"type":"content_block_stop","index": idx}));
        }

        let stop_reason = from_openai_finish_claude(fr);
        let usage = chunk.get("usage").cloned().unwrap_or(json!({}));
        let input_tokens = usage
            .get("prompt_tokens")
            .and_then(|v| v.as_u64())
            .unwrap_or(0);
        let output_tokens = usage
            .get("completion_tokens")
            .and_then(|v| v.as_u64())
            .unwrap_or(0);
        results.push(json!({
            "type":"message_delta",
            "delta": {"stop_reason": stop_reason, "stop_sequence": null},
            "usage": {"input_tokens": input_tokens, "output_tokens": output_tokens}
        }));
        results.push(json!({"type":"message_stop"}));
    } else if let Some(usage) = chunk.get("usage") {
        // usage-only chunk (rare) — merge into state for later
        state["pendingUsage"] = usage.clone();
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
    fn first_chunk_emits_message_start() {
        let chunk = json!({
            "id":"chatcmpl-1","model":"gpt-4","object":"chat.completion.chunk",
            "choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]
        });
        let mut state = json!({});
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["type"], "message_start");
        assert_eq!(out[0]["message"]["id"], "chatcmpl-1");
    }

    #[test]
    fn text_delta_emits_block_start_then_delta() {
        let mut state = json!({});
        let c1 = json!({"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]});
        translate(c1, &mut state);
        let c2 = json!({"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]});
        let out = translate(c2, &mut state).unwrap();
        let types: Vec<&str> = out.iter().map(|e| e["type"].as_str().unwrap()).collect();
        assert!(types.contains(&"content_block_start"));
        assert!(types.contains(&"content_block_delta"));
        let delta_ev = out
            .iter()
            .find(|e| e["type"] == "content_block_delta")
            .unwrap();
        assert_eq!(delta_ev["delta"]["text"], "hi");
    }

    #[test]
    fn finish_reason_stop_emits_message_delta_and_stop() {
        let mut state = json!({});
        let c1 = json!({"id":"c","model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]});
        translate(c1, &mut state);
        let c2 = json!({"id":"c","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}});
        let out = translate(c2, &mut state).unwrap();
        let types: Vec<&str> = out.iter().map(|e| e["type"].as_str().unwrap()).collect();
        assert!(types.contains(&"message_delta"));
        assert!(types.contains(&"message_stop"));
        let md = out.iter().find(|e| e["type"] == "message_delta").unwrap();
        assert_eq!(md["delta"]["stop_reason"], "end_turn");
        assert_eq!(md["usage"]["output_tokens"], 5);
    }

    #[test]
    fn finish_reason_length_maps_to_max_tokens() {
        let mut state = json!({});
        let c1 = json!({"id":"c","model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]});
        translate(c1, &mut state);
        let c2 = json!({"id":"c","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]});
        let out = translate(c2, &mut state).unwrap();
        let md = out.iter().find(|e| e["type"] == "message_delta").unwrap();
        assert_eq!(md["delta"]["stop_reason"], "max_tokens");
    }
}
