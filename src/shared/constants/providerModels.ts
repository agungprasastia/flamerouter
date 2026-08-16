import REGISTRY from "@/shared/constants/providersRegistry";

export const CODEX_REVIEW_SUFFIX = "-review";

export function modelQuotaFamily(model: any) {
  return model?.quotaFamily || model?.family || null;
}

export function modelStrip(model: any) {
  return model?.strip || [];
}

export function modelTargetFormat(model: any) {
  return model?.targetFormat || null;
}

export function modelSupportedFormats(model: any) {
  return model?.supportedFormats || null;
}

export function normalizeModelId(id: string) {
  return typeof id === "string" ? id.replace(/-(\d+)-(\d+)/g, ".$1.$2") : id;
}

// Build PROVIDER_MODELS from REGISTRY
export const PROVIDER_MODELS: Record<string, any[]> = {};
for (const entry of REGISTRY) {
  const key = entry.alias || entry.id;
  PROVIDER_MODELS[key] = entry.models || [];
  if (entry.id && !PROVIDER_MODELS[entry.id]) {
    PROVIDER_MODELS[entry.id] = entry.models || [];
  }
}

// Helper functions
export function getProviderModels(aliasOrId: string) {
  return PROVIDER_MODELS[aliasOrId] || [];
}

export function getDefaultModel(aliasOrId: string) {
  const models = PROVIDER_MODELS[aliasOrId];
  return models?.[0]?.id || null;
}

// Providers whose registry uses dots in version numbers (e.g. "claude-sonnet-4.5").
const DOT_VERSION_PROVIDERS = new Set(["kr", "kiro"]);

function findModel(models: any[], modelId: string, aliasOrId?: string) {
  if (!models) return undefined;
  const found = models.find((m) => m.id === modelId);
  if (found) return found;
  if (!aliasOrId || !DOT_VERSION_PROVIDERS.has(aliasOrId)) return undefined;
  const normalized = normalizeModelId(modelId);
  if (normalized === modelId) return undefined;
  return models.find((m) => m.id === normalized);
}

export function isValidModel(
  aliasOrId: string,
  modelId: string,
  passthroughProviders = new Set<string>(),
) {
  if (passthroughProviders.has(aliasOrId)) return true;
  const models = PROVIDER_MODELS[aliasOrId];
  if (!models) return false;
  return !!findModel(models, modelId, aliasOrId);
}

export function findModelName(aliasOrId: string, modelId: string) {
  const models = PROVIDER_MODELS[aliasOrId];
  if (!models) return modelId;
  const found = findModel(models, modelId, aliasOrId);
  return found?.name || modelId;
}

export function getModelTargetFormat(aliasOrId: string, modelId: string) {
  const models = PROVIDER_MODELS[aliasOrId];
  if (!models) return null;
  return modelTargetFormat(findModel(models, modelId, aliasOrId));
}

export function getModelSupportedFormats(aliasOrId: string, modelId: string) {
  const models = PROVIDER_MODELS[aliasOrId];
  if (!models) return null;
  return modelSupportedFormats(findModel(models, modelId, aliasOrId));
}

export function getModelType(aliasOrId: string, modelId: string) {
  const models = PROVIDER_MODELS[aliasOrId];
  if (!models) return null;
  const found = findModel(models, modelId, aliasOrId);
  return found?.kind || found?.type || null;
}

export function getModelUpstreamId(aliasOrId: string, modelId: string) {
  const sufMatch =
    typeof modelId === "string" ? modelId.match(/\([^()]+\)\s*$/) : null;
  const suffix = sufMatch ? sufMatch[0] : "";
  const baseId = suffix ? modelId.slice(0, sufMatch.index).trim() : modelId;
  const models = PROVIDER_MODELS[aliasOrId];
  const found = findModel(models, baseId, aliasOrId);
  const resolvedId = found?.upstreamModelId || found?.id;
  if (resolvedId) {
    const presetMatch = resolvedId.match(/\([^()]+\)\s*$/);
    const presetSuffix = presetMatch?.[0] || "";
    const resolvedBase = presetSuffix
      ? resolvedId.slice(0, presetMatch.index).trim()
      : resolvedId;
    return resolvedBase + (suffix || presetSuffix);
  }
  if (
    aliasOrId === "cx" &&
    typeof baseId === "string" &&
    baseId.endsWith(CODEX_REVIEW_SUFFIX)
  ) {
    return baseId.slice(0, -CODEX_REVIEW_SUFFIX.length) + suffix;
  }
  return baseId + suffix;
}

export function getModelQuotaFamily(aliasOrId: string, modelId: string) {
  const models = PROVIDER_MODELS[aliasOrId];
  return modelQuotaFamily(findModel(models, modelId, aliasOrId));
}

export const OAUTH_ALIASES = Object.fromEntries(
  REGISTRY.filter((r: any) => r.alias && r.alias !== r.id).map((r: any) => [
    r.id,
    r.alias,
  ]),
);

export const PROVIDER_ID_TO_ALIAS = Object.fromEntries(
  REGISTRY.map((r: any) => [r.id, r.alias || r.id]),
);

export function getModelsByProviderId(providerId: string) {
  const alias = PROVIDER_ID_TO_ALIAS[providerId] || providerId;
  return PROVIDER_MODELS[alias] || [];
}

export function getModelStrip(alias: string, modelId: string) {
  return modelStrip(findModel(PROVIDER_MODELS[alias], modelId, alias));
}
