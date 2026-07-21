import { useEffect } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useAuth } from "./store/auth";
import { useTheme } from "./store/theme";
import DashboardShell from "./layout/DashboardShell";
import LoginPage from "./pages/LoginPage";
import HomePage from "./pages/HomePage";
import ProvidersPage from "./pages/ProvidersPage";
import ProviderDetailPage from "./pages/ProviderDetailPage";
import UsagePage from "./pages/UsagePage";
import KeysPage from "./pages/KeysPage";
import SettingsPage from "./pages/SettingsPage";

function Protected({ children }) {
  const { authenticated, loading, requireLogin } = useAuth();
  const loc = useLocation();
  if (loading) return <div className="bg-[var(--bg)] p-8 text-[var(--muted)]">Loading…</div>;
  if (requireLogin && !authenticated) {
    return <Navigate to="/login" replace state={{ from: loc.pathname }} />;
  }
  return children;
}

export default function App() {
  const bootstrap = useAuth((s) => s.bootstrap);
  const syncTheme = useTheme((s) => s.syncTheme);
  useEffect(() => {
    bootstrap();
  }, [bootstrap]);
  useEffect(() => {
    syncTheme();
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    media.addEventListener("change", syncTheme);
    return () => media.removeEventListener("change", syncTheme);
  }, [syncTheme]);

  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/dashboard"
        element={
          <Protected>
            <DashboardShell />
          </Protected>
        }
      >
        <Route index element={<HomePage />} />
        <Route path="providers" element={<ProvidersPage />} />
        <Route path="providers/:id" element={<ProviderDetailPage />} />
        <Route path="usage" element={<UsagePage />} />
        <Route path="keys" element={<KeysPage />} />
        <Route path="settings" element={<SettingsPage />} />
      </Route>
      <Route path="/" element={<Navigate to="/dashboard" replace />} />
      <Route path="*" element={<Navigate to="/dashboard" replace />} />
    </Routes>
  );
}
