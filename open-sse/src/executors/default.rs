//! Default executor — HTTP call to OpenAI-compatible upstream.
//! Mirror of open-sse/executors/default.js (simplified).

use anyhow::{Context, Result, anyhow};
use bytes::Bytes;
use futures_util::Stream;
use reqwest::Client;
use serde_json::Value;
use std::pin::Pin;
use std::time::Duration;

use crate::providers::Provider;

pub struct DefaultExecutor {
    client: Client,
}

pub struct UpstreamResponse {
    pub status: u16,
    pub body: UpstreamBody,
    pub url: String,
}

pub enum UpstreamBody {
    Json(Value),
    Sse(Pin<Box<dyn Stream<Item = Result<Bytes>> + Send>>),
}

/// Build the header value for a token under an auth scheme.
/// Mirrors open-sse `setAuth`: "bearer" → `Bearer <token>`, "raw" → `<token>`,
/// any other prefix → `<scheme> <token>` (e.g. Cloud-IDE-JWT), and a template
/// scheme (`<field_a> <field_b>`) substitutes `<field>` placeholders from the
/// credential (zed's `<user_id> <access_token>` shape).
fn header_value_for(scheme: &str, credentials: &Value, token: &str) -> String {
    match scheme {
        "bearer" => format!("Bearer {token}"),
        "raw" => token.to_string(),
        s if s.contains('<') && s.contains('>') => {
            let mut out = String::new();
            let mut rest = s;
            while let Some(start) = rest.find('<') {
                out.push_str(&rest[..start]);
                let after = &rest[start + 1..];
                let Some(end_rel) = after.find('>') else {
                    break;
                };
                let field = &after[..end_rel];
                let value = credential_field(credentials, field);
                if value.is_empty() {
                    return token.to_string(); // unknown field — fail-open to raw token
                }
                out.push_str(&value);
                rest = &after[end_rel + 1..];
            }
            out.push_str(rest);
            out
        }
        prefix => format!("{prefix} {token}"),
    }
}

/// Look up a credential field by snake_case or camelCase spelling.
fn credential_field(credentials: &Value, field: &str) -> String {
    let candidates = [field.to_string(), to_camel(field), to_snake(field)];
    for key in candidates {
        if let Some(v) = credentials.get(&key).and_then(|v| v.as_str()) {
            return v.to_string();
        }
    }
    String::new()
}

fn to_camel(s: &str) -> String {
    let mut out = String::new();
    let mut upper = false;
    for c in s.chars() {
        if c == '_' {
            upper = true;
        } else if upper {
            out.push(c.to_ascii_uppercase());
            upper = false;
        } else {
            out.push(c);
        }
    }
    out
}

fn to_snake(s: &str) -> String {
    let mut out = String::new();
    for (i, c) in s.chars().enumerate() {
        if c.is_ascii_uppercase() {
            if i > 0 {
                out.push('_');
            }
            out.push(c.to_ascii_lowercase());
        } else {
            out.push(c);
        }
    }
    out
}

impl Default for DefaultExecutor {
    fn default() -> Self {
        Self::new()
    }
}

impl DefaultExecutor {
    pub fn new() -> Self {
        let client = Client::builder()
            .use_rustls_tls()
            // Connect timeout only — a hung upstream must not hang callers forever,
            // but an SSE stream may legitimately live for minutes (no total timeout).
            .connect_timeout(Duration::from_secs(30))
            .build()
            .expect("reqwest client");
        Self { client }
    }

