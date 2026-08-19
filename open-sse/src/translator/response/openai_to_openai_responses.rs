//! openai chat.completion.chunk → openai-responses SSE events.
//! Port of open-sse/translator/response/openai-responses.js (openaiToOpenAIResponsesResponse path).

use serde_json::{Value, json};

fn created_unix() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

fn next_seq(state: &mut Value) -> u64 {
    let seq = state.get("seq").and_then(|s| s.as_u64()).unwrap_or(0) + 1;
    state["seq"] = json!(seq);
    seq
}

fn emit(events: &mut Vec<Value>, state: &mut Value, event_type: &str, mut data: Value) {
    data["sequence_number"] = json!(next_seq(state));
    events.push(json!({"event": event_type, "data": data}));
}

fn response_id(state: &Value) -> String {
    state
        .get("responseId")
        .and_then(|r| r.as_str())
        .unwrap_or("resp_unknown")
        .to_string()
}

/// Extract reasoning text from delta (reasoning_content / reasoning / reasoning_details)
fn extract_reasoning_text(delta: &Value) -> String {
    if let Some(rc) = delta.get("reasoning_content").and_then(|r| r.as_str())
        && !rc.is_empty() {
            return rc.to_string();
        }
    if let Some(r) = delta.get("reasoning").and_then(|r| r.as_str())
        && !r.is_empty() {
            return r.to_string();
        }
    if let Some(details) = delta.get("reasoning_details").and_then(|r| r.as_array()) {
        let txt: String = details
            .iter()
            .map(|d| {
                if d.is_string() {
                    d.as_str().unwrap_or("").to_string()
                } else {
                    d.get("text")
                        .or_else(|| d.get("content"))
                        .and_then(|t| t.as_str())
                        .unwrap_or("")
                        .to_string()
                }
            })
            .collect::<Vec<_>>()
            .join("");
        if !txt.is_empty() {
            return txt;
        }
    }
    String::new()
}

fn start_reasoning(state: &mut Value, events: &mut Vec<Value>, idx: u64) {
    if state.get("reasoningId").is_some() {
        return;
    }
    let resp_id = response_id(state);
    let reasoning_id = format!("rs_{}_{}", resp_id, idx);
    state["reasoningId"] = json!(reasoning_id);
    state["reasoningIndex"] = json!(idx);
    state["reasoningBuf"] = json!("");

    emit(
        events,
        state,
        "response.output_item.added",
        json!({
            "type": "response.output_item.added",
            "output_index": idx,
            "item": { "id": reasoning_id, "type": "reasoning", "summary": [] }
        }),
    );

    emit(
        events,
        state,
        "response.reasoning_summary_part.added",
        json!({
            "type": "response.reasoning_summary_part.added",
            "item_id": reasoning_id,
            "output_index": idx,
            "summary_index": 0,
            "part": { "type": "summary_text", "text": "" }
        }),
    );
}

fn emit_reasoning_delta(state: &mut Value, events: &mut Vec<Value>, text: &str) {
    if text.is_empty() {
        return;
    }
    // Append to buffer
    let buf = state
        .get("reasoningBuf")
        .and_then(|b| b.as_str())
        .unwrap_or("")
        .to_string()
        + text;
    state["reasoningBuf"] = json!(buf);

    let reasoning_id = state
        .get("reasoningId")
        .and_then(|r| r.as_str())
        .unwrap_or("")
        .to_string();
    let reasoning_index = state
        .get("reasoningIndex")
        .and_then(|i| i.as_u64())
        .unwrap_or(0);

    emit(
        events,
        state,
        "response.reasoning_summary_text.delta",
        json!({
            "type": "response.reasoning_summary_text.delta",
            "item_id": reasoning_id,
            "output_index": reasoning_index,
            "summary_index": 0,
            "delta": text
        }),
    );
}

