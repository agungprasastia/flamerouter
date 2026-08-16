import { GITLAB_CONFIG } from "../constants/oauth";

interface GitLabConfigLike {
  defaultBaseUrl?: string;
  scope?: string;
  codeChallengeMethod?: string;
  authorizeUrlPath?: string;
  tokenUrlPath?: string;
  userInfoUrlPath?: string;
  [key: string]: unknown;
}

// GitLab Duo - Authorization Code Flow with PKCE
// Supports two login modes via loginMode metadata: "oauth" (default) or "pat"
const gitlab = {
  config: GITLAB_CONFIG,
  flowType: "authorization_code_pkce",
  buildAuthUrl: (config: GitLabConfigLike, redirectUri: string, state: string, codeChallenge?: string, meta: Record<string, unknown> = {}) => {
    const baseUrl = (meta.baseUrl as string) || config.defaultBaseUrl || "https://gitlab.com";
    const clientId = (meta.clientId as string) || "";
    const params = new URLSearchParams({
      client_id: clientId,
      redirect_uri: redirectUri,
      response_type: "code",
      state,
      scope: (config.scope as string) || "",
      code_challenge: codeChallenge || "",
      code_challenge_method: (config.codeChallengeMethod as string) || "S256",
    });
    return `${baseUrl}${config.authorizeUrlPath}?${params.toString()}`;
  },
  exchangeToken: async (
    config: GitLabConfigLike,
    code: string,
    redirectUri: string,
    codeVerifier: string,
    _state?: string,
    meta: Record<string, unknown> = {},
  ) => {
    const baseUrl = (meta.baseUrl as string) || config.defaultBaseUrl || "https://gitlab.com";
    const clientId = (meta.clientId as string) || "";
    const clientSecret = (meta.clientSecret as string) || "";
    const body = new URLSearchParams({
      client_id: clientId,
      grant_type: "authorization_code",
      code,
      redirect_uri: redirectUri,
      code_verifier: codeVerifier,
    });
    if (clientSecret) body.set("client_secret", clientSecret);
    const response = await fetch(`${baseUrl}${config.tokenUrlPath}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
      },
      body: body.toString(),
    });
    if (!response.ok)
      throw new Error(`GitLab token exchange failed: ${await response.text()}`);
    const tokens = (await response.json()) as Record<string, unknown>;
    // Fetch user info
    const userRes = await fetch(`${baseUrl}${config.userInfoUrlPath}`, {
      headers: { Authorization: `Bearer ${tokens.access_token}` },
    });
    const user = userRes.ok ? ((await userRes.json()) as Record<string, unknown>) : {};
    return { ...tokens, _user: user, _baseUrl: baseUrl, _clientId: clientId };
  },
  mapTokens: (tokens: Record<string, unknown>) => {
    const user = (tokens._user as Record<string, string> | undefined) || {};
    return {
      accessToken: tokens.access_token,
      refreshToken: tokens.refresh_token,
      expiresIn: tokens.expires_in,
      scope: tokens.scope,
      providerSpecificData: {
        username: user?.username || "",
        email: user?.email || user?.public_email || "",
        name: user?.name || "",
        baseUrl: tokens._baseUrl,
        clientId: tokens._clientId,
        authKind: "oauth",
      },
    };
  },
};

export default gitlab;
