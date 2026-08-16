import { v4 as uuidv4 } from "uuid";
import { getAdapter } from "../driver";
import { parseJson, stringifyJson } from "../helpers/jsonCol";

const OPTIONAL_FIELDS = [
  "displayName",
  "email",
  "globalPriority",
  "defaultModel",
  "accessToken",
  "refreshToken",
  "expiresAt",
  "tokenType",
  "scope",
  "projectId",
  "apiKey",
  "testStatus",
  "lastTested",
  "lastError",
  "lastErrorAt",
  "rateLimitedUntil",
  "expiresIn",
  "errorCode",
  "consecutiveUseCount",
  "idToken",
  "lastRefreshAt",
];

export interface ProviderConnectionRecord {
  id: string;
  provider: string;
  authType: string;
  name: string | null;
  email: string | null;
  priority: number | null;
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
  providerSpecificData?: Record<string, unknown>;
  [key: string]: unknown;
}

interface ConnectionDbRow {
  id: string;
  provider: string;
  authType: string;
  name: string | null;
  email: string | null;
  priority: number | null;
  isActive: number | boolean;
  data: string;
  createdAt: string;
  updatedAt: string;
}

function rowToConn(row: ConnectionDbRow | undefined | null): ProviderConnectionRecord | null {
  if (!row) return null;
  const extra = parseJson<Record<string, unknown>>(row.data, {}) || {};
  return {
    ...extra,
    id: row.id,
    provider: row.provider,
    authType: row.authType,
    name: row.name,
    email: row.email,
    priority: row.priority,
    isActive: row.isActive === 1 || row.isActive === true,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
  };
}

function connToRow(c: Partial<ProviderConnectionRecord> & { id: string; provider: string; authType: string }) {
  const {
    id,
    provider,
    authType,
    name,
    email,
    priority,
    isActive,
    createdAt = new Date().toISOString(),
    updatedAt = new Date().toISOString(),
    ...rest
  } = c;
  return {
    id,
    provider,
    authType,
    name: name ?? null,
    email: email ?? null,
    priority: priority ?? null,
    isActive: isActive === false ? 0 : 1,
    data: stringifyJson(rest),
    createdAt,
    updatedAt,
  };
}

import type { DatabaseAdapter } from "../driver";

function upsert(db: DatabaseAdapter, c: Partial<ProviderConnectionRecord> & { id: string; provider: string; authType: string }) {
  const r = connToRow(c);
  db.run(
    `INSERT INTO providerConnections(id, provider, authType, name, email, priority, isActive, data, createdAt, updatedAt)
     VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
     ON CONFLICT(id) DO UPDATE SET
       provider=excluded.provider, authType=excluded.authType, name=excluded.name,
       email=excluded.email, priority=excluded.priority, isActive=excluded.isActive,
       data=excluded.data, updatedAt=excluded.updatedAt`,
    [
      r.id,
      r.provider,
      r.authType,
      r.name,
      r.email,
      r.priority,
      r.isActive,
      r.data,
      r.createdAt,
      r.updatedAt,
    ],
  );
}

function deriveConnectionName(data: { provider?: string; email?: string | null; providerSpecificData?: Record<string, unknown> }, fallbackName: string): string {
  if (data.provider === "github") {
    return (
      (data.providerSpecificData?.githubLogin as string) ||
      (data.providerSpecificData?.githubEmail as string) ||
      (data.email as string) ||
      (data.providerSpecificData?.githubName as string) ||
      fallbackName
    );
  }
  return fallbackName;
}

export interface ConnectionFilter {
  provider?: string;
  isActive?: boolean;
}

