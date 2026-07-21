import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { listProviders } from "../api/providers";

export default function ProvidersPage() {
  const [rows, setRows] = useState([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

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

  return (
    <div className="p-6">
      <h1 className="mb-4 text-lg font-semibold text-slate-100">Providers</h1>
      {loading ? <p className="text-sm text-slate-400">Loading…</p> : null}
      {error ? <p className="mb-3 text-sm text-red-400">{error}</p> : null}
      {!loading && !error && rows.length === 0 ? (
        <p className="text-sm text-slate-400">No connections yet.</p>
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
