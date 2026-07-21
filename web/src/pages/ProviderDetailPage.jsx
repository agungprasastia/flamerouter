import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { createConnection, getProvider, listProviderModels } from "../api/providers";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Button } from "../components/Button";
import { Badge } from "../components/Badge";

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
    <div className="space-y-6">
      <Link to="/dashboard/providers" className="inline-block text-sm text-[var(--primary)] hover:underline">
        ← Providers
      </Link>
      <h1 className="text-lg font-semibold text-[var(--text)]">{providerId}</h1>
      <p className="text-sm text-[var(--muted)]">Provider detail</p>

      {loading ? <p className="text-sm text-[var(--muted)]">Loading…</p> : null}
      {error ? <p className="text-sm text-[var(--danger)]">{error}</p> : null}

      {!loading && connections.length > 0 ? (
        <section className="space-y-4">
          <h2 className="text-sm font-medium text-[var(--text)]">Connections</h2>
          <div className="space-y-1 text-sm text-[var(--muted)]">
            {connections.map((c) => (
              <Card key={c.id} className="p-3">
                <span className="text-[var(--text)]">{c.name || c.id}</span>
                <span className="ml-2 font-mono text-xs text-[var(--muted)]">{c.id}</span>
                {c.isActive === false ? (
                  <Badge variant="warning" className="ml-2">inactive</Badge>
                ) : null}
              </Card>
            ))}
          </div>
        </section>
      ) : null}

      {models.length > 0 ? (
        <section className="space-y-4">
          <h2 className="text-sm font-medium text-[var(--text)]">Models</h2>
          <div className="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
            {models.map((m) => (
              <Card key={m.id || m.name} className="p-3">
                <span className="text-[var(--text)]">{m.name || m.id}</span>
                {m.kind ? <span className="ml-2 text-xs text-[var(--muted)]">{m.kind}</span> : null}
              </Card>
            ))}
          </div>
        </section>
      ) : null}

      <Card className="p-4 max-w-md">
        <h2 className="mb-3 text-sm font-medium text-[var(--text)]">Add connection</h2>
        <form onSubmit={onSubmit} className="space-y-3">
          <Input
            id="conn-name"
            label="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={providerId}
          />
          <Input
            id="conn-key"
            label="API key"
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            required
          />
          <Input
            id="conn-base"
            label="Base URL (optional)"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="https://..."
          />
          {formMsg ? (
            <p className={`text-sm ${formMsg.startsWith("Connection") ? "text-[var(--success)]" : "text-[var(--danger)]"}`}>
              {formMsg}
            </p>
          ) : null}
          <Button type="submit" variant="primary" disabled={submitting}>
            {submitting ? "Saving…" : "Create connection"}
          </Button>
        </form>
      </Card>
    </div>
  );
}
