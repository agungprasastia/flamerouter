import { IFLOW_CONFIG } from "../constants/oauth";

interface IFlowConfigLike {
  clientId?: string;
  clientSecret?: string;
  authorizeUrl?: string;
  tokenUrl?: string;
  userInfoUrl?: string;
  extraParams?: {
    loginMethod?: string;
    type?: string;
  };
  [key: string]: unknown;
}

const iflow = {
  config: IFLOW_CONFIG,
  flowType: "authorization_code",
  buildAuthUrl: (config: IFlowConfigLike, redirectUri: string, state: string) => {
    const params = new URLSearchParams({
      loginMethod: config.extraParams?.loginMethod || "",
      type: config.extraParams?.type || "",
      redirect: redirectUri,
      state: state,
      client_id: config.clientId || "",
    });
    return `${config.authorizeUrl}?${params.toString()}`;
  },
  exchangeToken: async (config: IFlowConfigLike, code: string, redirectUri: string) => {
    // Create Basic Auth header
    const basicAuth = Buffer.from(
      `${config.clientId || ""}:${(config.clientSecret as string) || ""}`,
    ).toString("base64");

    const response = await fetch(config.tokenUrl || "", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
        Authorization: `Basic ${basicAuth}`,
      },
      body: new URLSearchParams({
        grant_type: "authorization_code",
        code: code,
        redirect_uri: redirectUri,
        client_id: config.clientId || "",
        client_secret: (config.clientSecret as string) || "",
      }),
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Token exchange failed: ${error}`);
    }

    return (await response.json()) as Record<string, unknown>;
  },
  postExchange: async (tokens: Record<string, unknown>) => {
    const userInfoUrl = (IFLOW_CONFIG as unknown as { userInfoUrl?: string })?.userInfoUrl || "";
    // Fetch user info (MUST succeed to get API key)
    const userInfoRes = await fetch(
      `${userInfoUrl}?accessToken=${encodeURIComponent(tokens.access_token as string)}`,
      {
        headers: {
          Accept: "application/json",
        },
      },
    );

    if (!userInfoRes.ok) {
      const errorText = await userInfoRes.text();
      throw new Error(`Failed to fetch user info: ${errorText}`);
    }

    const result = (await userInfoRes.json()) as { success?: boolean; message?: string; data?: { apiKey?: string; email?: string; phone?: string; nickname?: string } };
    if (!result.success) {
      throw new Error(
        `User info request failed: ${result.message || "Unknown error"}`,
      );
    }

    const userInfo = result.data || {};

    // Validate API key (critical for iFlow)
    if (!userInfo.apiKey || userInfo.apiKey.trim() === "") {
      throw new Error("Empty API key returned from iFlow");
    }

    // Validate email/phone
    const email = userInfo.email?.trim() || userInfo.phone?.trim();
    if (!email) {
      throw new Error("Missing account email/phone in user info");
    }

    return { userInfo };
  },
  mapTokens: (tokens: Record<string, unknown>, extra?: { userInfo?: { apiKey?: string; email?: string; phone?: string; nickname?: string } } | null) => ({
    accessToken: tokens.access_token as string,
    refreshToken: tokens.refresh_token as string,
    expiresIn: tokens.expires_in as number,
    apiKey: extra?.userInfo?.apiKey,
    email: extra?.userInfo?.email || extra?.userInfo?.phone,
    displayName: extra?.userInfo?.nickname,
  }),
};

export default iflow;
