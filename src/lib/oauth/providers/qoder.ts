import { QODER_CONFIG } from "../constants/oauth";

interface QoderConfigLike {
  loginUrl?: string;
  [key: string]: unknown;
}

const qoder = {
  config: QODER_CONFIG,
  flowType: "device_code",
  requestDeviceCode: async (config: QoderConfigLike) => {
    const { QoderService } = await import("@/lib/oauth/services/qoder");
    const flow = new QoderService().initiateDeviceFlow();
    return {
      device_code: flow.nonce,
      user_code: flow.nonce.slice(0, 8).toUpperCase(),
      verification_uri: config.loginUrl || "",
      verification_uri_complete: flow.verificationUriComplete,
      expires_in: 300,
      interval: 2,
      codeVerifier: flow.codeVerifier,
      _qoderNonce: flow.nonce,
      _qoderMachineId: flow.machineId,
    };
  },
  pollToken: async (
    _config: QoderConfigLike,
    deviceCode?: string,
    codeVerifier?: string,
    extraData?: Record<string, unknown>,
  ) => {
    const { QoderService } = await import("@/lib/oauth/services/qoder");
    const svc = new QoderService();
    const nonce = deviceCode || (extraData?._qoderNonce as string);
    const verifier = codeVerifier || (extraData?._qoderVerifier as string);
    if (!nonce || !verifier) {
      return {
        ok: false,
        data: {
          error: "invalid_request",
          error_description: "Missing nonce/verifier",
        },
      };
    }
    let result: { status: string; accessToken?: string; refreshToken?: string; userId?: string; expireTime?: number };
    try {
      result = await svc.pollDeviceToken({ nonce, codeVerifier: verifier });
    } catch (err: unknown) {
      const e = err as Error;
      return {
        ok: false,
        data: { error: "poll_failed", error_description: e.message },
      };
    }
    if (result.status === "pending" || !result.accessToken) {
      return { ok: false, data: { error: "authorization_pending" } };
    }
    // Best-effort profile lookup so we have a name/email to display.
    const userInfo = await svc.fetchUserInfo(result.accessToken);
    const minSeconds = 24 * 60 * 60;
    const remainingSeconds = Math.floor(
      ((result.expireTime || 0) - Date.now()) / 1000,
    );
    const expiresIn = Math.max(minSeconds, remainingSeconds);
    return {
      ok: true,
      data: {
        access_token: result.accessToken,
        refresh_token: result.refreshToken,
        expires_in: expiresIn,
        _qoderUserId: result.userId,
        _qoderMachineId: (extraData?._qoderMachineId as string) || "",
        _qoderName: userInfo.name,
        _qoderEmail: userInfo.email,
        _qoderOrganizationId: userInfo.organizationId,
      },
    };
  },
  mapTokens: (tokens: Record<string, unknown>) => {
    const rawEmail = ((tokens._qoderEmail as string) || "").trim();
    const displayName = ((tokens._qoderName as string) || "").trim() || null;
    const userId = (tokens._qoderUserId as string) || "";
    const email = rawEmail || (userId ? `qoder-user-${userId}` : null);
    return {
      accessToken: tokens.access_token,
      refreshToken: tokens.refresh_token || null,
      expiresIn: tokens.expires_in,
      email,
      displayName,
      providerSpecificData: {
        authMethod: "device",
        userId,
        machineId: tokens._qoderMachineId || "",
        organizationId: tokens._qoderOrganizationId || "",
      },
    };
  },
};

export default qoder;
