import { pathToFileURL } from "url";
import { getInstallInfo, libraryEntry } from "./install";

interface PxpipeModule {
  transformAnthropicMessages: (opts: { body: Uint8Array; model?: string; [key: string]: unknown }) => Promise<{
    applied: boolean;
    reason?: string;
    body: Uint8Array;
    tokensBeforeEst?: number;
    tokensAfterEst?: number;
    tokensSavedEst?: number;
    imageCount?: number;
    durationMs?: number;
    [key: string]: unknown;
  }>;
}

interface LoadedPxpipe {
  module: PxpipeModule;
  version: string | null;
  loadedAt: number;
}

let cached: LoadedPxpipe | null = null;
let loadPromise: Promise<LoadedPxpipe> | null = null;

export function getLoadedInfo(): { loaded: boolean; version?: string | null; loadedAt?: number } {
  return cached
    ? { loaded: true, version: cached.version, loadedAt: cached.loadedAt }
    : { loaded: false };
}

export async function loadPxpipe(): Promise<LoadedPxpipe> {
  if (cached) return cached;
  if (loadPromise) return loadPromise;
  loadPromise = doLoad().finally(() => {
    loadPromise = null;
  });
  return loadPromise;
}

async function doLoad(): Promise<LoadedPxpipe> {
  const info = getInstallInfo();
  if (!info.installed) {
    const err = new Error("PXPIPE is not installed") as Error & { code?: string };
    err.code = "NOT_INSTALLED";
    throw err;
  }
  // Cache-bust per version so Repair/upgrade takes effect without a server restart.
  const url = `${pathToFileURL(libraryEntry()).href}?v=${encodeURIComponent(info.version || "0")}`;
  const mod = (await import(/* webpackIgnore: true */ url)) as PxpipeModule;
  if (typeof mod.transformAnthropicMessages !== "function") {
    throw new Error(
      "installed pxpipe package does not export transformAnthropicMessages",
    );
  }
  cached = { module: mod, version: info.version, loadedAt: Date.now() };
  return cached;
}

export function unloadPxpipe(): boolean {
  const wasLoaded = !!cached;
  cached = null;
  return wasLoaded;
}

export interface GetTransformOptions {
  autoLoad?: boolean;
}

// Transform function for the request pipeline; null when unavailable (fail-open).
// autoLoad controls whether a cold cache triggers a load (first request warms it).
export async function getTransform({ autoLoad = true }: GetTransformOptions = {}): Promise<PxpipeModule["transformAnthropicMessages"] | null> {
  try {
    if (!cached && !autoLoad) return null;
    const { module: mod } = await loadPxpipe();
    return mod.transformAnthropicMessages;
  } catch {
    return null;
  }
}

// Health self-test: run a tiny synthetic Claude request through the transformer.
// A healthy module parses it and answers with a machine-readable reason.
export async function selfTest(): Promise<{ ok: boolean; reason?: string; durationMs: number }> {
  const startedAt = Date.now();
  const { module: mod } = await loadPxpipe();
  const body = new TextEncoder().encode(
    JSON.stringify({
      model: "claude-fable-5",
      max_tokens: 16,
      messages: [{ role: "user", content: "ping" }],
    }),
  );
  const result = await mod.transformAnthropicMessages({
    body,
    model: "claude-fable-5",
  });
  if (
    !result ||
    typeof result.applied !== "boolean" ||
    !(result.body instanceof Uint8Array)
  ) {
    throw new Error("transform returned an unexpected shape");
  }
  return {
    ok: true,
    reason: result.reason,
    durationMs: Date.now() - startedAt,
  };
}
