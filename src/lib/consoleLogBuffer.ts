import { EventEmitter } from "events";
import { CONSOLE_LOG_CONFIG } from "@/shared/constants/config";

const consoleLevels = ["log", "info", "warn", "error", "debug"] as const;
type ConsoleLevel = (typeof consoleLevels)[number];

interface ConsoleLogBufferState {
  logs: string[];
  patched: boolean;
  originals: Partial<Record<ConsoleLevel, (...args: unknown[]) => void>>;
  emitter: EventEmitter;
  pendingLines?: string[];
  flushTimer?: NodeJS.Timeout | null;
}

declare global {
  // eslint-disable-next-line no-var
  var _consoleLogBufferState: ConsoleLogBufferState | undefined;
}

if (!global._consoleLogBufferState) {
  global._consoleLogBufferState = {
    logs: [],
    patched: false,
    originals: {},
    emitter: new EventEmitter(),
  };
  global._consoleLogBufferState.emitter.setMaxListeners(50);
}

const state = global._consoleLogBufferState as ConsoleLogBufferState;

// Ensure emitter exists (handles hot reload with stale global)
if (!state.emitter) {
  state.emitter = new EventEmitter();
  state.emitter.setMaxListeners(50);
}

if (!state.pendingLines) state.pendingLines = [];
if (!state.flushTimer) state.flushTimer = null;

const FLUSH_INTERVAL_MS = 100;
const MAX_BATCH_LINES = 50;

function flushPendingLines() {
  state.flushTimer = null;
  if (!state.pendingLines?.length) return;

  const lines = state.pendingLines.splice(0, state.pendingLines.length);
  state.emitter.emit("lines", lines);
}

function scheduleFlush() {
  if (state.flushTimer) return;
  state.flushTimer = setTimeout(flushPendingLines, FLUSH_INTERVAL_MS);
  state.flushTimer?.unref?.();
}

function toLogLine(_level: ConsoleLevel, args: unknown[]): string {
  return args.map(formatArg).join(" ");
}

// Strip ANSI escape codes so terminal colors don't bleed into UI
const ANSI_RE = /\x1b\[[0-9;]*m/g;

function stripAnsi(str: string): string {
  return str.replace(ANSI_RE, "");
}

function formatArg(arg: unknown): string {
  if (typeof arg === "string") return stripAnsi(arg);
  if (arg instanceof Error)
    return stripAnsi(arg.stack || arg.message || String(arg));
  try {
    return stripAnsi(JSON.stringify(arg));
  } catch {
    return stripAnsi(String(arg));
  }
}

function appendLine(line: string) {
  state.logs.push(line);
  const maxLines = CONSOLE_LOG_CONFIG.maxLines;
  if (state.logs.length > maxLines) {
    state.logs = state.logs.slice(-maxLines);
  }
  state.pendingLines?.push(line);
  if ((state.pendingLines?.length || 0) >= MAX_BATCH_LINES) {
    if (state.flushTimer) {
      clearTimeout(state.flushTimer);
      state.flushTimer = null;
    }
    flushPendingLines();
  } else {
    scheduleFlush();
  }
}

export function initConsoleLogCapture(): void {
  if (state.patched) return;

  for (const level of consoleLevels) {
    state.originals[level] = console[level];
    console[level] = (...args: unknown[]) => {
      appendLine(toLogLine(level, args));
      state.originals[level]?.(...args);
    };
  }

  state.patched = true;
}

export function getConsoleLogs(): string[] {
  return state.logs;
}

export function clearConsoleLogs(): void {
  state.logs = [];
  state.emitter.emit("clear");
}

export function getConsoleEmitter(): EventEmitter {
  return state.emitter;
}
