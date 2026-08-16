import { v4 as uuidv4 } from "uuid";
import { getAdapter } from "../driver";
import { parseJson, stringifyJson } from "../helpers/jsonCol";

export interface ProxyPoolRecord {
  id: string;
  name?: string;
  proxyUrl?: string;
  noProxy?: string;
  type?: string;
  isActive: boolean;
  strictProxy?: boolean;
  testStatus?: string | null;
  lastTestedAt?: string | null;
  lastError?: string | null;
  createdAt: string;
  updatedAt: string;
  [key: string]: unknown;
}

interface ProxyPoolDbRow {
  id: string;
  isActive: number | boolean;
  testStatus: string | null;
  data: string;
  createdAt: string;
  updatedAt: string;
}

function rowToPool(row: ProxyPoolDbRow | undefined | null): ProxyPoolRecord | null {
  if (!row) return null;
  const extra = parseJson<Record<string, unknown>>(row.data, {}) || {};
  return {
    ...extra,
    id: row.id,
    isActive: row.isActive === 1 || row.isActive === true,
    testStatus: row.testStatus,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
  };
}

function poolToRow(p: Partial<ProxyPoolRecord> & { id: string }) {
  const { id, isActive, testStatus, createdAt = new Date().toISOString(), updatedAt = new Date().toISOString(), ...rest } = p;
  return {
    id,
    isActive: isActive === false ? 0 : 1,
    testStatus: testStatus ?? null,
    data: stringifyJson(rest),
    createdAt,
    updatedAt,
  };
}

import type { DatabaseAdapter } from "../driver";

function upsert(db: DatabaseAdapter, p: Partial<ProxyPoolRecord> & { id: string }) {
  const r = poolToRow(p);
  db.run(
    `INSERT INTO proxyPools(id, isActive, testStatus, data, createdAt, updatedAt)
     VALUES(?, ?, ?, ?, ?, ?)
     ON CONFLICT(id) DO UPDATE SET
       isActive=excluded.isActive, testStatus=excluded.testStatus,
       data=excluded.data, updatedAt=excluded.updatedAt`,
    [r.id, r.isActive, r.testStatus, r.data, r.createdAt, r.updatedAt],
  );
}

export interface ProxyPoolFilter {
  isActive?: boolean;
  testStatus?: string;
}

export async function getProxyPools(filter: ProxyPoolFilter = {}): Promise<ProxyPoolRecord[]> {
  const db = await getAdapter();
  const where: string[] = [];
  const params: unknown[] = [];
  if (filter.isActive !== undefined) {
    where.push("isActive = ?");
    params.push(filter.isActive ? 1 : 0);
  }
  if (filter.testStatus) {
    where.push("testStatus = ?");
    params.push(filter.testStatus);
  }
  const sql = `SELECT * FROM proxyPools${where.length ? ` WHERE ${where.join(" AND ")}` : ""}`;
  const list = db.all<ProxyPoolDbRow>(sql, params).map((r) => rowToPool(r) as ProxyPoolRecord);
  list.sort((a, b) => new Date(b.updatedAt || 0).getTime() - new Date(a.updatedAt || 0).getTime());
  return list;
}

export async function getProxyPoolById(id: string): Promise<ProxyPoolRecord | null> {
  const db = await getAdapter();
  return rowToPool(db.get<ProxyPoolDbRow>(`SELECT * FROM proxyPools WHERE id = ?`, [id]));
}

export async function createProxyPool(data: Partial<ProxyPoolRecord>): Promise<ProxyPoolRecord> {
  const db = await getAdapter();
  const now = new Date().toISOString();
  const pool: ProxyPoolRecord = {
    id: data.id || uuidv4(),
    name: data.name,
    proxyUrl: data.proxyUrl,
    noProxy: data.noProxy || "",
    type: data.type || "http",
    isActive: data.isActive !== undefined ? data.isActive : true,
    strictProxy: data.strictProxy === true,
    testStatus: data.testStatus || "unknown",
    lastTestedAt: data.lastTestedAt || null,
    lastError: data.lastError || null,
    createdAt: now,
    updatedAt: now,
  };
  upsert(db, pool);
  return pool;
}

export async function updateProxyPool(id: string, data: Partial<ProxyPoolRecord>): Promise<ProxyPoolRecord | null> {
  const db = await getAdapter();
  let result: ProxyPoolRecord | null = null;
  db.transaction(() => {
    const row = db.get<ProxyPoolDbRow>(`SELECT * FROM proxyPools WHERE id = ?`, [id]);
    if (!row) return;
    const current = rowToPool(row);
    if (!current) return;
    const merged: ProxyPoolRecord = {
      ...current,
      ...data,
      updatedAt: new Date().toISOString(),
    };
    upsert(db, merged);
    result = merged;
  });
  return result;
}

export async function deleteProxyPool(id: string): Promise<ProxyPoolRecord | null> {
  const db = await getAdapter();
  let removed: ProxyPoolRecord | null = null;
  db.transaction(() => {
    const row = db.get<ProxyPoolDbRow>(`SELECT * FROM proxyPools WHERE id = ?`, [id]);
    if (!row) return;
    removed = rowToPool(row);
    db.run(`DELETE FROM proxyPools WHERE id = ?`, [id]);
  });
  return removed;
}
