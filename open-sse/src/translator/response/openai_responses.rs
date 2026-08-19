//! openai-responses SSE event → openai chat.completion.chunk.
//! Port of open-sse/translator/response/openai-responses.js (openaiResponsesToOpenAIResponse path).

use serde_json::{Map, Value, json};

fn created_unix() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

fn chat_id(state: &Value) -> String {
    state
        .get("chatId")
        .and_then(|c| c.as_str())
        .map(|s| s.to_string())
        .unwrap_or_else(|| format!("chatcmpl-{}", created_unix()))
}

fn model_str(state: &Value) -> String {
    state
        .get("model")
        .and_then(|m| m.as_str())
        .unwrap_or("unknown")
        .to_string()
}

fn build_chunk(state: &Value, delta: Value, finish_reason: Option<&str>) -> Value {
    json!({
        "id": chat_id(state),
        "object": "chat.completion.chunk",
        "created": state.get("created").and_then(|c| c.as_u64()).unwrap_or_else(created_unix),
        "model": model_str(state),
        "choices": [{
            "index": 0,
            "delta": delta,
            "finish_reason": finish_reason
        }]
    })
}

fn compute_finish_reason(state: &Value) -> &'static str {
    let has_tool = state
        .get("toolCallIndex")
        .and_then(|i| i.as_u64())
        .unwrap_or(0)
        > 0
        || state
            .get("currentToolCallId")
            .and_then(|c| c.as_str())
            .is_some();
    if has_tool { "tool_calls" } else { "stop" }
}

fn build_usage_openai(input: u64, output: u64, cached: u64) -> Value {
    let mut usage = Map::new();
    usage.insert("prompt_tokens".into(), json!(input));
    usage.insert("completion_tokens".into(), json!(output));
    usage.insert("total_tokens".into(), json!(input + output));
    if cached > 0 {
        usage.insert(
            "prompt_tokens_details".into(),
            json!({"cached_tokens": cached}),
        );
    }
    Value::Object(usage)
}

