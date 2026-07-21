import { ChartLineUp, GearSix, Key, PlugsConnected, SignOut, SquaresFour } from "@phosphor-icons/react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { Button } from "../components/Button";
import { ThemeToggle } from "../components/ThemeToggle";
import { useAuth } from "../store/auth";

const nav = [
  { to: "/dashboard", end: true, label: "Dashboard", Icon: SquaresFour },
  { to: "/dashboard/providers", label: "Providers", Icon: PlugsConnected },
  { to: "/dashboard/usage", label: "Usage", Icon: ChartLineUp },
  { to: "/dashboard/keys", label: "Keys", Icon: Key },
  { to: "/dashboard/settings", label: "Settings", Icon: GearSix },
];

const titles = [
  ["/dashboard/providers", "Providers"],
  ["/dashboard/usage", "Usage"],
  ["/dashboard/keys", "API Keys"],
  ["/dashboard/settings", "Settings"],
  ["/dashboard", "Dashboard"],
];

function pageTitle(pathname) {
  return titles.find(([path]) => pathname.startsWith(path))?.[1] || "Dashboard";
}

export default function DashboardShell() {
  const logout = useAuth((s) => s.logout);
  const navigate = useNavigate();
  const location = useLocation();

  async function onLogout() {
    await logout();
    navigate("/login", { replace: true });
  }

  return (
    <div className="flex min-h-dvh bg-[var(--bg)] text-[var(--text)]">
      <aside className="hidden w-72 shrink-0 flex-col border-r border-[var(--border)] bg-[var(--surface)]/85 backdrop-blur-xl lg:flex">
        <div className="border-b border-[var(--border)] px-5 py-4">
          <div className="mb-4 flex gap-1.5">
            <span className="h-3 w-3 rounded-full bg-[#ff5f57]" />
            <span className="h-3 w-3 rounded-full bg-[#ffbd2e]" />
            <span className="h-3 w-3 rounded-full bg-[#28c840]" />
          </div>
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-[12px] bg-[var(--primary)] text-[var(--primary-contrast)] shadow-lg shadow-orange-900/10">
              <PlugsConnected size={22} weight="duotone" />
            </div>
            <div>
              <div className="text-sm font-semibold tracking-tight">FlameRouter</div>
              <div className="text-xs text-[var(--muted)]">Local AI gateway</div>
            </div>
          </div>
        </div>
        <nav className="flex flex-1 flex-col gap-1 p-3">
          {nav.map(({ Icon, ...item }) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-[10px] px-3 py-2 text-sm font-medium transition ${
                  isActive
                    ? "bg-[var(--primary)]/10 text-[var(--primary)]"
                    : "text-[var(--muted)] hover:bg-[var(--surface-2)] hover:text-[var(--text)]"
                }`
              }
            >
              <Icon size={18} weight="duotone" />
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <div className="min-w-0 flex-1 app-grid">
        <header className="sticky top-0 z-10 border-b border-[var(--border)] bg-[var(--bg)]/82 px-4 py-3 backdrop-blur-xl lg:px-10">
          <div className="mx-auto flex max-w-7xl items-center justify-between gap-3">
            <div>
              <p className="text-xs font-medium uppercase tracking-[0.18em] text-[var(--muted)]">FlameRouter</p>
              <h1 className="text-lg font-semibold tracking-tight lg:text-2xl">{pageTitle(location.pathname)}</h1>
            </div>
            <div className="flex items-center gap-2">
              <ThemeToggle />
              <Button type="button" variant="ghost" size="sm" onClick={onLogout}>
                <SignOut size={16} weight="duotone" />
                Logout
              </Button>
            </div>
          </div>
        </header>
        <main className="mx-auto max-w-7xl px-4 py-6 lg:px-10 lg:py-10">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
