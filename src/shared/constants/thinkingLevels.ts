// Resolve valid thinking levels per model — drives UI level picker (suffix "model(level)").
import { getCapabilitiesForModel } from "./capabilities";
import { matchPattern } from "./pricing";

export function resolveKiroEffortPath(model: string) {
  if (typeof model !== "string") return null;
  const normalized = model.toLowerCase().replace(/-/g, ".");
  if (/(?:^|[/.])gpt[/.]5[/.]6(?:[/.]|$)/.test(normalized)) {
    return "reasoning";
  }
  if (!normalized.includes("claude")) return null;
  const match = normalized.match(/(?:^|[/.])claude(?:[/.][a-z]+)*[/.](\d+)(?:[/.](\d+))?(?:[/.]|$)/);
  if (!match) return null;
  const [, majorText, minorText] = match;
  const major = Number(majorText);
  const minor = minorText === undefined ? null : Number(minorText);
  const dateSuffixMinor = minor !== null && minor >= 1000;
  return major < 4 || (major === 4 && (minor === null || minor <= 5 || dateSuffixMinor))
    ? null
    : "output_config";
}

// Shared level sets (deduped) — verified against provider docs + wire in thinkingUnified.applyFormat.
const L = {
  base: ["none", "low", "medium", "high"],                          // qwen, step, hunyuan, gemini-budget
  onOff: ["none", "thinking"],                                      // zai (binary), minimax (adaptive)
  openai: ["none", "minimal", "low", "medium", "high", "xhigh"],    // GPT-5.x / o-series (no "max")
  levelMax: ["none", "low", "medium", "high", "max"],               // claude-adaptive, kimi
  budgetX: ["none", "low", "medium", "high", "xhigh", "max"],       // claude-budget
  gemini: ["minimal", "low", "medium", "high"],                     // gemini-3 thinkingLevel (no disable)
  hiMax: ["none", "high", "max"],                                   // deepseek (low/med→high, xhigh→max)
};

// thinkingFormat → valid selectable levels (source of truth for UI options).
const FORMAT_LEVELS: Record<string, string[]> = {
  openai: L.openai,
  "claude-adaptive": L.levelMax,
  "claude-budget": L.budgetX,
  "gemini-level": L.gemini,
  "gemini-budget": L.base,
  zai: L.onOff,
  qwen: L.base,
  kimi: L.levelMax,
  deepseek: L.hiMax,
  minimax: L.onOff,
  hunyuan: L.base,
  step: L.base,
};

const CODEX_GPT_5_6_LEVELS = ["none", "minimal", "low", "medium", "high", "xhigh", "max"];

// Model-name pattern overrides (glob, first match wins) — more precise than format default.
const PATTERN_THINKING = [
  { provider: "codex", pattern: "*gpt-5.6-sol*", levels: [...CODEX_GPT_5_6_LEVELS, "ultra"] },
  { provider: "codex", pattern: "*gpt-5.6-terra*", levels: [...CODEX_GPT_5_6_LEVELS, "ultra"] },
  { provider: "codex", pattern: "*gpt-5.6-luna*", levels: CODEX_GPT_5_6_LEVELS },
  { pattern: "*codex*", levels: ["low", "medium", "high", "xhigh"] }, // codex cannot disable thinking
];

// Returns valid thinking levels for a model, or null when the model has no reasoning.
export function getThinkingLevels(provider: string, model: string) {
  if (provider === "kiro" && resolveKiroEffortPath(model) === null) return null;
  const caps = getCapabilitiesForModel(provider, model);
  if (!caps.reasoning) return null;
  const hit = PATTERN_THINKING.find((entry) =>
    (!entry.provider || entry.provider === provider) && matchPattern(entry.pattern, model)
  );
  let levels = hit?.levels || FORMAT_LEVELS[caps.thinkingFormat] || L.base;
  if (caps.thinkingCanDisable === false) levels = levels.filter((l) => l !== "none");
  return levels;
}
