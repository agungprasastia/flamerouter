//! Translator registry — pivot through OpenAI as intermediate format.
//! Mirror of open-sse/translator/index.js

pub mod concerns;
pub mod request;
pub mod response;
pub mod schema;

use crate::formats::Format;
use serde_json::Value;
use std::collections::HashMap;
use std::sync::OnceLock;

pub type RequestFn =
    fn(model: &str, body: Value, stream: bool, credentials: Option<&Value>) -> Value;
pub type ResponseFn = fn(chunk: Value, state: &mut Value) -> Option<Vec<Value>>;

pub struct TranslatorPair {
    pub request: Option<RequestFn>,
    pub response: Option<ResponseFn>,
}

static REQUEST_REGISTRY: OnceLock<HashMap<(&'static str, &'static str), RequestFn>> =
    OnceLock::new();
static RESPONSE_REGISTRY: OnceLock<HashMap<(&'static str, &'static str), ResponseFn>> =
    OnceLock::new();

fn build_request_registry() -> HashMap<(&'static str, &'static str), RequestFn> {
    let mut m = HashMap::new();
    // request translators
    m.insert(
        ("claude", "openai"),
        request::claude_to_openai::translate as RequestFn,
    );
    m.insert(
        ("openai", "claude"),
        request::openai_to_claude::translate as RequestFn,
    );
    m.insert(
        ("gemini", "openai"),
        request::gemini_to_openai::translate as RequestFn,
    );
    m.insert(
        ("openai", "gemini"),
        request::openai_to_gemini::translate as RequestFn,
    );
    m.insert(
        ("openai", "vertex"),
        request::openai_to_vertex::translate as RequestFn,
    );
    m.insert(
        ("openai", "kiro"),
        request::openai_to_kiro::translate as RequestFn,
    );
    m.insert(
        ("openai-responses", "openai"),
        request::openai_responses::translate as RequestFn,
    );
    m.insert(
        ("openai", "openai-responses"),
        request::openai_responses::translate_to_responses as RequestFn,
    );
    m.insert(
        ("openai", "openai"),
        request::openai_to_openai::translate as RequestFn,
    );
    m
}

fn build_response_registry() -> HashMap<(&'static str, &'static str), ResponseFn> {
    let mut m = HashMap::new();
    m.insert(
        ("claude", "openai"),
        response::claude_to_openai::translate as ResponseFn,
    );
    m.insert(
        ("openai", "claude"),
        response::openai_to_claude::translate as ResponseFn,
    );
    m.insert(
        ("gemini", "openai"),
        response::gemini_to_openai::translate as ResponseFn,
    );
    m.insert(
        ("openai", "gemini"),
        response::openai_to_gemini::translate as ResponseFn,
    );
    m.insert(
        ("antigravity", "openai"),
        response::antigravity_to_openai::translate as ResponseFn,
    );
    m.insert(
        ("kiro", "openai"),
        response::kiro_to_openai::translate as ResponseFn,
    );
    m.insert(
        ("openai-responses", "openai"),
        response::openai_responses::translate as ResponseFn,
    );
    m.insert(
        ("openai", "openai-responses"),
        response::openai_to_openai_responses::translate as ResponseFn,
    );
    m
}

pub fn request_registry() -> &'static HashMap<(&'static str, &'static str), RequestFn> {
    REQUEST_REGISTRY.get_or_init(build_request_registry)
}

pub fn response_registry() -> &'static HashMap<(&'static str, &'static str), ResponseFn> {
    RESPONSE_REGISTRY.get_or_init(build_response_registry)
}

/// Translate request: source → openai → target.
/// Simplified version of translateRequest — no thinking/cloaking/session capture yet.
pub fn translate_request(
    source: Format,
    target: Format,
    _model: &str,
    body: Value,
    stream: bool,
    credentials: Option<&Value>,
) -> Option<Value> {
    if source == target {
        return Some(body);
    }
    let reg = request_registry();
    // Direct route first
    if let Some(f) = reg.get(&(source.as_str(), target.as_str())) {
        return Some(f(_model, body, stream, credentials));
    }
    // Pivot: source → openai
    let intermediate = if source != Format::Openai {
        let f = reg.get(&(source.as_str(), "openai"))?;
        f(_model, body, stream, credentials)
    } else {
        body
    };
    // Pivot: openai → target
    if target != Format::Openai {
        let f = reg.get(&("openai", target.as_str()))?;
        Some(f(_model, intermediate, stream, credentials))
    } else {
        Some(intermediate)
    }
}

/// Translate response chunk: target → openai → source.
pub fn translate_response(
    target: Format,
    source: Format,
    chunk: Value,
    state: &mut Value,
) -> Vec<Value> {
    if source == target {
        return vec![chunk];
    }
    let reg = response_registry();
    // Direct route
    if let Some(f) = reg.get(&(target.as_str(), source.as_str())) {
        return f(chunk, state).unwrap_or_default();
    }
    // Pivot: target → openai
    let intermediate: Vec<Value> = if target != Format::Openai {
        match reg.get(&(target.as_str(), "openai")) {
            Some(f) => f(chunk, state).unwrap_or_default(),
            None => vec![chunk],
        }
    } else {
        vec![chunk]
    };
    // Pivot: openai → source
    if source != Format::Openai
        && let Some(f) = reg.get(&("openai", source.as_str())) {
            let mut out = Vec::new();
            for c in intermediate {
                if let Some(v) = f(c, state) {
                    out.extend(v);
                }
            }
            return out;
        }
    intermediate
}
