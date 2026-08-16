import { getAdapter } from "../driver";
import { parseJson, stringifyJson } from "../helpers/jsonCol";
import { makeKv } from "../helpers/kvStore";

export interface ModelPricing {
  input?: number;
  output?: number;
  cached?: number;
  reasoning?: number;
  cache_creation?: number;
  [key: string]: number | undefined;
}

export type PricingMap = Record<string, Record<string, ModelPricing>>;

const pricingKv = makeKv("pricing");
const CACHE_TTL_MS = 5000;

let cache: { value: PricingMap | null; expiresAt: number } = { value: null, expiresAt: 0 };

function invalidate() {
  cache = { value: null, expiresAt: 0 };
}

async function getUserPricing(): Promise<Record<string, Record<string, ModelPricing>>> {
  return (await pricingKv.getAll()) as Record<string, Record<string, ModelPricing>>;
}

export async function getPricing(): Promise<PricingMap> {
  const now = Date.now();
  if (cache.value && cache.expiresAt > now) return cache.value;

  const userPricing = await getUserPricing();
  const { PROVIDER_PRICING } = await import("@/shared/constants/pricing");
  const merged: PricingMap = {};

  for (const [provider, models] of Object.entries(PROVIDER_PRICING as unknown as PricingMap)) {
    merged[provider] = { ...models };
    if (userPricing[provider]) {
      for (const [model, pricing] of Object.entries(userPricing[provider])) {
        merged[provider][model] = merged[provider][model]
          ? { ...merged[provider][model], ...pricing }
          : pricing;
      }
    }
  }

  for (const [provider, models] of Object.entries(userPricing)) {
    if (!merged[provider]) {
      merged[provider] = { ...models };
    } else {
      for (const [model, pricing] of Object.entries(models)) {
        if (!merged[provider][model]) merged[provider][model] = pricing;
      }
    }
  }

  cache = { value: merged, expiresAt: now + CACHE_TTL_MS };
  return merged;
}

export async function getPricingForModel(provider?: string | null, model?: string | null): Promise<ModelPricing | null> {
  if (!model) return null;
  const userPricing = await getUserPricing();
  if (provider && userPricing[provider]?.[model])
    return userPricing[provider][model] ?? null;
  const { getPricingForModel: resolveConst } =
    await import("@/shared/constants/pricing");
  return resolveConst(provider, model);
}

// Atomic merge inside transaction (per-provider read-modify-write)
export async function updatePricing(pricingData: PricingMap): Promise<Record<string, Record<string, ModelPricing>>> {
  const db = await getAdapter();
  db.transaction(() => {
    for (const [provider, models] of Object.entries(pricingData)) {
      const row = db.get<{ value: string }>(
        `SELECT value FROM kv WHERE scope = 'pricing' AND key = ?`,
        [provider],
      );
      const current: Record<string, ModelPricing> = row ? (parseJson<Record<string, ModelPricing>>(row.value, {}) || {}) : {};
      const merged = { ...current };
      for (const [model, pricing] of Object.entries(models)) {
        merged[model] = pricing;
      }
      db.run(
        `INSERT INTO kv(scope, key, value) VALUES('pricing', ?, ?) ON CONFLICT(scope, key) DO UPDATE SET value = excluded.value`,
        [provider, stringifyJson(merged)],
      );
    }
  });
  invalidate();
  return await getUserPricing();
}

export async function resetPricing(provider?: string | null, model?: string | null): Promise<Record<string, Record<string, ModelPricing>>> {
  if (!provider) return await getUserPricing();
  const db = await getAdapter();
  db.transaction(() => {
    if (!model) {
      db.run(`DELETE FROM kv WHERE scope = 'pricing' AND key = ?`, [provider]);
      return;
    }
    const row = db.get<{ value: string }>(
      `SELECT value FROM kv WHERE scope = 'pricing' AND key = ?`,
      [provider],
    );
    const current: Record<string, ModelPricing> = row ? (parseJson<Record<string, ModelPricing>>(row.value, {}) || {}) : {};
    delete current[model];
    if (Object.keys(current).length === 0) {
      db.run(`DELETE FROM kv WHERE scope = 'pricing' AND key = ?`, [provider]);
    } else {
      db.run(
        `INSERT INTO kv(scope, key, value) VALUES('pricing', ?, ?) ON CONFLICT(scope, key) DO UPDATE SET value = excluded.value`,
        [provider, stringifyJson(current)],
      );
    }
  });
  invalidate();
  return await getUserPricing();
}

export async function resetAllPricing(): Promise<Record<string, unknown>> {
  await pricingKv.clear();
  invalidate();
  return {};
}
