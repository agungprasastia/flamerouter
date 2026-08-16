import { CODEX_CONFIG } from "../constants/oauth";
import {
  extractEmailFromAccessToken,
} from "../providerHelpers";

export function extractCodexAccountInfo(idToken?: string | null): { email?: string; chatgptAccountId?: string; chatgptPlanType?: string } {
  if (!idToken || typeof idToken !== "string") return {};
  const parts = idToken.split(".");
  if (parts.length !== 3) return {};
  try {
    const base64 = (parts[1] || "").replace(/-/g, "+").replace(/_/g, "/");
    const padding = (4 - (base64.length % 4)) % 4;
    const json = Buffer.from(base64 + "=".repeat(padding), "base64").toString("utf8");
    const payload = JSON.parse(json) as Record<string, unknown>;
    const auth = (payload["https://api.openai.com/auth"] || {}) as Record<string, unknown>;
    return {
      email: (payload.email || payload.preferred_username) as string | undefined,
      chatgptAccountId: (auth.chatgpt_account_id || auth.puid) as string | undefined,
      chatgptPlanType: (auth.chatgpt_plan_type || auth.plan_type) as string | undefined,
    };
  } catch {
    return {};
  }
}

export interface CodexConfigLike {
  clientId?: string;
  scope?: string;
  codeChallengeMethod?: string;
  extraParams?: Record<string, string>;
  authorizeUrl?: string;
  tokenUrl?: string;
  [key: string]: unknown;
}

const codex = {
  config: CODEX_CONFIG,
  flowType: "authorization_code_pkce",
  fixedPort: (CODEX_CONFIG as unknown as { fixedPort?: number })?.fixedPort,
  callbackPath: (CODEX_CONFIG as unknown as { callbackPath?: string })?.callbackPath,
  buildAuthUrl: (config: CodexConfigLike, redirectUri: string, state: string, codeChallenge?: string) => {
    const params: Record<string, string> = {
      response_type: "code",
      client_id: config.clientId || "",
      redirect_uri: redirectUri,
      scope: config.scope || "",
      code_challenge: codeChallenge || "",
      code_challenge_method: config.codeChallengeMethod || "S256",
      ...(config.extraParams || {}),
      state: state,
    };
    const queryString = Object.entries(params)
      .map(([key, value]) => `${key}=${encodeURIComponent(value)}`)
      .join("&");
    return `${config.authorizeUrl}?${queryString}`;
  },
  exchangeToken: async (config: CodexConfigLike, code: string, redirectUri: string, codeVerifier: string) => {
    const response = await fetch(config.tokenUrl || "", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
      },
      body: new URLSearchParams({
        grant_type: "authorization_code",
        client_id: config.clientId || "",
        code: code,
        redirect_uri: redirectUri,
        code_verifier: codeVerifier,
      }),
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Token exchange failed: ${error}`);
    }

    return (await response.json()) as Record<string, unknown>;
  },
  mapTokens: (tokens: Record<string, unknown>) => {
    const info = extractCodexAccountInfo(tokens.id_token as string);
    const mapped: Record<string, unknown> = {
      accessToken: tokens.access_token,
      refreshToken: tokens.refresh_token,
      idToken: tokens.id_token,
      expiresIn: tokens.expires_in,
      lastRefreshAt: new Date().toISOString(),
    };
    const email =
      info.email || extractEmailFromAccessToken(tokens.access_token as string);
    if (email) mapped.email = email;
    if (info.chatgptAccountId || info.chatgptPlanType) {
      mapped.providerSpecificData = {
        chatgptAccountId: info.chatgptAccountId,
        chatgptPlanType: info.chatgptPlanType,
      };
    }
    return mapped;
  },
};

export default codex;
