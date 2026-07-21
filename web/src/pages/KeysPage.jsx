import { useCallback, useEffect, useState } from "react";
import { createKey, deleteKey, listKeys, updateKey } from "../api/keys";

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
    <div className="p-6">
      <h1 className="mb-4 text-lg font-semibold text-slate-100">API Keys</h1>

      {newKey ? (
        <div className="mb-4 rounded border border-amber-700/50 bg-amber-950/40 px-4 py-3">
          <p className="mb-1 text-xs font-medium uppercase tracking-wide text-amber-200">
            Copy now — shown once
          </p>
          <div className="flex flex-wrap items-center gap-2">
            <code className="break-all font-mono text-sm text-amber-100">{newKey}</code>
            <button
              type="button"
              onClick={copyKey}
              className="rounded border border-amber-700 px-2 py-1 text-xs text-amber-100 hover:bg-amber-900/50"
            >
              {copied ? "Copied" : "Copy"}
            </button>
            <button
              type="button"
              onClick={() => setNewKey("")}
              className="rounded border border-slate-700 px-2 py-1 text-xs text-slate-400 hover:bg-slate-800"
            >
              Dismiss
            </button>
          </div>
        </div>
      ) : null}

      <form onSubmit={onCreate} className="mb-6 flex flex-wrap items-end gap-2">
        <div>
          <label className="mb-1 block text-xs text-slate-400" htmlFor="key-name">
            Name
          </label>
          <input
            id="key-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="rounded border border-slate-700 bg-[#0d1219] px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-sky-600"
            placeholder="my-client"
            required
          />
        </div>
        <button
          type="submit"
          disabled={creating || !name.trim()}
          className="rounded bg-sky-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-500 disabled:opacity-50"
        >
          {creating ? "Creating…" : "Create key"}
        </button>
      </form>

      {loading ? <p className="text-sm text-slate-400">Loading…</p> : null}
      {error ? <p className="mb-3 text-sm text-red-400">{error}</p> : null}

      {!loading && rows.length === 0 && !error ? (
        <p className="text-sm text-slate-400">No API keys yet.</p>
      ) : null}

      {rows.length > 0 ? (
        <div className="overflow-x-auto rounded border border-slate-800">
          <table className="w-full min-w-[32rem] text-left text-sm">
            <thead className="border-b border-slate-800 bg-[#0d1219] text-xs uppercase text-slate-400">
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
                <tr key={row.id} className="border-b border-slate-800/80 last:border-0">
                  <td className="px-3 py-2 text-slate-100">{row.name || "—"}</td>
                  <td className="px-3 py-2 font-mono text-xs text-slate-400">
                    {row.keyId || row.id}
                  </td>
                  <td className="px-3 py-2">
                    <button
                      type="button"
                      onClick={() => onToggle(row.id, !!row.isActive)}
                      className={`rounded px-2 py-0.5 text-xs ${
                        row.isActive
                          ? "bg-emerald-900/40 text-emerald-300"
                          : "bg-slate-800 text-slate-400"
                      }`}
                    >
                      {row.isActive ? "Active" : "Inactive"}
                    </button>
                  </td>
                  <td className="px-3 py-2 text-xs text-slate-400">{row.createdAt || "—"}</td>
                  <td className="px-3 py-2">
                    <button
                      type="button"
                      onClick={() => onDelete(row.id)}
                      className="text-xs text-red-400 hover:underline"
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
