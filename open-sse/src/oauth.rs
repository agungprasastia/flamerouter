//! OAuth token refresh — claude + codex only (most common).
//! Port of open-sse/services/tokenRefresh/providers.js (refreshAccessToken + refreshCodexToken paths).
//! ponytail: only claude+codex supported; add google/github/iflow/kiro when needed.

use base64::Engine;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};

const CLAUDE_CLIENT_ID: &str = "9d1c250a-e61b-44d9-88ed-5944d1962f5e";
const CLAUDE_TOKEN_URL: &str = "https://api.anthropic.com/v1/oauth/token";
const CODEX_CLIENT_ID: &str = "app_EMoamEEZ73f0CkXaXp7hrann";
const CODEX_TOKEN_URL: &str = "https://auth.openai.com/oauth/token";
const GOOGLE_TOKEN_URL: &str = "https://oauth2.googleapis.com/token";
const GITHUB_TOKEN_URL: &str = "https://github.com/login/oauth/access_token";
const XAI_TOKEN_URL: &str = "https://api.x.ai/oauth/token";
const CLAUDE_AUTHORIZE_URL: &str = "https://claude.ai/oauth/authorize";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RefreshedTokens {
    #[serde(rename = "accessToken")]
    pub access_token: String,
    #[serde(rename = "refreshToken")]
    pub refresh_token: String,
    #[serde(rename = "expiresIn", skip_serializing_if = "Option::is_none")]
    pub expires_in: Option<u64>,
    #[serde(rename = "idToken", skip_serializing_if = "Option::is_none")]
    pub id_token: Option<String>,
}

#[derive(Debug, Clone)]
pub struct Pkce {
    pub verifier: String,
    pub challenge: String,
    pub state: String,
}

pub fn generate_pkce() -> Pkce {
    let mut bytes = [0u8; 32];
    rand::fill(&mut bytes);
    let verifier = base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(bytes);
    let challenge = base64::engine::general_purpose::URL_SAFE_NO_PAD
        .encode(Sha256::digest(verifier.as_bytes()));
    rand::fill(&mut bytes);
    let state = base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(bytes);
    Pkce {
        verifier,
        challenge,
        state,
    }
}

pub fn claude_authorize_url(redirect_uri: &str, pkce: &Pkce) -> String {
    let mut url = reqwest::Url::parse(CLAUDE_AUTHORIZE_URL).expect("valid Claude URL");
    url.query_pairs_mut()
        .append_pair("client_id", CLAUDE_CLIENT_ID)
        .append_pair("response_type", "code")
        .append_pair("redirect_uri", redirect_uri)
        .append_pair("state", &pkce.state)
        .append_pair("code_challenge", &pkce.challenge)
        .append_pair("code_challenge_method", "S256")
        .append_pair("scope", "org:create_api_key user:profile user:inference");
    url.to_string()
}

pub fn codex_authorize_url(redirect_uri: &str, pkce: &Pkce) -> String {
    let mut url =
        reqwest::Url::parse("https://auth.openai.com/oauth/authorize").expect("valid Codex URL");
    url.query_pairs_mut()
        .append_pair("client_id", CODEX_CLIENT_ID)
        .append_pair("response_type", "code")
        .append_pair("redirect_uri", redirect_uri)
        .append_pair("state", &pkce.state)
        .append_pair("code_challenge", &pkce.challenge)
        .append_pair("code_challenge_method", "S256")
        .append_pair("scope", "openid profile email offline_access")
        .append_pair("id_token_add_organizations", "true")
        .append_pair("codex_cli_simplified_flow", "true")
        .append_pair("originator", "codex_cli_rs");
    url.to_string()
}

