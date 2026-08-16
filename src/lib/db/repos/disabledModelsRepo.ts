import { getAdapter } from "../driver";
import { parseJson, stringifyJson } from "../helpers/jsonCol";

const SCOPE = "disabledModels";

export async function getDisabledModels(): Promise<Record<string, string[]>> {
  const db = await getAdapter();
  const rows = db.all<{ key: string; value: string }>(`SELECT key, value FROM kv WHERE scope = ?`, [SCOPE]);
  const out: Record<string, string[]> = {};
  for (const r of rows) out[r.key] = parseJson<string[]>(r.value, []) || [];
  return out;
}

export async function getDisabledByProvider(providerAlias: string): Promise<string[]> {
  const db = await getAdapter();
  const row = db.get<{ value: string }>(`SELECT value FROM kv WHERE scope = ? AND key = ?`, [
    SCOPE,
    providerAlias,
  ]);
  return row ? (parseJson<string[]>(row.value, []) || []) : [];
}

// Atomic read-merge-write inside a transaction (no JS yield mid-transaction).
export async function disableModels(providerAlias: string, ids: string[]): Promise<void> {
  if (!providerAlias || !Array.isArray(ids)) return;
  const db = await getAdapter();
  db.transaction(() => {
    const row = db.get<{ value: string }>(`SELECT value FROM kv WHERE scope = ? AND key = ?`, [
      SCOPE,
      providerAlias,
    ]);
    const current = row ? (parseJson<string[]>(row.value, []) || []) : [];
    const merged = [...new Set([...current, ...ids])];
    db.run(
      `INSERT INTO kv(scope, key, value) VALUES(?, ?, ?) ON CONFLICT(scope, key) DO UPDATE SET value = excluded.value`,
      [SCOPE, providerAlias, stringifyJson(merged)],
    );
  });
}

export async function enableModels(providerAlias: string, ids?: string[]): Promise<void> {
  if (!providerAlias) return;
  const db = await getAdapter();
  db.transaction(() => {
    if (!Array.isArray(ids) || ids.length === 0) {
      db.run(`DELETE FROM kv WHERE scope = ? AND key = ?`, [
        SCOPE,
        providerAlias,
      ]);
      return;
    }
    const row = db.get<{ value: string }>(`SELECT value FROM kv WHERE scope = ? AND key = ?`, [
      SCOPE,
      providerAlias,
    ]);
    const current = row ? (parseJson<string[]>(row.value, []) || []) : [];
    const removeSet = new Set(ids);
    const next = current.filter((id) => !removeSet.has(id));
    if (next.length === 0) {
      db.run(`DELETE FROM kv WHERE scope = ? AND key = ?`, [
        SCOPE,
        providerAlias,
      ]);
    } else {
      db.run(
        `INSERT INTO kv(scope, key, value) VALUES(?, ?, ?) ON CONFLICT(scope, key) DO UPDATE SET value = excluded.value`,
        [SCOPE, providerAlias, stringifyJson(next)],
      );
    }
  });
}
