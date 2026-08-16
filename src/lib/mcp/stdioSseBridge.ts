// Inline stdio<->SSE bridge for MCP. Spawns one child per plugin on demand,
// broadcasts JSON-RPC frames over SSE, accepts client messages via HTTP POST.

import { spawn, type ChildProcess } from "child_process";
import crypto from "crypto";
import { LOCAL_STDIO_PLUGINS, type CoworkPlugin } from "@/shared/constants/coworkPlugins";

export interface BridgeEntry {
  proc: ChildProcess;
  sessions: Map<string, (event: string) => void>;
  buffer: string;
}

const G_KEY = "__flamerouterMcpBridges";
const MAX_TEXT_CHARS = 50000;
const COLLAPSE_THRESHOLD = 30;
const COLLAPSE_KEEP_HEAD = 10;
const COLLAPSE_KEEP_TAIL = 5;

// Drop noise nodes, collapse repeated siblings, hard-truncate. Preserve [ref=eXX].
function smartFilterText(text: string): string {
  if (typeof text !== "string" || text.length < 2000) return text;
  let out = text;
  out = out.replace(/^\s*-\s*generic:?\s*$/gm, "");
  out = out.replace(/^\s*-\s*text:\s*""\s*$/gm, "");
  out = collapseRepeated(out);
  if (out.length > MAX_TEXT_CHARS) {
    const head = out.slice(0, MAX_TEXT_CHARS - 300);
    out = `${head}\n\n... [truncated ${text.length - head.length} chars by flamerouter bridge. Page is large; ask user to scroll/navigate to a specific section, or click an element with the refs shown above]`;
  }
  return out;
}

// Group consecutive lines sharing the same leading indent + role prefix; collapse if >= COLLAPSE_THRESHOLD.
function collapseRepeated(text: string): string {
  const lines = text.split("\n");
  const out: string[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line === undefined) break;
    const m = line.match(/^(\s*)-\s*([a-zA-Z]+)\b/);
    if (!m || m[1] === undefined || m[2] === undefined) {
      out.push(line);
      i++;
      continue;
    }
    const indent = m[1];
    const role = m[2];
    let j = i;
    while (j < lines.length) {
      const ln = lines[j];
      if (ln === undefined) break;
      const mm = ln.match(/^(\s*)-\s*([a-zA-Z]+)\b/);
      if (mm && mm[1] === indent && mm[2] === role) {
        j++;
        continue;
      }
      if (ln.startsWith(`${indent} `) || ln.startsWith(`${indent}\t`)) {
        j++;
        continue;
      }
      break;
    }
    const groupLen = j - i;
    if (groupLen >= COLLAPSE_THRESHOLD) {
      const headEnd = findNthSiblingEnd(
        lines,
        i,
        indent,
        role,
        COLLAPSE_KEEP_HEAD,
      );
      const tailStart = findLastNSiblingStart(
        lines,
        j,
        indent,
        role,
        COLLAPSE_KEEP_TAIL,
      );
      for (let k = i; k < headEnd; k++) {
        const item = lines[k];
        if (item !== undefined) out.push(item);
      }
      out.push(
        `${indent}... [${groupLen - COLLAPSE_KEEP_HEAD - COLLAPSE_KEEP_TAIL} similar "${role}" items omitted by flamerouter bridge]`,
      );
      for (let k = tailStart; k < j; k++) {
        const item = lines[k];
        if (item !== undefined) out.push(item);
      }
    } else {
      for (let k = i; k < j; k++) {
        const item = lines[k];
        if (item !== undefined) out.push(item);
      }
    }
    i = j;
  }
  return out.join("\n");
}

function findNthSiblingEnd(
  lines: string[],
  start: number,
  indent: string,
  role: string,
  n: number,
): number {
  let count = 0;
  for (let k = start; k < lines.length; k++) {
    const line = lines[k];
    if (line === undefined) continue;
    const mm = line.match(/^(\s*)-\s*([a-zA-Z]+)\b/);
    if (mm && mm[1] === indent && mm[2] === role) {
      count++;
      if (count > n) return k;
    }
  }
  return lines.length;
}

function findLastNSiblingStart(
  lines: string[],
  end: number,
  indent: string,
  role: string,
  n: number,
): number {
  const positions: number[] = [];
  for (let k = 0; k < end; k++) {
    const line = lines[k];
    if (line === undefined) continue;
    const mm = line.match(/^(\s*)-\s*([a-zA-Z]+)\b/);
    if (mm && mm[1] === indent && mm[2] === role) positions.push(k);
  }
  const targetPos = positions[positions.length - n];
  return positions.length > n && targetPos !== undefined ? targetPos : end;
}