pub async fn exchange_codex_code(
    code: &str,
    redirect_uri: &str,
    verifier: &str,
) -> Result<RefreshedTokens, RefreshError> {
    let params = [
        ("grant_type", "authorization_code"),
        ("client_id", CODEX_CLIENT_ID),
        ("code", code),
        ("redirect_uri", redirect_uri),
        ("code_verifier", verifier),
    ];
    let resp = reqwest::Client::new()
        .post(token_url("codex", CODEX_TOKEN_URL))
        .header("accept", "application/json")
        .form(&params)
        .send()
        .await
        .map_err(|e| RefreshError::Transient(format!("network: {}", e)))?;
    let status = resp.status().as_u16();
    let text = resp
        .text()
        .await
        .map_err(|e| RefreshError::Transient(format!("read body: {}", e)))?;
    if status != 200 {
        return Err(classify_error(status, &text));
    }
    let tokens: Value = serde_json::from_str(&text)
        .map_err(|e| RefreshError::Transient(format!("parse json: {}", e)))?;
    let access = tokens
        .get("access_token")
        .and_then(Value::as_str)
        .ok_or_else(|| RefreshError::Transient("no access_token in response".into()))?;
    let refresh = tokens
        .get("refresh_token")
        .and_then(Value::as_str)
        .ok_or_else(|| RefreshError::Permanent("no refresh_token in response".into()))?;
    let id_token = tokens
        .get("id_token")
        .and_then(Value::as_str)
        .map(str::to_string);
    Ok(RefreshedTokens {
        access_token: access.into(),
        refresh_token: refresh.into(),
        expires_in: tokens.get("expires_in").and_then(Value::as_u64),
        id_token,
    })
}

pub async fn exchange_claude_code(
    code: &str,
    redirect_uri: &str,
    verifier: &str,
) -> Result<RefreshedTokens, RefreshError> {
    let body = serde_json::json!({
        "grant_type": "authorization_code",
        "client_id": CLAUDE_CLIENT_ID,
        "code": code,
        "redirect_uri": redirect_uri,
        "code_verifier": verifier,
    });
    let resp = reqwest::Client::new()
        .post(token_url("claude", CLAUDE_TOKEN_URL))
        .header("content-type", "application/json")
        .header("accept", "application/json")
        .json(&body)
        .send()
        .await
        .map_err(|e| RefreshError::Transient(format!("network: {}", e)))?;
    let status = resp.status().as_u16();
    let text = resp
        .text()
        .await
        .map_err(|e| RefreshError::Transient(format!("read body: {}", e)))?;
    if status != 200 {
        return Err(classify_error(status, &text));
    }
    let tokens: Value = serde_json::from_str(&text)
        .map_err(|e| RefreshError::Transient(format!("parse json: {}", e)))?;
    let access = tokens
        .get("access_token")
        .and_then(Value::as_str)
        .ok_or_else(|| RefreshError::Transient("no access_token in response".into()))?;
    let refresh = tokens
        .get("refresh_token")
        .and_then(Value::as_str)
        .ok_or_else(|| RefreshError::Permanent("no refresh_token in response".into()))?;
    Ok(RefreshedTokens {
        access_token: access.into(),
        refresh_token: refresh.into(),
        expires_in: tokens.get("expires_in").and_then(Value::as_u64),
        id_token: None,
    })
}

#[derive(Debug)]
pub enum RefreshError {
    /// Permanent failure (invalid_grant, refresh_token_expired) — re-auth required.
    Permanent(String),
    /// Transient failure (network, 5xx) — retry later.
    Transient(String),
}

impl std::fmt::Display for RefreshError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            RefreshError::Permanent(m) => write!(f, "permanent: {}", m),
            RefreshError::Transient(m) => write!(f, "transient: {}", m),
        }
    }
}

impl std::error::Error for RefreshError {}

fn classify_error(status: u16, body: &str) -> RefreshError {
    let parsed: Value = serde_json::from_str(body).unwrap_or(Value::Null);
    let code = parsed
        .get("error")
        .and_then(|e| {
            if e.is_string() {
                e.as_str().map(|s| s.to_string())
            } else {
                e.get("code")
                    .and_then(|c| c.as_str())
                    .map(|s| s.to_string())
            }
        })
        .unwrap_or_default();
    let desc = parsed
        .get("error_description")
        .or_else(|| parsed.get("message"))
        .and_then(|d| d.as_str())
        .unwrap_or(body);
    let combined = format!("{} {}", code, desc).to_lowercase();

    let permanent_markers = [
        "refresh_token_expired",
        "refresh_token_reused",
        "refresh_token_invalidated",
        "invalid_grant",
    ];
    if permanent_markers.iter().any(|m| combined.contains(m)) {
        RefreshError::Permanent(format!("{}: {}", code, desc))
    } else if status >= 500 {
        RefreshError::Transient(format!("upstream {}: {}", status, desc))
    } else {
        RefreshError::Transient(format!("{}: {}", code, desc))
    }
}

