import { CODEBUDDY_CONFIG } from "../constants/oauth";

interface CodeBuddyConfigLike {
  stateUrl?: string;
  platform?: string;
  userAgent?: string;
  pollInterval?: number;
  tokenUrl?: string;
  [key: string]: unknown;
}

// CodeBuddy (Tencent) - Browser OAuth Polling Flow
// 1. POST stateUrl → get { state, authUrl }
// 2. Open authUrl in browser
// 3. Poll tokenUrl with state until success (code 0) or timeout
const codebuddyCn = {
  config: CODEBUDDY_CONFIG,
  flowType: "device_code",
  requestDeviceCode: async (config: CodeBuddyConfigLike) => {
    const response = await fetch(
      `${config.stateUrl}?platform=${config.platform}`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
          "User-Agent": (config.userAgent as string) || "FlameRouter",
          "X-Requested-With": "XMLHttpRequest",
          "X-Domain": "copilot.tencent.com",
          "X-No-Authorization": "true",
          "X-No-User-Id": "true",
          "X-Product": "SaaS",
        },
        body: "{}",
      },
    );
    if (!response.ok)
      throw new Error(
        `CodeBuddy state request failed: ${await response.text()}`,
      );
    const data = (await response.json()) as { code?: number; msg?: string; data?: { state?: string; authUrl?: string } };
    if (data.code !== 0 || !data.data?.state || !data.data?.authUrl) {
      throw new Error(
        `CodeBuddy state error: ${data.msg || "missing state/authUrl"}`,
      );
    }
    return {
      device_code: data.data.state,
      verification_uri: data.data.authUrl,
      user_code: "",
      interval: ((config.pollInterval as number) || 3000) / 1000,
      _isCodeBuddy: true,
    };
  },
  pollToken: async (config: CodeBuddyConfigLike, deviceCode?: string) => {
    // CodeBuddy polls the token endpoint via GET with the state as a query
    // param (not POST/body) — matches the official CLI's /v2/plugin/auth/token?state=...
    const response = await fetch(
      `${config.tokenUrl}?state=${encodeURIComponent(deviceCode || "")}`,
      {
        method: "GET",
        headers: {
          Accept: "application/json",
          "User-Agent": (config.userAgent as string) || "FlameRouter",
          "X-Requested-With": "XMLHttpRequest",
          "X-Domain": "copilot.tencent.com",
          "X-No-Authorization": "true",
          "X-No-User-Id": "true",
          "X-No-Enterprise-Id": "true",
          "X-No-Department-Info": "true",
          "X-Product": "SaaS",
        },
      },
    );
    if (!response.ok) return { ok: false, data: { error: "request_failed" } };
    const data = (await response.json()) as { code?: number; msg?: string; data?: { accessToken?: string; user?: { nickname?: string; email?: string } } };
    // code 11217 = pending (RetryFetchToken), code 0 = success
    if (data.code === 0 && data.data?.accessToken) {
      return {
        ok: true,
        data: {
          access_token: data.data.accessToken,
          _user: data.data.user || {},
        },
      };
    }
    return {
      ok: false,
      data: {
        error: data.code === 11217 ? "authorization_pending" : "poll_error",
        error_description: data.msg,
      },
    };
  },
  mapTokens: (tokens: Record<string, unknown>) => {
    const user = (tokens._user as Record<string, string> | undefined) || {};
    return {
      accessToken: tokens.access_token,
      refreshToken: null,
      expiresIn: 30 * 86400,
      email: user?.email || user?.nickname || "",
      displayName: user?.nickname || "",
      providerSpecificData: {
        authKind: "qrcode",
      },
    };
  },
};

export default codebuddyCn;
