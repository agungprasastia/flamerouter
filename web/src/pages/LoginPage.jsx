import { useState } from "react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";
import { useAuth } from "../store/auth";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Button } from "../components/Button";

export default function LoginPage() {
  const { authenticated, loading, requireLogin, login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const from = location.state?.from || "/dashboard";
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  if (!loading && (!requireLogin || authenticated)) {
    return <Navigate to={from} replace />;
  }

  async function onSubmit(e) {
    e.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await login(password);
      navigate(from, { replace: true });
    } catch (err) {
      setError(err?.message || String(err) || "Login failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="grid min-h-dvh place-items-center bg-[var(--bg)] px-4 text-[var(--text)] app-grid">
      <Card as="form" onSubmit={onSubmit} className="w-full max-w-sm p-6">
        <h1 className="mb-1 text-lg font-semibold text-[var(--text)]">FlameRouter</h1>
        <p className="mb-4 text-sm text-[var(--muted)]">Sign in</p>
        <Input
          id="password"
          label="Password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        {error ? <p className="mb-3 text-sm text-[var(--danger)]">{error}</p> : null}
        <Button type="submit" variant="primary" disabled={submitting || loading} className="mt-3 w-full">
          {submitting ? "Signing in…" : "Sign in"}
        </Button>
      </Card>
    </div>
  );
}
