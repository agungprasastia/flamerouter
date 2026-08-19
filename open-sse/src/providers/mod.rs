//! Provider registry — auto-generated from open-sse provider definitions.
//! Regenerate with `node flamerouter/scripts/regenerate-providers.mjs` — don't hand-edit.
//! target_format + auth pairs derive from open-sse transport/transports entries;
//! unsupported exotic formats fall back to "openai" (no custom executor yet).

use std::collections::HashMap;
use std::sync::OnceLock;

#[derive(Debug, Clone)]
pub struct Provider {
    pub id: &'static str,
    pub base_url: &'static str,
    pub target_format: &'static str,
    /// API-key auth pair — used when credentials carry `apiKey`.
    pub auth_header: &'static str,
    pub auth_scheme: &'static str,
    /// OAuth auth pair — used when credentials carry `accessToken`.
    pub oauth_header: &'static str,
    pub oauth_scheme: &'static str,
    pub extra_headers: &'static [(&'static str, &'static str)],
}

static REGISTRY: OnceLock<HashMap<&'static str, Provider>> = OnceLock::new();

/// Allow tests / dev to override a provider's base_url via env:
/// `FLAMEROUTER_BASE_URL_<PROVIDER_UPPER>` (e.g. FLAMEROUTER_BASE_URL_OPENAI=http://127.0.0.1:29999/v1).
pub fn base_url_for(p: &Provider) -> String {
    let key = format!("FLAMEROUTER_BASE_URL_{}", p.id.to_uppercase().replace('-', "_"));
    std::env::var(&key).unwrap_or_else(|_| p.base_url.to_string())
}

