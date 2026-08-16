import { GITHUB_CONFIG } from "../constants/oauth";

interface GithubConfigLike {
  clientId?: string;
  scopes?: string;
  deviceCodeUrl?: string;
  tokenUrl?: string;
  copilotTokenUrl?: string;
  userInfoUrl?: string;
  apiVersion?: string;
  userAgent?: string;
  [key: string]: unknown;
}

const github = {
  config: GITHUB_CONFIG,
  flowType: "device_code",
  requestDeviceCode: async (config: GithubConfigLike) => {
    const response = await fetch(config.deviceCodeUrl || "", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
      },
      body: new URLSearchParams({
        client_id: config.clientId || "",
        scope: config.scopes || "",
      }),
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Device code request failed: ${error}`);
    }

    return (await response.json()) as Record<string, unknown>;
  },
  pollToken: async (config: GithubConfigLike, deviceCode: string) => {
    const response = await fetch(config.tokenUrl || "", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
      },
      body: new URLSearchParams({
        client_id: config.clientId || "",
        device_code: deviceCode,
        grant_type: "urn:ietf:params:oauth:grant-type:device_code",
      }),
    });

    let data: Record<string, unknown>;
    try {
      data = (await response.json()) as Record<string, unknown>;
    } catch {
      const text = await response.text();
      data = { error: "invalid_response", error_description: text };
    }

    return {
      ok: response.ok,
      data: data,
    };
  },
  postExchange: async (tokens: Record<string, unknown>) => {
    const cfg = GITHUB_CONFIG as unknown as GithubConfigLike;
    // Get Copilot token using GitHub access token
    const copilotRes = await fetch(cfg.copilotTokenUrl || "", {
      headers: {
        Authorization: `Bearer ${tokens.access_token}`,
        Accept: "application/json",
        "X-GitHub-Api-Version": cfg.apiVersion || "",
        "User-Agent": cfg.userAgent || "FlameRouter",
      },
    });
    const copilotToken = copilotRes.ok ? ((await copilotRes.json()) as Record<string, unknown>) : {};

    // Get user info from GitHub
    const userRes = await fetch(cfg.userInfoUrl || "", {
      headers: {
        Authorization: `Bearer ${tokens.access_token}`,
        Accept: "application/json",
        "X-GitHub-Api-Version": cfg.apiVersion || "",
        "User-Agent": cfg.userAgent || "FlameRouter",
      },
    });
    const userInfo = userRes.ok ? ((await userRes.json()) as Record<string, unknown>) : {};

    return { copilotToken, userInfo };
  },
  mapTokens: (tokens: Record<string, unknown>, extra?: { userInfo?: { email?: string; login?: string; name?: string }; copilotToken?: { token?: string; expires_at?: number } } | null) => ({
    accessToken: tokens.access_token,
    tokenType: tokens.token_type,
    scope: tokens.scope,
    email: extra?.userInfo?.email || extra?.userInfo?.login,
    displayName: extra?.userInfo?.name || extra?.userInfo?.login,
    providerSpecificData: {
      githubEmail: extra?.userInfo?.email,
      githubLogin: extra?.userInfo?.login,
      githubName: extra?.userInfo?.name,
      copilotToken: extra?.copilotToken?.token,
      copilotExpiresAt: extra?.copilotToken?.expires_at,
    },
  }),
};

export default github;