fn close_reasoning(state: &mut Value, events: &mut Vec<Value>) {
    if state.get("reasoningId").is_none() {
        return;
    }
    if state
        .get("reasoningDone")
        .and_then(|b| b.as_bool())
        .unwrap_or(false)
    {
        return;
    }
    state["reasoningDone"] = json!(true);

    let reasoning_id = state
        .get("reasoningId")
        .and_then(|r| r.as_str())
        .unwrap_or("")
        .to_string();
    let reasoning_index = state
        .get("reasoningIndex")
        .and_then(|i| i.as_u64())
        .unwrap_or(0);
    let buf = state
        .get("reasoningBuf")
        .and_then(|b| b.as_str())
        .unwrap_or("")
        .to_string();

    emit(
        events,
        state,
        "response.reasoning_summary_text.done",
        json!({
            "type": "response.reasoning_summary_text.done",
            "item_id": reasoning_id,
            "output_index": reasoning_index,
            "summary_index": 0,
            "text": buf
        }),
    );

    emit(
        events,
        state,
        "response.reasoning_summary_part.done",
        json!({
            "type": "response.reasoning_summary_part.done",
            "item_id": reasoning_id,
            "output_index": reasoning_index,
            "summary_index": 0,
            "part": { "type": "summary_text", "text": buf }
        }),
    );

    emit(
        events,
        state,
        "response.output_item.done",
        json!({
            "type": "response.output_item.done",
            "output_index": reasoning_index,
            "item": {
                "id": reasoning_id,
                "type": "reasoning",
                "summary": [{ "type": "summary_text", "text": buf }]
            }
        }),
    );
}

fn emit_text_content(state: &mut Value, events: &mut Vec<Value>, idx: u64, content: &str) {
    let idx_key = idx.to_string();
    let resp_id = response_id(state);
    let msg_id = format!("msg_{}_{}", resp_id, idx);

    // Track added items per index
    let added = state
        .get("msgItemAdded")
        .and_then(|m| m.get(&idx_key))
        .and_then(|b| b.as_bool())
        .unwrap_or(false);
    if !added {
        state["msgItemAdded"][&idx_key] = json!(true);
        emit(
            events,
            state,
            "response.output_item.added",
            json!({
                "type": "response.output_item.added",
                "output_index": idx,
                "item": { "id": msg_id, "type": "message", "content": [], "role": "assistant" }
            }),
        );
    }

    let content_added = state
        .get("msgContentAdded")
        .and_then(|m| m.get(&idx_key))
        .and_then(|b| b.as_bool())
        .unwrap_or(false);
    if !content_added {
        state["msgContentAdded"][&idx_key] = json!(true);
        emit(
            events,
            state,
            "response.content_part.added",
            json!({
                "type": "response.content_part.added",
                "item_id": msg_id,
                "output_index": idx,
                "content_index": 0,
                "part": { "type": "output_text", "annotations": [], "logprobs": [], "text": "" }
            }),
        );
    }

    emit(
        events,
        state,
        "response.output_text.delta",
        json!({
            "type": "response.output_text.delta",
            "item_id": msg_id,
            "output_index": idx,
            "content_index": 0,
            "delta": content,
            "logprobs": []
        }),
    );

    // Accumulate text buffer
    let buf = state
        .get("msgTextBuf")
        .and_then(|m| m.get(&idx_key))
        .and_then(|b| b.as_str())
        .unwrap_or("")
        .to_string()
        + content;
    state["msgTextBuf"][&idx_key] = json!(buf);
}

fn close_message(state: &mut Value, events: &mut Vec<Value>, idx: u64) {
    let idx_key = idx.to_string();
    let added = state
        .get("msgItemAdded")
        .and_then(|m| m.get(&idx_key))
        .and_then(|b| b.as_bool())
        .unwrap_or(false);
    let done = state
        .get("msgItemDone")
        .and_then(|m| m.get(&idx_key))
        .and_then(|b| b.as_bool())
        .unwrap_or(false);
    if !added || done {
        return;
    }
    state["msgItemDone"][&idx_key] = json!(true);

    let resp_id = response_id(state);
    let msg_id = format!("msg_{}_{}", resp_id, idx);
    let full_text = state
        .get("msgTextBuf")
        .and_then(|m| m.get(&idx_key))
        .and_then(|b| b.as_str())
        .unwrap_or("")
        .to_string();

    emit(
        events,
        state,
        "response.output_text.done",
        json!({
            "type": "response.output_text.done",
            "item_id": msg_id,
            "output_index": idx,
            "content_index": 0,
            "text": full_text,
            "logprobs": []
        }),
    );

    emit(
        events,
        state,
        "response.content_part.done",
        json!({
            "type": "response.content_part.done",
            "item_id": msg_id,
            "output_index": idx,
            "content_index": 0,
            "part": { "type": "output_text", "annotations": [], "logprobs": [], "text": full_text }
        }),
    );

    emit(
        events,
        state,
        "response.output_item.done",
        json!({
            "type": "response.output_item.done",
            "output_index": idx,
            "item": {
                "id": msg_id,
                "type": "message",
                "content": [{ "type": "output_text", "annotations": [], "logprobs": [], "text": full_text }],
                "role": "assistant"
            }
        }),
    );
}

