//! Config loader — reads ~/.flamerouter/config.json
//!
//! Shape:
//! ```json
//! {
//!   "providers": {
//!     "anthropic": { "api_key": "sk-ant-..." },
//!     "openai":    { "api_key": "sk-..." },
//!     "google":    { "api_key": "..." },
//!     "azure":     { "api_key": "...", "resource": "myres", "deployment": "gpt-4" },
//!     "codex":     { "access_token": "..." }
//!   },
//!   "combos": {
//!     "fast": ["openai/gpt-4o-mini", "anthropic/claude-3-haiku-20240307"],
//!     "smart": ["anthropic/claude-sonnet-4-20250514", "openai/gpt-4o"]
//!   },
//!   "combo_strategy": "fallback"
//! }
//! ```

use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashMap;
use std::path::PathBuf;

#[derive(Debug, Clone, Deserialize, Serialize, Default)]
pub struct Config {
    /// Provider accounts: name → list of credentials (single cred in JSON auto-wraps)
    #[serde(default, deserialize_with = "deserialize_providers")]
    pub providers: HashMap<String, Vec<ProviderCred>>,
    /// Combo definitions: name → list of "provider/model" strings
    #[serde(default)]
    pub combos: HashMap<String, Vec<String>>,
    /// "fallback" (default) or "round-robin"
    #[serde(default)]
    pub combo_strategy: Option<String>,
    /// For round-robin: requests per model before rotating (default 1)
    #[serde(default)]
    pub combo_sticky_limit: Option<usize>,
}

/// Accept either a single `ProviderCred` object or a list `Vec<ProviderCred>`.
fn deserialize_providers<'de, D>(
    deserializer: D,
) -> Result<HashMap<String, Vec<ProviderCred>>, D::Error>
where
    D: serde::Deserializer<'de>,
{
    #[derive(Deserialize)]
    #[serde(untagged)]
    enum OneOrMany {
        One(ProviderCred),
        Many(Vec<ProviderCred>),
    }

    let map: HashMap<String, OneOrMany> = HashMap::deserialize(deserializer)?;
    Ok(map
        .into_iter()
        .map(|(k, v)| match v {
            OneOrMany::One(c) => (k, vec![c]),
            OneOrMany::Many(v) => (k, v),
        })
        .collect())
}

#[derive(Debug, Clone, Deserialize, Serialize, Default)]
pub struct ProviderCred {
    pub api_key: Option<String>,
    pub access_token: Option<String>,
    pub refresh_token: Option<String>,
    pub expires_at: Option<u64>,
    #[serde(flatten)]
    pub extra: HashMap<String, Value>,
}

impl ProviderCred {
    /// Convert to the credentials shape translators/executors expect.
    pub fn to_credential_value(&self) -> Value {
        let mut m = serde_json::Map::new();
        if let Some(k) = &self.api_key {
            m.insert("apiKey".into(), Value::String(k.clone()));
        }
        if let Some(t) = &self.access_token {
            m.insert("accessToken".into(), Value::String(t.clone()));
        }
        if let Some(t) = &self.refresh_token {
            m.insert("refreshToken".into(), Value::String(t.clone()));
        }
        if let Some(t) = self.expires_at {
            m.insert("expiresAt".into(), Value::Number(t.into()));
        }
        for (k, v) in &self.extra {
            m.insert(k.clone(), v.clone());
        }
        Value::Object(m)
    }
}

pub fn config_path() -> PathBuf {
    if let Ok(p) = std::env::var("FLAMEROUTER_CONFIG") {
        return PathBuf::from(p);
    }
    let home = std::env::var("HOME").unwrap_or_else(|_| "/tmp".into());
    PathBuf::from(home).join(".flamerouter").join("config.json")
}

pub fn load() -> Config {
    let path = config_path();
    let Ok(text) = std::fs::read_to_string(&path) else {
        return Config::default();
    };
    serde_json::from_str(&text).unwrap_or_default()
}

pub fn save(cfg: &Config) -> std::io::Result<()> {
    let path = config_path();
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let text = serde_json::to_string_pretty(cfg)?;
    let tmp = path.with_extension("json.tmp");
    std::fs::write(&tmp, text)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&tmp, std::fs::Permissions::from_mode(0o600))?;
    }
    std::fs::rename(tmp, path)
}