export async function getProviderConnections(filter: ConnectionFilter = {}): Promise<ProviderConnectionRecord[]> {
  const db = await getAdapter();
  const where: string[] = [];
  const params: unknown[] = [];
  if (filter.provider) {
    where.push("provider = ?");
    params.push(filter.provider);
  }
  if (filter.isActive !== undefined) {
    where.push("isActive = ?");
    params.push(filter.isActive ? 1 : 0);
  }
  const sql = `SELECT * FROM providerConnections${where.length ? ` WHERE ${where.join(" AND ")}` : ""}`;
  const rows = db.all<ConnectionDbRow>(sql, params);
  const list = rows.map((r) => rowToConn(r) as ProviderConnectionRecord);
  list.sort((a, b) => (a.priority || 999) - (b.priority || 999));
  return list;
}

export async function getProviderConnectionById(id: string): Promise<ProviderConnectionRecord | null> {
  const db = await getAdapter();
  const row = db.get<ConnectionDbRow>(`SELECT * FROM providerConnections WHERE id = ?`, [id]);
  return rowToConn(row);
}

// Internal sync reorder — must be called INSIDE a transaction
function reorderInTx(db: DatabaseAdapter, providerId: string) {
  const list = db
    .all<ConnectionDbRow>(`SELECT * FROM providerConnections WHERE provider = ?`, [providerId])
    .map((r) => rowToConn(r) as ProviderConnectionRecord);
  list.sort((a, b) => {
    const pDiff = (a.priority || 0) - (b.priority || 0);
    if (pDiff !== 0) return pDiff;
    return new Date(b.updatedAt || 0).getTime() - new Date(a.updatedAt || 0).getTime();
  });
  list.forEach((c, i) => {
    db.run(`UPDATE providerConnections SET priority = ? WHERE id = ?`, [
      i + 1,
      c.id,
    ]);
  });
}

export async function createProviderConnection(data: Partial<ProviderConnectionRecord> & { provider: string }): Promise<ProviderConnectionRecord> {
  const db = await getAdapter();
  const now = new Date().toISOString();
  let result: ProviderConnectionRecord = {} as ProviderConnectionRecord;

  db.transaction(() => {
    const all = db
      .all<ConnectionDbRow>(`SELECT * FROM providerConnections WHERE provider = ?`, [
        data.provider,
      ])
      .map((r) => rowToConn(r) as ProviderConnectionRecord);

    let existing: ProviderConnectionRecord | null = null;
    if (data.authType === "oauth" && data.email) {
      const incomingUsername = data.providerSpecificData?.username;
      const incomingWs = data.providerSpecificData?.chatgptAccountId;
      existing = all.find((c) => {
        if (c.authType !== "oauth" || c.email !== data.email) return false;

        if (data.provider === "codex") {
          const existingWs = c.providerSpecificData?.chatgptAccountId;
          return !!incomingWs && !!existingWs && incomingWs === existingWs;
        }

        const existingWs = c.providerSpecificData?.chatgptAccountId;
        if (incomingWs && existingWs) return incomingWs === existingWs;
        if (incomingWs && !existingWs) return false;
        if (!incomingWs && existingWs) return false;

        const existingUsername = c.providerSpecificData?.username;
        if (incomingUsername && existingUsername) {
          return incomingUsername === existingUsername;
        }
        if (incomingUsername || existingUsername) return false;
        return true;
      }) ?? null;
    } else if (data.authType === "apikey" && data.name) {
      existing = all.find(
        (c) => c.authType === "apikey" && c.name === data.name,
      ) ?? null;
    }

    if (existing) {
      const merged: ProviderConnectionRecord = { ...existing, ...data, updatedAt: now };
      upsert(db, merged);
      result = merged;
      return;
    }

    let connectionName = data.name || null;
    if (
      !connectionName &&
      (data.authType === "oauth" || data.authType === "access_token")
    ) {
      connectionName = deriveConnectionName(
        data,
        data.email || `Account ${all.length + 1}`,
      );
    }
    let connectionPriority = data.priority;
    if (!connectionPriority) {
      connectionPriority =
        all.reduce((m, c) => Math.max(m, c.priority || 0), 0) + 1;
    }

    const conn: ProviderConnectionRecord = {
      id: uuidv4(),
      provider: data.provider,
      authType: data.authType || "oauth",
      name: connectionName,
      email: data.email ?? null,
      priority: connectionPriority,
      isActive: data.isActive !== undefined ? data.isActive : true,
      createdAt: now,
      updatedAt: now,
    };
    for (const f of OPTIONAL_FIELDS) {
      if (data[f] !== undefined && data[f] !== null) conn[f] = data[f];
    }
    if (
      data.providerSpecificData &&
      Object.keys(data.providerSpecificData).length > 0
    ) {
      conn.providerSpecificData = data.providerSpecificData;
    }

    upsert(db, conn);
    reorderInTx(db, data.provider);
    result = conn;
  });

  return result;
}

