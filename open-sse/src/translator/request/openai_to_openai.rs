//! openai → openai normalizer (stub for phase 1).

use serde_json::Value;

pub fn translate(_model: &str, body: Value, _stream: bool, _credentials: Option<&Value>) -> Value {
    body
}
