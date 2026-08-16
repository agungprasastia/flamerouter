"use client";

import { useState, useEffect, type FormEvent } from "react";
import { AlertCircle, Eye, EyeOff, KeyRound, ShieldAlert } from "lucide-react";
import { Button, Input, Skeleton } from "@/shared/components";
import AuthLayout from "@/shared/components/layouts/AuthLayout";

export default function LoginPage() {
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [resetHint, setResetHint] = useState("");
  const [retryAfter, setRetryAfter] = useState(0);
  const [loading, setLoading] = useState(false);
  const [hasPassword, setHasPassword] = useState<boolean | null>(null);
  const [authMode, setAuthMode] = useState("password");
  const [oidcConfigured, setOidcConfigured] = useState(false);
  const [oidcLoginLabel, setOidcLoginLabel] = useState("Sign in with OIDC");
  const [mustChange, setMustChange] = useState(false);
  const [newPassword, setNewPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [showNewPassword, setShowNewPassword] = useState(false);

  // Countdown for rate-limit
  useEffect(() => {
    if (retryAfter <= 0) return;
    const id = setInterval(
      () => setRetryAfter((s) => (s > 0 ? s - 1 : 0)),
      1000,
    );
    return () => clearInterval(id);
  }, [retryAfter]);

  useEffect(() => {
    async function checkAuth() {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 5000);
      const baseUrl =
        typeof window !== "undefined" ? window.location.origin : "";

      try {
        const res = await fetch(`${baseUrl}/api/auth/status`, {
          signal: controller.signal,
        });
        clearTimeout(timeoutId);

        if (res.ok) {
          const data = await res.json();
          if (data.authenticated === true || data.requireLogin === false) {
            window.location.assign("/dashboard");
            return;
          }
          setHasPassword(!!data.hasPassword);
          setAuthMode(data.authMode || "password");
          setOidcConfigured(data.oidcConfigured === true);
          setOidcLoginLabel(data.oidcLoginLabel || "Sign in with OIDC");
        } else {
          // Safe fallback on non-OK response to avoid infinite loading state.
          setHasPassword(true);
        }
      } catch (err) {
        clearTimeout(timeoutId);
        setHasPassword(true);
      }
    }
    checkAuth();
  }, []);

  const handleLogin = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setLoading(true);
    setError("");
    setResetHint("");

    try {
      const res = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password }),
      });

      if (res.ok) {
        const data = await res.json();
        if (data.mustChangePassword) {
          setMustChange(true);
          return;
        }
        window.location.assign("/dashboard");
      } else {
        const data = await res.json();
        setError(data.error || "Invalid password");
        if (data.resetHint) setResetHint(data.resetHint);
        if (data.retryAfter) setRetryAfter(Number(data.retryAfter));
      }
    } catch (err) {
      setError("An error occurred. Please try again.");
    } finally {
      setLoading(false);
    }
  };

  // Force a new password before entering the dashboard (default + remote).
  const handleSetNewPassword = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/settings", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ currentPassword: password, newPassword }),
      });
      if (res.ok) {
        window.location.assign("/dashboard");
      } else {
        const data = await res.json();
        setError(data.error || "Failed to set password");
      }
    } catch (err) {
      setError("An error occurred. Please try again.");
    } finally {
      setLoading(false);
    }
  };

  const handleOidcLogin = () => {
    window.location.href = "/api/auth/oidc/start";
  };

  const oidcAvailable = oidcConfigured && ["oidc", "both"].includes(authMode);
  const passwordAvailable = authMode !== "oidc" || !oidcConfigured;

  // Show loading state while checking password
  if (hasPassword === null) {
    return (
      <AuthLayout>
        <div role="status" aria-label="Loading sign in" className="space-y-8">
          <span className="sr-only">Loading sign in</span>
          <div className="space-y-3">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-10 w-52" />
            <Skeleton className="h-4 w-full max-w-sm" />
          </div>
          <div className="space-y-4 border-t border-border pt-8">
            <Skeleton className="h-4 w-20" />
            <Skeleton className="h-11 w-full" />
            <Skeleton className="h-11 w-full" />
          </div>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <div>
        <header className="mb-7">
          <h1 className="text-2xl font-semibold tracking-[-0.03em]">
            {mustChange ? "Set a new password" : "Access control plane"}
          </h1>
          <p className="mt-3 max-w-sm text-sm leading-6 text-text-muted">
            {authMode === "oidc" && oidcConfigured
              ? "Sign in with your OIDC provider to access the dashboard"
              : "Enter your password to access the dashboard"}
          </p>
        </header>

        <div>
          {mustChange ? (
            <form
              onSubmit={handleSetNewPassword}
              className="flex flex-col gap-5"
            >
              <div className="border-l-2 border-brand-600 py-1 pl-4 text-sm leading-6 text-text-muted">
                Set a new password before accessing the dashboard remotely.
              </div>
              <div className="flex flex-col gap-2">
                <label htmlFor="new-password" className="text-sm font-medium">
                  New password
                </label>
                <div className="relative">
                  <Input
                    id="new-password"
                    type={showNewPassword ? "text" : "password"}
                    placeholder="Enter new password"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    inputClassName="pr-12"
                    required
                    autoFocus
                  />
                  <button
                    type="button"
                    onClick={() => setShowNewPassword((visible) => !visible)}
                    className="absolute inset-y-0 right-0 flex w-11 items-center justify-center text-text-muted hover:text-text-main focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary/35"
                    aria-label={showNewPassword ? "Hide password" : "Show password"}
                  >
                    {showNewPassword ? (
                      <EyeOff size={18} strokeWidth={1.75} aria-hidden="true" />
                    ) : (
                      <Eye size={18} strokeWidth={1.75} aria-hidden="true" />
                    )}
                  </button>
                </div>
                {error && (
                  <p role="alert" className="flex gap-2 text-xs text-red-500">
                    <AlertCircle size={15} strokeWidth={1.75} aria-hidden="true" />
                    {error}
                  </p>
                )}
              </div>
              <Button
                type="submit"
                variant="primary"
                className="w-full"
                loading={loading}
                disabled={!newPassword}
              >
                Set password
              </Button>
            </form>
          ) : (
            <div className="flex flex-col gap-5">
              {oidcAvailable && (
                <Button
                  type="button"
                  variant="primary"
                  className="w-full"
                  size="lg"
                  onClick={handleOidcLogin}
                >
                  {oidcLoginLabel}
                </Button>
              )}

              {oidcAvailable && passwordAvailable && (
                <div className="flex items-center gap-4" aria-hidden="true">
                  <div className="h-px flex-1 bg-border" />
                  <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-text-muted">
                    or password
                  </span>
                  <div className="h-px flex-1 bg-border" />
                </div>
              )}

              {passwordAvailable ? (
                <form onSubmit={handleLogin} className="flex flex-col gap-4">
                  {((authMode === "oidc" && !oidcConfigured) ||
                    (authMode === "both" && !oidcConfigured)) && (
                    <p className="border-l-2 border-amber-500 py-1 pl-4 text-xs leading-5 text-amber-700 dark:text-amber-400">
                      OIDC login is enabled, but the issuer/client fields are
                      not configured yet. Password login is still available for
                      recovery.
                    </p>
                  )}

                  {authMode === "both" && oidcConfigured && (
                    <p className="text-xs text-text-muted">
                      Password and OIDC login are both enabled.
                    </p>
                  )}

                  <div className="flex flex-col gap-2">
                    <label htmlFor="password" className="text-sm font-medium">
                      Password
                    </label>
                    <div className="relative">
                      <Input
                        id="password"
                        type={showPassword ? "text" : "password"}
                        placeholder="Enter password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        inputClassName="pr-12"
                        required
                        autoFocus={!oidcAvailable}
                      />
                      <button
                        type="button"
                        onClick={() => setShowPassword((visible) => !visible)}
                        className="absolute inset-y-0 right-0 flex w-11 items-center justify-center text-text-muted hover:text-text-main focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary/35"
                        aria-label={showPassword ? "Hide password" : "Show password"}
                      >
                        {showPassword ? (
                          <EyeOff size={18} strokeWidth={1.75} aria-hidden="true" />
                        ) : (
                          <Eye size={18} strokeWidth={1.75} aria-hidden="true" />
                        )}
                      </button>
                    </div>
                    {error && (
                      <p role="alert" className="flex gap-2 text-xs text-red-500">
                        <AlertCircle size={15} strokeWidth={1.75} aria-hidden="true" />
                        {error}
                      </p>
                    )}
                    {retryAfter > 0 && (
                      <p aria-live="polite" className="text-xs text-amber-700 dark:text-amber-400">
                        Locked. Retry in{" "}
                        <span className="font-mono">{retryAfter}s</span>.
                      </p>
                    )}
                    {resetHint && (
                      <p className="text-xs text-text-muted">
                        Forgot password? Open{" "}
                        <code className="bg-sidebar px-1 rounded-[3px]">
                          flamerouter
                        </code>{" "}
                        CLI on the host → <b>Settings</b> →{" "}
                        <b>Reset Password to Default</b>.
                      </p>
                    )}
                  </div>

                  <Button
                    type="submit"
                    variant="primary"
                    className="w-full"
                    size="lg"
                    loading={loading}
                    disabled={retryAfter > 0}
                  >
                    {retryAfter > 0 ? `Wait ${retryAfter}s` : "Login"}
                  </Button>

                  <div className="mt-2 border-l-2 border-brand-600 py-1 pl-4">
                    <div className="flex gap-3">
                      {hasPassword === false ? (
                        <ShieldAlert
                          size={18}
                          strokeWidth={1.75}
                          className="mt-0.5 shrink-0 text-amber-600"
                          aria-hidden="true"
                        />
                      ) : (
                        <KeyRound
                          size={18}
                          strokeWidth={1.75}
                          className="mt-0.5 shrink-0 text-primary"
                          aria-hidden="true"
                        />
                      )}
                      <div className="space-y-1 text-xs leading-5 text-text-muted">
                        <p>
                          Default password is{" "}
                          <code className="rounded-[3px] bg-sidebar px-1">123456</code>
                        </p>
                        {hasPassword === false && (
                          <p className="text-amber-700 dark:text-amber-400">
                            Security risk: no password set. You will be asked to
                            set one when logging in remotely.
                          </p>
                        )}
                      </div>
                    </div>
                  </div>
                </form>
              ) : (
                error && (
                  <p role="alert" className="flex gap-2 text-xs text-red-500">
                    <AlertCircle size={15} strokeWidth={1.75} aria-hidden="true" />
                    {error}
                  </p>
                )
              )}
            </div>
          )}
        </div>
      </div>
    </AuthLayout>
  );
}
