import fs from "node:fs";
import initSqlJs, { type SqlJsStatic, type SqlJsDatabase, type BindParams } from "sql.js";
import { PRAGMA_SQL } from "../schema";
import type { DatabaseAdapter } from "../driver";

let SQL: SqlJsStatic | null = null;

async function loadSql(): Promise<SqlJsStatic> {
  if (SQL) return SQL;
  SQL = await initSqlJs();
  return SQL;
}

export async function createSqlJsAdapter(filePath: string): Promise<DatabaseAdapter & { raw: unknown; persist: () => void }> {
  const SQLLib = await loadSql();
  const buf = fs.existsSync(filePath) ? fs.readFileSync(filePath) : null;
  const db: SqlJsDatabase = new SQLLib.Database(buf);
  db.exec(PRAGMA_SQL);
  // Schema is created/synced by migrate.js after adapter init

  let dirty = false;
  let saveTimer: NodeJS.Timeout | null = null;
  const SAVE_DEBOUNCE_MS = 100;

  function persist() {
    const data = db.export();
    fs.writeFileSync(filePath, Buffer.from(data));
    dirty = false;
  }

  function scheduleSave() {
    dirty = true;
    if (saveTimer) clearTimeout(saveTimer);
    saveTimer = setTimeout(() => {
      saveTimer = null;
      if (dirty) {
        try {
          persist();
        } catch (e) {
          console.error("[sqljs] save failed:", e);
        }
      }
    }, SAVE_DEBOUNCE_MS);
  }

  function paramsObj(params?: unknown[]): BindParams | undefined {
    if (!params || (Array.isArray(params) && params.length === 0))
      return undefined;
    return params as unknown as BindParams;
  }

  function run(sql: string, params: unknown[] = []) {
    const stmt = db.prepare(sql);
    try {
      stmt.bind(paramsObj(params));
      stmt.step();
      const changes = db.getRowsModified();
      const lastInsertRowid =
        ((db.exec("SELECT last_insert_rowid() as id") as { values?: unknown[][] }[])[0]?.values?.[0]?.[0] as number | bigint | undefined) ??
        null;
      scheduleSave();
      return { changes, lastInsertRowid: lastInsertRowid ?? undefined };
    } finally {
      stmt.free();
    }
  }

  function get<T = unknown>(sql: string, params: unknown[] = []): T | undefined {
    const stmt = db.prepare(sql);
    try {
      stmt.bind(paramsObj(params));
      if (stmt.step()) return stmt.getAsObject() as unknown as T;
      return undefined;
    } finally {
      stmt.free();
    }
  }

  function all<T = unknown>(sql: string, params: unknown[] = []): T[] {
    const stmt = db.prepare(sql);
    try {
      stmt.bind(paramsObj(params));
      const rows: T[] = [];
      while (stmt.step()) rows.push(stmt.getAsObject() as unknown as T);
      return rows;
    } finally {
      stmt.free();
    }
  }

  function exec(sql: string) {
    db.exec(sql);
    scheduleSave();
  }

  function transaction<T>(fn: () => T): T {
    const sp = `sp_${Math.random().toString(36).slice(2)}`;
    db.exec(`SAVEPOINT ${sp}`);
    try {
      const result = fn();
      db.exec(`RELEASE ${sp}`);
      scheduleSave();
      return result;
    } catch (e) {
      try {
        db.exec(`ROLLBACK TO ${sp}`);
        db.exec(`RELEASE ${sp}`);
      } catch {}
      throw e;
    }
  }

  function close() {
    if (saveTimer) clearTimeout(saveTimer);
    if (dirty) persist();
    db.close();
  }

  // Flush on shutdown
  const flush = () => {
    if (dirty)
      try {
        persist();
      } catch {}
  };
  process.on("beforeExit", flush);
  process.on("SIGINT", flush);
  process.on("SIGTERM", flush);

  return { driver: "sql.js", run, get, all, exec, transaction, close, persist, raw: db };
}
