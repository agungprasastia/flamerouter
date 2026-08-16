import { v4 as uuidv4 } from "uuid";
import { getAdapter } from "../driver";

export interface ApiKeyRecord {
  id: string;
  key: string;
  name: string | null;
  machineId: string | null;
  isActive: boolean;
  createdAt: string;
}

interface ApiKeyDbRow {
  id: string;
  key: string;
  name: string | null;
  machineId: string | null;
  isActive: number | boolean;
  createdAt: string;
}

function rowToKey(row: ApiKeyDbRow | undefined | null): ApiKeyRecord | null {
  if (!row) return null;
  return {
    id: row.id,
    key: row.key,
    name: row.name,
    machineId: row.machineId,
    isActive: row.isActive === 1 || row.isActive === true,
    createdAt: row.createdAt,
  };
}

export async function getApiKeys(): Promise<ApiKeyRecord[]> {
  const db = await getAdapter();
  const rows = db.all<ApiKeyDbRow>(`SELECT * FROM apiKeys ORDER BY createdAt ASC`);
  return rows.map((r) => rowToKey(r) as ApiKeyRecord);
}

export async function getApiKeyById(id: string): Promise<ApiKeyRecord | null> {
  const db = await getAdapter();
  const row = db.get<ApiKeyDbRow>(`SELECT * FROM apiKeys WHERE id = ?`, [id]);
  return rowToKey(row);
}

export async function createApiKey(name: string, machineId: string): Promise<ApiKeyRecord> {
  if (!machineId) throw new Error("machineId is required");
  const db = await getAdapter();
  const { generateApiKeyWithMachine } = await import("@/shared/utils/apiKey");
  const result = generateApiKeyWithMachine(machineId);
  const apiKey: ApiKeyRecord = {
    id: uuidv4(),
    name,
    key: result.key,
    machineId,
    isActive: true,
    createdAt: new Date().toISOString(),
  };
  db.run(
    `INSERT INTO apiKeys(id, key, name, machineId, isActive, createdAt) VALUES(?, ?, ?, ?, ?, ?)`,
    [apiKey.id, apiKey.key, apiKey.name, apiKey.machineId, 1, apiKey.createdAt],
  );
  return apiKey;
}

export async function updateApiKey(id: string, data: Partial<ApiKeyRecord>): Promise<ApiKeyRecord | null> {
  const db = await getAdapter();
  let result: ApiKeyRecord | null = null;
  db.transaction(() => {
    const row = db.get<ApiKeyDbRow>(`SELECT * FROM apiKeys WHERE id = ?`, [id]);
    if (!row) return;
    const current = rowToKey(row);
    if (!current) return;
    const merged: ApiKeyRecord = { ...current, ...data };
    db.run(
      `UPDATE apiKeys SET key = ?, name = ?, machineId = ?, isActive = ? WHERE id = ?`,
      [merged.key, merged.name, merged.machineId, merged.isActive ? 1 : 0, id],
    );
    result = merged;
  });
  return result;
}

export async function deleteApiKey(id: string): Promise<boolean> {
  const db = await getAdapter();
  const res = db.run(`DELETE FROM apiKeys WHERE id = ?`, [id]);
  return (res?.changes ?? 0) > 0;
}

export async function validateApiKey(key: string): Promise<boolean> {
  const db = await getAdapter();
  const row = db.get<{ isActive: number | boolean }>(`SELECT isActive FROM apiKeys WHERE key = ?`, [key]);
  if (!row) return false;
  return row.isActive === 1 || row.isActive === true;
}
