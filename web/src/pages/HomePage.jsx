import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { apiJSON } from "../api/client";
import { useAuth } from "../store/auth";

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
    <div className="p-6">
      <h1 className="mb-4 text-lg font-semibold text-slate-100">Dashboard</h1>
      {error ? <p className="mb-3 text-sm text-red-400">{error}</p> : null}

      <div className="mb-6 grid gap-3 sm:grid-cols-3">
        <div className="rounded border border-slate-800 bg-[#0d1219] px-4 py-3">
          <p className="text-xs uppercase tracking-wide text-slate-400">Health</p>
          <p className={`mt-1 text-xl font-semibold ${ok ? "text-emerald-400" : "text-slate-300"}`}>
            {health == null ? "…" : ok ? "OK" : "Down"}
          </p>
        </div>
        <div className="rounded border border-slate-800 bg-[#0d1219] px-4 py-3">
          <p className="text-xs uppercase tracking-wide text-slate-400">Version</p>
          <p className="mt-1 text-xl font-semibold tabular-nums text-slate-100">{ver}</p>
        </div>
        <div className="rounded border border-slate-800 bg-[#0d1219] px-4 py-3">
          <p className="text-xs uppercase tracking-wide text-slate-400">Auth</p>
          <p className="mt-1 text-sm text-slate-100">
            {requireLogin ? (authenticated ? "Signed in" : "Required") : "Open (login off)"}
          </p>
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        {links.map((l) => (
          <Link
            key={l.to}
            to={l.to}
            className="rounded border border-slate-700 px-3 py-1.5 text-sm text-sky-400 hover:bg-slate-800"
          >
            {l.label}
          </Link>
        ))}
      </div>
    </div>
  );
}