/// Refresh Claude (Anthropic) OAuth token — JSON body, no client_secret.
pub async fn refresh_claude(refresh_token: &str) -> Result<RefreshedTokens, RefreshError> {
    let client = reqwest::Client::new();
    let body = serde_json::json!({
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
        "client_id": CLAUDE_CLIENT_ID,
    });

    let resp = client
        .post(token_url("claude", CLAUDE_TOKEN_URL))
        .header("content-type", "application/json")
        .header("accept", "application/json")
        .json(&body)
        .send()
        .await
        .map_err(|e| RefreshError::Transient(format!("network: {}", e)))?;

    let status = resp.status().as_u16();
    let text = resp
        .text()
        .await
        .map_err(|e| RefreshError::Transient(format!("read body: {}", e)))?;

    if status != 200 {
        return Err(classify_error(status, &text));
    }

    let tokens: Value = serde_json::from_str(&text)
        .map_err(|e| RefreshError::Transient(format!("parse json: {}", e)))?;

    let access = tokens
        .get("access_token")
        .and_then(|t| t.as_str())
        .ok_or_else(|| RefreshError::Transient("no access_token in response".into()))?;
    let new_refresh = tokens
        .get("refresh_token")
        .and_then(|t| t.as_str())
        .unwrap_or(refresh_token);
    let expires_in = tokens.get("expires_in").and_then(|e| e.as_u64());

    Ok(RefreshedTokens {
        access_token: access.to_string(),
        refresh_token: new_refresh.to_string(),
        expires_in,
        id_token: None,
    })
}

/// Refresh Codex (OpenAI) OAuth token — JSON body, captures id_token for continuity.
pub async fn refresh_codex(refresh_token: &str) -> Result<RefreshedTokens, RefreshError> {
    let client = reqwest::Client::new();
    let body = serde_json::json!({
        "client_id": CODEX_CLIENT_ID,
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
    });

    let resp = client
        .post(token_url("codex", CODEX_TOKEN_URL))
        .header("content-type", "application/json")
        .header("accept", "application/json")
        .json(&body)
        .send()
        .await
        .map_err(|e| RefreshError::Transient(format!("network: {}", e)))?;

    let status = resp.status().as_u16();
    let text = resp
        .text()
        .await
        .map_err(|e| RefreshError::Transient(format!("read body: {}", e)))?;

    if status != 200 {
        return Err(classify_error(status, &text));
    }

    let tokens: Value = serde_json::from_str(&text)
        .map_err(|e| RefreshError::Transient(format!("parse json: {}", e)))?;

    let access = tokens
        .get("access_token")
        .and_then(|t| t.as_str())
        .ok_or_else(|| RefreshError::Transient("no access_token in response".into()))?;
    let new_refresh = tokens
        .get("refresh_token")
        .and_then(|t| t.as_str())
        .unwrap_or(refresh_token);
    let expires_in = tokens.get("expires_in").and_then(|e| e.as_u64());
    let id_token = tokens
        .get("id_token")
        .and_then(|t| t.as_str())
        .map(|s| s.to_string());

    Ok(RefreshedTokens {
        access_token: access.to_string(),
        refresh_token: new_refresh.to_string(),
        expires_in,
        id_token,
    })
}

/// Refresh generic OAuth 2.0 token via standard token endpoint using refresh_token grant type.
pub async fn refresh_generic_oauth(
    token_url: &str,
    client_id: Option<&str>,
    client_secret: Option<&str>,
    refresh_token: &str,
) -> Result<RefreshedTokens, RefreshError> {
    let client = reqwest::Client::new();
    let mut map = std::collections::HashMap::new();
    map.insert("grant_type", "refresh_token");
    map.insert("refresh_token", refresh_token);
    if let Some(cid) = client_id {
        map.insert("client_id", cid);
    }
    if let Some(cs) = client_secret {
        map.insert("client_secret", cs);
    }

    let resp = client
        .post(token_url)
        .header("content-type", "application/json")
        .header("accept", "application/json")
        .json(&map)
        .send()
        .await
        .map_err(|e| RefreshError::Transient(format!("network: {}", e)))?;

    let status = resp.status().as_u16();
    let text = resp
        .text()
        .await
        .map_err(|e| RefreshError::Transient(format!("read body: {}", e)))?;

    if status != 200 {
        return Err(classify_error(status, &text));
    }

    let tokens: Value = serde_json::from_str(&text)
        .map_err(|e| RefreshError::Transient(format!("parse json: {}", e)))?;

    let access = tokens
        .get("access_token")
        .and_then(|t| t.as_str())
        .ok_or_else(|| RefreshError::Transient("no access_token in response".into()))?;
    let new_refresh = tokens
        .get("refresh_token")
        .and_then(|t| t.as_str())
        .unwrap_or(refresh_token);
    let expires_in = tokens.get("expires_in").and_then(|e| e.as_u64());

    Ok(RefreshedTokens {
        access_token: access.to_string(),
        refresh_token: new_refresh.to_string(),
        expires_in,
        id_token: None,
    })
}

