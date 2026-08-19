//! RTK Token Saver — pre-translate hook to compress large tool outputs.
//! Port of open-sse/rtk with smart filters:
//! - git diff / git status / git log
//! - build output (npm/cargo/etc)
//! - grep / find / tree / ls
//! - numbered file dumps & log deduplication
//! - smart truncation fallback

use serde_json::Value;

const MIN_COMPRESS_SIZE: usize = 300;
const RAW_CAP: usize = 500_000;
const SMART_TRUNCATE_MIN_LINES: usize = 80;
const HEAD_LINES: usize = 40;
const TAIL_LINES: usize = 20;

/// Compress tool_result / tool message content in-place.
pub fn compress_messages(body: &mut Value) {
    let Some(obj) = body.as_object_mut() else {
        return;
    };

    // OpenAI / Claude `messages` array
    if let Some(messages) = obj.get_mut("messages").and_then(|m| m.as_array_mut()) {
        for msg in messages.iter_mut() {
            compress_single_message(msg);
        }
    }

    // OpenAI Responses API `input` array
    if let Some(input) = obj.get_mut("input").and_then(|i| i.as_array_mut()) {
        for item in input.iter_mut() {
            if item.get("type").and_then(|t| t.as_str()) == Some("function_call_output")
                && let Some(output) = item.get_mut("output")
                    && let Some(text) = output.as_str()
                        && let Some(compressed) = compress_text(text) {
                            *output = Value::String(compressed);
                        }
        }
    }

    // Kiro conversationState
    if let Some(conv) = obj.get_mut("conversationState")
        && let Some(hist) = conv.get_mut("history").and_then(|h| h.as_array_mut()) {
            for item in hist {
                if let Some(tool_results) = item
                    .pointer_mut("/userInputMessage/userInputMessageContext/toolResults")
                    .and_then(|tr| tr.as_array_mut())
                {
                    for tr in tool_results {
                        if tr.get("status").and_then(|s| s.as_str()) == Some("error") {
                            continue;
                        }
                        if let Some(content) = tr.get_mut("content").and_then(|c| c.as_array_mut())
                        {
                            for part in content {
                                if let Some(text) = part.get_mut("text")
                                    && let Some(s) = text.as_str()
                                        && let Some(compressed) = compress_text(s) {
                                            *text = Value::String(compressed);
                                        }
                            }
                        }
                    }
                }
            }
        }
}

fn compress_single_message(msg: &mut Value) {
    let role = msg.get("role").and_then(|r| r.as_str()).unwrap_or("");

    // OpenAI tool message: {"role": "tool", "content": "..."}
    if role == "tool" {
        if let Some(content) = msg.get_mut("content")
            && let Some(text) = content.as_str()
                && let Some(compressed) = compress_text(text) {
                    *content = Value::String(compressed);
                }
        return;
    }

    // Claude / OpenAI array content blocks
    if let Some(content) = msg.get_mut("content").and_then(|c| c.as_array_mut()) {
        for block in content.iter_mut() {
            // Claude tool_result: {"type": "tool_result", "is_error": false, "content": "..."}
            if block.get("type").and_then(|t| t.as_str()) == Some("tool_result") {
                if block
                    .get("is_error")
                    .and_then(|e| e.as_bool())
                    .unwrap_or(false)
                {
                    continue; // skip error traces
                }
                if let Some(c) = block.get_mut("content")
                    && let Some(text) = c.as_str()
                        && let Some(compressed) = compress_text(text) {
                            *c = Value::String(compressed);
                        }
            }
        }
    }
}

/// Smart compression pipeline matching open-sse filter detection.
pub fn compress_text(text: &str) -> Option<String> {
    let len = text.len();
    if !(MIN_COMPRESS_SIZE..=RAW_CAP).contains(&len) {
        return None;
    }

    let head = if len > 2048 { &text[..2048] } else { text };

    // 1. Git Log
    if head.contains("commit ") && (head.contains("Author:") || head.contains("Date:"))
        && let Some(res) = filter_git_log(text) {
            return Some(res);
        }

    // 2. Git Diff
    if (head.starts_with("diff --git ") || head.contains("\ndiff --git ") || head.contains("@@ -"))
        && let Some(res) = filter_git_diff(text) {
            return Some(res);
        }

    // 3. Git Status
    if (head.contains("On branch ")
        || head.contains("nothing to commit")
        || head.contains("Changes not staged"))
        && let Some(res) = filter_git_status(text) {
            return Some(res);
        }

    // 4. Build Output (cargo/npm/webpack errors/warnings)
    if (head.contains("Compiling ") || head.contains("npm error") || head.contains("BUILD SUCCESS"))
        && let Some(res) = filter_build_output(text) {
            return Some(res);
        }

    // 5. Grep output (file:line:content)
    let lines: Vec<&str> = text.lines().collect();
    if lines.len() >= 5 && lines.iter().take(5).any(|l| is_grep_line(l))
        && let Some(res) = filter_grep(&lines) {
            return Some(res);
        }

    // 6. Tree output
    if (head.contains("├── ") || head.contains("└── ") || head.contains("│   "))
        && let Some(res) = filter_tree(&lines) {
            return Some(res);
        }

    // 7. Generic deduplication
    if lines.len() >= 10
        && let Some(res) = filter_dedup_log(&lines) {
            return Some(res);
        }

    // 8. Smart Truncation fallback
    if lines.len() >= SMART_TRUNCATE_MIN_LINES {
        return filter_smart_truncate(&lines);
    }

    None
}

fn is_grep_line(line: &str) -> bool {
    let mut parts = line.splitn(3, ':');
    if let (Some(_file), Some(num), Some(_content)) = (parts.next(), parts.next(), parts.next()) {
        return num.parse::<usize>().is_ok();
    }
    false
}

