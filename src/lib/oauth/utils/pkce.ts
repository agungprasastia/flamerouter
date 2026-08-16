import crypto from "crypto";

export interface PKCEPair {
  codeVerifier: string;
  codeChallenge: string;
  state: string;
}

/**
 * Generate PKCE code verifier (43-128 characters)
 *
 * @param {number} [bytes=32] number of random bytes (xAI uses 96)
 */
export function generateCodeVerifier(bytes = 32): string {
  return crypto.randomBytes(bytes).toString("base64url");
}

/**
 * Generate PKCE code challenge from verifier (S256 method)
 */
export function generateCodeChallenge(verifier: string): string {
  return crypto.createHash("sha256").update(verifier).digest("base64url");
}

/**
 * Generate random state for CSRF protection
 */
export function generateState(): string {
  return crypto.randomBytes(32).toString("base64url");
}

/**
 * Generate complete PKCE pair
 */
export function generatePKCE(bytes = 32): PKCEPair {
  const codeVerifier = generateCodeVerifier(bytes);
  const codeChallenge = generateCodeChallenge(codeVerifier);
  const state = generateState();

  return {
    codeVerifier,
    codeChallenge,
    state,
  };
}