// Critical: OAuth refresh token race — atomic merge inside transaction
export async function updateProviderConnection(id: string, data: Partial<ProviderConnectionRecord>): Promise<ProviderConnectionRecord | null> {
  const db = await getAdapter();
  let result: ProviderConnectionRecord | null = null;
  db.transaction(() => {
    const row = db.get<ConnectionDbRow>(`SELECT * FROM providerConnections WHERE id = ?`, [id]);
    if (!row) {
      result = null;
      return;
    }
    const existing = rowToConn(row);
    if (!existing) return;
    const merged: ProviderConnectionRecord = {
      ...existing,
      ...data,
      updatedAt: new Date().toISOString(),
    };
    upsert(db, merged);
    if (data.priority !== undefined) reorderInTx(db, existing.provider);
    result = merged;
  });
  return result;
}

export async function deleteProviderConnection(id: string): Promise<boolean> {
  const db = await getAdapter();
  let ok = false;
  db.transaction(() => {
    const row = db.get<{ provider: string }>(
      `SELECT provider FROM providerConnections WHERE id = ?`,
      [id],
    );
    if (!row) return;
    db.run(`DELETE FROM providerConnections WHERE id = ?`, [id]);
    reorderInTx(db, row.provider);
    ok = true;
  });
  return ok;
}

export async function deleteProviderConnectionsByProvider(providerId: string): Promise<number> {
  const db = await getAdapter();
  const before = db.get<{ n: number }>(
    `SELECT COUNT(*) AS n FROM providerConnections WHERE provider = ?`,
    [providerId],
  );
  db.run(`DELETE FROM providerConnections WHERE provider = ?`, [providerId]);
  return before?.n || 0;
}

export async function reorderProviderConnections(providerId: string): Promise<void> {
  const db = await getAdapter();
  db.transaction(() => reorderInTx(db, providerId));
}

export async function cleanupProviderConnections(): Promise<number> {
  const db = await getAdapter();
  const fieldsToCheck = [
    "displayName",
    "email",
    "globalPriority",
    "defaultModel",
    "accessToken",
    "refreshToken",
    "expiresAt",
    "tokenType",
    "scope",
    "projectId",
    "apiKey",
    "testStatus",
    "lastTested",
    "lastError",
    "lastErrorAt",
    "rateLimitedUntil",
    "expiresIn",
    "consecutiveUseCount",
  ];
  let cleaned = 0;
  db.transaction(() => {
    const rows = db.all<ConnectionDbRow>(`SELECT * FROM providerConnections`);
    for (const row of rows) {
      const conn = rowToConn(row);
      if (!conn) continue;
      let dirty = false;
      for (const f of fieldsToCheck) {
        if (conn[f] === null || conn[f] === undefined) {
          if (f in conn) {
            delete conn[f];
            cleaned++;
            dirty = true;
          }
        }
      }
      if (
        conn.providerSpecificData &&
        Object.keys(conn.providerSpecificData).length === 0
      ) {
        delete conn.providerSpecificData;
        cleaned++;
        dirty = true;
      }
      if (dirty) upsert(db, conn);
    }
  });
  return cleaned;
}
