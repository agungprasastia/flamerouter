// Provider definitions
import REGISTRY from "@/shared/constants/providersRegistry";
import { RISK_NOTICE } from "@/shared/constants/providersDisplay";

// Antigravity OAuth client credentials (public CLI client)
export const ANTIGRAVITY_OAUTH_CLIENT = {
  clientId: "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
  clientSecret: process.env.ANTIGRAVITY_OAUTH_CLIENT_SECRET || "",
};

// Gemini (Google) OAuth client credentials (public CLI client)
export const GOOGLE_OAUTH_CLIENT = {
  clientId: "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com",
  clientSecret: process.env.GOOGLE_OAUTH_CLIENT_SECRET || process.env.GEMINI_OAUTH_CLIENT_SECRET || "",
};

const MEDIA_ENTRY_KEYS = [
  "serviceKinds",
  "ttsConfig",
  "sttConfig",
  "embeddingConfig",
  "imageConfig",
  "imageToTextConfig",
  "videoConfig",
  "musicConfig",
  "searchViaChat",
  "searchConfig",
  "fetchConfig",
  "modelsFetcher",
  "mediaPriority",
  "hiddenKinds",
];

export interface ProviderEntry {
  id: string;
  alias?: string;
  name?: string;
  hidden?: boolean;
  priority?: number;
  mediaPriority?: number;
  hasFree?: boolean;
  thinkingConfig?: unknown;
  regions?: string[];
  defaultRegion?: string;
  hasProviderSpecificData?: boolean;
  noAuth?: boolean;
  passthroughModels?: boolean;
  hasOAuth?: boolean;
  authModes?: string[];
  authType?: string;
  authHint?: string;
  serviceKinds?: string[];
  hiddenKinds?: string[];
  [key: string]: unknown;
}

// Build provider UI object from registry entry
function buildProviderEntry(r: Record<string, unknown>): ProviderEntry {
  const mediaFields: Record<string, unknown> = {};
  if (r.media && typeof r.media === "object") Object.assign(mediaFields, r.media);
  for (const k of MEDIA_ENTRY_KEYS) {
    if (r[k] !== undefined) mediaFields[k] = r[k];
  }
  const display = { ...((r.display as Record<string, unknown>) || {}) };
  if (display.deprecationNotice === "RISK_NOTICE")
    display.deprecationNotice = RISK_NOTICE;
  return {
    ...display,
    id: String(r.id),
    alias: (r.uiAlias || r.alias) as string | undefined,
    ...(r.hidden ? { hidden: true } : {}),
    ...mediaFields,
    ...(r.priority !== undefined ? { priority: Number(r.priority) } : {}),
    ...(r.hasFree ? { hasFree: true } : {}),
    ...(r.thinkingConfig ? { thinkingConfig: r.thinkingConfig } : {}),
    ...(r.regions
      ? { regions: r.regions as string[], defaultRegion: r.defaultRegion as string }
      : {}),
    ...(r.hasProviderSpecificData ? { hasProviderSpecificData: true } : {}),
    ...(r.noAuth ? { noAuth: true } : {}),
    ...(r.passthroughModels ? { passthroughModels: true } : {}),
    ...(r.hasOAuth ? { hasOAuth: true } : {}),
    ...(r.authModes ? { authModes: r.authModes as string[] } : {}),
    ...(r.authType ? { authType: r.authType as string } : {}),
    ...(r.authHint ? { authHint: r.authHint as string } : {}),
  };
}

const byCategory = (cat: string): Record<string, ProviderEntry> =>
  Object.fromEntries(
    (REGISTRY as Array<Record<string, unknown>>)
      .filter((r) => r.category === cat)
      .map((r) => [
        String(r.id),
        buildProviderEntry(r),
      ]),
  );

export const FREE_PROVIDERS = byCategory("free");
export const FREE_TIER_PROVIDERS = byCategory("freeTier");

// Thinking config definitions
// options: list of selectable modes ("auto" = no override from server)
// defaultMode: fallback when user hasn't configured
// extended: claude-style thinking (thinking.type + budget_tokens) — used by most providers
// effort: openai-style reasoning_effort — only openai + codex
export const THINKING_CONFIG = {
  extended: {
    options: ["auto", "on", "off"],
    defaultMode: "auto",
    defaultBudgetTokens: 10000,
  },
  effort: {
    options: ["auto", "none", "low", "medium", "high"],
    defaultMode: "auto",
  },
};

export const OAUTH_PROVIDERS = byCategory("oauth");
export const APIKEY_PROVIDERS = byCategory("apikey");

// Web Cookie Providers (use browser session cookie instead of API key)
export const WEB_COOKIE_PROVIDERS = byCategory("webCookie");

