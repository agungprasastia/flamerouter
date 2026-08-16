import { GROK_CLI_CONFIG } from "../constants/oauth";
import {
  decodeXaiIdTokenEmail,
  extractEmailFromAccessToken,
} from "../providerHelpers";

interface GrokCliConfigLike {
  clientId?: string;
  scope?: string;
  referrer?: string;
  deviceCodeUrl?: string;
  tokenUrl?: string;
  [key: string]: unknown;
}

// Grok CLI / Grok Build — device code flow to auth.x.ai, inference on cli-chat-proxy.grok.com
const grokCli = {
  config: GROK_CLI_CONFIG,
  flowType: "device_code",
  requestDeviceCode: async (config: GrokCliConfigLike) => {
    const body = new URLSearchParams({
      client_id: config.clientId || "",
      scope: config.scope || "",
    });
    // Official CLI sends referrer=grok-build
    if (config.referrer) body.set("referrer", config.referrer);

    const response = await fetch(config.deviceCodeUrl || "", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
        "User-Agent": "grok-pager/0.2.93 grok-shell/0.2.93 (linux; x86_64)",
      },
      body,
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Grok CLI device code request failed: ${error}`);
    }

    return (await response.json()) as Record<string, unknown>;
  },
  pollToken: async (config: GrokCliConfigLike, deviceCode?: string) => {
    const response = await fetch(config.tokenUrl || "", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
        "User-Agent": "grok-pager/0.2.93 grok-shell/0.2.93 (linux; x86_64)",
      },
      body: new URLSearchParams({
        grant_type: "urn:ietf:params:oauth:grant-type:device_code",
        device_code: deviceCode || "",
        client_id: config.clientId || "",
      }),
    });

    let data: Record<string, unknown>;
    try {
      data = (await response.json()) as Record<string, unknown>;
    } catch {
      const text = await response.text();
      data = { error: "invalid_response", error_description: text };
    }

    // Device flow: 400 + authorization_pending is expected while user authorizes
    const pending =
      data?.error === "authorization_pending" || data?.error === "slow_down";
    return {
      ok: response.ok || pending,
      data,
    };
  },
  postExchange: async (tokens: Record<string, unknown>) => {
    // Best-effort user profile from cli-chat-proxy (non-fatal)
    try {
      const res = await fetch("https://cli-chat-proxy.grok.com/v1/user", {
        headers: {
          Authorization: `Bearer ${tokens.access_token}`,
          Accept: "application/json",
          "User-Agent": "grok-pager/0.2.93 grok-shell/0.2.93 (linux; x86_64)",
          "x-xai-token-auth": "xai-grok-cli",
          "x-grok-client-version": "0.2.93",
        },
      });
      if (res.ok) return { user: (await res.json()) as Record<string, unknown> };
    } catch {
      /* ignore */
    }
    return {};
  },
  mapTokens: (tokens: Record<string, unknown>, extra?: { user?: Record<string, unknown> } | null) => {
    const user = extra?.user;
    const u = (user?.user as Record<string, string> | undefined) || {};
    const email =
      decodeXaiIdTokenEmail(tokens.id_token as string) ||
      u.email ||
      extractEmailFromAccessToken(tokens.access_token as string);
    const displayName =
      u.name ||
      u.username ||
      u.display_name ||
      u.handle ||
      null;

    const mapped: Record<string, unknown> = {
      accessToken: tokens.access_token,
      refreshToken: tokens.refresh_token,
      expiresIn: tokens.expires_in,
      tokenType: tokens.token_type,
      scope: tokens.scope,
      idToken: tokens.id_token,
      providerSpecificData: {
        authMethod: "device_code",
      },
    };
    if (email) mapped.email = email;
    if (displayName) mapped.displayName = displayName;
    return mapped;
  },
};

export default grokCli;
