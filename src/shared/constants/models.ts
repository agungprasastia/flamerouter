// Import directly from file to avoid pulling in server-side dependencies via index.js
export {
  PROVIDER_MODELS,
  getProviderModels,
  getDefaultModel,
  isValidModel as isValidModelCore,
  findModelName,
  getModelTargetFormat,
  getModelStrip,
  PROVIDER_ID_TO_ALIAS,
  getModelsByProviderId,
  getModelUpstreamId,
  getModelQuotaFamily,
} from "@/shared/constants/providerModels";

import { AI_PROVIDERS, isOpenAICompatibleProvider } from "./providers";
import { PROVIDER_MODELS as MODELS } from "@/shared/constants/providerModels";

// Providers that accept any model (passthrough)
const PASSTHROUGH_PROVIDERS = new Set(
  Object.entries(AI_PROVIDERS)
    .filter(([, p]) => p.passthroughModels)
    .map(([key]) => key),
);

// Wrap isValidModel with passthrough providers
export function isValidModel(aliasOrId: string, modelId: string): boolean {
  if (isOpenAICompatibleProvider(aliasOrId)) return true;
  if (PASSTHROUGH_PROVIDERS.has(aliasOrId)) return true;
  const models = (MODELS as Record<string, Array<{ id: string; [key: string]: unknown }>>)[aliasOrId];
  if (!models) return false;
  return models.some((m) => m.id === modelId);
}

// Legacy AI_MODELS for backward compatibility
export const AI_MODELS = Object.entries(MODELS).flatMap(([alias, models]) =>
  models.map((m) => ({ provider: alias, model: m.id, name: m.name })),
);

export const getModelKind = (m?: { kind?: string | null; type?: string | null; [key: string]: unknown } | null, fallback: string | null = null): string | null =>
  m?.kind || m?.type || fallback;

// Capacity metadata for UI badges — icon + label + color per capability.
export const CAPACITY_META = {
  vision: {
    icon: "visibility",
    label: "Vision",
    desc: "Supports image input",
    color: "text-blue-500",
  },
  // search: temporarily hidden (feature not wired yet)
  reasoning: {
    icon: "neurology",
    label: "Reasoning",
    desc: "Supports reasoning / thinking",
    color: "text-amber-500",
  },
};