fn filter_git_diff(text: &str) -> Option<String> {
    let mut out = String::new();
    let mut hunk_count = 0;
    for line in text.lines() {
        if line.starts_with("diff --git ")
            || line.starts_with("--- ")
            || line.starts_with("+++ ")
            || line.starts_with("@@ ")
        {
            out.push_str(line);
            out.push('\n');
            hunk_count += 1;
        } else if line.starts_with('+') || line.starts_with('-') {
            out.push_str(line);
            out.push('\n');
        }
    }
    if hunk_count > 0 && out.len() < text.len() {
        Some(out)
    } else {
        None
    }
}

fn filter_git_status(text: &str) -> Option<String> {
    let mut out = String::new();
    for line in text.lines() {
        let trimmed = line.trim();
        if trimmed.starts_with('#') || trimmed.is_empty() {
            continue;
        }
        if trimmed.starts_with("On branch ")
            || trimmed.starts_with("modified:")
            || trimmed.starts_with("deleted:")
            || trimmed.starts_with("new file:")
            || trimmed.starts_with("Untracked files:")
        {
            out.push_str(trimmed);
            out.push('\n');
        }
    }
    if !out.is_empty() && out.len() < text.len() {
        Some(out)
    } else {
        None
    }
}

fn filter_git_log(text: &str) -> Option<String> {
    let mut out = String::new();
    for line in text.lines() {
        if line.starts_with("commit ") || line.starts_with("Author: ") || line.starts_with("Date: ")
        {
            out.push_str(line);
            out.push('\n');
        } else if !line.trim().is_empty() && !line.starts_with("    ") {
            out.push_str(line);
            out.push('\n');
        }
    }
    if out.len() < text.len() {
        Some(out)
    } else {
        None
    }
}

fn filter_build_output(text: &str) -> Option<String> {
    let mut out = String::new();
    for line in text.lines() {
        if line.contains("error")
            || line.contains("Error")
            || line.contains("warning")
            || line.contains("Warning")
            || line.contains("Finished")
            || line.contains("FAILED")
        {
            out.push_str(line);
            out.push('\n');
        }
    }
    if !out.is_empty() && out.len() < text.len() {
        Some(out)
    } else {
        None
    }
}

fn filter_grep(lines: &[&str]) -> Option<String> {
    if lines.len() <= 30 {
        return None;
    }
    let head = &lines[..20];
    let tail = &lines[lines.len() - 10..];
    let omitted = lines.len() - 30;

    let mut out = String::new();
    for l in head {
        out.push_str(l);
        out.push('\n');
    }
    out.push_str(&format!("... [{omitted} matching lines omitted] ...\n"));
    for l in tail {
        out.push_str(l);
        out.push('\n');
    }
    Some(out)
}

fn filter_tree(lines: &[&str]) -> Option<String> {
    if lines.len() <= 50 {
        return None;
    }
    let mut out = String::new();
    for line in lines.iter().take(40) {
        out.push_str(line);
        out.push('\n');
    }
    out.push_str(&format!(
        "... [{} directories/files omitted] ...\n",
        lines.len() - 40
    ));
    Some(out)
}

fn filter_dedup_log(lines: &[&str]) -> Option<String> {
    let mut out = String::new();
    let mut prev = "";
    let mut dup_count = 0;

    for line in lines {
        if *line == prev {
            dup_count += 1;
        } else {
            if dup_count > 0 {
                out.push_str(&format!("  [repeated {dup_count} times]\n"));
                dup_count = 0;
            }
            out.push_str(line);
            out.push('\n');
            prev = line;
        }
    }
    if dup_count > 0 {
        out.push_str(&format!("  [repeated {dup_count} times]\n"));
    }

    if out.len() + 20 < lines.iter().map(|l| l.len() + 1).sum::<usize>() {
        Some(out)
    } else {
        None
    }
}

fn filter_smart_truncate(lines: &[&str]) -> Option<String> {
    let head = &lines[..HEAD_LINES];
    let tail = &lines[lines.len() - TAIL_LINES..];
    let omitted = lines.len() - HEAD_LINES - TAIL_LINES;

    let mut out = String::new();
    for line in head {
        out.push_str(line);
        out.push('\n');
    }
    out.push_str(&format!(
        "\n... [{omitted} lines omitted by RTK token saver] ...\n\n"
    ));
    for line in tail {
        out.push_str(line);
        out.push('\n');
    }
    Some(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_compress_tool_message_smart_truncate() {
        let mut lines = Vec::new();
        for i in 0..300 {
            lines.push(format!("line {}", i));
        }
        let big_text = lines.join("\n");

        let mut body = json!({
            "messages": [
                { "role": "user", "content": "hello" },
                { "role": "tool", "content": big_text }
            ]
        });

        compress_messages(&mut body);

        let tool_content = body["messages"][1]["content"].as_str().unwrap();
        assert!(tool_content.contains("lines omitted by RTK token saver"));
    }

    #[test]
    fn test_filter_dedup_repeats() {
        let mut lines = Vec::new();
        for _ in 0..50 {
            lines.push("Same repeated log error message here from server loop");
        }
        let line_refs: Vec<&str> = lines.iter().map(|s| *s).collect();
        let filtered = filter_dedup_log(&line_refs).unwrap();
        assert!(filtered.contains("repeated 49 times"));
    }

    #[test]
    fn test_filter_git_diff() {
        let diff = r#"diff --git a/foo.rs b/foo.rs
index 123..456 100644
--- a/foo.rs
+++ b/foo.rs
@@ -1,3 +1,3 @@
-old line 1
+new line 1
 uninteresting context line that is kept
"#;
        let compressed = filter_git_diff(diff);
        assert!(compressed.is_some());
    }
}
