import { WINDSURF_CONFIG } from "../constants/oauth";
import { extractJsonPath } from "./_shared";

// ───────────────────────────────────────────────────────────────────────────
// Windsurf OAuth helpers
// ───────────────────────────────────────────────────────────────────────────

interface WindsurfConfigLike {
  userAgent?: string;
  registerApiBaseUrl?: string;
  registerPath?: string;
  defaultApiServerUrl?: string;
  oneTimeAuthPath?: string;
  currentUserPath?: string;
  authPageUrl?: string;
  tokenLifetimeDays?: number;
  callbackPath?: string;
  [key: string]: unknown;
}

async function windsurfSeatRequest(
  baseUrl: string,
  path: string,
  body: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  const url = `${baseUrl.replace(/\/$/, "")}${path}`;
  const res = await fetch(url, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      "User-Agent":
        (WINDSURF_CONFIG as unknown as WindsurfConfigLike).userAgent ||
        "FlameRouter",
    },
    body: JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok)
    throw new Error(
      `Windsurf ${path} HTTP ${res.status}: ${text.slice(0, 200)}`,
    );
  try {
    return JSON.parse(text) as Record<string, unknown>;
  } catch {
    throw new Error(`Windsurf ${path} invalid JSON`);
  }
}

// Parse Windsurf callback (query string or full URL): ?access_token=...&state=...
function parseWindsurfCallback(raw?: string | null, expectedState?: string) {
  const text = String(raw || "").trim();
  let queryStr = text;
  if (text.includes("?")) queryStr = text.slice(text.indexOf("?") + 1);
  if (text.startsWith("#")) queryStr = text.slice(1);
  const params = Object.fromEntries(new URLSearchParams(queryStr));
  const pick = (keys: string[]): string | null => {
    for (const k of keys) {
      const v = params[k];
      if (v && String(v).trim()) return String(v).trim();
    }
    return null;
  };
  const err = pick(["error"]);
  if (err) {
    const desc = pick(["error_description"]);
    throw new Error(
      desc
        ? `Windsurf auth failed: ${err} (${desc})`
        : `Windsurf auth failed: ${err}`,
    );
  }
  const accessToken = pick(["access_token", "token"]);
  if (!accessToken) throw new Error("Windsurf callback missing access_token");
  const state = pick(["state"]);
  if (expectedState && state && state !== expectedState) {
    throw new Error("Windsurf callback state mismatch");
  }
  return { firebaseIdToken: accessToken };
}

// POST RegisterUser {firebase_id_token} → {apiKey, apiServerUrl, name}
async function fetchWindsurfRegisterUser(firebaseIdToken: string) {
  const cfg = WINDSURF_CONFIG as unknown as WindsurfConfigLike;
  const data = await windsurfSeatRequest(
    cfg.registerApiBaseUrl || "https://api.codeium.com",
    cfg.registerPath || "/api/v1/register",
    {
      firebase_id_token: firebaseIdToken,
    },
  );
  const apiKey = extractJsonPath(data, [["apiKey"], ["api_key"]]);
  if (!apiKey) throw new Error("Windsurf RegisterUser missing apiKey");
  const apiServerUrl =
    extractJsonPath(data, [["apiServerUrl"], ["api_server_url"]]) ||
    cfg.defaultApiServerUrl ||
    "https://api.codeium.com";
  const name = extractJsonPath(data, [["name"]]);
  return { apiKey, apiServerUrl, name };
}

// Best-effort: GetOneTimeAuthToken → GetCurrentUser → email/name.
async function fetchWindsurfUserInfo(
  apiServerUrl: string,
  firebaseIdToken: string,
) {
  const cfg = WINDSURF_CONFIG as unknown as WindsurfConfigLike;
  try {
    const authRes = await windsurfSeatRequest(
      apiServerUrl,
      cfg.oneTimeAuthPath || "/api/v1/one_time_auth",
      { firebaseIdToken },
    );
    const authToken = extractJsonPath(authRes, [["authToken"], ["auth_token"]]);
    if (!authToken) return { email: null, name: null };
    const userRes = await windsurfSeatRequest(
      apiServerUrl,
      cfg.currentUserPath || "/api/v1/user",
      {
        authToken,
        includeSubscription: true,
      },
    );
    const user = (userRes.user || userRes) as Record<string, unknown>;
    return {
      email: extractJsonPath(user, [["email"]]),
      name: extractJsonPath(user, [["name"]]),
    };
  } catch {
    return { email: null, name: null };
  }
}

// Windsurf — browser OAuth: open auth page with redirect_uri + state
// → local callback (access_token) → RegisterUser (apiKey) → GetUserInfo.
const windsurf = {
  config: WINDSURF_CONFIG,
  flowType: "authorization_code",
  callbackPath: (WINDSURF_CONFIG as unknown as WindsurfConfigLike).callbackPath,
  buildAuthUrl: (
    config: WindsurfConfigLike,
    redirectUri: string,
    state: string,
  ) => {
    const url = new URL(config.authPageUrl || "https://codeium.com/profile");
    url.searchParams.set("redirect_uri", redirectUri);
    url.searchParams.set("state", state);
    return url.toString();
  },
  exchangeToken: async (
    _config: WindsurfConfigLike,
    code: string,
    _redirectUri: string,
    _codeVerifier: string,
    state?: string,
  ) => {
    const cfg = WINDSURF_CONFIG as unknown as WindsurfConfigLike;
    const trimmed = String(code || "").trim();
    const looksCallback =
      /[?=&]/.test(trimmed) &&
      (trimmed.includes("access_token") || trimmed.includes("token"));
    if (!looksCallback) {
      return {
        apiKey: trimmed,
        apiServerUrl: cfg.defaultApiServerUrl,
        name: null,
        expiresIn: (cfg.tokenLifetimeDays || 30) * 24 * 60 * 60,
        _authMethod: "imported",
      };
    }
    const { firebaseIdToken } = parseWindsurfCallback(trimmed, state);
    const reg = await fetchWindsurfRegisterUser(firebaseIdToken);
    return {
      ...reg,
      firebaseIdToken,
      expiresIn: (cfg.tokenLifetimeDays || 30) * 24 * 60 * 60,
      _authMethod: "oauth",
    };
  },
  postExchange: async (tokens: Record<string, unknown>) => {
    if (!tokens.firebaseIdToken)
      return { userInfo: { email: null, name: null } };
    const userInfo = await fetchWindsurfUserInfo(
      tokens.apiServerUrl as string,
      tokens.firebaseIdToken as string,
    );
    return { userInfo };
  },
  mapTokens: (
    tokens: Record<string, unknown>,
    extra?: {
      userInfo?: { email?: string | null; name?: string | null };
    } | null,
  ) => {
    const ui = extra?.userInfo || {};
    const displayName = ui.name || (tokens.name as string) || undefined;
    return {
      accessToken: tokens.apiKey,
      refreshToken: null,
      expiresIn: tokens.expiresIn,
      email: ui.email || undefined,
      displayName,
      providerSpecificData: {
        authMethod: tokens._authMethod || "oauth",
        apiKey: tokens.apiKey,
        apiServerUrl: tokens.apiServerUrl,
      },
    };
  },
};

export default windsurf;
