import { generatePKCE } from "../utils/pkce";
import {
  fetchKiroProfileArn,
} from "../providerHelpers";
import { extractCodexAccountInfo } from "./codex";

import claude from "./claude";
import codex from "./codex";
import xai from "./xai";
import grokCli from "./grok-cli";
import geminiCli from "./gemini-cli";
import antigravity from "./antigravity";
import iflow from "./iflow";
import qoder from "./qoder";
import github from "./github";
import kiro from "./kiro";
import cursor from "./cursor";
import kimi from "./kimi";
import kilocode from "./kilocode";
import cline from "./cline";
import clinepass from "./clinepass";
import gitlab from "./gitlab";
import codebuddyCn from "./codebuddy-cn";
import codebuddyIntl from "./codebuddy-intl";
import kimchi from "./kimchi";
import trae from "./trae";
import windsurf from "./windsurf";
import zed from "./zed";

export interface OAuthProviderHandler {
  config: Record<string, unknown>;
  flowType: string;
  fixedPort?: number;
  callbackPath?: string;
  pkceVerifierBytes?: number;
  prepareConfig?: (config: Record<string, unknown>, meta: Record<string, unknown>) => Promise<Record<string, unknown>> | Record<string, unknown>;
  buildAuthUrl: (config: Record<string, unknown>, redirectUri: string, state: string, codeChallenge?: string, meta?: Record<string, unknown>) => string;
  exchangeToken: (config: Record<string, unknown>, code: string, redirectUri: string, codeVerifier: string, state?: string, meta?: Record<string, unknown>) => Promise<Record<string, unknown>>;
  postExchange?: (tokens: Record<string, unknown>) => Promise<Record<string, unknown> | null>;
  mapTokens: (tokens: Record<string, unknown>, extra?: Record<string, unknown> | null) => Record<string, unknown>;
  requestDeviceCode?: (config: Record<string, unknown>, codeChallenge?: string, options?: Record<string, unknown>) => Promise<Record<string, unknown>>;
  pollToken?: (config: Record<string, unknown>, deviceCode: string, codeVerifier?: string, extraData?: Record<string, unknown>) => Promise<{ ok: boolean; data: Record<string, unknown> }>;
  [key: string]: unknown;
}

// Provider configurations
const PROVIDERS: Record<string, OAuthProviderHandler> = {
  claude: claude as unknown as OAuthProviderHandler,
  codex: codex as unknown as OAuthProviderHandler,
  xai: xai as unknown as OAuthProviderHandler,
  "grok-cli": grokCli as unknown as OAuthProviderHandler,
  "gemini-cli": geminiCli as unknown as OAuthProviderHandler,
  antigravity: antigravity as unknown as OAuthProviderHandler,
  iflow: iflow as unknown as OAuthProviderHandler,
  qoder: qoder as unknown as OAuthProviderHandler,
  github: github as unknown as OAuthProviderHandler,
  kiro: kiro as unknown as OAuthProviderHandler,
  cursor: cursor as unknown as OAuthProviderHandler,
  kimi: kimi as unknown as OAuthProviderHandler,
  kilocode: kilocode as unknown as OAuthProviderHandler,
  cline: cline as unknown as OAuthProviderHandler,
  clinepass: clinepass as unknown as OAuthProviderHandler,
  gitlab: gitlab as unknown as OAuthProviderHandler,
  "codebuddy-cn": codebuddyCn as unknown as OAuthProviderHandler,
  "codebuddy-intl": codebuddyIntl as unknown as OAuthProviderHandler,
  kimchi: kimchi as unknown as OAuthProviderHandler,
  trae: trae as unknown as OAuthProviderHandler,
  windsurf: windsurf as unknown as OAuthProviderHandler,
  zed: zed as unknown as OAuthProviderHandler,
};

export { PROVIDERS };

// Re-export helpers that other files import from this path
export { extractCodexAccountInfo, fetchKiroProfileArn };

/**
 * Get provider handler
 */
export function getProvider(name: string): OAuthProviderHandler {
  // Legacy kimi-coding → kimi (dual-auth merge)
  const key = name === "kimi-coding" ? "kimi" : name;
  const provider = PROVIDERS[key];
  if (!provider) {
    throw new Error(`Unknown provider: ${name}`);
  }
  return provider;
}

/**
 * Get all provider names
 */
export function getProviderNames(): string[] {
  return Object.keys(PROVIDERS);
}

/**
 * Generate auth data for a provider
 * @param {object} [meta] - Provider-specific metadata (e.g. gitlab clientId/baseUrl)
 */
export async function generateAuthData(providerName: string, redirectUri: string, meta?: Record<string, unknown>) {
  const provider = getProvider(providerName);
  const config = provider.prepareConfig
    ? await provider.prepareConfig(provider.config, meta || {})
    : provider.config;
  const {
    codeVerifier: pkceVerifier,
    codeChallenge,
    state: pkceState,
  } = generatePKCE(provider.pkceVerifierBytes);
  // Trae uses loginTraceID (set by prepareConfig) as the callback matcher, not PKCE state.
  const state = (config.loginTraceID as string) || pkceState;
  // Zed: codeVerifier carries the encoded RSA private key (from prepareConfig), not a PKCE verifier.
  const codeVerifier = (config.privateKeyVerifier as string) || pkceVerifier;

  let authUrl: string | null;
  if (provider.flowType === "device_code") {
    // Device code flow doesn't have auth URL upfront
    authUrl = null;
  } else if (provider.flowType === "authorization_code_pkce") {
    authUrl = provider.buildAuthUrl(
      config,
      redirectUri,
      state,
      codeChallenge,
      meta || {},
    );
  } else {
    authUrl = provider.buildAuthUrl(
      config,
      redirectUri,
      state,
      undefined,
      meta || {},
    );
  }

  return {
    authUrl,
    state,
    codeVerifier,
    codeChallenge,
    redirectUri,
    flowType: provider.flowType,
    fixedPort: provider.fixedPort,
    callbackPath: provider.callbackPath || "/callback",
  };
}

