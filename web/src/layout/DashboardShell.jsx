import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../store/auth";

const nav = [
  { to: "/dashboard", end: true, label: "Dashboard" },
  { to: "/dashboard/providers", label: "Providers" },
  { to: "/dashboard/usage", label: "Usage" },
  { to: "/dashboard/keys", label: "Keys" },
  { to: "/dashboard/settings", label: "Settings" },
];

const linkClass = ({ isActive }) =>
  `block rounded px-3 py-1.5 text-sm ${
    isActive ? "bg-slate-800 text-sky-400" : "text-slate-300 hover:bg-slate-800/60"
  }`;

export default function DashboardShell() {
  const logout = useAuth((s) => s.logout);
  const navigate = useNavigate();

  async function onLogout() {
    await logout();
    navigate("/login", { replace: true });
  }

  return (
    <div className="flex min-h-screen bg-[#0b0f14] text-slate-200">
      <aside className="flex w-60 shrink-0 flex-col border-r border-slate-800 bg-[#0d1219]">
        <div className="border-b border-slate-800 px-4 py-3 text-sm font-semibold tracking-wide text-slate-100">
          FlameRouter
        </div>
        <nav className="flex flex-1 flex-col gap-0.5 p-2">
          {nav.map((item) => (
            <NavLink key={item.to} to={item.to} end={item.end} className={linkClass}>
              {item.label}
            </NavLink>
          ))}
        </nav>
        <button
          type="button"
          onClick={onLogout}
          className="m-2 rounded border border-slate-700 px-3 py-1.5 text-left text-sm text-slate-400 hover:bg-slate-800 hover:text-slate-200"
        >
          Logout
        </button>
      </aside>
      <main className="min-w-0 flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
