import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { createConnection, getProvider, listProviderModels } from "../api/providers";

export default function ProviderDetailPage() {
  const { id } = useParams();
  const providerId = id || "";

  const [detail, setDetail] = useState(null);
  const [models, setModels] = useState([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const [name, setName] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formMsg, setFormMsg] = useState("");

  useEffect(() => {
    if (!providerId) return;
    let alive = true;
    (async () => {
      setLoading(true);
      setError("");
      try {
        const [p, m] = await Promise.all([
          getProvider(providerId),
          listProviderModels(providerId).catch(() => null),
        ]);
        if (!alive) return;
        setDetail(p);
        const modelList = m?.models ?? p?.models ?? [];
        setModels(Array.isArray(modelList) ? modelList : []);
      } catch (err) {
        if (alive) setError(err?.message || String(err));
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [providerId]);

  async function onSubmit(e) {
    e.preventDefault();
    setFormMsg("");
    setSubmitting(true);
    try {
      const body = {
        provider: providerId,
        name: name || providerId,
        api_key: apiKey,
      };
      if (baseUrl) body.base_url = baseUrl;
      const res = await createConnection(body);
      setFormMsg(res?.id ? `Connection created: ${res.id}` : "Connection created");
      setApiKey("");
      const p = await getProvider(providerId);
      setDetail(p);
    } catch (err) {
      setFormMsg(err?.message || String(err));
    } finally {
      setSubmitting(false);
    }
  }

  const connections =
    detail?.connections ??
    (detail?.connection ? [detail.connection] : []);

  return (
    <div className="p-6">
      <Link to="/dashboard/providers" className="mb-3 inline-block text-sm text-sky-400 hover:underline">
        ← Providers
      </Link>
      <h1 className="mb-1 text-lg font-semibold text-slate-100">{providerId}</h1>
      <p className="mb-4 text-sm text-slate-400">Provider detail</p>

      {loading ? <p className="text-sm text-slate-400">Loading…</p> : null}
      {error ? <p className="mb-3 text-sm text-red-400">{error}</p> : null}

      {!loading && connections.length > 0 ? (
        <section className="mb-6">
          <h2 className="mb-2 text-sm font-medium text-slate-300">Connections</h2>
          <ul className="space-y-1 text-sm text-slate-400">
            {connections.map((c) => (
              <li key={c.id} className="rounded border border-slate-800 px-3 py-2">
                <span className="text-slate-200">{c.name || c.id}</span>
                <span className="ml-2 font-mono text-xs">{c.id}</span>
                {c.isActive === false ? (
                  <span className="ml-2 text-xs text-amber-400">inactive</span>
                ) : null}
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {models.length > 0 ? (
        <section className="mb-6">
          <h2 className="mb-2 text-sm font-medium text-slate-300">Models</h2>
          <ul className="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
            {models.map((m) => (
              <li
                key={m.id || m.name}
                className="rounded border border-slate-800 px-3 py-1.5 text-sm text-slate-300"
              >
                {m.name || m.id}
                {m.kind ? <span className="ml-2 text-xs text-slate-500">{m.kind}</span> : null}
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <section className="max-w-md rounded border border-slate-800 bg-[#0d1219] p-4">
        <h2 className="mb-3 text-sm font-medium text-slate-300">Add connection</h2>
        <form onSubmit={onSubmit}>
          <label className="mb-1 block text-xs text-slate-400" htmlFor="conn-name">
            Name
          </label>
          <input
            id="conn-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={providerId}
            className="mb-3 w-full rounded border border-slate-700 bg-[#0b0f14] px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-500"
          />
          <label className="mb-1 block text-xs text-slate-400" htmlFor="conn-key">
            API key
          </label>
          <input
            id="conn-key"
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            className="mb-3 w-full rounded border border-slate-700 bg-[#0b0f14] px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-500"
            required
          />
          <label className="mb-1 block text-xs text-slate-400" htmlFor="conn-base">
            Base URL (optional)
          </label>
          <input
            id="conn-base"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="https://..."
            className="mb-3 w-full rounded border border-slate-700 bg-[#0b0f14] px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-500"
          />
          {formMsg ? (
            <p className={`mb-3 text-sm ${formMsg.startsWith("Connection") ? "text-emerald-400" : "text-red-400"}`}>
              {formMsg}
            </p>
          ) : null}
          <button
            type="submit"
            disabled={submitting}
            className="rounded bg-sky-500 px-3 py-2 text-sm font-medium text-slate-950 hover:bg-sky-400 disabled:opacity-50"
          >
            {submitting ? "Saving…" : "Create connection"}
          </button>
        </form>
      </section>
    </div>
  );
}
