import crypto from "node:crypto";
import { createRemoteJWKSet, jwtVerify, type JWTPayload } from "jose";
import { getSettings } from "@/lib/localDb";

export const OIDC_COOKIE_NAMES = {
  state: "oidc_state",
  nonce: "oidc_nonce",
  verifier: "oidc_code_verifier",
};

const DEFAULT_SCOPES = "openid profile email";
const DEFAULT_LOGIN_LABEL = "Sign in with OIDC";

function trimTrailingSlashes(value?: string | null): string {
  return (value || "").trim().replace(/\/+$/, "");
}

function normalizeScopes(value?: string | null): string {
  return (value || DEFAULT_SCOPES).trim() || DEFAULT_SCOPES;
}

export interface RequestLike {
  headers?: {
    get?(name: string): string | null;
  };
  url: string;
}

export function getPublicOrigin(request?: RequestLike | null): string {
  const configuredBaseUrl =
    process.env.BASE_URL || process.env.NEXT_PUBLIC_BASE_URL || "";

  if (configuredBaseUrl) {
    return trimTrailingSlashes(configuredBaseUrl);
  }

  const forwardedProto = request?.headers?.get?.("x-forwarded-proto") || "";
  const forwardedHost = request?.headers?.get?.("x-forwarded-host") || "";
  const host = forwardedHost || request?.headers?.get?.("host") || "";
  if (host && request?.url) {
    const protocol = (
      forwardedProto ||
      new URL(request.url).protocol ||
      "http:"
    ).replace(/:$/, "");
    return `${protocol}://${host}`.replace(/\/+$/, "");
  }

  if (request?.url) {
    return trimTrailingSlashes(new URL(request.url).origin);
  }

  return "";
}

export interface OidcSettingsLike {
  oidcIssuerUrl?: string | null;
  oidcClientId?: string | null;
  oidcClientSecret?: string | null;
  authMode?: string | null;
  oidcScopes?: string | null;
  oidcLoginLabel?: string | null;
  [key: string]: unknown;
}

export function isOidcConfigured(settings?: OidcSettingsLike | null): boolean {
  return !!(
    trimTrailingSlashes(settings?.oidcIssuerUrl) &&
    (settings?.oidcClientId || "").trim() &&
    (settings?.oidcClientSecret || "").trim()
  );
}

export interface OidcRuntimeConfig {
  issuerUrl: string;
  clientId: string;
  clientSecret: string;
  scopes: string;
  loginLabel: string;
}

export async function getOidcRuntimeConfig(): Promise<OidcRuntimeConfig | null> {
  const settings = await getSettings();
  if (
    !["oidc", "both"].includes(String(settings.authMode || "")) ||
    !isOidcConfigured(settings as OidcSettingsLike)
  )
    return null;

  const issuerUrl = trimTrailingSlashes(settings.oidcIssuerUrl as string);
  return {
    issuerUrl,
    clientId: String(settings.oidcClientId || "").trim(),
    clientSecret: String(settings.oidcClientSecret || "").trim(),
    scopes: normalizeScopes(settings.oidcScopes as string),
    loginLabel:
      String(settings.oidcLoginLabel || DEFAULT_LOGIN_LABEL).trim() ||
      DEFAULT_LOGIN_LABEL,
  };
}

export interface OidcDiscoveryDoc {
  issuer?: string;
  authorization_endpoint?: string;
  token_endpoint?: string;
  jwks_uri?: string;
  userinfo_endpoint?: string;
  [key: string]: unknown;
}

export async function fetchOidcDiscovery(issuerUrl: string): Promise<OidcDiscoveryDoc> {
  const discoveryUrl = `${trimTrailingSlashes(issuerUrl)}/.well-known/openid-configuration`;
  const res = await fetch(discoveryUrl, { cache: "no-store" });
  if (!res.ok) {
    throw new Error(
      `Failed to load OIDC discovery document from ${discoveryUrl}`,
    );
  }
  return (await res.json()) as OidcDiscoveryDoc;
}

export function createPkcePair(): { verifier: string; challenge: string } {
  const verifier = crypto.randomBytes(32).toString("base64url");
  const challenge = crypto
    .createHash("sha256")
    .update(verifier)
    .digest("base64url");
  return { verifier, challenge };
}

export function createOidcState(): string {
  return crypto.randomBytes(16).toString("base64url");
}

export function createOidcNonce(): string {
  return crypto.randomBytes(16).toString("base64url");
}

export interface BuildOidcAuthUrlParams {
  authorizationEndpoint: string;
  clientId: string;
  redirectUri: string;
  scopes?: string;
  state: string;
  nonce: string;
  codeChallenge: string;
}