    /// Execute request against provider. Caller supplies already-translated body.
    pub async fn execute(
        &self,
        provider: &Provider,
        model: &str,
        body: Value,
        stream: bool,
        credentials: &Value,
    ) -> Result<UpstreamResponse> {
        if provider.id == "zed" {
            return crate::executors::zed::execute(provider, model, body, stream, credentials)
                .await;
        }
        if provider.id == "commandcode" {
            return crate::executors::commandcode::execute(
                provider,
                model,
                body,
                stream,
                credentials,
            )
            .await;
        }
        if provider.id == "trae" {
            return crate::executors::trae::execute(provider, model, body, stream, credentials)
                .await;
        }
        if provider.id == "antigravity" {
            return crate::executors::antigravity::execute(
                provider,
                model,
                body,
                stream,
                credentials,
            )
            .await;
        }
        if provider.id == "qoder" {
            return crate::executors::qoder::execute(provider, model, body, stream, credentials)
                .await;
        }
        if provider.id == "kiro" {
            return crate::executors::kiro::execute(provider, model, body, stream, credentials)
                .await;
        }
        if provider.id == "windsurf" {
            return crate::executors::windsurf::execute(provider, model, body, stream, credentials)
                .await;
        }
        if provider.id == "cursor" {
            return crate::executors::cursor::execute(provider, model, body, stream, credentials)
                .await;
        }
        if provider.id == "devin-cli" || provider.id == "devin" {
            return crate::executors::devin::execute(provider, model, body, stream, credentials)
                .await;
        }
        if provider.id == "azure" {
            return crate::executors::azure::execute(provider, model, body, stream, credentials)
                .await;
        }
        if provider.id == "github" {
            return crate::executors::github::execute(provider, model, body, stream, credentials)
                .await;
        }
        if provider.id == "vertex" || provider.id == "vertex-partner" {
            return crate::executors::vertex::execute(provider, model, body, stream, credentials)
                .await;
        }
        let url = self.build_url(provider, model, stream);
        let mut req = self.client.post(&url).json(&body);
        // Non-streaming requests get a hard total timeout (streams don't, see new()).
        if !stream {
            req = req.timeout(Duration::from_secs(300));
        }

        // Auth: pick the pair matching the credential kind — apiKey uses the
        // provider's API-key header/scheme, OAuth access tokens use oauth pair.
        let (auth_header, auth_scheme, token) = if credentials.get("apiKey").is_some() {
            (
                provider.auth_header,
                provider.auth_scheme,
                credentials.get("apiKey"),
            )
        } else if credentials.get("api_key").is_some() {
            (
                provider.auth_header,
                provider.auth_scheme,
                credentials.get("api_key"),
            )
        } else {
            (
                provider.oauth_header,
                provider.oauth_scheme,
                credentials
                    .get("accessToken")
                    .or_else(|| credentials.get("access_token")),
            )
        };
        let token = token
            .and_then(|t| t.as_str())
            .ok_or_else(|| anyhow!("missing credential token"))?;

        if auth_scheme == "bearer" {
            req = req.header(auth_header, format!("Bearer {}", token));
        } else {
            req = req.header(
                auth_header,
                header_value_for(auth_scheme, credentials, token),
            );
        }
        for (k, v) in provider.extra_headers {
            req = req.header(*k, *v);
        }

        if stream {
            req = req.header("Accept", "text/event-stream");
        }

        let resp = req.send().await.context("upstream request")?;
        let status = resp.status().as_u16();
        let url_str = url.clone();

        if stream && status == 200 {
            use futures_util::StreamExt;
            let byte_stream = resp.bytes_stream();
            let mapped = byte_stream.map(|r| r.map_err(|e| anyhow!(e)));
            return Ok(UpstreamResponse {
                status,
                body: UpstreamBody::Sse(Box::pin(mapped)),
                url: url_str,
            });
        }

        let json: Value = resp.json().await.context("parse upstream json")?;
        Ok(UpstreamResponse {
            status,
            body: UpstreamBody::Json(json),
            url: url_str,
        })
    }

    fn build_url(&self, provider: &Provider, model: &str, stream: bool) -> String {
        let base = crate::providers::base_url_for(provider);
        match provider.target_format {
            "claude" => format!("{}/messages", base),
            "openai-responses" => {
                let url = format!("{}/responses", base);
                url.replace("/responses/responses", "/responses")
            }
            "gemini" => {
                // Google-style: POST {base}/{model}:streamGenerateContent (SSE) or :generateContent (JSON)
                let suffix = if stream {
                    ":streamGenerateContent?alt=sse"
                } else {
                    ":generateContent"
                };
                format!("{}/{}{}", base.trim_end_matches('/'), model, suffix)
            }
            _ => format!("{}/chat/completions", base),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn bearer_and_raw_schemes() {
        assert_eq!(header_value_for("bearer", &json!({}), "t1"), "Bearer t1");
        assert_eq!(header_value_for("raw", &json!({}), "t2"), "t2");
    }

    #[test]
    fn custom_prefix_scheme() {
        // trae: Authorization: Cloud-IDE-JWT <jwt>
        assert_eq!(
            header_value_for("Cloud-IDE-JWT", &json!({}), "jwt-1"),
            "Cloud-IDE-JWT jwt-1"
        );
    }

    #[test]
    fn template_scheme_substitutes_fields() {
        // zed: Authorization: <user_id> <access_token>
        let creds = json!({"accessToken": "tok-9", "userId": "user-7"});
        assert_eq!(
            header_value_for("<user_id> <access_token>", &creds, "tok-9"),
            "user-7 tok-9"
        );
    }

    #[test]
    fn template_scheme_missing_field_falls_back_raw() {
        let creds = json!({"accessToken": "tok-9"});
        assert_eq!(
            header_value_for("<user_id> <access_token>", &creds, "tok-9"),
            "tok-9"
        );
    }

    #[test]
    fn snake_and_camel_field_lookup() {
        let creds = json!({"userId": "uid", "client_secret": "sec"});
        assert_eq!(credential_field(&creds, "user_id"), "uid");
        assert_eq!(credential_field(&creds, "clientSecret"), "sec");
        assert_eq!(credential_field(&creds, "missing"), "");
    }
}
