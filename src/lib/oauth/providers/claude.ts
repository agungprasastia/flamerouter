import { CLAUDE_CONFIG } from "../constants/oauth";

export interface OAuthConfigLike {
  clientId?: string;
  scopes?: string[];
  codeChallengeMethod?: string;
  authorizeUrl?: string;
  tokenUrl?: string;
  [key: string]: unknown;
}

const claude = {
  config: CLAUDE_CONFIG,
  flowType: "authorization_code_pkce",
  buildAuthUrl: (config: OAuthConfigLike, redirectUri: string, state: string, codeChallenge?: string) => {
    const params = new URLSearchParams({
      code: "true",
      client_id: config.clientId || "",
      response_type: "code",
      redirect_uri: redirectUri,
      scope: (config.scopes || []).join(" "),
      code_challenge: codeChallenge || "",
      code_challenge_method: config.codeChallengeMethod || "S256",
      state: state,
    });
    return `${config.authorizeUrl}?${params.toString()}`;
  },
  exchangeToken: async (config: OAuthConfigLike, code: string, redirectUri: string, codeVerifier: string, state?: string) => {
    // Parse code - may contain state after #
    let authCode = code;
    let codeState = "";
    if (authCode.includes("#")) {
      const parts = authCode.split("#");
      authCode = parts[0] || "";
      codeState = parts[1] || "";
    }

    const response = await fetch(config.tokenUrl || "", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify({
        code: authCode,
        state: codeState || state,
        grant_type: "authorization_code",
        client_id: config.clientId,
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
  mapTokens: (tokens: Record<string, unknown>) => ({
    accessToken: tokens.access_token as string,
    refreshToken: tokens.refresh_token as string,
    expiresIn: tokens.expires_in as number,
    scope: tokens.scope as string,
  }),
};

export default claude;
