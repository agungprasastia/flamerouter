const assert = require("assert");
const fs = require("fs");
const path = require("path");
const Module = require("module");

const mockLogger = { err: () => {}, log: () => {} };
const mockConfig = { IS_DEV: false };
const mockBase = { fetchRouter: async () => {}, pipeTransformedEventStream: async () => {} };

// Intercept module resolution to point TypeScript files or mock modules
const originalResolve = Module._resolveFilename;
Module._resolveFilename = function(request, parent, isMain, options) {
  if (parent && parent.filename.includes("kiro.tsx")) {
    if (request === "../logger") return "mock:logger";
    if (request === "../config") return "mock:config";
    if (request === "./base") return "mock:base";
  }
  return originalResolve.call(this, request, parent, isMain, options);
};

require.cache["mock:logger"] = { id: "mock:logger", filename: "mock:logger", loaded: true, exports: mockLogger };
require.cache["mock:config"] = { id: "mock:config", filename: "mock:config", loaded: true, exports: mockConfig };
require.cache["mock:base"] = { id: "mock:base", filename: "mock:base", loaded: true, exports: mockBase };

const kiroPath = path.join(process.cwd(), "src", "mitm", "handlers", "kiro.tsx");
const kiro = require(kiroPath);

console.log("Testing Kiro MITM Handler refactored functions...");

// Test 1: initKiroState
const state = kiro.initKiroState("test-model");
assert.strictEqual(state.modelId, "test-model");
assert.deepStrictEqual(state.toolCallInit, {});
assert.strictEqual(state.hasToolCalls, false);
assert.strictEqual(state.finishSent, false);
console.log("✓ initKiroState test passed");

// Test 2: extractThinking
const res = kiro.extractThinking("<thinking>Deep thoughts</thinking>Hello world", state);
assert.strictEqual(res.thinking, "Deep thoughts");
assert.strictEqual(res.text, "Hello world");
console.log("✓ extractThinking test passed");

// Test 3: processTextContent
const textFrames = kiro.processTextContent("Hello Kiro!", state, "test-model");
assert.strictEqual(textFrames.length, 1);
console.log("✓ processTextContent test passed");

// Test 4: processReasoningContent
const reasoningFrame = kiro.processReasoningContent("Reasoning content", "test-model");
assert.ok(reasoningFrame instanceof Uint8Array || Buffer.isBuffer(reasoningFrame));
console.log("✓ processReasoningContent test passed");

// Test 5: processToolCalls
const toolState = kiro.initKiroState("test-model");
const toolCalls = [
  { index: 0, id: "call_123", function: { name: "test_func", arguments: '{"arg": 1}' } }
];
const toolFrames = kiro.processToolCalls(toolCalls, toolState);
assert.strictEqual(toolFrames.length, 2); // 1 init frame + 1 fragment frame
assert.strictEqual(toolState.hasToolCalls, true);
console.log("✓ processToolCalls test passed");

// Test 6: handleFlush
const flushFrame = kiro.handleFlush(state);
assert.ok(flushFrame instanceof Uint8Array || Buffer.isBuffer(flushFrame));
console.log("✓ handleFlush test passed");

// Test 7: convertOpenAIToKiro with text chunk
const s1 = kiro.initKiroState("claude-3-5-sonnet");
const chunk1 = {
  model: "claude-3-5-sonnet",
  choices: [{ delta: { content: "Hello Kiro!" } }],
};
const frame1 = kiro.convertOpenAIToKiro(chunk1, s1);
assert.ok(frame1 instanceof Uint8Array || Buffer.isBuffer(frame1));
assert.strictEqual(s1.modelId, "claude-3-5-sonnet");
console.log("✓ convertOpenAIToKiro chunk test passed");

console.log("All Kiro handler unit tests passed successfully!");
