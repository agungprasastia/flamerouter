// Bun runtime adapter — uses built-in bun:sqlite (native, fastest under Bun).
// Loaded only when process.versions.bun is present.
import { PRAGMA_SQL } from "../schema";
import type { DatabaseAdapter } from "../driver";

const CHECKPOINT_INTERVAL_MS = 60 * 1000;

interface BunStatement {
  run(...params: unknown[]): { changes?: number; lastInsertRowid?: number | bigint };
  get(...params: unknown[]): unknown;
  all(...params: unknown[]): unknown[];
}

interface BunDatabase {
  exec(sql: string): void;
  prepare(sql: string): BunStatement;
  transaction<T>(fn: () => T): () => T;
  close(): void;
}

export async function createBunSqliteAdapter(filePath: string): Promise<DatabaseAdapter & { raw: unknown; checkpoint: () => void }> {
  // Dynamic import — only resolves under Bun runtime
  // eslint-disable-next-line @typescript-eslint/ban-ts-comment
  // @ts-ignore - bun:sqlite is only present in bun runtime
  const bunSqlite = await import("bun:sqlite");
  const db = new (bunSqlite.Database as unknown as new (path: string, opts: { create: boolean }) => BunDatabase)(filePath, { create: true });
  db.exec(PRAGMA_SQL);

  const stmtCache = new Map<string, BunStatement>();
  function prepare(sql: string): BunStatement {
    let stmt = stmtCache.get(sql);
    if (!stmt) {
      stmt = db.prepare(sql);
      stmtCache.set(sql, stmt);
    }
    return stmt;
  }

  const checkpointTimer = setInterval(() => {
    try {
      db.exec("PRAGMA wal_checkpoint(TRUNCATE)");
    } catch {}
  }, CHECKPOINT_INTERVAL_MS);
  if (typeof checkpointTimer.unref === "function") checkpointTimer.unref();

  function gracefulClose() {
    try {
      db.exec("PRAGMA wal_checkpoint(TRUNCATE)");
    } catch {}
    try {
      stmtCache.clear();
    } catch {}
    try {
      db.close();
    } catch {}
  }
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

  return {
    driver: "bun:sqlite",
    run(sql: string, params: unknown[] = []) {
      const r = prepare(sql).run(...params);
      return {
        changes: Number(r.changes ?? 0),
        lastInsertRowid: Number(r.lastInsertRowid ?? 0),
      };
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
      // bun:sqlite has db.transaction() API (similar to better-sqlite3)
      const tx = db.transaction(fn);
      return tx();
    },
    checkpoint() {
      try {
        db.exec("PRAGMA wal_checkpoint(TRUNCATE)");
      } catch {}
    },
    close() {
      clearInterval(checkpointTimer);
      gracefulClose();
    },
    raw: db,
  };
}
