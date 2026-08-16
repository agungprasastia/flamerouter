import { GEMINI_CONFIG, getOAuthClientMetadata } from "../constants/oauth";

interface GeminiConfigLike {
  clientId?: string;
  clientSecret?: string;
  scopes?: string[];
  authorizeUrl?: string;
  tokenUrl?: string;
  userInfoUrl?: string;
  [key: string]: unknown;
}

const geminiCli = {
  config: GEMINI_CONFIG,
  flowType: "authorization_code",
  buildAuthUrl: (config: GeminiConfigLike, redirectUri: string, state: string) => {
    const params = new URLSearchParams({
      client_id: config.clientId || "",
      response_type: "code",
      redirect_uri: redirectUri,
      scope: (config.scopes || []).join(" "),
      state: state,
      access_type: "offline",
      prompt: "consent",
    });
    return `${config.authorizeUrl}?${params.toString()}`;
  },
  exchangeToken: async (config: GeminiConfigLike, code: string, redirectUri: string) => {
    const response = await fetch(config.tokenUrl || "", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
      },
      body: new URLSearchParams({
        grant_type: "authorization_code",
        client_id: config.clientId || "",
        client_secret: (config.clientSecret as string) || "",
        code: code,
        redirect_uri: redirectUri,
      }),
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Token exchange failed: ${error}`);
    }

    return (await response.json()) as Record<string, unknown>;
  },
  postExchange: async (tokens: Record<string, unknown>) => {
    // Fetch user info
    const userInfoUrl = (GEMINI_CONFIG as unknown as { userInfoUrl?: string })?.userInfoUrl || "https://www.googleapis.com/oauth2/v1/userinfo";
    const userInfoRes = await fetch(`${userInfoUrl}?alt=json`, {
      headers: { Authorization: `Bearer ${tokens.access_token}` },
    });
    const userInfo = userInfoRes.ok ? ((await userInfoRes.json()) as Record<string, unknown>) : {};

    // Fetch project ID
    let projectId = "";
    try {
      const projectRes = await fetch(
        "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
        {
          method: "POST",
          headers: {
            Authorization: `Bearer ${tokens.access_token}`,
            "Content-Type": "application/json",
          },
          body: JSON.stringify({
            metadata: getOAuthClientMetadata(),
            mode: 1,
          }),
        },
      );
      if (projectRes.ok) {
        const data = (await projectRes.json()) as { cloudaicompanionProject?: { id?: string } | string };
        projectId =
          (typeof data.cloudaicompanionProject === "object" ? data.cloudaicompanionProject?.id : data.cloudaicompanionProject) ||
          "";
      }
    } catch (e) {
      console.log("Failed to fetch project ID:", e);
    }

    return { userInfo, projectId };
  },
  mapTokens: (tokens: Record<string, unknown>, extra?: { userInfo?: { name?: string; email?: string }; projectId?: string } | null) => ({
    accessToken: tokens.access_token as string,
    refreshToken: tokens.refresh_token as string,
    expiresIn: tokens.expires_in as number,
    scope: tokens.scope as string,
    email: extra?.userInfo?.email,
    displayName: extra?.userInfo?.name,
    projectId: extra?.projectId,
  }),
};

export default geminiCli;