pub fn registry() -> &'static HashMap<&'static str, Provider> {
    REGISTRY.get_or_init(|| {
        let mut m = HashMap::new();
        m.insert(
            "alicode",
            Provider {
                id: "alicode",
                base_url: "https://coding.dashscope.aliyuncs.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "alicode-intl",
            Provider {
                id: "alicode-intl",
                base_url: "https://coding-intl.dashscope.aliyuncs.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "alims-intl",
            Provider {
                id: "alims-intl",
                base_url: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "alitp-intl",
            Provider {
                id: "alitp-intl",
                base_url: "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "anthropic",
            Provider {
                id: "anthropic",
                base_url: "https://api.anthropic.com/v1/messages",
                target_format: "claude",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "antigravity",
            Provider {
                id: "antigravity",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "api-airforce",
            Provider {
                id: "api-airforce",
                base_url: "https://api.airforce/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "assemblyai",
            Provider {
                id: "assemblyai",
                base_url: "https://api.assemblyai.com/v1/audio/transcriptions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "aws-polly",
            Provider {
                id: "aws-polly",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "azure",
            Provider {
                id: "azure",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "baidu",
            Provider {
                id: "baidu",
                base_url: "https://qianfan.baidubce.com/v2/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "bazaarlink",
            Provider {
                id: "bazaarlink",
                base_url: "https://bazaarlink.ai/api/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "black-forest-labs",
            Provider {
                id: "black-forest-labs",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "blackbox",
            Provider {
                id: "blackbox",
                base_url: "https://api.blackbox.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "bluesminds",
            Provider {
                id: "bluesminds",
                base_url: "https://api.bluesminds.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "brave-search",
            Provider {
                id: "brave-search",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "byteplus",
            Provider {
                id: "byteplus",
                base_url: "https://ark.ap-southeast.bytepluses.com/api/coding/v3/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "cartesia",
            Provider {
                id: "cartesia",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "cerebras",
            Provider {
                id: "cerebras",
                base_url: "https://api.cerebras.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "chutes",
            Provider {
                id: "chutes",
                base_url: "https://llm.chutes.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "claude",
            Provider {
                id: "claude",
                base_url: "https://api.anthropic.com/v1/messages",
                target_format: "claude",
                auth_header: "x-api-key",
                auth_scheme: "raw",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "cline",
            Provider {
                id: "cline",
                base_url: "https://api.cline.bot/api/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "clinepass",
            Provider {
                id: "clinepass",
                base_url: "https://api.cline.bot/api/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "cloudflare-ai",
            Provider {
                id: "cloudflare-ai",
                base_url: "https://api.cloudflare.com/client/v4/accounts/{accountId}/ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "codebuddy-cn",
            Provider {
                id: "codebuddy-cn",
                base_url: "https://copilot.tencent.com/v2/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "codebuddy-intl",
            Provider {
                id: "codebuddy-intl",
                base_url: "https://www.codebuddy.ai/v2/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "codex",
            Provider {
                id: "codex",
                base_url: "https://chatgpt.com/backend-api/codex/responses",
                target_format: "openai-responses",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "cohere",
            Provider {
                id: "cohere",
                base_url: "https://api.cohere.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "comfyui",
            Provider {
                id: "comfyui",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "commandcode",
            Provider {
                id: "commandcode",
                base_url: "https://api.commandcode.ai/alpha/generate",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "coqui",
            Provider {
                id: "coqui",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "cursor",
            Provider {
                id: "cursor",
                base_url: "https://api2.cursor.sh",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "deepgram",
            Provider {
                id: "deepgram",
                base_url: "https://api.deepgram.com/v1/listen",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "deepseek",
            Provider {
                id: "deepseek",
                base_url: "https://api.deepseek.com/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "devin-cli",
            Provider {
                id: "devin-cli",
                base_url: "devin://acp/stdio",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "edge-tts",
            Provider {
                id: "edge-tts",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "elevenlabs",
            Provider {
                id: "elevenlabs",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "exa",
            Provider {
                id: "exa",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "fal-ai",
            Provider {
                id: "fal-ai",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "featherless",
            Provider {
                id: "featherless",
                base_url: "https://api.featherless.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "firecrawl",
            Provider {
                id: "firecrawl",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "fireworks",
            Provider {
                id: "fireworks",
                base_url: "https://api.fireworks.ai/inference/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "fish-audio",
            Provider {
                id: "fish-audio",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "gemini",
            Provider {
                id: "gemini",
                base_url: "https://generativelanguage.googleapis.com/v1beta/models",
                target_format: "gemini",
                auth_header: "x-goog-api-key",
                auth_scheme: "raw",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "gemini-cli",
            Provider {
                id: "gemini-cli",
                base_url: "https://cloudcode-pa.googleapis.com/v1internal",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "github",
            Provider {
                id: "github",
                base_url: "https://api.githubcopilot.com/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "gitlab",
            Provider {
                id: "gitlab",
                base_url: "https://gitlab.com/api/v4/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "glm",
            Provider {
                id: "glm",
                base_url: "https://api.z.ai/api/coding/paas/v4/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "glm-cn",
            Provider {
                id: "glm-cn",
                base_url: "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "google",
            Provider {
                id: "google",
                base_url: "https://generativelanguage.googleapis.com/v1beta/models",
                target_format: "gemini",
                auth_header: "x-goog-api-key",
                auth_scheme: "raw",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "google-pse",
            Provider {
                id: "google-pse",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "google-tts",
            Provider {
                id: "google-tts",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "grok-cli",
            Provider {
                id: "grok-cli",
                base_url: "https://cli-chat-proxy.grok.com/v1/responses",
                target_format: "openai-responses",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "grok-web",
            Provider {
                id: "grok-web",
                base_url: "https://grok.com/rest/app-chat/conversations/new",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "groq",
            Provider {
                id: "groq",
                base_url: "https://api.groq.com/openai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "huggingface",
            Provider {
                id: "huggingface",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "hyperbolic",
            Provider {
                id: "hyperbolic",
                base_url: "https://api.hyperbolic.xyz/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "iflow",
            Provider {
                id: "iflow",
                base_url: "https://apis.iflow.cn/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "inworld",
            Provider {
                id: "inworld",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "jina-ai",
            Provider {
                id: "jina-ai",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "jina-reader",
            Provider {
                id: "jina-reader",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "kilo-gateway",
            Provider {
                id: "kilo-gateway",
                base_url: "https://api.kilo.ai/api/gateway/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "kilocode",
            Provider {
                id: "kilocode",
                base_url: "https://api.kilo.ai/api/openrouter/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "kimchi",
            Provider {
                id: "kimchi",
                base_url: "https://llm.kimchi.dev/openai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "kimi",
            Provider {
                id: "kimi",
                base_url: "https://api.kimi.com/coding/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "kiro",
            Provider {
                id: "kiro",
                base_url: "https://runtime.us-east-1.kiro.dev/generateAssistantResponse",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "linkup",
            Provider {
                id: "linkup",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "llm7",
            Provider {
                id: "llm7",
                base_url: "https://api.llm7.io/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "local-device",
            Provider {
                id: "local-device",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "mimo-free",
            Provider {
                id: "mimo-free",
                base_url: "https://api.xiaomimimo.com/api/free-ai/openai/chat",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "minimax",
            Provider {
                id: "minimax",
                base_url: "https://api.minimax.io/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "minimax-cn",
            Provider {
                id: "minimax-cn",
                base_url: "https://api.minimaxi.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "mistral",
            Provider {
                id: "mistral",
                base_url: "https://api.mistral.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "mmf",
            Provider {
                id: "mmf",
                base_url: "https://api.xiaomimimo.com/api/free-ai/openai/chat",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "morph",
            Provider {
                id: "morph",
                base_url: "https://api.morphllm.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "nanobanana",
            Provider {
                id: "nanobanana",
                base_url: "https://api.nanobananaapi.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "nebius",
            Provider {
                id: "nebius",
                base_url: "https://api.studio.nebius.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "nvidia",
            Provider {
                id: "nvidia",
                base_url: "https://integrate.api.nvidia.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "ollama",
            Provider {
                id: "ollama",
                base_url: "https://ollama.com/api/chat",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "ollama-local",
            Provider {
                id: "ollama-local",
                base_url: "http://localhost:11434/api/chat",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "openai",
            Provider {
                id: "openai",
                base_url: "https://api.openai.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "opencode",
            Provider {
                id: "opencode",
                base_url: "https://opencode.ai",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "opencode-go",
            Provider {
                id: "opencode-go",
                base_url: "https://opencode.ai/zen/go/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "openrouter",
            Provider {
                id: "openrouter",
                base_url: "https://openrouter.ai/api/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "perplexity",
            Provider {
                id: "perplexity",
                base_url: "https://api.perplexity.ai/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "perplexity-agent",
            Provider {
                id: "perplexity-agent",
                base_url: "https://api.perplexity.ai/v1/responses",
                target_format: "openai-responses",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "perplexity-web",
            Provider {
                id: "perplexity-web",
                base_url: "https://www.perplexity.ai/rest/sse/perplexity_ask",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "playht",
            Provider {
                id: "playht",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "poolside",
            Provider {
                id: "poolside",
                base_url: "https://inference.poolside.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "qoder",
            Provider {
                id: "qoder",
                base_url: "https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "recraft",
            Provider {
                id: "recraft",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "runwayml",
            Provider {
                id: "runwayml",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "sambanova",
            Provider {
                id: "sambanova",
                base_url: "https://api.sambanova.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "sdwebui",
            Provider {
                id: "sdwebui",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "searchapi",
            Provider {
                id: "searchapi",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "searxng",
            Provider {
                id: "searxng",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "selfhosted-embedding",
            Provider {
                id: "selfhosted-embedding",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "selfhosted-stt",
            Provider {
                id: "selfhosted-stt",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "selfhosted-tts",
            Provider {
                id: "selfhosted-tts",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "serper",
            Provider {
                id: "serper",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "siliconflow",
            Provider {
                id: "siliconflow",
                base_url: "https://api.siliconflow.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "stability-ai",
            Provider {
                id: "stability-ai",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "tavily",
            Provider {
                id: "tavily",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "tencent",
            Provider {
                id: "tencent",
                base_url: "https://api.hunyuan.cloud.tencent.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "together",
            Provider {
                id: "together",
                base_url: "https://api.together.xyz/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "tokenrouter",
            Provider {
                id: "tokenrouter",
                base_url: "https://api.tokenrouter.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "topaz",
            Provider {
                id: "topaz",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "tortoise",
            Provider {
                id: "tortoise",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "trae",
            Provider {
                id: "trae",
                base_url: "https://core-normal.trae.ai/api/remote/v1",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "Cloud-IDE-JWT",
                oauth_header: "Authorization",
                oauth_scheme: "Cloud-IDE-JWT",
                extra_headers: &[],
            },
        );
        m.insert(
            "venice",
            Provider {
                id: "venice",
                base_url: "https://api.venice.ai/api/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "vercel-ai-gateway",
            Provider {
                id: "vercel-ai-gateway",
                base_url: "https://ai-gateway.vercel.sh/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "vertex",
            Provider {
                id: "vertex",
                base_url: "https://aiplatform.googleapis.com",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "vertex-partner",
            Provider {
                id: "vertex-partner",
                base_url: "https://aiplatform.googleapis.com",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "volcengine-ark",
            Provider {
                id: "volcengine-ark",
                base_url: "https://ark.cn-beijing.volces.com/api/coding/v3/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "voyage-ai",
            Provider {
                id: "voyage-ai",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "windsurf",
            Provider {
                id: "windsurf",
                base_url: "https://server.codeium.com/exa.language_server_pb.LanguageServerService/GetChatMessage",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "xai",
            Provider {
                id: "xai",
                base_url: "https://api.x.ai/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "xiaomi-mimo",
            Provider {
                id: "xiaomi-mimo",
                base_url: "https://api.xiaomimimo.com/v1/chat/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "xiaomi-tokenplan",
            Provider {
                id: "xiaomi-tokenplan",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "youcom",
            Provider {
                id: "youcom",
                base_url: "",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "bearer",
                oauth_header: "Authorization",
                oauth_scheme: "bearer",
                extra_headers: &[],
            },
        );
        m.insert(
            "zed",
            Provider {
                id: "zed",
                base_url: "https://cloud.zed.dev/completions",
                target_format: "openai",
                auth_header: "Authorization",
                auth_scheme: "<user_id> <access_token>",
                oauth_header: "Authorization",
                oauth_scheme: "<user_id> <access_token>",
                extra_headers: &[],
            },
        );
        m
    })
}

/// Look up a provider by id.
pub fn get(id: &str) -> Option<&'static Provider> {
    registry().get(id)
}
