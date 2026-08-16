import crypto from "crypto";
import { KIMI_CONFIG } from "../constants/oauth";

interface KimiConfigLike {
  clientId?: string;
  deviceCodeUrl?: string;
  tokenUrl?: string;
  authorizeDeviceUrl?: string;
  [key: string]: unknown;
}

const kimi = {
  config: KIMI_CONFIG,
  flowType: "device_code",
  requestDeviceCode: async (config: KimiConfigLike) => {
    const { buildKimiHeaders } = { buildKimiHeaders: (t: string) => ({ Authorization: `Bearer ${t}` }) };
    const deviceId = crypto.randomUUID();
    const headers = {
      "Content-Type": "application/x-www-form-urlencoded",
      Accept: "application/json",
      ...buildKimiHeaders(deviceId),
    };
    const response = await fetch(config.deviceCodeUrl || "", {
      method: "POST",
      headers,
      body: new URLSearchParams({ client_id: config.clientId || "" }),
    });
    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Device code request failed: ${error}`);
    }
    const data = (await response.json()) as {
      device_code: string;
      user_code: string;
      verification_uri?: string;
      verification_uri_complete?: string;
      expires_in?: number;
      interval?: number;
    };
    const authorizeDeviceUrl =
      config.authorizeDeviceUrl || "https://www.kimi.com/code/authorize_device";
    return {
      device_code: data.device_code,
      user_code: data.user_code,
      verification_uri: data.verification_uri || authorizeDeviceUrl,
      verification_uri_complete:
        data.verification_uri_complete ||
        `${authorizeDeviceUrl}?user_code=${data.user_code}`,
      expires_in: data.expires_in,
      interval: data.interval || 5,
      _kimiDeviceId: deviceId,
    };
  },
  pollToken: async (config: KimiConfigLike, deviceCode?: string, _codeVerifier?: string, extraData?: Record<string, unknown>) => {
    const { buildKimiHeaders } = { buildKimiHeaders: (t: string) => ({ Authorization: `Bearer ${t}` }) };
    const deviceId = (extraData?._kimiDeviceId as string) || "";
    const headers = {
      "Content-Type": "application/x-www-form-urlencoded",
      Accept: "application/json",
      ...buildKimiHeaders(deviceId),
    };
    const response = await fetch(config.tokenUrl || "", {
      method: "POST",
      headers,
      body: new URLSearchParams({
        grant_type: "urn:ietf:params:oauth:grant-type:device_code",
        client_id: config.clientId || "",
        device_code: deviceCode || "",
      }),
    });
    let data: Record<string, unknown>;
    try {
      data = (await response.json()) as Record<string, unknown>;
    } catch {
      data = {
        error: "invalid_response",
        error_description: "non-json token response",
      };
    }
    if (data.error === "authorization_pending" || data.error === "slow_down") {
      return { ok: true, data };
    }
    if (data.access_token && deviceId) data._kimiDeviceId = deviceId;
    return { ok: response.ok || !!data.access_token || !!data.error, data };
  },
  mapTokens: (tokens: Record<string, unknown>) => ({
    accessToken: tokens.access_token,
    refreshToken: tokens.refresh_token,
    expiresIn: tokens.expires_in,
    providerSpecificData: {
      authMethod: "device_code",
      ...(tokens._kimiDeviceId ? { deviceId: tokens._kimiDeviceId } : {}),
    },
  }),
};

export default kimi;
