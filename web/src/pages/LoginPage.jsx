import { useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { useAuth } from "../store/auth";

export default function LoginPage() {
  const { authenticated, loading, requireLogin, login } = useAuth();
  const navigate = useNavigate();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  if (!loading && (!requireLogin || authenticated)) {
    return <Navigate to="/dashboard" replace />;
  }

  async function onSubmit(e) {
    e.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await login(password);
      navigate("/dashboard", { replace: true });
    } catch (err) {
      setError(err?.message || String(err) || "Login failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-[#0b0f14] px-4">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-sm rounded-lg border border-slate-800 bg-[#0d1219] p-6 shadow-lg"
      >
        <h1 className="mb-1 text-lg font-semibold text-slate-100">FlameRouter</h1>
        <p className="mb-4 text-sm text-slate-400">Sign in</p>
        <label className="mb-1 block text-xs text-slate-400" htmlFor="password">
          Password
        </label>
        <input
          id="password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="mb-3 w-full rounded border border-slate-700 bg-[#0b0f14] px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-500"
          required
        />
        {error ? <p className="mb-3 text-sm text-red-400">{error}</p> : null}
        <button
          type="submit"
          disabled={submitting || loading}
          className="w-full rounded bg-sky-500 px-3 py-2 text-sm font-medium text-slate-950 hover:bg-sky-400 disabled:opacity-50"
        >
          {submitting ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </div>
  );
}
