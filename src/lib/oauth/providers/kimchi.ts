import { KIMCHI_CONFIG } from "../constants/oauth";

interface KimchiConfigLike {
  webAppUrl?: string;
  validationUrl?: string;
  userInfoUrl?: string;
  [key: string]: unknown;
}

const kimchi = {
  config: KIMCHI_CONFIG,
  flowType: "browser_token",
  buildAuthUrl: (config: KimchiConfigLike, redirectUri: string, state: string) => {
    const baseUrl = (config.webAppUrl || "https://app.kimchi.dev").replace(
      /\/+$/,
      "",
    );
    const params = new URLSearchParams({
      callback: redirectUri,
      state,
    });
    return `${baseUrl}/cli-auth?${params.toString()}`;
  },
  exchangeToken: async (config: KimchiConfigLike, token: string) => {
    const accessToken = String(token || "").trim();
    if (!accessToken) {
      throw new Error("Missing Kimchi token");
    }

    const validationUrl =
      config.validationUrl ||
      "https://api.cast.ai/v1/llm/openai/supported-providers";
    const validationRes = await fetch(validationUrl, {
      method: "GET",
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${accessToken}`,
      },
    });
    if (!validationRes.ok) {
      throw new Error(
        `Kimchi token validation failed: ${validationRes.status}`,
      );
    }

    let userInfo: Record<string, unknown> = {};
    if (config.userInfoUrl) {
      try {
        const userRes = await fetch(config.userInfoUrl, {
          method: "GET",
          headers: {
            Accept: "application/json",
            Authorization: `Bearer ${accessToken}`,
          },
        });
        if (userRes.ok) {
          userInfo = (await userRes.json()) as Record<string, unknown>;
        }
      } catch {
        userInfo = {};
      }
    }

    return {
      access_token: accessToken,
      token_type: "Bearer",
      _kimchiUser: userInfo,
    };
  },
  mapTokens: (tokens: Record<string, unknown>) => {
    const user = (tokens._kimchiUser as Record<string, string> | undefined) || {};
    return {
      accessToken: tokens.access_token,
      refreshToken: null,
      expiresIn: 30 * 86400,
      email: user.email || "",
      displayName: user.name || user.username || "",
      providerSpecificData: {
        authKind: "browser_token",
      },
    };
  },
};

export default kimchi;
