//! Antigravity response translator (Google wrapped candidate -> OpenAI SSE chunks).

use serde_json::Value;

pub fn translate(chunk: Value, state: &mut Value) -> Option<Vec<Value>> {
    // Unwraps {"response": {"candidates": [...]}}
    let gemini_chunk = chunk.get("response").cloned().unwrap_or(chunk);
    crate::translator::response::gemini_to_openai::translate(gemini_chunk, state)
}
