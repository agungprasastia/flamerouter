import { getAdapter } from "../driver";

export interface SyncAdapterLike {
  get<T = unknown>(sql: string, params?: unknown[]): T | undefined;
  run(sql: string, params?: unknown[]): void;
}

export async function getMeta(key: string, fallback: string | null = null): Promise<string | null> {
  const db = await getAdapter();
  const row = db.get<{ value: string }>(`SELECT value FROM _meta WHERE key = ?`, [key]);
  return row ? row.value : fallback;
}

export async function setMeta(key: string, value: unknown): Promise<void> {
  const db = await getAdapter();
  db.run(
    `INSERT INTO _meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
    [key, String(value)],
  );
}

// Sync versions for use during migration (adapter passed directly)
export function getMetaSync(adapter: SyncAdapterLike, key: string, fallback: string | null = null): string | null {
  const row = adapter.get<{ value: string }>(`SELECT value FROM _meta WHERE key = ?`, [key]);
  return row ? row.value : fallback;
}

export function setMetaSync(adapter: SyncAdapterLike, key: string, value: unknown): void {
  adapter.run(
    `INSERT INTO _meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
    [key, String(value)],
  );
}
