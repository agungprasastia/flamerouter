import { v4 as uuidv4 } from "uuid";
import { getAdapter } from "../driver";
import { parseJson, stringifyJson } from "../helpers/jsonCol";

export interface ProviderNodeRecord {
  id: string;
  type: string | null;
  name: string | null;
  createdAt: string;
  updatedAt: string;
  prefix?: string;
  apiType?: string;
  baseUrl?: string;
  [key: string]: unknown;
}

interface NodeDbRow {
  id: string;
  type: string | null;
  name: string | null;
  data: string;
  createdAt: string;
  updatedAt: string;
}

function rowToNode(row: NodeDbRow | undefined | null): ProviderNodeRecord | null {
  if (!row) return null;
  const extra = parseJson<Record<string, unknown>>(row.data, {}) || {};
  return {
    ...extra,
    id: row.id,
    type: row.type,
    name: row.name,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
  };
}

function nodeToRow(n: Partial<ProviderNodeRecord> & { id: string }) {
  const { id, type, name, createdAt = new Date().toISOString(), updatedAt = new Date().toISOString(), ...rest } = n;
  return {
    id,
    type: type ?? null,
    name: name ?? null,
    data: stringifyJson(rest),
    createdAt,
    updatedAt,
  };
}

import type { DatabaseAdapter } from "../driver";

function upsert(db: DatabaseAdapter, n: Partial<ProviderNodeRecord> & { id: string }) {
  const r = nodeToRow(n);
  db.run(
    `INSERT INTO providerNodes(id, type, name, data, createdAt, updatedAt)
     VALUES(?, ?, ?, ?, ?, ?)
     ON CONFLICT(id) DO UPDATE SET
       type=excluded.type, name=excluded.name, data=excluded.data, updatedAt=excluded.updatedAt`,
    [r.id, r.type, r.name, r.data, r.createdAt, r.updatedAt],
  );
}

export interface NodeFilter {
  type?: string;
}

export async function getProviderNodes(filter: NodeFilter = {}): Promise<ProviderNodeRecord[]> {
  const db = await getAdapter();
  const where: string[] = [];
  const params: unknown[] = [];
  if (filter.type) {
    where.push("type = ?");
    params.push(filter.type);
  }
  const sql = `SELECT * FROM providerNodes${where.length ? ` WHERE ${where.join(" AND ")}` : ""}`;
  return db.all<NodeDbRow>(sql, params).map((r) => rowToNode(r) as ProviderNodeRecord);
}

export async function getProviderNodeById(id: string): Promise<ProviderNodeRecord | null> {
  const db = await getAdapter();
  return rowToNode(db.get<NodeDbRow>(`SELECT * FROM providerNodes WHERE id = ?`, [id]));
}

export async function createProviderNode(data: Partial<ProviderNodeRecord>): Promise<ProviderNodeRecord> {
  const db = await getAdapter();
  const now = new Date().toISOString();
  const node: ProviderNodeRecord = {
    id: data.id || uuidv4(),
    type: data.type ?? null,
    name: data.name ?? null,
    prefix: data.prefix,
    apiType: data.apiType,
    baseUrl: data.baseUrl,
    createdAt: now,
    updatedAt: now,
  };
  upsert(db, node);
  return node;
}

export async function updateProviderNode(id: string, data: Partial<ProviderNodeRecord>): Promise<ProviderNodeRecord | null> {
  const db = await getAdapter();
  let result: ProviderNodeRecord | null = null;
  db.transaction(() => {
    const row = db.get<NodeDbRow>(`SELECT * FROM providerNodes WHERE id = ?`, [id]);
    if (!row) return;
    const current = rowToNode(row);
    if (!current) return;
    const merged: ProviderNodeRecord = {
      ...current,
      ...data,
      updatedAt: new Date().toISOString(),
    };
    upsert(db, merged);
    result = merged;
  });
  return result;
}

export async function deleteProviderNode(id: string): Promise<ProviderNodeRecord | null> {
  const db = await getAdapter();
  let removed: ProviderNodeRecord | null = null;
  db.transaction(() => {
    const row = db.get<NodeDbRow>(`SELECT * FROM providerNodes WHERE id = ?`, [id]);
    if (!row) return;
    removed = rowToNode(row);
    db.run(`DELETE FROM providerNodes WHERE id = ?`, [id]);
  });
  return removed;
}
