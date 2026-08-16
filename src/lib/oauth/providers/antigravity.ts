import { ANTIGRAVITY_CONFIG, getOAuthClientMetadata } from "../constants/oauth";

interface AntigravityConfigLike {
  clientId?: string;
  clientSecret?: string;
  scopes?: string[];
  authorizeUrl?: string;
  tokenUrl?: string;
  userInfoUrl?: string;
  loadCodeAssistUserAgent?: string;
  loadCodeAssistEndpoint?: string;
  onboardUserEndpoint?: string;
  [key: string]: unknown;
}

const antigravity = {
  config: ANTIGRAVITY_CONFIG,
  flowType: "authorization_code",
  buildAuthUrl: (config: AntigravityConfigLike, redirectUri: string, state: string) => {
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
  exchangeToken: async (config: AntigravityConfigLike, code: string, redirectUri: string) => {
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
    const cfg = ANTIGRAVITY_CONFIG as unknown as AntigravityConfigLike;
    const loadHeaders = {
      Authorization: `Bearer ${tokens.access_token}`,
      "Content-Type": "application/json",
      "User-Agent": cfg.loadCodeAssistUserAgent || "",
      "x-request-source": "local",
    };
    const metadata = getOAuthClientMetadata();

    // Fetch user info
    const userInfoRes = await fetch(
      `${cfg.userInfoUrl}?alt=json`,
      {
        headers: {
          Authorization: `Bearer ${tokens.access_token}`,
          "x-request-source": "local",
        },
      },
    );
    const userInfo = userInfoRes.ok ? ((await userInfoRes.json()) as Record<string, unknown>) : {};

    // Load Code Assist to get project ID and tier
    let projectId = "";
    let tierId = "legacy-tier";
    try {
      const loadRes = await fetch(cfg.loadCodeAssistEndpoint || "", {
        method: "POST",
        headers: loadHeaders,
        body: JSON.stringify({ metadata }),
      });
      if (loadRes.ok) {
        const data = (await loadRes.json()) as {
          cloudaicompanionProject?: { id?: string } | string;
          allowedTiers?: Array<{ isDefault?: boolean; id?: string }>;
        };
        projectId =
          (typeof data.cloudaicompanionProject === "object" ? data.cloudaicompanionProject?.id : data.cloudaicompanionProject) ||
          "";
        if (Array.isArray(data.allowedTiers)) {
          for (const tier of data.allowedTiers) {
            if (tier.isDefault && tier.id) {
              tierId = tier.id.trim();
              break;
            }
          }
        }
      }
    } catch (e) {
      console.log("Failed to load code assist:", e);
    }

    // Fire-and-forget onboarding — does not block DB save
    if (projectId) {
      const doOnboard = async () => {
        for (let i = 0; i < 10; i++) {
          try {
            const onboardRes = await fetch(
              cfg.onboardUserEndpoint || "",
              {
                method: "POST",
                headers: loadHeaders,
                body: JSON.stringify({ tierId, metadata }),
              },
            );
            if (onboardRes.ok) {
              const resData = (await onboardRes.json()) as { done?: boolean };
              if (resData.done) break;
            }
          } catch {
            /* ignore */
          }
          await new Promise((resolve) => setTimeout(resolve, 2000));
        }
      };
      doOnboard().catch((e) => console.log("Onboarding error:", e));
    }

    return { userInfo, projectId, tierId };
  },
  mapTokens: (tokens: Record<string, unknown>, extra?: { userInfo?: { email?: string; name?: string }; projectId?: string; tierId?: string } | null) => ({
    accessToken: tokens.access_token as string,
    refreshToken: tokens.refresh_token as string,
    expiresIn: tokens.expires_in as number,
    scope: tokens.scope as string,
    email: extra?.userInfo?.email,
    displayName: extra?.userInfo?.name,
    projectId: extra?.projectId,
    providerSpecificData: {
      tierId: extra?.tierId || "legacy-tier",
      projectId: extra?.projectId,
    },
  }),
};

export default antigravity;
