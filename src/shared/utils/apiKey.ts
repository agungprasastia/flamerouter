import crypto from "crypto";
import fs from "fs";
import path from "path";
import { DATA_DIR } from "@/lib/dataDir";

let cachedApiKeySecret: string | null = null;

/**
 * Resets the in-memory cached secret (useful for testing when process.env changes).
 */
export function resetApiKeySecretCache() {
  cachedApiKeySecret = null;
}

/**
 * Resolve the API key secret dynamically:
 * 1. process.env.API_KEY_SECRET if set
 * 2. Cached secret in memory
 * 3. Secret stored in DATA_DIR/api-key-secret
 * 4. Newly generated and persisted 32-byte hex secret
 * 5. In-memory generated secret as fallback if disk operations fail
 */
export function getApiKeySecret(): string {
  const envSecret = process.env.API_KEY_SECRET?.trim();
  if (envSecret) return envSecret;

  if (cachedApiKeySecret) return cachedApiKeySecret;

  try {
    const filePath = path.join(DATA_DIR, "api-key-secret");
    if (fs.existsSync(filePath)) {
      const data = fs.readFileSync(filePath, "utf8").trim();
      if (data) {
        cachedApiKeySecret = data;
        return data;
      }
    }

    const generated = crypto.randomBytes(32).toString("hex");
    fs.mkdirSync(DATA_DIR, { recursive: true });
    fs.writeFileSync(filePath, generated, { mode: 0o600 });
    cachedApiKeySecret = generated;
    return generated;
  } catch {
    cachedApiKeySecret = crypto.randomBytes(32).toString("hex");
    return cachedApiKeySecret;
  }
}

/**
 * Generate 6-char random keyId
 */
function generateKeyId() {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  let result = "";
  for (let i = 0; i < 6; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return result;
}

/**
 * Generate CRC (8-char HMAC)
 */
function generateCrc(machineId: string, keyId: string) {
  return crypto
    .createHmac("sha256", getApiKeySecret())
    .update(machineId + keyId)
    .digest("hex")
    .slice(0, 8);
}

/**
 * Generate API key with machineId embedded
 * Format: sk-{machineId}-{keyId}-{crc8}
 * @param {string} machineId - 16-char machine ID
 * @returns {{ key: string, keyId: string }}
 */
export function generateApiKeyWithMachine(machineId: string) {
  const keyId = generateKeyId();
  const crc = generateCrc(machineId, keyId);
  const key = `sk-${machineId}-${keyId}-${crc}`;
  return { key, keyId };
}

/**
 * Parse API key and extract machineId + keyId
 * Supports both formats:
 * - New: sk-{machineId}-{keyId}-{crc8}
 * - Old: sk-{random8}
 * @param {string} apiKey
 * @returns {{ machineId: string | null, keyId: string, isNewFormat: boolean } | null}
 */
export function parseApiKey(apiKey: string) {
  if (!apiKey || !apiKey.startsWith("sk-")) return null;

  const parts = apiKey.split("-");

  // New format: sk-{machineId}-{keyId}-{crc8} = 4 parts
  if (parts.length === 4) {
    const [, machineId, keyId, crc] = parts;

    // Validate CRC
    const expectedCrc = generateCrc(machineId, keyId);
    if (crc !== expectedCrc) return null;

    return { machineId, keyId, isNewFormat: true };
  }

  // Old format: sk-{random8} = 2 parts
  if (parts.length === 2) {
    return { machineId: null, keyId: parts[1], isNewFormat: false };
  }

  return null;
}

/**
 * Verify API key CRC (only for new format)
 * @param {string} apiKey
 * @returns {boolean}
 */
export function verifyApiKeyCrc(apiKey: string) {
  const parsed = parseApiKey(apiKey);
  if (!parsed) return false;

  // Old format doesn't have CRC, always valid if parsed
  if (!parsed.isNewFormat) return true;

  // New format already verified in parseApiKey
  return true;
}

/**
 * Check if API key is new format (contains machineId)
 * @param {string} apiKey
 * @returns {boolean}
 */
export function isNewFormatKey(apiKey: string) {
  const parsed = parseApiKey(apiKey);
  return parsed?.isNewFormat === true;
}
