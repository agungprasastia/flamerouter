import test from "node:test";
import assert from "node:assert";
import {
  generateApiKeyWithMachine,
  parseApiKey,
  verifyApiKeyCrc,
  isNewFormatKey,
  getApiKeySecret,
  resetApiKeySecretCache,
} from "./apiKey";

test("API key secret dynamic resolution", () => {
  const originalEnv = process.env.API_KEY_SECRET;

  try {
    // 1. Test process.env override
    process.env.API_KEY_SECRET = "custom-test-secret-1234567890";
    resetApiKeySecretCache();
    assert.strictEqual(getApiKeySecret(), "custom-test-secret-1234567890");

    // 2. Test fallback when process.env is not set
    delete process.env.API_KEY_SECRET;
    resetApiKeySecretCache();
    const secret = getApiKeySecret();
    assert.ok(secret);
    assert.strictEqual(typeof secret, "string");
    assert.notStrictEqual(secret, "endpoint-proxy-api-key-secret");
    assert.strictEqual(secret.length >= 32, true);

    // 3. Secret caching
    const cachedSecret = getApiKeySecret();
    assert.strictEqual(secret, cachedSecret);
  } finally {
    process.env.API_KEY_SECRET = originalEnv;
    resetApiKeySecretCache();
  }
});

test("API key generation, parsing, and CRC verification", () => {
  const machineId = "0123456789abcdef";
  const { key, keyId } = generateApiKeyWithMachine(machineId);

  assert.ok(key.startsWith("sk-"));
  assert.strictEqual(key.split("-").length, 4);

  const parsed = parseApiKey(key);
  assert.notStrictEqual(parsed, null);
  assert.strictEqual(parsed?.machineId, machineId);
  assert.strictEqual(parsed?.keyId, keyId);
  assert.strictEqual(parsed?.isNewFormat, true);

  assert.strictEqual(verifyApiKeyCrc(key), true);
  assert.strictEqual(isNewFormatKey(key), true);
});

test("Invalid API key handling", () => {
  // Tampered CRC
  const invalidCrcKey = "sk-0123456789abcdef-keyid1-invalid0";
  assert.strictEqual(parseApiKey(invalidCrcKey), null);
  assert.strictEqual(verifyApiKeyCrc(invalidCrcKey), false);

  // Invalid prefix
  assert.strictEqual(parseApiKey("invalid-prefix-key"), null);

  // Old format parsing (sk-{random8})
  const oldKey = "sk-random12";
  const oldParsed = parseApiKey(oldKey);
  assert.notStrictEqual(oldParsed, null);
  assert.strictEqual(oldParsed?.isNewFormat, false);
  assert.strictEqual(oldParsed?.keyId, "random12");
  assert.strictEqual(oldParsed?.machineId, null);
  assert.strictEqual(verifyApiKeyCrc(oldKey), true);
  assert.strictEqual(isNewFormatKey(oldKey), false);
});
