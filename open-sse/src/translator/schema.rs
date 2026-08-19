//! Schema constants — mirror of open-sse/translator/schema/

pub mod role {
    pub const USER: &str = "user";
    pub const ASSISTANT: &str = "assistant";
    pub const TOOL: &str = "tool";
    pub const SYSTEM: &str = "system";
    pub const DEVELOPER: &str = "developer";
}

pub mod gemini_role {
    pub const USER: &str = "user";
    pub const MODEL: &str = "model";
}

pub mod openai_block {
    pub const TEXT: &str = "text";
    pub const IMAGE_URL: &str = "image_url";
    pub const IMAGE: &str = "image";
    pub const INPUT_AUDIO: &str = "input_audio";
    pub const AUDIO_URL: &str = "audio_url";
    pub const FILE: &str = "file";
    pub const FUNCTION: &str = "function";
}

pub mod claude_block {
    pub const TEXT: &str = "text";
    pub const IMAGE: &str = "image";
    pub const DOCUMENT: &str = "document";
    pub const TOOL_USE: &str = "tool_use";
    pub const TOOL_RESULT: &str = "tool_result";
    pub const THINKING: &str = "thinking";
    pub const REDACTED_THINKING: &str = "redacted_thinking";
}

pub const DEFAULT_MAX_TOKENS: u64 = 64000;
pub const DEFAULT_MIN_TOKENS: u64 = 32000;
