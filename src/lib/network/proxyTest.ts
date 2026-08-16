import { ProxyAgent, fetch as undiciFetch } from "undici";

const DEFAULT_TEST_URL = "https://google.com/";
const DEFAULT_TIMEOUT_MS = 8000;

interface ErrorLike {
  message?: string;
  code?: string;
  cause?: {
    code?: string;
    message?: string;
  };
}

function getErrorMessage(err: unknown): string {
  if (!err) return "Unknown error";
  const e = err as ErrorLike;
  const base = e?.message || String(err);
  const causeCode = e?.cause?.code || e?.code;
  const causeMessage = e?.cause?.message;

  if (causeMessage && causeMessage !== base) {
    return causeCode
      ? `${base}: ${causeMessage} (${causeCode})`
      : `${base}: ${causeMessage}`;
  }

  if (causeCode && !base.includes(causeCode)) {
    return `${base} (${causeCode})`;
  }

  return base;
}

function normalizeString(value?: unknown): string {
  if (value === undefined || value === null) return "";
  return String(value).trim();
}

export interface TestProxyUrlOptions {
  proxyUrl?: string;
  testUrl?: string;
  timeoutMs?: number | string;
}

export interface TestProxyResult {
  ok: boolean;
  status: number;
  statusText?: string;
  url?: string;
  elapsedMs?: number;
  error?: string;
}

export async function testProxyUrl({ proxyUrl, testUrl, timeoutMs }: TestProxyUrlOptions = {}): Promise<TestProxyResult> {
  const normalizedProxyUrl = normalizeString(proxyUrl);
  if (!normalizedProxyUrl) {
    return { ok: false, status: 400, error: "proxyUrl is required" };
  }

  const normalizedTestUrl = normalizeString(testUrl) || DEFAULT_TEST_URL;
  const timeoutMsRaw = Number(timeoutMs);
  const normalizedTimeoutMs =
    Number.isFinite(timeoutMsRaw) && timeoutMsRaw > 0
      ? Math.min(timeoutMsRaw, 30000)
      : DEFAULT_TIMEOUT_MS;

  let dispatcher: ProxyAgent | undefined;

  try {
    try {
      dispatcher = new ProxyAgent({ uri: normalizedProxyUrl });
    } catch (err: unknown) {
      const e = err as Error;
      return {
        ok: false,
        status: 400,
        error: `Invalid proxy URL: ${e?.message || String(err)}`,
      };
    }

    const controller = new AbortController();
    const startedAt = Date.now();
    const timer = setTimeout(() => controller.abort(), normalizedTimeoutMs);

    try {
      const res = await undiciFetch(normalizedTestUrl, {
        method: "HEAD",
        dispatcher,
        signal: controller.signal,
        headers: {
          "User-Agent": "FlameRouter",
        },
      });

      return {
        ok: res.ok,
        status: res.status,
        statusText: res.statusText,
        url: normalizedTestUrl,
        elapsedMs: Date.now() - startedAt,
      };
    } catch (err: unknown) {
      const e = err as { name?: string };
      const message =
        e?.name === "AbortError"
          ? "Proxy test timed out"
          : getErrorMessage(err);
      return { ok: false, status: 500, error: message };
    } finally {
      clearTimeout(timer);
    }
  } finally {
    try {
      await dispatcher?.close?.();
    } catch {
      // ignore
    }
  }
}
