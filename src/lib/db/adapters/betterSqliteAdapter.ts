import { PRAGMA_SQL } from "../schema";
import type { DatabaseAdapter } from "../driver";

declare global {
  // eslint-disable-next-line no-var
  var _betterSqliteShutdownRegistered: boolean | undefined;
}

// Periodic checkpoint to keep WAL file small (avoid huge -wal/-shm growth)
const CHECKPOINT_INTERVAL_MS = 60 * 1000;

interface BetterSqliteStatement {
  run(...params: unknown[]): { changes: number; lastInsertRowid: number | bigint };
  get(...params: unknown[]): unknown;
  all(...params: unknown[]): unknown[];
}

interface BetterSqliteDatabase {
  exec(sql: string): void;
  prepare(sql: string): BetterSqliteStatement;
  transaction<T>(fn: () => T): () => T;
  pragma(sql: string): unknown;
  close(): void;
}

export function createBetterSqliteAdapter(filePath: string): DatabaseAdapter & { raw: unknown; checkpoint: () => void } {
  let Database: new (path: string) => BetterSqliteDatabase;
  try {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const mod = require("better-sqlite3");
    Database = (mod.default || mod) as new (path: string) => BetterSqliteDatabase;
  } catch {
    throw new Error("better-sqlite3 is not installed");
  }

  const db = new Database(filePath);
  db.exec(PRAGMA_SQL);

  const stmtCache = new Map<string, BetterSqliteStatement>();

  function prepare(sql: string): BetterSqliteStatement {
    let stmt = stmtCache.get(sql);
    if (!stmt) {
      stmt = db.prepare(sql);
      stmtCache.set(sql, stmt);
    }
    return stmt;
  }

  // Truncate WAL periodically so file stays small for backup/copy
  const checkpointTimer = setInterval(() => {
    try {
      db.pragma("wal_checkpoint(TRUNCATE)");
    } catch {}
  }, CHECKPOINT_INTERVAL_MS);
  if (typeof checkpointTimer.unref === "function") checkpointTimer.unref();

  function gracefulClose() {
    try {
      db.pragma("wal_checkpoint(TRUNCATE)");
    } catch {}
    try {
      stmtCache.clear();
    } catch {}
    try {
      db.close();
    } catch {}
  }

  if (!global._betterSqliteShutdownRegistered) {
    global._betterSqliteShutdownRegistered = true;
    const onShutdown = () => gracefulClose();
    process.once("beforeExit", onShutdown);
    process.once("SIGINT", () => {
      onShutdown();
      process.exit(0);
    });
    process.once("SIGTERM", () => {
      onShutdown();
      process.exit(0);
    });
  }

  return {
    driver: "better-sqlite3",
    run(sql: string, params: unknown[] = []) {
      return prepare(sql).run(...params);
    },
    get<T = unknown>(sql: string, params: unknown[] = []): T | undefined {
      return prepare(sql).get(...params) as T | undefined;
    },
    all<T = unknown>(sql: string, params: unknown[] = []): T[] {
      return prepare(sql).all(...params) as T[];
    },
    exec(sql: string) {
      return db.exec(sql);
    },
    transaction<T>(fn: () => T): T {
      return db.transaction(fn)();
    },
    checkpoint() {
      try {
        db.pragma("wal_checkpoint(TRUNCATE)");
      } catch {}
    },
    close() {
      clearInterval(checkpointTimer);
      gracefulClose();
    },
    raw: db,
  };
}
