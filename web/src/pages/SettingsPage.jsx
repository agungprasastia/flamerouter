import { useEffect, useState } from "react";
import {
  getRequireLogin,
  getSettings,
  patchRequireLogin,
  patchSettings,
} from "../api/settings";
import { useAuth } from "../store/auth";
import { Card } from "../components/Card";
import { Button } from "../components/Button";
import { Input } from "../components/Input";

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
    <div className="space-y-6">
      <h1 className="text-lg font-semibold text-[var(--text)]">Settings</h1>
      {loading ? <p className="text-sm text-[var(--muted)]">Loading…</p> : null}
      {error ? <p className="text-sm text-[var(--danger)]">{error}</p> : null}
      {msg ? <p className="text-sm text-[var(--success)]">{msg}</p> : null}

      {!loading ? (
        <div className="max-w-lg space-y-6">
          <Card className="px-4 py-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-medium text-[var(--text)]">Require login</p>
                <p className="text-xs text-[var(--muted)]">Gate dashboard behind password</p>
              </div>
              <Button
                type="button"
                onClick={onToggleRequireLogin}
                variant={requireLogin ? "primary" : "secondary"}
                size="sm"
              >
                {requireLogin ? "On" : "Off"}
              </Button>
            </div>
          </Card>

          <Card className="px-4 py-3">
            <form onSubmit={onSaveStrings} className="space-y-3">
              <p className="text-sm font-medium text-[var(--text)]">General</p>
              <Input
                id="locale"
                label="Locale"
                value={locale}
                onChange={(e) => setLocale(e.target.value)}
              />
              <Input
                id="fallback"
                label="Fallback strategy"
                value={fallbackStrategy}
                onChange={(e) => setFallbackStrategy(e.target.value)}
                placeholder="e.g. round-robin"
              />
              <Button type="submit" variant="primary" disabled={saving}>
                {saving ? "Saving…" : "Save"}
              </Button>
            </form>
          </Card>

          {settings ? (
            <Card className="px-4 py-3 text-xs text-[var(--muted)]">
              <p>
                hasPassword: {settings.hasPassword ? "yes" : "no"} · oidcConfigured:{" "}
                {settings.oidcConfigured ? "yes" : "no"}
              </p>
            </Card>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