/**
 * Exchange code for tokens
 * @param {object} [meta] - Provider-specific metadata (e.g. gitlab clientId/baseUrl)
 */
export async function exchangeTokens(
  providerName: string,
  code: string,
  redirectUri: string,
  codeVerifier: string,
  state?: string,
  meta?: Record<string, unknown>,
) {
  const provider = getProvider(providerName);
  const config = provider.prepareConfig
    ? await provider.prepareConfig(provider.config, meta || {})
    : provider.config;

  const tokens = await provider.exchangeToken(
    config,
    code,
    redirectUri,
    codeVerifier,
    state,
    meta || {},
  );

  let extra: Record<string, unknown> | null = null;
  if (provider.postExchange) {
    extra = await provider.postExchange(tokens);
  }

  return provider.mapTokens(tokens, extra);
}

/**
 * Request device code (for device_code flow)
 */
export async function requestDeviceCode(providerName: string, codeChallenge?: string, options?: Record<string, unknown>) {
  const provider = getProvider(providerName);
  if (provider.flowType !== "device_code" || !provider.requestDeviceCode) {
    throw new Error(
      `Provider ${providerName} does not support device code flow`,
    );
  }
  return await provider.requestDeviceCode(
    provider.config,
    codeChallenge,
    options || {},
  );
}

/**
 * Poll for token (for device_code flow)
 * @param {string} providerName - Provider name
 * @param {string} deviceCode - Device code from requestDeviceCode
 * @param {string} codeVerifier - PKCE code verifier (optional for some providers)
 * @param {object} extraData - Extra data from device code response (e.g. clientId/clientSecret for Kiro)
 */
export async function pollForToken(
  providerName: string,
  deviceCode: string,
  codeVerifier?: string,
  extraData?: Record<string, unknown>,
) {
  const provider = getProvider(providerName);
  if (provider.flowType !== "device_code" || !provider.pollToken) {
    throw new Error(
      `Provider ${providerName} does not support device code flow`,
    );
  }

  const result = await provider.pollToken(
    provider.config,
    deviceCode,
    codeVerifier,
    extraData,
  );

  if (result.ok) {
    // For device code flows, success is only when we have an access token
    if (result.data.access_token) {
      // Call postExchange to get additional data (copilotToken, userInfo, etc.)
      let extra: Record<string, unknown> | null = null;
      if (provider.postExchange) {
        extra = await provider.postExchange(result.data);
      }
      const tokens = provider.mapTokens(result.data, extra);
      // Kiro IDC/Builder-ID tokens lack profileArn; resolve it to avoid 403
      const specific = (tokens.providerSpecificData as Record<string, unknown>) || {};
      if (providerName === "kiro" && !specific.profileArn) {
        const profileArn = await fetchKiroProfileArn(tokens.accessToken as string);
        if (profileArn) {
          specific.profileArn = profileArn;
          tokens.providerSpecificData = specific;
        }
      }
      return { success: true, tokens };
    } else {
      // Check if it's still pending authorization
      if (
        result.data.error === "authorization_pending" ||
        result.data.error === "slow_down"
      ) {
        // This is not a failure, just still waiting
        return {
          success: false,
          error: result.data.error,
          errorDescription:
            result.data.error_description || result.data.message,
          pending: result.data.error === "authorization_pending",
        };
      } else {
        // Actual error
        return {
          success: false,
          error: result.data.error || "no_access_token",
          errorDescription:
            result.data.error_description ||
            result.data.message ||
            "No access token received",
        };
      }
    }
  }

  return {
    success: false,
    error: result.data?.error,
    errorDescription: result.data?.error_description,
  };
}

// Run-once guard across the process lifetime
let codexBackfillDone = false;

// Backfill email + chatgpt account info for existing codex OAuth connections missing them
export async function backfillCodexEmails(): Promise<void> {
  if (codexBackfillDone) return;
  codexBackfillDone = true;
  try {
    const { getProviderConnections, updateProviderConnection } =
      await import("@/lib/localDb");
    const connections = await getProviderConnections();
    const targets = connections.filter((c) => {
      if (c.provider !== "codex" || c.authType !== "oauth" || !c.idToken)
        return false;
      const hasEmail = !!c.email;
      const hasAccountInfo = !!(c.providerSpecificData as Record<string, unknown> | undefined)?.chatgptAccountId;
      return !hasEmail || !hasAccountInfo;
    });
    for (const conn of targets) {
      const info = extractCodexAccountInfo(conn.idToken as string);
      if (!info.email && !info.chatgptAccountId) continue;
      const patch: Record<string, unknown> = {};
      if (!conn.email && info.email) patch.email = info.email;
      if (info.chatgptAccountId || info.chatgptPlanType) {
        patch.providerSpecificData = {
          ...((conn.providerSpecificData as Record<string, unknown>) || {}),
          chatgptAccountId: info.chatgptAccountId,
          chatgptPlanType: info.chatgptPlanType,
        };
      }
      if (Object.keys(patch).length) {
        await updateProviderConnection(conn.id, patch);
      }
    }
  } catch (err: unknown) {
    codexBackfillDone = false;
    const e = err as Error;
    console.log("backfillCodexEmails failed:", e?.message || String(err));
  }
}