fn is_custom_tool(state: &Value, name: &str) -> bool {
    if name.is_empty() {
        return false;
    }
    state
        .get("customToolNames")
        .and_then(|s| s.as_array())
        .map(|arr| arr.iter().any(|v| v.as_str() == Some(name)))
        .unwrap_or(false)
}

fn extract_custom_tool_input(args: &str) -> String {
    if let Ok(parsed) = serde_json::from_str::<Value>(args)
        && let Some(obj) = parsed.as_object()
            && let Some(input) = obj.get("input").and_then(|i| i.as_str()) {
                return input.to_string();
            }
    args.to_string()
}

fn emit_tool_call(state: &mut Value, events: &mut Vec<Value>, tc: &Value) {
    let tc_idx = tc.get("index").and_then(|i| i.as_u64()).unwrap_or(0);
    let tc_idx_key = tc_idx.to_string();
    let new_call_id = tc.get("id").and_then(|i| i.as_str());
    let func_name = tc
        .get("function")
        .and_then(|f| f.get("name"))
        .and_then(|n| n.as_str());

    if let Some(name) = func_name {
        state["funcNames"][&tc_idx_key] = json!(name);
    }
    if let Some(cid) = new_call_id {
        state["funcCallIds"][&tc_idx_key] = json!(cid);
    }

    let call_id = state
        .get("funcCallIds")
        .and_then(|m| m.get(&tc_idx_key))
        .and_then(|c| c.as_str())
        .map(|s| s.to_string());
    let stored_name = state
        .get("funcNames")
        .and_then(|m| m.get(&tc_idx_key))
        .and_then(|n| n.as_str())
        .map(|s| s.to_string());
    let item_added = state
        .get("funcItemAdded")
        .and_then(|m| m.get(&tc_idx_key))
        .and_then(|b| b.as_bool())
        .unwrap_or(false);

    // Wait for both call_id and name before announcing
    if !item_added
        && let (Some(cid), Some(name)) = (&call_id, &stored_name) {
            state["funcItemAdded"][&tc_idx_key] = json!(true);
            let custom = is_custom_tool(state, name);
            let prefix = if custom { "ctc" } else { "fc" };
            let item_type = if custom {
                "custom_tool_call"
            } else {
                "function_call"
            };
            let mut item = serde_json::Map::new();
            item.insert("id".into(), json!(format!("{}_{}", prefix, cid)));
            item.insert("type".into(), json!(item_type));
            if custom {
                item.insert("input".into(), json!(""));
            } else {
                item.insert("arguments".into(), json!(""));
            }
            item.insert("call_id".into(), json!(cid));
            item.insert("name".into(), json!(name));

            emit(
                events,
                state,
                "response.output_item.added",
                json!({
                    "type": "response.output_item.added",
                    "output_index": tc_idx,
                    "item": Value::Object(item)
                }),
            );
        }

    // Init args buffer
    if state
        .get("funcArgsBuf")
        .and_then(|m| m.get(&tc_idx_key))
        .is_none()
    {
        state["funcArgsBuf"][&tc_idx_key] = json!("");
    }

    if let Some(args) = tc
        .get("function")
        .and_then(|f| f.get("arguments"))
        .and_then(|a| a.as_str())
        && !args.is_empty() {
            let ref_call_id = state
                .get("funcCallIds")
                .and_then(|m| m.get(&tc_idx_key))
                .and_then(|c| c.as_str())
                .map(|s| s.to_string());
            let name = state
                .get("funcNames")
                .and_then(|m| m.get(&tc_idx_key))
                .and_then(|n| n.as_str())
                .unwrap_or("");
            let item_added_now = state
                .get("funcItemAdded")
                .and_then(|m| m.get(&tc_idx_key))
                .and_then(|b| b.as_bool())
                .unwrap_or(false);

            if item_added_now && ref_call_id.is_some() && !is_custom_tool(state, name) {
                let cid = ref_call_id.unwrap();
                emit(
                    events,
                    state,
                    "response.function_call_arguments.delta",
                    json!({
                        "type": "response.function_call_arguments.delta",
                        "item_id": format!("fc_{}", cid),
                        "output_index": tc_idx,
                        "delta": args
                    }),
                );
            }
            // Accumulate
            let buf = state
                .get("funcArgsBuf")
                .and_then(|m| m.get(&tc_idx_key))
                .and_then(|b| b.as_str())
                .unwrap_or("")
                .to_string()
                + args;
            state["funcArgsBuf"][&tc_idx_key] = json!(buf);
        }
}

