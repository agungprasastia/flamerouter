//! Per-request usage tracking: append-only `log.txt` + aggregated `usage.json`
//! (request count per provider). Data dir: `$FLAMEROUTER_DATA_DIR`, else `~/.flamerouter`.

use serde_json::{Value, json};
use std::io::Write;
use std::path::PathBuf;

fn data_dir() -> PathBuf {
    if let Ok(d) = std::env::var("FLAMEROUTER_DATA_DIR") {
        return PathBuf::from(d);
    }
    std::env::var("HOME")
        .map(|h| PathBuf::from(h).join(".flamerouter"))
        .unwrap_or_else(|_| PathBuf::from(".flamerouter"))
}

/// Append one log line + bump the provider's request counter in usage.json.
pub fn record(pathname: &str, provider: &str, model: &str, status: u16, dur_ms: u64) {
    if provider.is_empty() {
        return;
    }
    let dir = data_dir();
    let _ = std::fs::create_dir_all(&dir);
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    if let Ok(mut f) = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(dir.join("log.txt"))
    {
        let _ = writeln!(
            f,
            "{} {} {} {} {} {}ms",
            now, pathname, provider, model, status, dur_ms
        );
    }
    let path = dir.join("usage.json");
    let mut usage: Value = std::fs::read_to_string(&path)
        .ok()
        .and_then(|t| serde_json::from_str(&t).ok())
        .unwrap_or_else(|| json!({}));
    let requests = usage
        .get(provider)
        .and_then(|e| e.get("requests"))
        .and_then(|r| r.as_u64())
        .unwrap_or(0)
        + 1;
    usage[provider] = json!({ "requests": requests });
    let tmp = dir.join("usage.json.tmp");
    if std::fs::write(
        &tmp,
        serde_json::to_string_pretty(&usage).unwrap_or_default(),
    )
    .is_ok()
    {
        let _ = std::fs::rename(&tmp, &path);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn record_writes_log_and_counts() {
        let dir =
            std::env::temp_dir().join(format!("flamerouter-usage-test-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        // SAFETY: test-only; single-threaded, no other thread reads this env var.
        unsafe { std::env::set_var("FLAMEROUTER_DATA_DIR", &dir) };

        record("/v1/chat/completions", "openai", "gpt-4o-mini", 200, 42);
        record("/v1/chat/completions", "openai", "gpt-4o-mini", 200, 10);
        record("/v1/messages", "claude", "claude-3-5-sonnet", 200, 7);
        record("", "", "", 200, 0);
        record("/v1/chat/completions", "openai", "gpt-4o-mini", 503, 1);

        let usage: Value =
            serde_json::from_str(&std::fs::read_to_string(dir.join("usage.json")).unwrap())
                .unwrap();
        assert_eq!(usage["openai"]["requests"], 3);
        assert_eq!(usage["claude"]["requests"], 1);
        assert_eq!(usage.get(""), None, "empty-provider rows must be skipped");

        let log = std::fs::read_to_string(dir.join("log.txt")).unwrap();
        let lines: Vec<&str> = log.lines().collect();
        assert_eq!(lines.len(), 4);
        assert!(lines[0].contains("/v1/chat/completions openai gpt-4o-mini 200 42ms"));

        // SAFETY: test-only; single-threaded, no other thread reads this env var.
        unsafe { std::env::remove_var("FLAMEROUTER_DATA_DIR") };
        let _ = std::fs::remove_dir_all(&dir);
    }
}
