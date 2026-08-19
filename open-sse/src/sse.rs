//! SSE parsing — split byte stream into `data: ...` events.

use bytes::Bytes;
use futures_util::{Stream, StreamExt};
use std::pin::Pin;

pub struct SseEvent {
    pub data: String,
}

/// Split a byte stream into SSE events. Handles multi-line data and \r\n.
pub fn parse_sse<S>(stream: S) -> Pin<Box<dyn Stream<Item = SseEvent> + Send>>
where
    S: Stream<Item = anyhow::Result<Bytes>> + Send + 'static,
{
    let s = async_stream::stream! {
        let mut buf: Vec<u8> = Vec::new();
        let mut data_lines: Vec<String> = Vec::new();
        tokio::pin!(stream);
        while let Some(chunk) = stream.next().await {
            let Ok(bytes) = chunk else { continue };
            buf.extend_from_slice(&bytes);
            // Drain complete lines
            while let Some(pos) = buf.iter().position(|&b| b == b'\n') {
                let mut line: Vec<u8> = buf.drain(..=pos).collect();
                // Strip \n and trailing \r
                line.pop();
                if line.last() == Some(&b'\r') {
                    line.pop();
                }
                let Ok(line_str) = String::from_utf8(line) else { continue };
                if line_str.is_empty() {
                    // event boundary
                    if !data_lines.is_empty() {
                        yield SseEvent { data: data_lines.join("\n") };
                        data_lines.clear();
                    }
                    continue;
                }
                if let Some(rest) = line_str.strip_prefix("data:") {
                    let rest = rest.strip_prefix(' ').unwrap_or(rest);
                    data_lines.push(rest.to_string());
                }
            }
        }
        // flush tail
        if !data_lines.is_empty() {
            yield SseEvent { data: data_lines.join("\n") };
        }
    };
    Box::pin(s)
}

/// Serialize a single SSE event back to bytes.
pub fn encode_sse_event(data: &str) -> Bytes {
    Bytes::from(format!("data: {}\n\n", data))
}

/// Serialize SSE event with event type field (for Claude / Responses API format).
/// Output: `event: <event_type>\ndata: <data_json>\n\n`
pub fn encode_sse_event_typed(event_type: &str, data: &str) -> Bytes {
    Bytes::from(format!("event: {}\ndata: {}\n\n", event_type, data))
}
