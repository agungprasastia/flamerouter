import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { listProviders } from "../api/providers";

const COMMON_IDS = [
  "openai",
  "anthropic",
  "claude",
  "gemini",
  "deepseek",
  "openrouter",
  "groq",
  "ollama",
  "github",
  "azure",
];

export default function ProvidersPage() {
  const navigate = useNavigate();
  const [rows, setRows] = useState([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [providerId, setProviderId] = useState("");

  useEffect(() => {
    let alive = true;
    (async () => {
      setLoading(true);
      setError("");
      try {
        const data = await listProviders();
        const list = data?.connections ?? data?.providers ?? (Array.isArray(data) ? data : []);
        if (alive) setRows(list);
      } catch (err) {
        if (alive) setError(err?.message || String(err));
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  function openProvider(id) {
    const trimmed = (id || "").trim();
    if (!trimmed) return;
    navigate(`/dashboard/providers/${encodeURIComponent(trimmed)}`);
  }

  function onConnect(e) {
    e.preventDefault();
    openProvider(providerId);
  }

  return (
    <div className="p-6">
      <h1 className="mb-4 text-lg font-semibold text-slate-100">Providers</h1>

      <form onSubmit={onConnect} className="mb-6 rounded border border-slate-800 bg-[#0d1219] p-4">
        <p className="mb-2 text-sm text-slate-300">Connect a provider</p>
        <div className="flex flex-wrap gap-2">
          <input
            type="text"
            value={providerId}
            onChange={(e) => setProviderId(e.target.value)}
            placeholder="provider id (e.g. openai)"
            className="min-w-[12rem] flex-1 rounded border border-slate-700 bg-[#0b0f14] px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-500"
            list="common-provider-ids"
          />
          <datalist id="common-provider-ids">
            {COMMON_IDS.map((id) => (
              <option key={id} value={id} />
            ))}
          </datalist>
          <button
            type="submit"
            disabled={!providerId.trim()}
            className="rounded bg-sky-500 px-3 py-2 text-sm font-medium text-slate-950 hover:bg-sky-400 disabled:opacity-50"
          >
            Open
          </button>
        </div>
        <div className="mt-3 flex flex-wrap gap-2">
          {COMMON_IDS.map((id) => (
            <button
              key={id}
              type="button"
              onClick={() => openProvider(id)}
              className="rounded border border-slate-700 px-2 py-1 font-mono text-xs text-slate-300 hover:border-sky-500 hover:text-sky-300"
            >
              {id}
            </button>
          ))}
        </div>
      </form>

      {loading ? <p className="text-sm text-slate-400">Loading…</p> : null}
      {error ? <p className="mb-3 text-sm text-red-400">{error}</p> : null}
      {!loading && !error && rows.length === 0 ? (
        <p className="text-sm text-slate-400">
          No connections yet — open a provider id to connect.
        </p>
      ) : null}
      {rows.length > 0 ? (
        <div className="overflow-x-auto rounded border border-slate-800">
          <table className="w-full min-w-[28rem] text-left text-sm">
            <thead className="border-b border-slate-800 bg-[#0d1219] text-xs uppercase text-slate-400">
              <tr>
                <th className="px-3 py-2 font-medium">Name</th>
                <th className="px-3 py-2 font-medium">ID</th>
                <th className="px-3 py-2 font-medium">Category</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const id = row.id || row.provider;
                const hrefId = row.provider || row.id;
                return (
                  <tr key={id} className="border-b border-slate-800/80 last:border-0 hover:bg-slate-900/40">
                    <td className="px-3 py-2">
                      <Link
                        to={`/dashboard/providers/${encodeURIComponent(hrefId)}`}
                        className="text-sky-400 hover:underline"
                      >
                        {row.name || row.provider || id}
                      </Link>
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-slate-400">{id}</td>
                    <td className="px-3 py-2 text-slate-300">
                      {row.category || row.authType || row.provider || "—"}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  );
}
