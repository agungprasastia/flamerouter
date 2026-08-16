import crypto from "crypto";
import { XAI_CONFIG, XAI_PKCE_VERIFIER_BYTES } from "../constants/xai";
import {
  validateXaiOAuthEndpoint,
  decodeXaiIdTokenEmail,
} from "../providerHelpers";

interface XaiEndpoints {
  authorizeUrl: string;
  tokenUrl: string;
}

interface XaiConfigLike {
  discoveryUrl?: string;
  authorizeUrl?: string;
  tokenUrl?: string;
  loopbackPort?: number;
  callbackPath?: string;
  clientId?: string;
  scope?: string;
  codeChallengeMethod?: string;
  [key: string]: unknown;
}

// Inlined from services/xai.js to keep web route bundle free of `open` (CLI-only) package
let cachedXaiDiscovery: XaiEndpoints | null = null;

async function discoverXaiEndpoints(): Promise<XaiEndpoints> {
  if (cachedXaiDiscovery) return cachedXaiDiscovery;
  try {
    const res = await fetch((XAI_CONFIG as unknown as XaiConfigLike).discoveryUrl || "", {
      headers: { Accept: "application/json" },
    });
    if (res.ok) {
      const data = (await res.json()) as { authorization_endpoint?: string; token_endpoint?: string };
      cachedXaiDiscovery = {
        authorizeUrl: validateXaiOAuthEndpoint(
          data.authorization_endpoint,
          "authorization_endpoint",
        ),
        tokenUrl: validateXaiOAuthEndpoint(
          data.token_endpoint,
          "token_endpoint",
        ),
      };
      return cachedXaiDiscovery;
    }
  } catch {
    /* fall through to static fallback */
  }
  const cfg = XAI_CONFIG as unknown as XaiConfigLike;
  cachedXaiDiscovery = {
    authorizeUrl: cfg.authorizeUrl || "",
    tokenUrl: cfg.tokenUrl || "",
  };
  return cachedXaiDiscovery;
}

const xai = {
  config: XAI_CONFIG,
  flowType: "authorization_code_pkce",
  fixedPort: (XAI_CONFIG as unknown as XaiConfigLike).loopbackPort,
  callbackPath: (XAI_CONFIG as unknown as XaiConfigLike).callbackPath,
  pkceVerifierBytes: XAI_PKCE_VERIFIER_BYTES,
  prepareConfig: async (config: XaiConfigLike) => {
    const endpoints = await discoverXaiEndpoints();
    return {
      ...config,
      authorizeUrl: endpoints.authorizeUrl,
      tokenUrl: endpoints.tokenUrl,
    };
  },
  buildAuthUrl: (config: XaiConfigLike, redirectUri: string, state: string, codeChallenge?: string) => {
    // Mirror CLIProxyAPI BuildAuthorizeURL: includes nonce, plan, referrer
    const nonce = crypto.randomBytes(16).toString("hex");
    const params: Record<string, string> = {
      response_type: "code",
      client_id: config.clientId || "",
      redirect_uri: redirectUri,
      scope: config.scope || "",
      code_challenge: codeChallenge || "",
      code_challenge_method: config.codeChallengeMethod || "S256",
      state,
      nonce,
      plan: "generic",
      referrer: "cli-proxy-api",
    };
    const qs = Object.entries(params)
      .map(([k, v]) => `${k}=${encodeURIComponent(v)}`)
      .join("&");
    return `${config.authorizeUrl}?${qs}`;
  },
  exchangeToken: async (config: XaiConfigLike, code: string, redirectUri: string, codeVerifier: string) => {
    const response = await fetch(config.tokenUrl || "", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
      },
      body: new URLSearchParams({
        grant_type: "authorization_code",
        client_id: config.clientId || "",
        code,
        redirect_uri: redirectUri,
        code_verifier: codeVerifier,
      }),
    });
    if (!response.ok) {
      const error = await response.text();
      throw new Error(`xAI token exchange failed: ${error}`);
    }
    return (await response.json()) as Record<string, unknown>;
  },
  mapTokens: (tokens: Record<string, unknown>) => {
    const mapped: Record<string, unknown> = {
      accessToken: tokens.access_token,
      refreshToken: tokens.refresh_token,
      expiresIn: tokens.expires_in,
      tokenType: tokens.token_type,
      scope: tokens.scope,
      idToken: tokens.id_token,
    };
    const email = decodeXaiIdTokenEmail(tokens.id_token as string);
    if (email) mapped.email = email;
    return mapped;
  },
};

export default xai;
