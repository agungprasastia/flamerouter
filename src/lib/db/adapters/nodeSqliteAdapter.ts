// Built-in node:sqlite adapter — available in Node >= 22.5.0.
// No native build, no npm install. API mirrors betterSqliteAdapter.
import { PRAGMA_SQL } from "../schema";
import type { DatabaseAdapter } from "../driver";

declare global {
  // eslint-disable-next-line no-var
  var _nodeSqliteShutdownRegistered: boolean | undefined;
}

const CHECKPOINT_INTERVAL_MS = 60 * 1000;

interface NodeSqliteStatement {
  run(...params: unknown[]): { changes?: number | bigint; lastInsertRowid?: number | bigint };
  get(...params: unknown[]): unknown;
  all(...params: unknown[]): unknown[];
}

interface NodeSqliteDatabase {
  exec(sql: string): void;
  prepare(sql: string): NodeSqliteStatement;
  close(): void;
}

export async function createNodeSqliteAdapter(filePath: string): Promise<DatabaseAdapter & { raw: unknown; checkpoint: () => void }> {
  // Suppress "ExperimentalWarning: SQLite is an experimental feature" from node:sqlite.
  // Stable enough for production use as of Node 22.x (RC quality).
  const origEmit = process.emit;
  // eslint-disable-next-line @typescript-eslint/ban-ts-comment
  // @ts-ignore
  process.emit = function (name: string, data: unknown, ...rest: unknown[]) {
    const warning = data as { name?: string; message?: string } | undefined;
    if (
      name === "warning" &&
      warning?.name === "ExperimentalWarning" &&
      /SQLite/i.test(warning?.message || "")
    ) {
      return false;
    }
    return (origEmit as (...args: unknown[]) => boolean).apply(process, [name, data as Error, ...rest]);
  };

  // Dynamic import — fails on Node < 22.5 → driver.js falls back to sql.js
  // eslint-disable-next-line @typescript-eslint/ban-ts-comment
  // @ts-ignore
  const sqlite = await import("node:sqlite");
  const Database = sqlite.DatabaseSync as new (path: string) => NodeSqliteDatabase;
  const db = new Database(filePath);

  db.exec(PRAGMA_SQL);

  const stmtCache = new Map<string, NodeSqliteStatement>();
  function prepare(sql: string): NodeSqliteStatement {
    let stmt = stmtCache.get(sql);
    if (!stmt) {
      stmt = db.prepare(sql);
      stmtCache.set(sql, stmt);
    }
    return stmt;
  }

  // Periodic WAL checkpoint to keep -wal/-shm small
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
  if (!global._nodeSqliteShutdownRegistered) {
    global._nodeSqliteShutdownRegistered = true;
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
    driver: "node:sqlite",
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
      // node:sqlite has no transaction wrapper. Use SAVEPOINT for nested support.
      const sp = `sp_${Math.random().toString(36).slice(2)}`;
      db.exec(`SAVEPOINT ${sp}`);
      try {
        const r = fn();
        db.exec(`RELEASE ${sp}`);
        return r;
      } catch (e) {
        try {
          db.exec(`ROLLBACK TO ${sp}`);
          db.exec(`RELEASE ${sp}`);
        } catch {}
        throw e;
      }
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
