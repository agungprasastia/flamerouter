import { CODEBUDDY_INTL_CONFIG } from "../constants/oauth";

interface CodeBuddyIntlConfigLike {
  stateUrl?: string;
  platform?: string;
  userAgent?: string;
  pollInterval?: number;
  tokenUrl?: string;
  [key: string]: unknown;
}

// CodeBuddy International — mirrors codebuddy-cn flow against the .ai domain.
const codebuddyIntl = {
  config: CODEBUDDY_INTL_CONFIG,
  flowType: "device_code",
  requestDeviceCode: async (config: CodeBuddyIntlConfigLike) => {
    const response = await fetch(
      `${config.stateUrl}?platform=${config.platform}`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
          "User-Agent": (config.userAgent as string) || "FlameRouter",
          "X-Requested-With": "XMLHttpRequest",
          "X-Domain": "www.codebuddy.ai",
          "X-No-Authorization": "true",
          "X-No-User-Id": "true",
          "X-Product": "SaaS",
        },
        body: "{}",
      },
    );
    if (!response.ok)
      throw new Error(
        `CodeBuddy Intl state request failed: ${await response.text()}`,
      );
    const data = (await response.json()) as { code?: number; msg?: string; data?: { state?: string; authUrl?: string } };
    if (data.code !== 0 || !data.data?.state || !data.data?.authUrl) {
      throw new Error(
        `CodeBuddy Intl state error: ${data.msg || "missing state/authUrl"}`,
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
  pollToken: async (config: CodeBuddyIntlConfigLike, deviceCode?: string) => {
    const response = await fetch(
      `${config.tokenUrl}?state=${encodeURIComponent(deviceCode || "")}`,
      {
        method: "GET",
        headers: {
          Accept: "application/json",
          "User-Agent": (config.userAgent as string) || "FlameRouter",
          "X-Requested-With": "XMLHttpRequest",
          "X-Domain": "www.codebuddy.ai",
          "X-No-Authorization": "true",
          "X-No-User-Id": "true",
          "X-No-Enterprise-Id": "true",
          "X-No-Department-Info": "true",
          "X-Product": "SaaS",
        },
      },
    );
    if (!response.ok) return { ok: false, data: { error: "request_failed" } };
    const data = (await response.json()) as { code?: number; msg?: string; data?: { accessToken?: string; refreshToken?: string; tokenType?: string; expiresIn?: number } };
    if (data.code === 0 && data.data?.accessToken) {
      return {
        ok: true,
        data: {
          access_token: data.data.accessToken,
          refresh_token: data.data.refreshToken || "",
          token_type: data.data.tokenType || "Bearer",
          expires_in: data.data.expiresIn,
        },
      };
    }
    if (data.code === 11217)
      return { ok: true, data: { error: "authorization_pending" } };
    return {
      ok: false,
      data: { error: "poll_error", error_description: data.msg },
    };
  },
  mapTokens: (tokens: Record<string, unknown>) => ({
    accessToken: tokens.access_token,
    refreshToken: tokens.refresh_token || null,
    expiresIn: tokens.expires_in || 30 * 86400,
    email: "",
    displayName: "",
    providerSpecificData: {
      authKind: "oauth",
    },
  }),
};

export default codebuddyIntl;
