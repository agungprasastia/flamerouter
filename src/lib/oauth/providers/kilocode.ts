import { KILOCODE_CONFIG } from "../constants/oauth";

interface KiloCodeConfigLike {
  initiateUrl?: string;
  pollUrlBase?: string;
  apiBaseUrl?: string;
  [key: string]: unknown;
}

const kilocode = {
  config: KILOCODE_CONFIG,
  flowType: "device_code",
  requestDeviceCode: async (config: KiloCodeConfigLike) => {
    const response = await fetch(config.initiateUrl || "", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
    });
    if (!response.ok) {
      if (response.status === 429) {
        throw new Error(
          "Too many pending authorization requests. Please try again later.",
        );
      }
      const error = await response.text();
      throw new Error(`Device auth initiation failed: ${error}`);
    }
    const data = (await response.json()) as {
      code: string;
      verificationUrl: string;
      expiresIn?: number;
    };
    return {
      device_code: data.code,
      user_code: data.code,
      verification_uri: data.verificationUrl,
      verification_uri_complete: data.verificationUrl,
      expires_in: data.expiresIn || 300,
      interval: 3,
    };
  },
  pollToken: async (config: KiloCodeConfigLike, deviceCode?: string) => {
    const response = await fetch(`${config.pollUrlBase}/${deviceCode}`);
    if (response.status === 202)
      return { ok: false, data: { error: "authorization_pending" } };
    if (response.status === 403)
      return {
        ok: false,
        data: {
          error: "access_denied",
          error_description: "Authorization denied by user",
        },
      };
    if (response.status === 410)
      return {
        ok: false,
        data: {
          error: "expired_token",
          error_description: "Authorization code expired",
        },
      };
    if (!response.ok)
      return {
        ok: false,
        data: {
          error: "poll_failed",
          error_description: `Poll failed: ${response.status}`,
        },
      };
    const data = (await response.json()) as {
      status?: string;
      token?: string;
      userEmail?: string;
    };
    if (data.status === "approved" && data.token) {
      // Fetch profile to get orgId for X-Kilocode-OrganizationID header
      let orgId: string | null = null;
      try {
        const profileRes = await fetch(`${config.apiBaseUrl}/api/profile`, {
          headers: { Authorization: `Bearer ${data.token}` },
        });
        if (profileRes.ok) {
          const profile = (await profileRes.json()) as { organizations?: Array<{ id?: string }> };
          orgId = profile.organizations?.[0]?.id || null;
        }
      } catch {}
      return {
        ok: true,
        data: {
          access_token: data.token,
          _userEmail: data.userEmail,
          _orgId: orgId,
        },
      };
    }
    return { ok: false, data: { error: "authorization_pending" } };
  },
  mapTokens: (tokens: Record<string, unknown>) => ({
    accessToken: tokens.access_token,
    refreshToken: null,
    expiresIn: 30 * 86400,
    email: tokens._userEmail || null,
    providerSpecificData: {
      authMethod: "device",
      orgId: tokens._orgId || null,
    },
  }),
};

export default kilocode;