fn close_tool_call(state: &mut Value, events: &mut Vec<Value>, idx_key: &str) {
    let call_id = match state
        .get("funcCallIds")
        .and_then(|m| m.get(idx_key))
        .and_then(|c| c.as_str())
    {
        Some(c) => c.to_string(),
        None => return,
    };
    let done = state
        .get("funcItemDone")
        .and_then(|m| m.get(idx_key))
        .and_then(|b| b.as_bool())
        .unwrap_or(false);
    if done {
        return;
    }

    let args = state
        .get("funcArgsBuf")
        .and_then(|m| m.get(idx_key))
        .and_then(|b| b.as_str())
        .unwrap_or("{}")
        .to_string();
    let name = state
        .get("funcNames")
        .and_then(|m| m.get(idx_key))
        .and_then(|n| n.as_str())
        .unwrap_or("")
        .to_string();
    let custom = is_custom_tool(state, &name);
    let idx: u64 = idx_key.parse().unwrap_or(0);

    if custom {
        let input = extract_custom_tool_input(&args);
        emit(
            events,
            state,
            "response.custom_tool_call_input.delta",
            json!({
                "type": "response.custom_tool_call_input.delta",
                "item_id": format!("ctc_{}", call_id),
                "output_index": idx,
                "delta": input
            }),
        );
        emit(
            events,
            state,
            "response.custom_tool_call_input.done",
            json!({
                "type": "response.custom_tool_call_input.done",
                "item_id": format!("ctc_{}", call_id),
                "output_index": idx,
                "input": input
            }),
        );
    } else {
        emit(
            events,
            state,
            "response.function_call_arguments.done",
            json!({
                "type": "response.function_call_arguments.done",
                "item_id": format!("fc_{}", call_id),
                "output_index": idx,
                "arguments": args
            }),
        );
    }

    let prefix = if custom { "ctc" } else { "fc" };
    let item_type = if custom {
        "custom_tool_call"
    } else {
        "function_call"
    };
    let mut item = serde_json::Map::new();
    item.insert("id".into(), json!(format!("{}_{}", prefix, call_id)));
    item.insert("type".into(), json!(item_type));
    if custom {
        item.insert("input".into(), json!(extract_custom_tool_input(&args)));
    } else {
        item.insert("arguments".into(), json!(args));
    }
    item.insert("call_id".into(), json!(call_id));
    item.insert("name".into(), json!(name));

    emit(
        events,
        state,
        "response.output_item.done",
        json!({
            "type": "response.output_item.done",
            "output_index": idx,
            "item": Value::Object(item)
        }),
    );

    state["funcItemDone"][idx_key] = json!(true);
}

fn send_completed(state: &mut Value, events: &mut Vec<Value>) {
    if state
        .get("completedSent")
        .and_then(|b| b.as_bool())
        .unwrap_or(false)
    {
        return;
    }
    state["completedSent"] = json!(true);

    let resp_id = response_id(state);
    let created = state
        .get("created")
        .and_then(|c| c.as_u64())
        .unwrap_or_else(created_unix);

    emit(
        events,
        state,
        "response.completed",
        json!({
            "type": "response.completed",
            "response": {
                "id": resp_id,
                "object": "response",
                "created_at": created,
                "status": "completed",
                "background": false,
                "error": null
            }
        }),
    );
}

/// Collect all funcCallIds keys for iteration
fn func_call_keys(state: &Value) -> Vec<String> {
    state
        .get("funcCallIds")
        .and_then(|m| m.as_object())
        .map(|obj| obj.keys().cloned().collect())
        .unwrap_or_default()
}

