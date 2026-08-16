"use client";
/* eslint-disable react-hooks/set-state-in-effect */

import { useState, useEffect, useCallback } from "react";
import { getCapabilitiesForModel } from "@/shared/constants/capabilities";

export interface ModelCapsSummary {
  vision?: boolean;
  search?: boolean;
  reasoning?: boolean;
  contextWindow?: number;
  maxOutput?: number;
  [key: string]: unknown;
}

export interface ModelEntry {
  caps?: ModelCapsSummary;
  fullModel?: string;
  routedModel?: string;
  model?: string;
  [key: string]: unknown;
}

interface ModelCapsCache {
  byFull: Record<string, ModelCapsSummary>;
  byId: Record<string, ModelCapsSummary>;
}

// Module cache: one /api/models fetch shared by every useModelCaps instance.
let cache: ModelCapsCache | null = null;
let inflight: Promise<ModelCapsCache> | null = null;

function buildMaps(models?: ModelEntry[]): ModelCapsCache {
  const byFull: Record<string, ModelCapsSummary> = {};
  const byId: Record<string, ModelCapsSummary> = {};
  for (const m of models || []) {
    if (!m.caps) continue;
    if (m.fullModel) byFull[m.fullModel] = m.caps;
    if (m.routedModel) byFull[m.routedModel] = m.caps;
    if (m.model) byId[m.model] = m.caps;
  }
  return { byFull, byId };
}

function loadModelCaps(): Promise<ModelCapsCache> {
  if (cache) return Promise.resolve(cache);
  if (inflight) return inflight;
  inflight = fetch("/api/models")
    .then(async (res) => {
      if (!res.ok) throw new Error(`models ${res.status}`);
      const data = (await res.json()) as { models?: ModelEntry[] };
      cache = buildMaps(data.models);
      return cache;
    })
    .catch(() => {
      // Keep null so a later mount can retry
      return { byFull: {}, byId: {} };
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

// Resolve caps from a "provider/model" string or a bare model id.
function resolveCaps(
  byFull: Record<string, ModelCapsSummary>,
  byId: Record<string, ModelCapsSummary>,
  key: string | null | undefined
): ModelCapsSummary | null {
  if (!key) return null;
  if (byFull[key]) return byFull[key];
  const bare = key.includes("/") ? key.slice(key.indexOf("/") + 1) : key;
  if (byId[bare]) return byId[bare];
  const provider = key.includes("/") ? key.slice(0, key.indexOf("/")) : null;
  const c = getCapabilitiesForModel(provider, bare);
  return {
    vision: c.vision,
    search: c.search,
    reasoning: c.reasoning,
    contextWindow: c.contextWindow,
    maxOutput: c.maxOutput,
  };
}

export function useModelCaps() {
  const [byFull, setByFull] = useState<Record<string, ModelCapsSummary>>(() => cache?.byFull || {});
  const [byId, setById] = useState<Record<string, ModelCapsSummary>>(() => cache?.byId || {});

  useEffect(() => {
    if (cache) {
      setByFull(cache.byFull);
      setById(cache.byId);
      return;
    }
    let alive = true;
    loadModelCaps().then((maps) => {
      if (alive) {
        setByFull(maps.byFull);
        setById(maps.byId);
      }
    });
    return () => {
      alive = false;
    };
  }, []);

  const getCaps = useCallback(
    (key: string | null | undefined) => resolveCaps(byFull, byId, key),
    [byFull, byId],
  );

  return { getCaps };
}
