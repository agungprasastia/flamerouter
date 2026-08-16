import { ensureDirs, DATA_FILE } from "./paths";

export interface DatabaseAdapter {
  driver: string;
  exec(sql: string): void;
  run(sql: string, params?: unknown[]): { changes?: number; lastInsertRowid?: number | bigint };
  get<T = unknown>(sql: string, params?: unknown[]): T | undefined;
  all<T = unknown>(sql: string, params?: unknown[]): T[];
  transaction<T>(fn: () => T): T;
  close?(): void;
}

interface GlobalDbAdapterState {
  instance: DatabaseAdapter | null;
  initPromise: Promise<DatabaseAdapter> | null;
  logged: boolean;
}

declare global {
  // eslint-disable-next-line no-var
  var _dbAdapter: GlobalDbAdapterState | undefined;
}

// Use global to survive Next.js dev hot-reload (module state resets on reload)
if (!global._dbAdapter) {
  global._dbAdapter = { instance: null, initPromise: null, logged: false };
}
const state = global._dbAdapter as GlobalDbAdapterState;

async function tryBunSqlite(): Promise<DatabaseAdapter | null> {
  // Bun runtime only — built-in, no install needed
  if (!process.versions.bun) return null;
  try {
    const { createBunSqliteAdapter } =
      await import("./adapters/bunSqliteAdapter");
    return (await createBunSqliteAdapter(DATA_FILE)) as DatabaseAdapter;
  } catch (e: unknown) {
    const err = e as Error;
    console.warn(`[DB] bun:sqlite unavailable: ${err.message}`);
    return null;
  }
}

async function tryBetterSqlite(): Promise<DatabaseAdapter | null> {
  // Skip on Bun — better-sqlite3 native bindings unsupported
  if (process.versions.bun) return null;
  try {
    const { createBetterSqliteAdapter } =
      await import("./adapters/betterSqliteAdapter");
    return createBetterSqliteAdapter(DATA_FILE) as DatabaseAdapter;
  } catch (e: unknown) {
    const err = e as Error;
    console.warn(`[DB] better-sqlite3 unavailable: ${err.message}`);
    return null;
  }
}

async function tryNodeSqlite(): Promise<DatabaseAdapter | null> {
  // Built-in since Node 22.5.0 — no install needed. Skip under Bun (no node:sqlite).
  if (process.versions.bun) return null;
  const [maj, min] = (process.versions.node || "0.0.0").split(".").map(Number);
  if ((maj ?? 0) < 22 || (maj === 22 && (min ?? 0) < 5)) return null;
  try {
    const { createNodeSqliteAdapter } =
      await import("./adapters/nodeSqliteAdapter");
    return (await createNodeSqliteAdapter(DATA_FILE)) as DatabaseAdapter;
  } catch (e: unknown) {
    const err = e as Error;
    console.warn(`[DB] node:sqlite unavailable: ${err.message}`);
    return null;
  }
}

async function trySqlJs(): Promise<DatabaseAdapter | null> {
  try {
    const { createSqlJsAdapter } = await import("./adapters/sqljsAdapter");
    return (await createSqlJsAdapter(DATA_FILE)) as DatabaseAdapter;
  } catch (e: unknown) {
    const err = e as Error;
    console.warn(`[DB] sql.js unavailable: ${err.message}`);
    return null;
  }
}

async function initAdapter(): Promise<DatabaseAdapter> {
  ensureDirs();
  // Order per runtime:
  //   Bun:  bun:sqlite → sql.js
  //   Node: better-sqlite3 → node:sqlite (≥22.5) → sql.js
  let adapter = await tryBunSqlite();
  if (!adapter) adapter = await tryBetterSqlite();
  if (!adapter) adapter = await tryNodeSqlite();
  if (!adapter) adapter = await trySqlJs();
  if (!adapter)
    throw new Error(
      "[DB] No SQLite driver available (bun/better/node/sql.js all failed)",
    );

  if (!state.logged) {
    console.log(`[DB] Driver: ${adapter.driver} | file: ${DATA_FILE}`);
    state.logged = true;
  }

  const { runMigrationOnce } = await import("./migrate");
  await runMigrationOnce(adapter);
  return adapter;
}

export async function getAdapter(): Promise<DatabaseAdapter> {
  if (state.instance) return state.instance;
  if (!state.initPromise)
    state.initPromise = initAdapter().then((a) => {
      state.instance = a;
      return a;
    });
  return state.initPromise;
}

export function getAdapterSync(): DatabaseAdapter {
  if (!state.instance)
    throw new Error("[DB] adapter not initialized — await getAdapter() first");
  return state.instance;
}
