//! Parity regression tests for the auto-generated provider registry.
//!
//! Lives OUTSIDE `mod.rs` because the regenerator (`scripts/regenerate-providers.mjs`)
//! overwrites that file wholesale. Wired from `lib.rs` with `#[path]`.
//! If a regen break these, the derivation logic changed — fix the script, not the tests.

use crate::providers::{base_url_for, get, registry};

const SUPPORTED_FORMATS: [&str; 4] = ["openai", "claude", "gemini", "openai-responses"];

#[test]
fn registry_has_expected_size() {
    assert_eq!(registry().len(), 121);
}

#[test]
fn all_ids_unique_and_known() {
    for (id, p) in registry() {
        assert_eq!(*id, p.id, "map key must match provider id");
        assert!(!p.id.is_empty());
    }
    assert!(get("openai").is_some());
    assert!(get("nope-unknown").is_none());
}

#[test]
fn target_format_within_supported_set() {
    for (_, p) in registry() {
        assert!(
            SUPPORTED_FORMATS.contains(&p.target_format),
            "{}: unsupported target_format {:?}",
            p.id,
            p.target_format
        );
    }
}

#[test]
fn auth_pairs_never_empty() {
    for (_, p) in registry() {
        assert!(
            !p.auth_header.is_empty() && !p.auth_scheme.is_empty(),
            "{}: empty api-key pair",
            p.id
        );
        assert!(
            !p.oauth_header.is_empty() && !p.oauth_scheme.is_empty(),
            "{}: empty oauth pair",
            p.id
        );
    }
}

#[test]
fn claude_family_format() {
    assert_eq!(get("claude").unwrap().target_format, "claude");
    assert_eq!(get("anthropic").unwrap().target_format, "claude");
}

#[test]
fn gemini_family_format_and_auth() {
    for id in ["gemini", "google"] {
        let p = get(id).unwrap();
        assert_eq!(p.target_format, "gemini", "{id}");
        assert_eq!(p.auth_header, "x-goog-api-key", "{id}");
        assert_eq!(p.auth_scheme, "raw", "{id}");
        assert_eq!(p.oauth_header, "Authorization", "{id}");
    }
}

#[test]
fn claude_auth_pairs() {
    let p = get("claude").unwrap();
    assert_eq!(p.auth_header, "x-api-key");
    assert_eq!(p.auth_scheme, "raw");
    assert_eq!(p.oauth_header, "Authorization");
    assert_eq!(p.oauth_scheme, "bearer");
}

#[test]
fn openai_responses_family() {
    for id in ["codex", "grok-cli", "perplexity-agent"] {
        assert_eq!(get(id).unwrap().target_format, "openai-responses", "{id}");
    }
}

#[test]
fn multi_transport_providers_route_to_first_openai_endpoint() {
    for id in [
        "kimi",
        "glm",
        "minimax",
        "minimax-cn",
        "xiaomi-mimo",
        "xiaomi-tokenplan",
        "deepseek",
        "github",
    ] {
        assert_eq!(get(id).unwrap().target_format, "openai", "{id}");
    }
}

#[test]
fn exotic_auth_schemes_preserved() {
    let trae = get("trae").unwrap();
    assert_eq!(trae.auth_scheme, "Cloud-IDE-JWT");
    assert_eq!(trae.oauth_scheme, "Cloud-IDE-JWT");
    let windsurf = get("windsurf").unwrap();
    assert_eq!(windsurf.auth_scheme, "bearer");
    let zed = get("zed").unwrap();
    assert_eq!(zed.auth_scheme, "<user_id> <access_token>");
}

#[test]
fn critical_base_urls() {
    assert_eq!(
        get("zed").unwrap().base_url,
        "https://cloud.zed.dev/completions"
    );
    assert_eq!(
        get("grok-cli").unwrap().base_url,
        "https://cli-chat-proxy.grok.com/v1/responses"
    );
    assert_eq!(
        get("codex").unwrap().base_url,
        "https://chatgpt.com/backend-api/codex/responses"
    );
    assert_eq!(
        get("gemini").unwrap().base_url,
        "https://generativelanguage.googleapis.com/v1beta/models"
    );
    assert!(base_url_for(get("openai").unwrap()).contains("api.openai.com"));
}

#[test]
fn non_chat_providers_register_with_empty_base_url() {
    for id in [
        "elevenlabs",
        "jina-ai",
        "selfhosted-stt",
        "brave-search",
        "stability-ai",
    ] {
        assert_eq!(get(id).unwrap().base_url, "", "{id} should be non-chat");
    }
}
