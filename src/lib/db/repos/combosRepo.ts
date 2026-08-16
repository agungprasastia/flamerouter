import { v4 as uuidv4 } from "uuid";
import { getAdapter } from "../driver";
import { parseJson, stringifyJson } from "../helpers/jsonCol";

export interface ComboRecord {
  id: string;
  name: string;
  kind: string | null;
  models: unknown[];
  createdAt: string;
  updatedAt: string;
}

interface ComboDbRow {
  id: string;
  name: string;
  kind: string | null;
  models: string;
  createdAt: string;
  updatedAt: string;
}

function rowToCombo(row: ComboDbRow | undefined | null): ComboRecord | null {
  if (!row) return null;
  return {
    id: row.id,
    name: row.name,
    kind: row.kind,
    models: parseJson<unknown[]>(row.models, []) || [],
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
  };
}

export async function getCombos(): Promise<ComboRecord[]> {
  const db = await getAdapter();
  const rows = db.all<ComboDbRow>(`SELECT * FROM combos ORDER BY createdAt ASC`);
  return rows.map((r) => rowToCombo(r) as ComboRecord);
}

export async function getComboById(id: string): Promise<ComboRecord | null> {
  const db = await getAdapter();
  const row = db.get<ComboDbRow>(`SELECT * FROM combos WHERE id = ?`, [id]);
  return rowToCombo(row);
}

export async function getComboByName(name: string): Promise<ComboRecord | null> {
  const db = await getAdapter();
  const row = db.get<ComboDbRow>(`SELECT * FROM combos WHERE name = ?`, [name]);
  return rowToCombo(row);
}

export async function createCombo(data: { name: string; kind?: string | null; models?: unknown[] }): Promise<ComboRecord> {
  const db = await getAdapter();
  const now = new Date().toISOString();
  const combo: ComboRecord = {
    id: uuidv4(),
    name: data.name,
    kind: data.kind || null,
    models: data.models || [],
    createdAt: now,
    updatedAt: now,
  };
  db.run(
    `INSERT INTO combos(id, name, kind, models, createdAt, updatedAt) VALUES(?, ?, ?, ?, ?, ?)`,
    [
      combo.id,
      combo.name,
      combo.kind,
      stringifyJson(combo.models),
      combo.createdAt,
      combo.updatedAt,
    ],
  );
  return combo;
}

export async function updateCombo(id: string, data: Partial<ComboRecord>): Promise<ComboRecord | null> {
  const db = await getAdapter();
  let result: ComboRecord | null = null;
  db.transaction(() => {
    const row = db.get<ComboDbRow>(`SELECT * FROM combos WHERE id = ?`, [id]);
    if (!row) return;
    const current = rowToCombo(row);
    if (!current) return;
    const merged: ComboRecord = {
      ...current,
      ...data,
      updatedAt: new Date().toISOString(),
    };
    db.run(
      `UPDATE combos SET name = ?, kind = ?, models = ?, updatedAt = ? WHERE id = ?`,
      [
        merged.name,
        merged.kind,
        stringifyJson(merged.models || []),
        merged.updatedAt,
        id,
      ],
    );
    result = merged;
  });
  return result;
}

export async function deleteCombo(id: string): Promise<boolean> {
  const db = await getAdapter();
  const res = db.run(`DELETE FROM combos WHERE id = ?`, [id]);
  return (res?.changes ?? 0) > 0;
}