// Apply filter to JSON-RPC tool/result content text blocks only.
function filterFrame(line: string): string {
  try {
    const msg = JSON.parse(line) as {
      result?: { content?: Array<{ type?: string; text?: string }> };
    };
    const content = msg?.result?.content;
    if (!Array.isArray(content)) return line;
    let mutated = false;
    for (const item of content) {
      if (item?.type === "text" && typeof item.text === "string") {
        const filtered = smartFilterText(item.text);
        if (filtered !== item.text) {
          item.text = filtered;
          mutated = true;
        }
      }
    }
    return mutated ? JSON.stringify(msg) : line;
  } catch {
    return line;
  }
}

declare global {
  // eslint-disable-next-line no-var
  var __flamerouterMcpBridges: Map<string, BridgeEntry> | undefined;
}

const getStore = (): Map<string, BridgeEntry> => {
  if (!globalThis[G_KEY]) globalThis[G_KEY] = new Map<string, BridgeEntry>();
  return globalThis[G_KEY] as Map<string, BridgeEntry>;
};

// Only preset stdio plugins may spawn. No user-defined commands (RCE prevention).
export function findPlugin(name: string): CoworkPlugin | null {
  return (
    (LOCAL_STDIO_PLUGINS.find((p) => p.name === name) as CoworkPlugin | undefined) ||
    null
  );
}

export function getOrSpawn(name: string): BridgeEntry {
  const store = getStore();
  let entry = store.get(name);
  if (entry?.proc && !entry.proc.killed && entry.proc.exitCode === null)
    return entry;

  const plugin = findPlugin(name);
  if (
    !plugin ||
    typeof plugin.command !== "string" ||
    !Array.isArray(plugin.args) ||
    plugin.args.some((arg) => typeof arg !== "string")
  ) {
    throw new Error(`Unknown local plugin: ${name}`);
  }

  const proc = spawn(plugin.command, plugin.args as string[], {
    stdio: ["pipe", "pipe", "pipe"],
    env: process.env,
  });
  entry = { proc, sessions: new Map<string, (event: string) => void>(), buffer: "" };
  store.set(name, entry);

  // Parse newline-delimited JSON-RPC from child stdout, broadcast to all sessions.
  proc.stdout?.on("data", (chunk: Buffer | string) => {
    if (!entry) return;
    entry.buffer += chunk.toString("utf8");
    let idx: number;
    while ((idx = entry.buffer.indexOf("\n")) >= 0) {
      const raw = entry.buffer.slice(0, idx).trim();
      entry.buffer = entry.buffer.slice(idx + 1);
      if (!raw) continue;
      const line = filterFrame(raw);
      for (const send of entry.sessions.values()) {
        try {
          send(`event: message\ndata: ${line}\n\n`);
        } catch {
          /* ignore broken pipe */
        }
      }
    }
  });

  proc.stderr?.on("data", (d: Buffer | string) =>
    console.log(`[mcp:${name}]`, d.toString().trim()),
  );
  proc.on("exit", (code: number | null) => {
    console.log(`[mcp:${name}] exited`, code);
    store.delete(name);
  });

  return entry;
}

export function registerSession(
  name: string,
  sendFn: (event: string) => void,
): string {
  const entry = getOrSpawn(name);
  const sid = crypto.randomUUID();
  entry.sessions.set(sid, sendFn);
  return sid;
}

export function unregisterSession(name: string, sid: string): void {
  const entry = getStore().get(name);
  if (!entry) return;
  entry.sessions.delete(sid);
  // No sessions left → kill child to avoid idle orphan process leak.
  if (entry.sessions.size === 0) {
    try {
      entry.proc.kill();
    } catch {
      /* ignore */
    }
    getStore().delete(name);
  }
}

// Kill all spawned MCP children — called on app shutdown to prevent orphans.
export function killAllBridges(): void {
  const store = getStore();
  for (const [name, entry] of store) {
    try {
      entry.proc.kill();
    } catch {
      /* ignore */
    }
    store.delete(name);
  }
}

export function sendToChild(name: string, jsonRpc: unknown): void {
  const entry = getStore().get(name);
  if (!entry?.proc?.stdin?.writable)
    throw new Error(`Bridge not running: ${name}`);
  entry.proc.stdin.write(`${JSON.stringify(jsonRpc)}\n`);
}

export function isRunning(name: string): boolean {
  const entry = getStore().get(name);
  return !!(entry?.proc && !entry.proc.killed && entry.proc.exitCode === null);
}
