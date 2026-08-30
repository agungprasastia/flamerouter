import { describe, it, expect } from "vitest";
import path from "path";
import Module from "module";

const mockLogger = { err: () => {}, log: () => {} };
const mockConfig = { IS_DEV: false };
const mockBase = { fetchRouter: async () => {}, pipeTransformedEventStream: async () => {} };

// Intercept module resolution to point TypeScript files or mock modules
const originalResolve = (Module as unknown as { _resolveFilename: typeof Module._resolveFilename })._resolveFilename;
(Module as unknown as { _resolveFilename: unknown })._resolveFilename = function (
  request: string,
  parent: { filename: string } | null,
  isMain: boolean,
  options: unknown,
) {
  if (parent && parent.filename.includes("kiro.tsx")) {
    if (request === "../logger") return "mock:logger";
    if (request === "../config") return "mock:config";
    if (request === "./base") return "mock:base";
  }
  return originalResolve.call(this, request, parent, isMain, options);
};

require.cache["mock:logger"] = { id: "mock:logger", filename: "mock:logger", loaded: true, exports: mockLogger } as unknown as NodeModule;
require.cache["mock:config"] = { id: "mock:config", filename: "mock:config", loaded: true, exports: mockConfig } as unknown as NodeModule;
require.cache["mock:base"] = { id: "mock:base", filename: "mock:base", loaded: true, exports: mockBase } as unknown as NodeModule;

const kiroPath = path.join(process.cwd(), "src", "mitm", "handlers", "kiro.tsx");
// eslint-disable-next-line @typescript-eslint/no-require-imports
const kiro = require(kiroPath);

describe("Kiro MITM Handler", () => {
  it("initKiroState initializes state correctly", () => {
    const state = kiro.initKiroState("test-model");
    expect(state.modelId).toBe("test-model");
    expect(state.toolCallInit).toEqual({});
    expect(state.hasToolCalls).toBe(false);
    expect(state.finishSent).toBe(false);
  });

  it("extractThinking extracts thinking block", () => {
    const state = kiro.initKiroState("test-model");
    const res = kiro.extractThinking("<thinking>Deep thoughts</thinking>Hello world", state);
    expect(res.thinking).toBe("Deep thoughts");
    expect(res.text).toBe("Hello world");
  });

  it("processTextContent processes text frame", () => {
    const state = kiro.initKiroState("test-model");
    const textFrames = kiro.processTextContent("Hello Kiro!", state, "test-model");
    expect(textFrames.length).toBe(1);
  });

  it("processReasoningContent processes reasoning frame", () => {
    const reasoningFrame = kiro.processReasoningContent("Reasoning content", "test-model");
    expect(reasoningFrame instanceof Uint8Array || Buffer.isBuffer(reasoningFrame)).toBe(true);
  });

  it("processToolCalls processes tool calls", () => {
    const toolState = kiro.initKiroState("test-model");
    const toolCalls = [
      { index: 0, id: "call_123", function: { name: "test_func", arguments: '{"arg": 1}' } },
    ];
    const toolFrames = kiro.processToolCalls(toolCalls, toolState);
    expect(toolFrames.length).toBe(2);
    expect(toolState.hasToolCalls).toBe(true);
  });

  it("handleFlush produces flush frame", () => {
    const state = kiro.initKiroState("test-model");
    const flushFrame = kiro.handleFlush(state);
    expect(flushFrame instanceof Uint8Array || Buffer.isBuffer(flushFrame)).toBe(true);
  });

  it("convertOpenAIToKiro handles text chunk", () => {
    const s1 = kiro.initKiroState("claude-3-5-sonnet");
    const chunk1 = {
      model: "claude-3-5-sonnet",
      choices: [{ delta: { content: "Hello Kiro!" } }],
    };
    const frame1 = kiro.convertOpenAIToKiro(chunk1, s1);
    expect(frame1 instanceof Uint8Array || Buffer.isBuffer(frame1)).toBe(true);
    expect(s1.modelId).toBe("claude-3-5-sonnet");
  });
});
