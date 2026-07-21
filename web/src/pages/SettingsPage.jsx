import { useEffect, useState } from "react";
import {
  getRequireLogin,
  getSettings,
  patchRequireLogin,
  patchSettings,
} from "../api/settings";
import { useAuth } from "../store/auth";

export default function SettingsPage() {
  const bootstrap = useAuth((s) => s.bootstrap);
  const [settings, setSettings] = useState(null);
  const [requireLogin, setRequireLogin] = useState(true);
  const [locale, setLocale] = useState("en");
  const [fallbackStrategy, setFallbackStrategy] = useState("");
  const [error, setError] = useState("");
  const [msg, setMsg] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let alive = true;
    (async () => {
      setLoading(true);
      setError("");
      try {
        const [s, rl] = await Promise.all([getSettings(), getRequireLogin()]);
        if (!alive) return;
        setSettings(s);
        setRequireLogin(rl?.requireLogin !== false);
        setLocale(typeof s?.locale === "string" && s.locale ? s.locale : "en");
        setFallbackStrategy(
          typeof s?.fallbackStrategy === "string" ? s.fallbackStrategy : "",
        );
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

  async function onToggleRequireLogin() {
    setError("");
    setMsg("");
    const next = !requireLogin;
    try {
      const res = await patchRequireLogin(next);
      setRequireLogin(res?.requireLogin !== false);
      setMsg("Require login updated");
      await bootstrap();
    } catch (err) {
      setError(err?.message || String(err));
    }
  }

  async function onSaveStrings(e) {
    e.preventDefault();
    setSaving(true);
    setError("");
    setMsg("");
    try {
      const body = {
        locale: locale.trim() || "en",
        fallbackStrategy: fallbackStrategy.trim(),
      };
      const res = await patchSettings(body);
      setSettings(res);
      setLocale(typeof res?.locale === "string" && res.locale ? res.locale : body.locale);
      setFallbackStrategy(
        typeof res?.fallbackStrategy === "string" ? res.fallbackStrategy : body.fallbackStrategy,
      );
      setMsg("Settings saved");
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="p-6">
      <h1 className="mb-4 text-lg font-semibold text-slate-100">Settings</h1>
      {loading ? <p className="text-sm text-slate-400">Loading…</p> : null}
      {error ? <p className="mb-3 text-sm text-red-400">{error}</p> : null}
      {msg ? <p className="mb-3 text-sm text-emerald-400">{msg}</p> : null}

      {!loading ? (
        <div className="max-w-lg space-y-6">
          <div className="rounded border border-slate-800 bg-[#0d1219] px-4 py-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-medium text-slate-100">Require login</p>
                <p className="text-xs text-slate-400">Gate dashboard behind password</p>
              </div>
              <button
                type="button"
                onClick={onToggleRequireLogin}
                className={`rounded px-3 py-1 text-xs font-medium ${
                  requireLogin
                    ? "bg-sky-600 text-white"
                    : "border border-slate-700 text-slate-300"
                }`}
              >
                {requireLogin ? "On" : "Off"}
              </button>
            </div>
          </div>

          <form
            onSubmit={onSaveStrings}
            className="space-y-3 rounded border border-slate-800 bg-[#0d1219] px-4 py-3"
          >
            <p className="text-sm font-medium text-slate-100">General</p>
            <div>
              <label className="mb-1 block text-xs text-slate-400" htmlFor="locale">
                Locale
              </label>
              <input
                id="locale"
                value={locale}
                onChange={(e) => setLocale(e.target.value)}
                className="w-full rounded border border-slate-700 bg-[#0b0f14] px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-sky-600"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-400" htmlFor="fallback">
                Fallback strategy
              </label>
              <input
                id="fallback"
                value={fallbackStrategy}
                onChange={(e) => setFallbackStrategy(e.target.value)}
                className="w-full rounded border border-slate-700 bg-[#0b0f14] px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-sky-600"
                placeholder="e.g. round-robin"
              />
            </div>
            <button
              type="submit"
              disabled={saving}
              className="rounded bg-sky-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-500 disabled:opacity-50"
            >
              {saving ? "Saving…" : "Save"}
            </button>
          </form>

          {settings ? (
            <div className="rounded border border-slate-800 bg-[#0d1219] px-4 py-3 text-xs text-slate-400">
              <p>
                hasPassword: {settings.hasPassword ? "yes" : "no"} · oidcConfigured:{" "}
                {settings.oidcConfigured ? "yes" : "no"}
              </p>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