/// Token URL with optional test override: `FLAMEROUTER_OAUTH_TOKEN_URL_<PROVIDER_UPPER>`.
fn token_url(provider: &str, default: &str) -> String {
    std::env::var(format!(
        "FLAMEROUTER_OAUTH_TOKEN_URL_{}",
        provider.to_uppercase().replace('-', "_")
    ))
    .unwrap_or_else(|_| default.to_string())
}

/// Refresh by provider id — returns None if provider doesn't support OAuth refresh.
pub async fn refresh_for_provider(
    provider: &str,
    refresh_token: &str,
) -> Option<Result<RefreshedTokens, RefreshError>> {
    match provider {
        "claude" | "anthropic" => Some(refresh_claude(refresh_token).await),
        "codex" | "openai" => Some(refresh_codex(refresh_token).await),
        "google" | "gemini" => Some(
            refresh_generic_oauth(
                &token_url("google", GOOGLE_TOKEN_URL),
                None,
                None,
                refresh_token,
            )
            .await,
        ),
        "github" => Some(
            refresh_generic_oauth(
                &token_url("github", GITHUB_TOKEN_URL),
                None,
                None,
                refresh_token,
            )
            .await,
        ),
        "xai" | "grok-web" => Some(
            refresh_generic_oauth(&token_url("xai", XAI_TOKEN_URL), None, None, refresh_token)
                .await,
        ),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn classify_invalid_grant_permanent() {
        let e = classify_error(
            400,
            r#"{"error":"invalid_grant","error_description":"refresh token expired"}"#,
        );
        assert!(matches!(e, RefreshError::Permanent(_)));
    }

    #[test]
    fn classify_refresh_token_reused_permanent() {
        let e = classify_error(
            400,
            r#"{"error":"invalid_request","error_description":"refresh_token_reused"}"#,
        );
        assert!(matches!(e, RefreshError::Permanent(_)));
    }

    #[test]
    fn classify_5xx_transient() {
        let e = classify_error(503, "service unavailable");
        assert!(matches!(e, RefreshError::Transient(_)));
    }

    #[test]
    fn classify_400_other_transient() {
        let e = classify_error(
            400,
            r#"{"error":"invalid_request","error_description":"bad param"}"#,
        );
        assert!(matches!(e, RefreshError::Transient(_)));
    }

    #[test]
    fn pkce_uses_s256_and_unique_state() {
        let a = generate_pkce();
        let b = generate_pkce();
        assert_ne!(a.state, b.state);
        assert_eq!(
            a.challenge,
            base64::engine::general_purpose::URL_SAFE_NO_PAD
                .encode(Sha256::digest(a.verifier.as_bytes()))
        );
        assert!(
            claude_authorize_url("http://127.0.0.1:1/callback", &a)
                .contains("code_challenge_method=S256")
        );
    }

    #[tokio::test]
    async fn test_mock_oauth_refresh_success() {
        use axum::{Json, Router, routing::post};
        use serde_json::json;

        // Mock OAuth Server
        let app = Router::new().route(
            "/oauth/token",
            post(|| async {
                Json(json!({
                    "access_token": "new_access_token_123",
                    "refresh_token": "new_refresh_token_456",
                    "expires_in": 3600
                }))
            }),
        );

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });

        let url = format!("http://{}/oauth/token", addr);
        let res = refresh_generic_oauth(&url, Some("client1"), None, "old_refresh_token")
            .await
            .unwrap();

        assert_eq!(res.access_token, "new_access_token_123");
        assert_eq!(res.refresh_token, "new_refresh_token_456");
        assert_eq!(res.expires_in, Some(3600));
    }
}