pub fn translate(chunk: Value, state: &mut Value) -> Option<Vec<Value>> {
    let event_type = chunk
        .get("type")
        .or_else(|| chunk.get("event"))
        .and_then(|t| t.as_str())?;
    let data = chunk.get("data").cloned().unwrap_or_else(|| chunk.clone());

    // Initialize state on first event
    if state.get("started").is_none() {
        state["started"] = json!(true);
        state["chatId"] = json!(format!("chatcmpl-{}", created_unix()));
        state["created"] = json!(created_unix());
        state["toolCallIndex"] = json!(0);
    }

    match event_type {
        "response.output_text.delta" => {
            let delta = data.get("delta").and_then(|d| d.as_str())?;
            if delta.is_empty() {
                return None;
            }
            Some(vec![build_chunk(state, json!({"content": delta}), None)])
        }

        "response.output_text.done" => None,

        "response.output_item.added" => {
            let item = data.get("item")?;
            let item_type = item.get("type").and_then(|t| t.as_str())?;
            if item_type != "function_call" && item_type != "custom_tool_call" {
                return None;
            }
            let call_id = item
                .get("call_id")
                .and_then(|c| c.as_str())
                .map(|s| s.to_string())
                .unwrap_or_else(|| format!("call_{}", created_unix()));
            state["currentToolCallId"] = json!(call_id);
            let tc_idx = state
                .get("toolCallIndex")
                .and_then(|i| i.as_u64())
                .unwrap_or(0);
            let name = item.get("name").and_then(|n| n.as_str()).unwrap_or("");
            Some(vec![build_chunk(
                state,
                json!({
                    "tool_calls": [{
                        "index": tc_idx,
                        "id": call_id,
                        "type": "function",
                        "function": {"name": name, "arguments": ""}
                    }]
                }),
                None,
            )])
        }

        "response.function_call_arguments.delta" | "response.custom_tool_call_input.delta" => {
            let args_delta = data.get("delta").and_then(|d| d.as_str())?;
            if args_delta.is_empty() {
                return None;
            }
            let tc_idx = state
                .get("toolCallIndex")
                .and_then(|i| i.as_u64())
                .unwrap_or(0);
            Some(vec![build_chunk(
                state,
                json!({"tool_calls": [{"index": tc_idx, "function": {"arguments": args_delta}}]}),
                None,
            )])
        }

        "response.output_item.done" => {
            let item = data.get("item")?;
            let item_type = item.get("type").and_then(|t| t.as_str())?;
            if item_type == "function_call" || item_type == "custom_tool_call" {
                let idx = state
                    .get("toolCallIndex")
                    .and_then(|i| i.as_u64())
                    .unwrap_or(0);
                state["toolCallIndex"] = json!(idx + 1);
            }
            None
        }

        "response.completed" | "response.done" => {
            // Extract usage
            if let Some(usage) = data.get("response").and_then(|r| r.get("usage")) {
                let input = usage
                    .get("input_tokens")
                    .or_else(|| usage.get("prompt_tokens"))
                    .and_then(|v| v.as_u64())
                    .unwrap_or(0);
                let output = usage
                    .get("output_tokens")
                    .or_else(|| usage.get("completion_tokens"))
                    .and_then(|v| v.as_u64())
                    .unwrap_or(0);
                let cached = usage
                    .get("input_tokens_details")
                    .and_then(|d| d.get("cached_tokens"))
                    .and_then(|v| v.as_u64())
                    .or_else(|| {
                        usage
                            .get("cache_read_input_tokens")
                            .and_then(|v| v.as_u64())
                    })
                    .unwrap_or(0);
                state["usage"] = build_usage_openai(input, output, cached);
            }

            if state
                .get("finishReasonSent")
                .and_then(|b| b.as_bool())
                .unwrap_or(false)
            {
                return None;
            }
            let finish = compute_finish_reason(state);
            state["finishReasonSent"] = json!(true);
            let mut final_chunk = build_chunk(state, json!({}), Some(finish));
            if let Some(u) = state.get("usage") {
                final_chunk["usage"] = u.clone();
            }
            Some(vec![final_chunk])
        }

        "error" | "response.failed" => {
            if state
                .get("finishReasonSent")
                .and_then(|b| b.as_bool())
                .unwrap_or(false)
            {
                return None;
            }
            let error = data
                .get("error")
                .or_else(|| data.get("response").and_then(|r| r.get("error")))?;
            let msg = error
                .get("message")
                .and_then(|m| m.as_str())
                .map(|s| s.to_string())
                .unwrap_or_else(|| serde_json::to_string(error).unwrap_or_default());
            state["finishReasonSent"] = json!(true);
            Some(vec![build_chunk(
                state,
                json!({"content": format!("[Error] {}", msg)}),
                Some("stop"),
            )])
        }

        "response.reasoning_summary_text.delta" => {
            let delta = data.get("delta").and_then(|d| d.as_str())?;
            if delta.is_empty() {
                return None;
            }
            Some(vec![build_chunk(
                state,
                json!({"reasoning_content": delta}),
                None,
            )])
        }

        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn init_state() -> Value {
        json!({})
    }

    #[test]
    fn text_delta_becomes_content() {
        let chunk = json!({"type":"response.output_text.delta","data":{"delta":"hello"}});
        let mut state = init_state();
        let out = translate(chunk, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["delta"]["content"], "hello");
    }

    #[test]
    fn function_call_started() {
        let chunk = json!({
            "type":"response.output_item.added",
            "data":{"item":{"type":"function_call","call_id":"c1","name":"search"}}
        });
        let mut state = init_state();
        let out = translate(chunk, &mut state).unwrap();
        let tc = &out[0]["choices"][0]["delta"]["tool_calls"][0];
        assert_eq!(tc["id"], "c1");
        assert_eq!(tc["function"]["name"], "search");
    }

    #[test]
    fn function_call_args_delta() {
        let mut state = init_state();
        // Start
        let c1 = json!({"type":"response.output_item.added","data":{"item":{"type":"function_call","call_id":"c1","name":"s"}}});
        translate(c1, &mut state);
        // Args
        let c2 =
            json!({"type":"response.function_call_arguments.delta","data":{"delta":"{\"q\":"}});
        let out = translate(c2, &mut state).unwrap();
        assert_eq!(
            out[0]["choices"][0]["delta"]["tool_calls"][0]["function"]["arguments"],
            "{\"q\":"
        );
    }

    #[test]
    fn function_call_done_increments_index() {
        let mut state = init_state();
        let c1 = json!({"type":"response.output_item.added","data":{"item":{"type":"function_call","call_id":"c1","name":"s"}}});
        translate(c1, &mut state);
        let c2 =
            json!({"type":"response.output_item.done","data":{"item":{"type":"function_call"}}});
        translate(c2, &mut state);
        assert_eq!(state["toolCallIndex"], 1);
    }

    #[test]
    fn completed_emits_finish_stop() {
        let mut state = init_state();
        let c = json!({"type":"response.completed","data":{"response":{"usage":{"input_tokens":10,"output_tokens":5}}}});
        let out = translate(c, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["finish_reason"], "stop");
        assert_eq!(out[0]["usage"]["prompt_tokens"], 10);
        assert_eq!(out[0]["usage"]["completion_tokens"], 5);
    }

    #[test]
    fn completed_with_tools_emits_tool_calls_finish() {
        let mut state = init_state();
        let c1 = json!({"type":"response.output_item.added","data":{"item":{"type":"function_call","call_id":"c1","name":"s"}}});
        translate(c1, &mut state);
        let c2 = json!({"type":"response.completed","data":{}});
        let out = translate(c2, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["finish_reason"], "tool_calls");
    }

    #[test]
    fn cached_tokens_in_usage() {
        let mut state = init_state();
        let c = json!({
            "type":"response.completed",
            "data":{"response":{"usage":{"input_tokens":10,"output_tokens":5,"input_tokens_details":{"cached_tokens":3}}}}
        });
        let out = translate(c, &mut state).unwrap();
        assert_eq!(out[0]["usage"]["prompt_tokens_details"]["cached_tokens"], 3);
    }

    #[test]
    fn error_becomes_content_with_stop() {
        let mut state = init_state();
        let c = json!({"type":"error","data":{"error":{"message":"model_not_found"}}});
        let out = translate(c, &mut state).unwrap();
        assert_eq!(
            out[0]["choices"][0]["delta"]["content"],
            "[Error] model_not_found"
        );
        assert_eq!(out[0]["choices"][0]["finish_reason"], "stop");
    }

    #[test]
    fn reasoning_summary_becomes_reasoning_content() {
        let mut state = init_state();
        let c = json!({"type":"response.reasoning_summary_text.delta","data":{"delta":"hmm"}});
        let out = translate(c, &mut state).unwrap();
        assert_eq!(out[0]["choices"][0]["delta"]["reasoning_content"], "hmm");
    }

    #[test]
    fn completed_only_once() {
        let mut state = init_state();
        let c = json!({"type":"response.completed","data":{}});
        translate(c.clone(), &mut state);
        let out2 = translate(c, &mut state);
        assert!(out2.is_none());
    }
}
