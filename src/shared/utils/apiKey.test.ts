import { describe, it, expect } from "vitest";
import {
  generateApiKeyWithMachine,
  parseApiKey,
  verifyApiKeyCrc,
  isNewFormatKey,
  getApiKeySecret,
  resetApiKeySecretCache,
} from "./apiKey";

describe("apiKey utils", () => {
  it("API key secret dynamic resolution", () => {
    const originalEnv = process.env.API_KEY_SECRET;

    try {
      // 1. Test process.env override
      process.env.API_KEY_SECRET = "custom-test-secret-1234567890";
      resetApiKeySecretCache();
      expect(getApiKeySecret()).toBe("custom-test-secret-1234567890");

      // 2. Test fallback when process.env is not set
      delete process.env.API_KEY_SECRET;
      resetApiKeySecretCache();
      const secret = getApiKeySecret();
      expect(secret).toBeTruthy();
      expect(typeof secret).toBe("string");
      expect(secret).not.toBe("endpoint-proxy-api-key-secret");
      expect(secret.length >= 32).toBe(true);

      // 3. Secret caching
      const cachedSecret = getApiKeySecret();
      expect(cachedSecret).toBe(secret);
    } finally {
      process.env.API_KEY_SECRET = originalEnv;
      resetApiKeySecretCache();
    }
  });

  it("API key generation, parsing, and CRC verification", () => {
    const machineId = "0123456789abcdef";
    const { key, keyId } = generateApiKeyWithMachine(machineId);

    expect(key.startsWith("sk-")).toBe(true);
    expect(key.split("-").length).toBe(4);

    const parsed = parseApiKey(key);
    expect(parsed).not.toBeNull();
    expect(parsed?.machineId).toBe(machineId);
    expect(parsed?.keyId).toBe(keyId);
    expect(parsed?.isNewFormat).toBe(true);

    expect(verifyApiKeyCrc(key)).toBe(true);
    expect(isNewFormatKey(key)).toBe(true);
  });

  it("Invalid API key handling", () => {
    // Tampered CRC
    const invalidCrcKey = "sk-0123456789abcdef-keyid1-invalid0";
    expect(parseApiKey(invalidCrcKey)).toBeNull();
    expect(verifyApiKeyCrc(invalidCrcKey)).toBe(false);

    // Invalid prefix
    expect(parseApiKey("invalid-prefix-key")).toBeNull();

    // Old format parsing (sk-{random8})
    const oldKey = "sk-random12";
    const oldParsed = parseApiKey(oldKey);
    expect(oldParsed).not.toBeNull();
    expect(oldParsed?.isNewFormat).toBe(false);
    expect(oldParsed?.keyId).toBe("random12");
    expect(oldParsed?.machineId).toBeNull();
    expect(verifyApiKeyCrc(oldKey)).toBe(true);
    expect(isNewFormatKey(oldKey)).toBe(false);
  });
});
