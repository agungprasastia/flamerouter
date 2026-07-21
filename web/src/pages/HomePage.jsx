import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { apiJSON } from "../api/client";
import { useAuth } from "../store/auth";
import { Card } from "../components/Card";
import { Button } from "../components/Button";

const links = [
  { to: "/dashboard/providers", label: "Providers" },
  { to: "/dashboard/usage", label: "Usage" },
  { to: "/dashboard/keys", label: "API Keys" },
  { to: "/dashboard/settings", label: "Settings" },
];

export default function HomePage() {
  const { authenticated, requireLogin } = useAuth();
  const [health, setHealth] = useState(null);
  const [version, setVersion] = useState(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    (async () => {
      setError("");
      try {
        const [h, v] = await Promise.all([
          apiJSON("/api/health"),
          apiJSON("/api/version").catch(() => null),
        ]);
        if (!alive) return;
        setHealth(h);
        setVersion(v);
      } catch (err) {
        if (alive) setError(err?.message || String(err));
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  const ok = health?.ok === true;
  const ver = version?.version ?? version?.current ?? "—";

  return (
    <div className="space-y-6">
      <h1 className="text-lg font-semibold text-[var(--text)]">Dashboard</h1>
      {error ? <p className="text-sm text-[var(--danger)]">{error}</p> : null}

      <div className="grid gap-3 sm:grid-cols-3">
        <Card className="p-4">
          <p className="text-xs uppercase tracking-wide text-[var(--muted)]">Health</p>
          <p className={`mt-1 text-xl font-semibold ${ok ? "text-[var(--success)]" : "text-[var(--text)]"}`}>
            {health == null ? "…" : ok ? "OK" : "Down"}
          </p>
        </Card>
        <Card className="p-4">
          <p className="text-xs uppercase tracking-wide text-[var(--muted)]">Version</p>
          <p className="mt-1 text-xl font-semibold tabular-nums text-[var(--text)]">{ver}</p>
        </Card>
        <Card className="p-4">
          <p className="text-xs uppercase tracking-wide text-[var(--muted)]">Auth</p>
          <p className="mt-1 text-sm text-[var(--text)]">
            {requireLogin ? (authenticated ? "Signed in" : "Required") : "Open (login off)"}
          </p>
        </Card>
      </div>

      <div className="flex flex-wrap gap-2">
        {links.map((l) => (
          <Button key={l.to} variant="secondary" as={Link} to={l.to}>
            {l.label}
          </Button>
        ))}
      </div>
    </div>
  );
}