export function buildOidcAuthorizationUrl({
  authorizationEndpoint,
  clientId,
  redirectUri,
  scopes = DEFAULT_SCOPES,
  state,
  nonce,
  codeChallenge,
}: BuildOidcAuthUrlParams): string {
  const url = new URL(authorizationEndpoint);
  url.searchParams.set("response_type", "code");
  url.searchParams.set("client_id", clientId);
  url.searchParams.set("redirect_uri", redirectUri);
  url.searchParams.set("scope", normalizeScopes(scopes));
  url.searchParams.set("state", state);
  url.searchParams.set("nonce", nonce);
  url.searchParams.set("code_challenge", codeChallenge);
  url.searchParams.set("code_challenge_method", "S256");
  return url.toString();
}

export interface ExchangeOidcCodeParams {
  tokenEndpoint: string;
  clientId: string;
  clientSecret?: string;
  code: string;
  redirectUri: string;
  codeVerifier: string;
}

export interface OidcTokenResponse {
  access_token?: string;
  id_token?: string;
  refresh_token?: string;
  token_type?: string;
  expires_in?: number;
  [key: string]: unknown;
}

export async function exchangeOidcCode({
  tokenEndpoint,
  clientId,
  clientSecret,
  code,
  redirectUri,
  codeVerifier,
}: ExchangeOidcCodeParams): Promise<OidcTokenResponse> {
  const body = new URLSearchParams({
    grant_type: "authorization_code",
    client_id: clientId,
    code,
    redirect_uri: redirectUri,
    code_verifier: codeVerifier,
  });

  if (clientSecret) {
    body.set("client_secret", clientSecret);
  }

  const res = await fetch(tokenEndpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });

  const data = (await res.json().catch(() => ({}))) as { error_description?: string; error?: string } & OidcTokenResponse;
  if (!res.ok) {
    const message =
      data?.error_description ||
      data?.error ||
      `OIDC token exchange failed (${res.status})`;
    throw new Error(message);
  }

  return data;
}

export interface ProbeOidcParams {
  tokenEndpoint: string;
  clientId: string;
  clientSecret?: string;
  redirectUri: string;
}

export interface ProbeOidcResult {
  tested: boolean;
  valid: boolean | null;
  message: string;
  raw?: unknown;
}

export async function probeOidcClientSecret({
  tokenEndpoint,
  clientId,
  clientSecret,
  redirectUri,
}: ProbeOidcParams): Promise<ProbeOidcResult> {
  if (!clientSecret) {
    return {
      tested: false,
      valid: null,
      message:
        "No client secret was provided, so secret validation was skipped.",
    };
  }

  const body = new URLSearchParams({
    grant_type: "authorization_code",
    client_id: clientId,
    client_secret: clientSecret,
    code: "__oidc_test_invalid_code__",
    redirect_uri: redirectUri,
    code_verifier: "__oidc_test_invalid_verifier__",
  });

  const res = await fetch(tokenEndpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });

  const data = (await res.json().catch(() => ({}))) as { error?: string; error_description?: string };
  const error = (data?.error || "").toLowerCase();
  const errorDescription = data?.error_description || data?.error || "";

  if (res.ok) {
    return {
      tested: true,
      valid: true,
      message: "Client secret was accepted by the token endpoint.",
      raw: data,
    };
  }

  if (
    error === "invalid_client" ||
    error === "unauthorized_client" ||
    /client.*(invalid|failed|mismatch)/i.test(errorDescription)
  ) {
    return {
      tested: true,
      valid: false,
      message: errorDescription || "Client secret is not valid.",
      raw: data,
    };
  }

  if (
    error === "invalid_grant" ||
    error === "invalid_code" ||
    /grant|code/i.test(errorDescription)
  ) {
    return {
      tested: true,
      valid: true,
      message:
        "Client secret was accepted; the token exchange failed only because the test authorization code is invalid.",
      raw: data,
    };
  }

  return {
    tested: true,
    valid: null,
    message: errorDescription || `Token endpoint responded with ${res.status}`,
    raw: data,
  };
}

export interface VerifyOidcIdTokenParams {
  idToken: string;
  issuer: string;
  audience: string;
  jwksUri: string;
  nonce?: string;
}

export async function verifyOidcIdToken({
  idToken,
  issuer,
  audience,
  jwksUri,
  nonce,
}: VerifyOidcIdTokenParams): Promise<JWTPayload> {
  const jwks = createRemoteJWKSet(new URL(jwksUri));
  const opts: Record<string, string> = { issuer, audience };
  if (nonce) opts.nonce = nonce;
  const { payload } = await jwtVerify(idToken, jwks, opts);
  return payload;
}

export function pickOidcDisplayName(payload: Partial<JWTPayload & { preferred_username?: string; email?: string; name?: string; given_name?: string }> = {}): string {
  return (
    payload.preferred_username ||
    payload.email ||
    payload.name ||
    payload.given_name ||
    payload.sub ||
    "OIDC user"
  );
}

export function pickOidcEmail(payload: Partial<{ email?: string }> = {}): string {
  return payload.email || "";
}
