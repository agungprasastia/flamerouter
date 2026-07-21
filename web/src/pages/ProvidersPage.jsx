import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { listProviders } from "../api/providers";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Button } from "../components/Button";

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
    <div className="space-y-6">
      <h1 className="text-lg font-semibold text-[var(--text)]">Providers</h1>

      <Card as="form" onSubmit={onConnect} className="p-4">
        <p className="mb-2 text-sm text-[var(--text)]">Connect a provider</p>
        <div className="flex flex-wrap gap-2">
          <div className="min-w-[12rem] flex-1">
            <Input
              type="text"
              value={providerId}
              onChange={(e) => setProviderId(e.target.value)}
              placeholder="provider id (e.g. openai)"
              list="common-provider-ids"
            />
          </div>
          <datalist id="common-provider-ids">
            {COMMON_IDS.map((id) => (
              <option key={id} value={id} />
            ))}
          </datalist>
          <Button type="submit" variant="primary" disabled={!providerId.trim()}>
            Open
          </Button>
        </div>
        <div className="mt-3 flex flex-wrap gap-2">
          {COMMON_IDS.map((id) => (
            <button
              key={id}
              type="button"
              onClick={() => openProvider(id)}
              className="rounded-full border border-[var(--border)] bg-[var(--surface-2)] px-2.5 py-1 font-mono text-xs text-[var(--muted)] transition hover:border-[var(--primary)] hover:text-[var(--primary)]"
            >
              {id}
            </button>
          ))}
        </div>
      </Card>

      {loading ? <p className="text-sm text-[var(--muted)]">Loading…</p> : null}
      {error ? <p className="text-sm text-[var(--danger)]">{error}</p> : null}
      {!loading && !error && rows.length === 0 ? (
        <p className="text-sm text-[var(--muted)]">
          No connections yet — open a provider id to connect.
        </p>
      ) : null}
      {rows.length > 0 ? (
        <div className="rounded border border-[var(--border)] overflow-x-auto">
          <table className="w-full min-w-[28rem] text-left text-sm">
            <thead className="border-b border-[var(--border)] text-xs uppercase text-[var(--muted)] bg-[var(--surface)]">
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
                  <tr key={id} className="border-b border-[var(--border)] last:border-0 hover:bg-[var(--surface-2)]/40">
                    <td className="px-3 py-2">
                      <Link
                        to={`/dashboard/providers/${encodeURIComponent(hrefId)}`}
                        className="text-[var(--primary)] hover:underline"
                      >
                        {row.name || row.provider || id}
                      </Link>
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-[var(--muted)]">{id}</td>
                    <td className="px-3 py-2 text-[var(--text)]">
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