impl Config {
    /// Primary (first) credential for a provider.
    pub fn credential_for(&self, provider: &str) -> Option<Value> {
        self.providers
            .get(provider)?
            .first()
            .map(|c| c.to_credential_value())
    }

    /// All credentials for a provider (multi-account).
    pub fn credentials_for(&self, provider: &str) -> Vec<Value> {
        self.providers
            .get(provider)
            .map(|list| list.iter().map(|c| c.to_credential_value()).collect())
            .unwrap_or_default()
    }

    /// Update stored tokens for the first matching account after a successful OAuth refresh.
    pub fn update_tokens(&mut self, provider: &str, access: &str, refresh: &str) {
        let entry = self.providers.entry(provider.to_string()).or_default();
        if entry.is_empty() {
            entry.push(ProviderCred::default());
        }
        entry[0].access_token = Some(access.to_string());
        entry[0].refresh_token = Some(refresh.to_string());
    }

    pub fn update_oauth(
        &mut self,
        provider: &str,
        access: &str,
        refresh: &str,
        expires_at: Option<u64>,
    ) {
        let entry = self.providers.entry(provider.to_string()).or_default();
        if entry.is_empty() {
            entry.push(ProviderCred::default());
        }
        entry[0].api_key = None;
        entry[0].access_token = Some(access.to_string());
        entry[0].refresh_token = Some(refresh.to_string());
        entry[0].expires_at = expires_at;
    }

    pub fn logout_oauth(&mut self, provider: &str) {
        if let Some(accounts) = self.providers.get_mut(provider) {
            accounts.retain(|c| c.api_key.is_some());
            if accounts.is_empty() {
                self.providers.remove(provider);
            }
        }
    }

    /// Look up combo models by name. Returns None if not a combo.
    pub fn combo_models(&self, name: &str) -> Option<&Vec<String>> {
        let models = self.combos.get(name)?;
        if models.is_empty() {
            return None;
        }
        Some(models)
    }

    pub fn combo_strategy_str(&self) -> &str {
        self.combo_strategy.as_deref().unwrap_or("fallback")
    }

    pub fn sticky_limit(&self) -> usize {
        self.combo_sticky_limit.unwrap_or(1).max(1)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn credential_value_shape() {
        let cred = ProviderCred {
            api_key: Some("sk-x".into()),
            access_token: None,
            refresh_token: None,
            expires_at: None,
            extra: Default::default(),
        };
        let v = cred.to_credential_value();
        assert_eq!(v["apiKey"], "sk-x");
        assert!(v.get("accessToken").is_none());
    }

    #[test]
    fn parse_multi_account_json() {
        let text = r#"{"providers":{"anthropic":[{"api_key":"sk-1"},{"api_key":"sk-2"}]}}"#;
        let c: Config = serde_json::from_str(text).unwrap();
        let creds = c.credentials_for("anthropic");
        assert_eq!(creds.len(), 2);
        assert_eq!(creds[0]["apiKey"], "sk-1");
        assert_eq!(creds[1]["apiKey"], "sk-2");
    }

    #[test]
    fn parse_combos() {
        let text = r#"{"providers":{},"combos":{"fast":["openai/gpt-4o-mini","anthropic/claude-3-haiku"]},"combo_strategy":"round-robin","combo_sticky_limit":3}"#;
        let c: Config = serde_json::from_str(text).unwrap();
        assert_eq!(
            c.combo_models("fast").unwrap(),
            &["openai/gpt-4o-mini", "anthropic/claude-3-haiku"]
        );
        assert!(c.combo_models("missing").is_none());
        assert_eq!(c.combo_strategy_str(), "round-robin");
        assert_eq!(c.sticky_limit(), 3);
    }

    #[test]
    fn combos_default_empty() {
        let text = r#"{"providers":{}}"#;
        let c: Config = serde_json::from_str(text).unwrap();
        assert!(c.combo_models("anything").is_none());
        assert_eq!(c.combo_strategy_str(), "fallback");
        assert_eq!(c.sticky_limit(), 1);
    }

    #[test]
    fn oauth_credential_round_trip() {
        let mut c = Config::default();
        c.update_oauth("claude", "access", "refresh", Some(123));
        let value = c.credential_for("claude").unwrap();
        assert_eq!(value["accessToken"], "access");
        assert_eq!(value["refreshToken"], "refresh");
        assert_eq!(value["expiresAt"], 123);
        c.logout_oauth("claude");
        assert!(c.credential_for("claude").is_none());
    }
}
