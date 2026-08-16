import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import os from "node:os";
import { DATA_DIR } from "@/lib/dataDir";

const MACHINE_ID_FILE = path.join(DATA_DIR, "machine-id");
const AUTH_DIR = path.join(DATA_DIR, "auth");
const CLI_SECRET_FILE = path.join(AUTH_DIR, "cli-secret");
const CLI_AUTH_SALT = "9r-cli-auth";
let cachedRawId: string | null = null;
let cachedCliSecret: string | null = null;

function generateFallbackId(): string {
  try {
    const net = os.networkInterfaces();
    const macs = Object.values(net)
      .flat()
      .filter((iface): iface is os.NetworkInterfaceInfo => !!iface && !iface.internal && iface.mac !== "00:00:00:00:00:00")
      .map((iface) => iface.mac)
      .sort()
      .join(";");
    if (macs) {
      return crypto.createHash("sha256").update(macs + os.hostname()).digest("hex");
    }
  } catch {}
  return crypto.randomUUID();
}

function loadRawMachineId(): string {
  if (cachedRawId) return cachedRawId;
  try {
    cachedRawId = fs.readFileSync(MACHINE_ID_FILE, "utf8").trim();
    if (cachedRawId) return cachedRawId;
  } catch {}

  cachedRawId = generateFallbackId();
  try {
    fs.mkdirSync(DATA_DIR, { recursive: true });
    fs.writeFileSync(MACHINE_ID_FILE, cachedRawId, { mode: 0o600 });
  } catch {}
  return cachedRawId;
}

function loadCliSecret(): string {
  if (cachedCliSecret) return cachedCliSecret;
  try {
    cachedCliSecret = fs.readFileSync(CLI_SECRET_FILE, "utf8").trim();
    if (cachedCliSecret) return cachedCliSecret;
  } catch {}
  cachedCliSecret = crypto.randomBytes(32).toString("hex");
  try {
    fs.mkdirSync(AUTH_DIR, { recursive: true });
    fs.writeFileSync(CLI_SECRET_FILE, cachedCliSecret, { mode: 0o600 });
  } catch {}
  return cachedCliSecret;
}

export function getRawMachineId(): string {
  return loadRawMachineId();
}

export function getConsistentMachineId(salt = ""): string {
  const raw = loadRawMachineId();
  if (!salt) return raw;
  return crypto.createHash("sha256").update(raw + salt).digest("hex");
}

export function getCliToken(): string {
  const raw = loadRawMachineId();
  const secret = loadCliSecret();
  return crypto
    .createHash("sha256")
    .update(`${raw}:${secret}:${CLI_AUTH_SALT}`)
    .digest("hex");
}

export function verifyCliToken(token: string): boolean {
  if (!token || typeof token !== "string") return false;
  const expected = getCliToken();
  if (token.length !== expected.length) return false;
  return crypto.timingSafeEqual(Buffer.from(token), Buffer.from(expected));
}
