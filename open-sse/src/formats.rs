//! Format identifiers — mirror of open-sse/translator/formats.js

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Format {
    #[serde(rename = "openai")]
    Openai,
    #[serde(rename = "openai-responses")]
    OpenaiResponses,
    #[serde(rename = "openai-response")]
    OpenaiResponse,
    #[serde(rename = "claude")]
    Claude,
    #[serde(rename = "gemini")]
    Gemini,
    #[serde(rename = "gemini-cli")]
    GeminiCli,
    #[serde(rename = "vertex")]
    Vertex,
    #[serde(rename = "codex")]
    Codex,
    #[serde(rename = "antigravity")]
    Antigravity,
    #[serde(rename = "kiro")]
    Kiro,
    #[serde(rename = "cursor")]
    Cursor,
    #[serde(rename = "ollama")]
    Ollama,
    #[serde(rename = "commandcode")]
    Commandcode,
}

impl Format {
    pub fn as_str(&self) -> &'static str {
        match self {
            Format::Openai => "openai",
            Format::OpenaiResponses => "openai-responses",
            Format::OpenaiResponse => "openai-response",
            Format::Claude => "claude",
            Format::Gemini => "gemini",
            Format::GeminiCli => "gemini-cli",
            Format::Vertex => "vertex",
            Format::Codex => "codex",
            Format::Antigravity => "antigravity",
            Format::Kiro => "kiro",
            Format::Cursor => "cursor",
            Format::Ollama => "ollama",
            Format::Commandcode => "commandcode",
        }
    }
}

/// Mirror of detectFormatByEndpoint in open-sse/translator/formats.js
pub fn detect_format_by_endpoint(pathname: &str, body: &serde_json::Value) -> Option<Format> {
    if pathname.contains("/v1/responses") {
        return Some(Format::OpenaiResponses);
    }
    if pathname.contains("/v1/messages") {
        return Some(Format::Claude);
    }
    if pathname.contains("/v1/chat/completions")
        && body.get("input").map(|v| v.is_array()).unwrap_or(false)
    {
        return Some(Format::Openai);
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn endpoint_detect() {
        assert_eq!(
            detect_format_by_endpoint("/v1/messages", &json!({})),
            Some(Format::Claude)
        );
        assert_eq!(
            detect_format_by_endpoint("/v1/responses", &json!({})),
            Some(Format::OpenaiResponses)
        );
        assert_eq!(
            detect_format_by_endpoint("/v1/chat/completions", &json!({"input": []})),
            Some(Format::Openai)
        );
        assert_eq!(
            detect_format_by_endpoint("/v1/chat/completions", &json!({})),
            None
        );
    }
}
