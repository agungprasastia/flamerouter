// JSON cache for mitmAlias — read by standalone MITM server (no SQLite native binding).
// Source of truth = SQLite kv['mitmAlias']. JSON is a read-replica synced on app start
// and after every UI write.
import fs from "fs";
import path from "path";
import os from "os";

const DATA_DIR =
  process.env.DATA_DIR ||
  (process.platform === "win32"
    ? path.join(
        process.env.APPDATA || path.join(os.homedir(), "AppData", "Roaming"),
        "flamerouter",
      )
    : path.join(os.homedir(), ".flamerouter"));

const CACHE_FILE = path.join(DATA_DIR, "mitm", "aliases.json");

function writeAtomic(data: unknown): void {
  const dir = path.dirname(CACHE_FILE);
  fs.mkdirSync(dir, { recursive: true });
  const tmp = `${CACHE_FILE}.tmp`;
  fs.writeFileSync(tmp, JSON.stringify(data, null, 2), "utf8");
  fs.renameSync(tmp, CACHE_FILE);
}

// Sync entire mitmAlias map from DB → JSON file
export async function syncToJson(): Promise<void> {
  try {
    const { getMitmAlias } = await import("@/lib/db/repos/aliasRepo");
    const all = await getMitmAlias();
    writeAtomic(all || {});
  } catch (e: unknown) {
    const err = e as Error;
    console.log("[mitmAliasCache] sync failed:", err.message);
  }
}

// Update cache for a single tool after UI saves to DB
export function writeAliasForTool(tool: string, mappings: unknown): void {
  try {
    let current: Record<string, unknown> = {};
    if (fs.existsSync(CACHE_FILE)) {
      try {
        current = JSON.parse(fs.readFileSync(CACHE_FILE, "utf8")) as Record<string, unknown>;
      } catch {
        /* corrupted → reset */
      }
    }
    current[tool] = mappings || {};
    writeAtomic(current);
  } catch (e: unknown) {
    const err = e as Error;
    console.log("[mitmAliasCache] write failed:", err.message);
  }
}