/// Collect all msgItemAdded keys for iteration
fn msg_item_keys(state: &Value) -> Vec<u64> {
    state
        .get("msgItemAdded")
        .and_then(|m| m.as_object())
        .map(|obj| obj.keys().filter_map(|k| k.parse::<u64>().ok()).collect())
        .unwrap_or_default()
}

/// openai chat.completion.chunk → Responses API SSE events
pub fn translate(chunk: Value, state: &mut Value) -> Option<Vec<Value>> {
    let choices = chunk.get("choices").and_then(|c| c.as_array())?;
    if choices.is_empty() {
        return None;
    }

    let mut events: Vec<Value> = Vec::new();

    let choice = &choices[0];
    let idx = choice.get("index").and_then(|i| i.as_u64()).unwrap_or(0);
    let delta = choice.get("delta").cloned().unwrap_or(json!({}));

    // Emit initial events
    if !state
        .get("started")
        .and_then(|b| b.as_bool())
        .unwrap_or(false)
    {
        state["started"] = json!(true);
        let created = state
            .get("created")
            .and_then(|c| c.as_u64())
            .unwrap_or_else(created_unix);
        if let Some(id) = chunk.get("id").and_then(|i| i.as_str()) {
            state["responseId"] = json!(format!("resp_{}", id));
        }
        if state.get("responseId").is_none() {
            state["responseId"] = json!(format!("resp_chatcmpl-{}", created_unix()));
        }
        state["created"] = json!(created);
        state["msgItemAdded"] = json!({});
        state["msgContentAdded"] = json!({});
        state["msgTextBuf"] = json!({});
        state["msgItemDone"] = json!({});
        state["funcNames"] = json!({});
        state["funcCallIds"] = json!({});
        state["funcItemAdded"] = json!({});
        state["funcArgsBuf"] = json!({});
        state["funcItemDone"] = json!({});

        let resp_id = response_id(state);
        emit(
            &mut events,
            state,
            "response.created",
            json!({
                "type": "response.created",
                "response": {
                    "id": resp_id,
                    "object": "response",
                    "created_at": created,
                    "status": "in_progress",
                    "background": false,
                    "error": null,
                    "output": []
                }
            }),
        );

        emit(
            &mut events,
            state,
            "response.in_progress",
            json!({
                "type": "response.in_progress",
                "response": {
                    "id": resp_id,
                    "object": "response",
                    "created_at": created,
                    "status": "in_progress"
                }
            }),
        );
    }

    // Handle reasoning
    let reasoning_text = extract_reasoning_text(&delta);
    if !reasoning_text.is_empty() {
        start_reasoning(state, &mut events, idx);
        emit_reasoning_delta(state, &mut events, &reasoning_text);
    }

    // Handle text content
    if let Some(raw_content) = delta.get("content").and_then(|c| c.as_str()) {
        let mut content = raw_content.to_string();

        if content.contains("<think>") {
            state["inThinking"] = json!(true);
            content = content.replace("<think>", "");
            start_reasoning(state, &mut events, idx);
        }

        if content.contains("</think>") {
            let parts: Vec<&str> = content.splitn(2, "</think>").collect();
            let think_part = parts[0];
            let text_part = if parts.len() > 1 { parts[1] } else { "" };
            if !think_part.is_empty() {
                emit_reasoning_delta(state, &mut events, think_part);
            }
            close_reasoning(state, &mut events);
            state["inThinking"] = json!(false);
            content = text_part.to_string();
        }

        if state
            .get("inThinking")
            .and_then(|b| b.as_bool())
            .unwrap_or(false)
            && !content.is_empty()
        {
            emit_reasoning_delta(state, &mut events, &content);
            if events.is_empty() {
                return None;
            }
            return Some(events);
        }

        if !content.is_empty() {
            emit_text_content(state, &mut events, idx, &content);
        }
    }

    // Handle tool_calls
    if let Some(tool_calls) = delta.get("tool_calls").and_then(|t| t.as_array()) {
        // Close message items before tool calls
        let msg_keys = msg_item_keys(state);
        for k in msg_keys {
            close_message(state, &mut events, k);
        }
        for tc in tool_calls {
            emit_tool_call(state, &mut events, tc);
        }
    }

    // Handle finish_reason
    if choice.get("finish_reason").is_some() && !choice["finish_reason"].is_null() {
        let msg_keys = msg_item_keys(state);
        for k in msg_keys {
            close_message(state, &mut events, k);
        }
        close_reasoning(state, &mut events);
        let fc_keys = func_call_keys(state);
        for k in &fc_keys {
            close_tool_call(state, &mut events, k);
        }
        send_completed(state, &mut events);
    }

    if events.is_empty() {
        None
    } else {
        Some(events)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn init_state() -> Value {
        json!({"created": 1700000000})
    }

    fn make_chunk(delta: Value, finish_reason: Option<&str>) -> Value {
        json!({
            "id": "chatcmpl-test",
            "object": "chat.completion.chunk",
            "created": 1700000000,
            "model": "gpt-4",
            "choices": [{
                "index": 0,
                "delta": delta,
                "finish_reason": finish_reason
            }]
        })
    }

    #[test]
    fn initial_events_emitted() {
        let mut state = init_state();
        let chunk = make_chunk(json!({"content": "hi"}), None);
        let out = translate(chunk, &mut state).unwrap();
        // response.created, response.in_progress, output_item.added, content_part.added, output_text.delta
        assert!(out.len() >= 3);
        assert_eq!(out[0]["event"], "response.created");
        assert_eq!(out[1]["event"], "response.in_progress");
    }

    #[test]
    fn text_delta_emits_output_text_delta() {
        let mut state = init_state();
        // First chunk to init
        let c1 = make_chunk(json!({"content": "hello"}), None);
        let out = translate(c1, &mut state).unwrap();
        let text_events: Vec<_> = out
            .iter()
            .filter(|e| e["event"] == "response.output_text.delta")
            .collect();
        assert_eq!(text_events.len(), 1);
        assert_eq!(text_events[0]["data"]["delta"], "hello");
    }

    #[test]
    fn finish_reason_emits_completed() {
        let mut state = init_state();
        let c1 = make_chunk(json!({"content": "hi"}), None);
        translate(c1, &mut state);
        let c2 = make_chunk(json!({}), Some("stop"));
        let out = translate(c2, &mut state).unwrap();
        let completed: Vec<_> = out
            .iter()
            .filter(|e| e["event"] == "response.completed")
            .collect();
        assert_eq!(completed.len(), 1);
        assert_eq!(completed[0]["data"]["response"]["status"], "completed");
    }

    #[test]
    fn tool_call_emits_function_call_events() {
        let mut state = init_state();
        // First chunk with role
        let c1 = make_chunk(json!({"role": "assistant"}), None);
        translate(c1, &mut state);
        // Tool call start
        let c2 = make_chunk(
            json!({"tool_calls": [{"index": 0, "id": "call_1", "type": "function", "function": {"name": "search", "arguments": ""}}]}),
            None,
        );
        let out = translate(c2, &mut state).unwrap();
        let added: Vec<_> = out
            .iter()
            .filter(|e| e["event"] == "response.output_item.added")
            .collect();
        assert_eq!(added.len(), 1);
        assert_eq!(added[0]["data"]["item"]["type"], "function_call");
        assert_eq!(added[0]["data"]["item"]["name"], "search");
    }

    #[test]
    fn tool_call_args_delta() {
        let mut state = init_state();
        let c1 = make_chunk(
            json!({"tool_calls": [{"index": 0, "id": "call_1", "type": "function", "function": {"name": "search", "arguments": ""}}]}),
            None,
        );
        translate(c1, &mut state);
        let c2 = make_chunk(
            json!({"tool_calls": [{"index": 0, "function": {"arguments": "{\"q\":"}}]}),
            None,
        );
        let out = translate(c2, &mut state).unwrap();
        let args_events: Vec<_> = out
            .iter()
            .filter(|e| e["event"] == "response.function_call_arguments.delta")
            .collect();
        assert_eq!(args_events.len(), 1);
        assert_eq!(args_events[0]["data"]["delta"], "{\"q\":");
    }

    #[test]
    fn finish_with_tools_closes_tool_calls() {
        let mut state = init_state();
        let c1 = make_chunk(
            json!({"tool_calls": [{"index": 0, "id": "call_1", "type": "function", "function": {"name": "s", "arguments": "{}"}}]}),
            None,
        );
        translate(c1, &mut state);
        let c2 = make_chunk(json!({}), Some("tool_calls"));
        let out = translate(c2, &mut state).unwrap();
        let done_events: Vec<_> = out
            .iter()
            .filter(|e| e["event"] == "response.function_call_arguments.done")
            .collect();
        assert_eq!(done_events.len(), 1);
    }

    #[test]
    fn reasoning_content_emits_reasoning_events() {
        let mut state = init_state();
        let c1 = make_chunk(json!({"reasoning_content": "thinking..."}), None);
        let out = translate(c1, &mut state).unwrap();
        let reasoning: Vec<_> = out
            .iter()
            .filter(|e| e["event"] == "response.reasoning_summary_text.delta")
            .collect();
        assert_eq!(reasoning.len(), 1);
        assert_eq!(reasoning[0]["data"]["delta"], "thinking...");
    }

    #[test]
    fn think_tags_handled() {
        let mut state = init_state();
        let c1 = make_chunk(json!({"content": "<think>hmm"}), None);
        let out = translate(c1, &mut state).unwrap();
        assert!(state["inThinking"].as_bool().unwrap());
        let reasoning: Vec<_> = out
            .iter()
            .filter(|e| e["event"] == "response.reasoning_summary_text.delta")
            .collect();
        assert_eq!(reasoning.len(), 1);
        assert_eq!(reasoning[0]["data"]["delta"], "hmm");

        let c2 = make_chunk(json!({"content": "done</think>real output"}), None);
        let out2 = translate(c2, &mut state).unwrap();
        assert!(!state["inThinking"].as_bool().unwrap());
        let text: Vec<_> = out2
            .iter()
            .filter(|e| e["event"] == "response.output_text.delta")
            .collect();
        assert_eq!(text.len(), 1);
        assert_eq!(text[0]["data"]["delta"], "real output");
    }

    #[test]
    fn sequence_numbers_increment() {
        let mut state = init_state();
        let c1 = make_chunk(json!({"content": "a"}), None);
        let out = translate(c1, &mut state).unwrap();
        let seqs: Vec<u64> = out
            .iter()
            .map(|e| e["data"]["sequence_number"].as_u64().unwrap())
            .collect();
        // All unique and monotonically increasing
        for w in seqs.windows(2) {
            assert!(w[1] > w[0]);
        }
    }

    #[test]
    fn completed_only_once() {
        let mut state = init_state();
        let c1 = make_chunk(json!({"content": "hi"}), None);
        translate(c1, &mut state);
        let c2 = make_chunk(json!({}), Some("stop"));
        translate(c2, &mut state);
        // Second finish should produce nothing
        let c3 = make_chunk(json!({}), Some("stop"));
        let out = translate(c3, &mut state);
        // Either None or no completed event
        if let Some(evts) = out {
            let completed: Vec<_> = evts
                .iter()
                .filter(|e| e["event"] == "response.completed")
                .collect();
            assert!(completed.is_empty());
        }
    }

    #[test]
    fn custom_tool_call_detection() {
        let mut state = init_state();
        state["customToolNames"] = json!(["exec"]);
        let c1 = make_chunk(
            json!({"tool_calls": [{"index": 0, "id": "call_1", "type": "function", "function": {"name": "exec", "arguments": ""}}]}),
            None,
        );
        let out = translate(c1, &mut state).unwrap();
        let added: Vec<_> = out
            .iter()
            .filter(|e| e["event"] == "response.output_item.added")
            .collect();
        assert_eq!(added.len(), 1);
        assert_eq!(added[0]["data"]["item"]["type"], "custom_tool_call");
        assert_eq!(added[0]["data"]["item"]["id"], "ctc_call_1");
    }

    #[test]
    fn close_message_emits_done_events() {
        let mut state = init_state();
        let c1 = make_chunk(json!({"content": "hello world"}), None);
        translate(c1, &mut state);
        let c2 = make_chunk(json!({}), Some("stop"));
        let out = translate(c2, &mut state).unwrap();
        let text_done: Vec<_> = out
            .iter()
            .filter(|e| e["event"] == "response.output_text.done")
            .collect();
        assert_eq!(text_done.len(), 1);
        assert_eq!(text_done[0]["data"]["text"], "hello world");
    }
}
