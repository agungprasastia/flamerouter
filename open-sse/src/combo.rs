//! Combo routing — model-combo fallback and round-robin rotation.
//!
//! A "combo" maps a short name (e.g. "fast") to a list of "provider/model" strings.
//! On each request the list is ordered by strategy, then tried sequentially until one
//! succeeds (status < 500 or non-error).

use std::collections::HashMap;
use std::sync::{LazyLock, Mutex};

/// In-memory round-robin state per combo name.
struct RotationState {
    index: usize,
    consecutive_uses: usize,
}

static ROTATION: LazyLock<Mutex<HashMap<String, RotationState>>> =
    LazyLock::new(|| Mutex::new(HashMap::new()));

/// Return the model list ordered by the chosen strategy.
/// - "fallback": original order (first = preferred)
/// - "round-robin": rotate starting index, advance after `sticky_limit` requests
pub fn get_ordered_models(
    models: &[String],
    combo_name: &str,
    strategy: &str,
    sticky_limit: usize,
) -> Vec<String> {
    if models.len() <= 1 || strategy != "round-robin" {
        return models.to_vec();
    }
    let mut map = ROTATION.lock().unwrap();
    let state = map.entry(combo_name.to_string()).or_insert(RotationState {
        index: 0,
        consecutive_uses: 0,
    });
    let current = state.index % models.len();
    // Rotate: put current index first, then wrap
    let mut out = Vec::with_capacity(models.len());
    for i in 0..models.len() {
        out.push(models[(current + i) % models.len()].clone());
    }
    // Advance rotation
    state.consecutive_uses += 1;
    if state.consecutive_uses >= sticky_limit {
        state.index = (current + 1) % models.len();
        state.consecutive_uses = 0;
    }
    out
}

/// Reset rotation state (e.g. when config changes).
pub fn reset_rotation(combo_name: Option<&str>) {
    let mut map = ROTATION.lock().unwrap();
    if let Some(name) = combo_name {
        map.remove(name);
    } else {
        map.clear();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fallback_preserves_order() {
        let models: Vec<String> = vec!["a/m1".into(), "b/m2".into(), "c/m3".into()];
        let out = get_ordered_models(&models, "test-fb", "fallback", 1);
        assert_eq!(out, models);
    }

    #[test]
    fn round_robin_rotates() {
        reset_rotation(Some("test-rr"));
        let models: Vec<String> = vec!["a/m1".into(), "b/m2".into(), "c/m3".into()];
        let r1 = get_ordered_models(&models, "test-rr", "round-robin", 1);
        assert_eq!(r1[0], "a/m1");
        let r2 = get_ordered_models(&models, "test-rr", "round-robin", 1);
        assert_eq!(r2[0], "b/m2");
        let r3 = get_ordered_models(&models, "test-rr", "round-robin", 1);
        assert_eq!(r3[0], "c/m3");
        let r4 = get_ordered_models(&models, "test-rr", "round-robin", 1);
        assert_eq!(r4[0], "a/m1"); // wraps
    }

    #[test]
    fn round_robin_sticky() {
        reset_rotation(Some("test-sticky"));
        let models: Vec<String> = vec!["a/m1".into(), "b/m2".into()];
        let r1 = get_ordered_models(&models, "test-sticky", "round-robin", 3);
        assert_eq!(r1[0], "a/m1");
        let r2 = get_ordered_models(&models, "test-sticky", "round-robin", 3);
        assert_eq!(r2[0], "a/m1");
        let r3 = get_ordered_models(&models, "test-sticky", "round-robin", 3);
        assert_eq!(r3[0], "a/m1"); // 3rd use = hits limit, will rotate after
        let r4 = get_ordered_models(&models, "test-sticky", "round-robin", 3);
        assert_eq!(r4[0], "b/m2"); // rotated
    }

    #[test]
    fn single_model_no_rotation() {
        let models: Vec<String> = vec!["a/m1".into()];
        let out = get_ordered_models(&models, "single", "round-robin", 1);
        assert_eq!(out, vec!["a/m1"]);
    }

    #[test]
    fn reset_clears() {
        let models: Vec<String> = vec!["a/m1".into(), "b/m2".into()];
        get_ordered_models(&models, "test-reset", "round-robin", 1);
        reset_rotation(Some("test-reset"));
        let out = get_ordered_models(&models, "test-reset", "round-robin", 1);
        assert_eq!(out[0], "a/m1"); // back to start
    }
}
