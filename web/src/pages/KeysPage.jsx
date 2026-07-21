import { useCallback, useEffect, useState } from "react";
import { createKey, deleteKey, listKeys, updateKey } from "../api/keys";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Button } from "../components/Button";
import { Badge } from "../components/Badge";

export default function KeysPage() {
  const [rows, setRows] = useState([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState("");
  const [copied, setCopied] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const data = await listKeys();
      setRows(Array.isArray(data) ? data : data?.keys ?? []);
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function onCreate(e) {
    e.preventDefault();
    const n = name.trim();
    if (!n) return;
    setCreating(true);
    setError("");
    try {
      const res = await createKey(n);
      setNewKey(res?.key || "");
      setName("");
      setCopied(false);
      await load();
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      setCreating(false);
    }
  }

  async function onDelete(id) {
    if (!window.confirm("Delete this API key?")) return;
    setError("");
    try {
      await deleteKey(id);
      await load();
    } catch (err) {
      setError(err?.message || String(err));
    }
  }

  async function onToggle(id, isActive) {
    setError("");
    try {
      await updateKey(id, !isActive);
      await load();
    } catch (err) {
      setError(err?.message || String(err));
    }
  }

  async function copyKey() {
    if (!newKey) return;
    try {
      await navigator.clipboard.writeText(newKey);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="space-y-6">
      <h1 className="text-lg font-semibold text-[var(--text)]">API Keys</h1>

      {newKey ? (
        <div className="rounded border border-[var(--warning)]/30 bg-[var(--warning)]/10 px-4 py-3">
          <p className="mb-1 text-xs font-medium uppercase tracking-wide text-[var(--warning)]">
            Copy now — shown once
          </p>
          <div className="flex flex-wrap items-center gap-2">
            <code className="break-all font-mono text-sm text-[var(--text)]">{newKey}</code>
            <Button type="button" variant="secondary" size="sm" onClick={copyKey}>
              {copied ? "Copied" : "Copy"}
            </Button>
            <Button type="button" variant="secondary" size="sm" onClick={() => setNewKey("")}>
              Dismiss
            </Button>
          </div>
        </div>
      ) : null}

      <form onSubmit={onCreate} className="flex flex-wrap items-end gap-2">
        <div className="min-w-[12rem] flex-1">
          <Input
            id="key-name"
            label="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="my-client"
            required
          />
        </div>
        <Button type="submit" variant="primary" disabled={creating || !name.trim()}>
          {creating ? "Creating…" : "Create key"}
        </Button>
      </form>

      {loading ? <p className="text-sm text-[var(--muted)]">Loading…</p> : null}
      {error ? <p className="text-sm text-[var(--danger)]">{error}</p> : null}

      {!loading && rows.length === 0 && !error ? (
        <p className="text-sm text-[var(--muted)]">No API keys yet.</p>
      ) : null}

      {rows.length > 0 ? (
        <div className="rounded border border-[var(--border)] overflow-x-auto">
          <table className="w-full min-w-[32rem] text-left text-sm">
            <thead className="border-b border-[var(--border)] bg-[var(--surface)] text-xs uppercase text-[var(--muted)]">
              <tr>
                <th className="px-3 py-2 font-medium">Name</th>
                <th className="px-3 py-2 font-medium">Key ID</th>
                <th className="px-3 py-2 font-medium">Active</th>
                <th className="px-3 py-2 font-medium">Created</th>
                <th className="px-3 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id} className="border-b border-[var(--border)] last:border-0">
                  <td className="px-3 py-2 text-[var(--text)]">{row.name || "—"}</td>
                  <td className="px-3 py-2 font-mono text-xs text-[var(--muted)]">
                    {row.keyId || row.id}
                  </td>
                  <td className="px-3 py-2">
                    <button
                      type="button"
                      onClick={() => onToggle(row.id, !!row.isActive)}
                      className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium border-0 cursor-pointer transition ${
                        row.isActive
                          ? "bg-green-500/10 text-[var(--success)]"
                          : "bg-[var(--surface-2)] text-[var(--muted)]"
                      }`}
                    >
                      {row.isActive ? "Active" : "Inactive"}
                    </button>
                  </td>
                  <td className="px-3 py-2 text-xs text-[var(--muted)]">{row.createdAt || "—"}</td>
                  <td className="px-3 py-2">
                    <button
                      type="button"
                      onClick={() => onDelete(row.id)}
                      className="text-xs text-[var(--danger)] hover:underline"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  );
}