// Media provider kinds — each kind maps to a route and endpoint config
export const MEDIA_PROVIDER_KINDS = [
  {
    id: "embedding",
    label: "Embedding",
    icon: "data_array",
    endpoint: { method: "POST", path: "/v1/embeddings" },
  },
  {
    id: "image",
    label: "Text to Image",
    icon: "brush",
    endpoint: { method: "POST", path: "/v1/images/generations" },
  },
  {
    id: "imageToText",
    label: "Image to Text",
    icon: "image_search",
    endpoint: { method: "POST", path: "/v1/images/understanding" },
  },
  {
    id: "tts",
    label: "Text To Speech",
    icon: "record_voice_over",
    endpoint: { method: "POST", path: "/v1/audio/speech" },
  },
  {
    id: "stt",
    label: "Speech To Text",
    icon: "mic",
    endpoint: { method: "POST", path: "/v1/audio/transcriptions" },
  },
  {
    id: "webSearch",
    label: "Web Search",
    icon: "travel_explore",
    endpoint: { method: "POST", path: "/v1/search" },
  },
  {
    id: "webFetch",
    label: "Web Fetch",
    icon: "language",
    endpoint: { method: "POST", path: "/v1/web/fetch" },
  },
  {
    id: "video",
    label: "Video",
    icon: "movie",
    endpoint: { method: "POST", path: "/v1/videos/generations" },
  },
  {
    id: "music",
    label: "Music",
    icon: "music_note",
    endpoint: { method: "POST", path: "/v1/audio/music" },
  },
];

export const OPENAI_COMPATIBLE_PREFIX = "openai-compatible-";
export const ANTHROPIC_COMPATIBLE_PREFIX = "anthropic-compatible-";
export const CUSTOM_EMBEDDING_PREFIX = "custom-embedding-";

export function isOpenAICompatibleProvider(providerId?: string | null): boolean {
  return (
    typeof providerId === "string" &&
    providerId.startsWith(OPENAI_COMPATIBLE_PREFIX)
  );
}

export function isAnthropicCompatibleProvider(providerId?: string | null): boolean {
  return (
    typeof providerId === "string" &&
    providerId.startsWith(ANTHROPIC_COMPATIBLE_PREFIX)
  );
}

export function isCustomEmbeddingProvider(providerId?: string | null): boolean {
  return (
    typeof providerId === "string" &&
    providerId.startsWith(CUSTOM_EMBEDDING_PREFIX)
  );
}

// All providers (combined)
export const AI_PROVIDERS: Record<string, ProviderEntry> = {
  ...FREE_PROVIDERS,
  ...FREE_TIER_PROVIDERS,
  ...OAUTH_PROVIDERS,
  ...APIKEY_PROVIDERS,
  ...WEB_COOKIE_PROVIDERS,
};

export const AUTH_METHODS = {
  api_key: { id: "api_key", name: "API Key" },
  oauth2: { id: "oauth2", name: "OAuth2" },
  oauth: { id: "oauth", name: "OAuth" },
  apikey: { id: "apikey", name: "API Key" },
  cookie: { id: "cookie", name: "Cookie" },
};

// Helper: Get provider by alias
export function getProviderByAlias(alias?: string | null): ProviderEntry | null {
  if (!alias) return null;
  for (const provider of Object.values(AI_PROVIDERS)) {
    if (provider.alias === alias || provider.id === alias) {
      return provider;
    }
  }
  return null;
}

// Helper: Get provider ID from alias
export function resolveProviderId(aliasOrId: string): string {
  const provider = getProviderByAlias(aliasOrId);
  return provider?.id || aliasOrId;
}

// Helper: Get alias from provider ID
export function getProviderAlias(providerId: string): string {
  const provider = AI_PROVIDERS[providerId];
  return provider?.alias || providerId;
}

// Alias to ID mapping (for quick lookup)
export const ALIAS_TO_ID: Record<string, string> = Object.values(AI_PROVIDERS).reduce((acc: Record<string, string>, p) => {
  if (p.alias) acc[p.alias] = p.id;
  return acc;
}, {});

// ID to Alias mapping
export const ID_TO_ALIAS: Record<string, string> = Object.values(AI_PROVIDERS).reduce((acc: Record<string, string>, p) => {
  if (p.alias) acc[p.id] = p.alias;
  return acc;
}, {});

// Helper: Get providers by service kind (e.g. "tts", "embedding", "image")
// Providers without serviceKinds default to ["llm"]
export function getProvidersByKind(kind: string): ProviderEntry[] {
  return Object.values(AI_PROVIDERS)
    .filter((p) => {
      const kinds = p.serviceKinds ?? ["llm"];
      if (!kinds.includes(kind)) return false;
      if (p.hidden) return false;
      if (p.hiddenKinds?.includes(kind)) return false;
      return true;
    })
    .sort(
      (a, b) =>
        (a.priority ?? a.mediaPriority ?? 999) -
        (b.priority ?? b.mediaPriority ?? 999),
    );
}

// Derive từ registry features flags
export const USAGE_SUPPORTED_PROVIDERS = REGISTRY.filter(
  (r) => r.features?.usage,
).map((r) => r.id);

export const USAGE_APIKEY_PROVIDERS = REGISTRY.filter(
  (r) => r.features?.usageApikey,
).map((r) => r.id);

export const PROVIDERS = AI_PROVIDERS;
export default AI_PROVIDERS;

