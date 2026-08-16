"use client";

import { useEffect, useRef, useState } from "react";
import { usePathname } from "next/navigation";
import { useNotificationStore } from "@/store/notificationStore";
import Sidebar from "../Sidebar";
import Header from "../Header";
import {
  CheckCircle2,
  CircleAlert,
  Info,
  TriangleAlert,
  X,
} from "lucide-react";

const toastIcons = {
  success: CheckCircle2,
  error: CircleAlert,
  warning: TriangleAlert,
  info: Info,
};

function getToastStyle(type) {
  if (type === "success") {
    return {
      wrapper: "border-green-500/30 text-green-600 dark:text-green-400",
    };
  }
  if (type === "error") {
    return {
      wrapper: "border-red-500/30 text-red-600 dark:text-red-400",
    };
  }
  if (type === "warning") {
    return {
      wrapper: "border-amber-500/30 text-amber-600 dark:text-amber-400",
    };
  }
  return {
    wrapper: "border-blue-500/30 text-blue-600 dark:text-blue-400",
  };
}

export default function DashboardLayout({ children }) {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const menuButtonRef = useRef(null);
  const mobileSidebarRef = useRef(null);
  const pathname = usePathname();
  const notifications = useNotificationStore((state) => state.notifications);
  const removeNotification = useNotificationStore(
    (state) => state.removeNotification,
  );

  useEffect(() => {
    if (!sidebarOpen) return;

    const menuButton = menuButtonRef.current;
    const frame = requestAnimationFrame(() => {
      mobileSidebarRef.current
        ?.querySelector("a, button")
        ?.focus();
    });
    const dismissOnEscape = (event) => {
      if (event.key === "Escape") setSidebarOpen(false);
    };
    document.addEventListener("keydown", dismissOnEscape);

    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener("keydown", dismissOnEscape);
      menuButton?.focus();
    };
  }, [sidebarOpen]);

  return (
    <div className="flex min-h-[100dvh] h-[100dvh] w-full overflow-hidden bg-bg">
      <div className="fixed top-4 right-4 z-[80] flex w-[min(92vw,380px)] flex-col gap-2">
        {notifications.map((n) => {
          const style = getToastStyle(n.type);
          const ToastIcon = toastIcons[n.type] || Info;
          return (
            <div
              key={n.id}
              role={n.type === "error" ? "alert" : "status"}
              className={`relative overflow-hidden rounded-[5px] border bg-surface px-3 py-2 shadow-lg before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:bg-current ${style.wrapper}`}
            >
              <div className="flex items-start gap-2">
                <ToastIcon
                  size={18}
                  strokeWidth={1.75}
                  className="mt-0.5 shrink-0"
                  aria-hidden="true"
                />
                <div className="min-w-0 flex-1">
                  {n.title ? (
                    <p className="text-xs font-semibold mb-0.5">{n.title}</p>
                  ) : null}
                  <p className="text-xs whitespace-pre-wrap break-words">
                    {n.message}
                  </p>
                </div>
                {n.dismissible ? (
                  <button
                    type="button"
                    onClick={() => removeNotification(n.id)}
                    className="rounded-sm text-current/70 hover:text-current focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-current/35"
                    aria-label="Dismiss notification"
                  >
                    <X size={16} strokeWidth={1.75} aria-hidden="true" />
                  </button>
                ) : null}
              </div>
            </div>
          );
        })}
      </div>
      {/* Mobile sidebar overlay */}
      {sidebarOpen && (
        <button
          type="button"
          className="fixed inset-0 z-40 bg-[#161915]/70 lg:hidden"
          onClick={() => setSidebarOpen(false)}
          aria-label="Close navigation"
        />
      )}

      {/* Sidebar - Desktop */}
      <div className="hidden lg:flex">
        <Sidebar onClose={() => {}} />
      </div>

      {/* Sidebar - Mobile */}
      {sidebarOpen && (
        <div
          ref={mobileSidebarRef}
          className="fixed inset-y-0 left-0 z-50 animate-in slide-in-from-left duration-200 lg:hidden"
        >
          <Sidebar onClose={() => setSidebarOpen(false)} />
        </div>
      )}

      {/* Main content */}
      <main className="flex flex-col flex-1 h-full min-w-0 relative transition-colors duration-300 isolate">
        <Header
          key={pathname}
          menuButtonRef={menuButtonRef}
          onMenuClick={() => setSidebarOpen(true)}
        />
        <div
          className={`flex-1 overflow-y-auto custom-scrollbar ${pathname === "/dashboard/basic-chat" ? "" : "px-4 py-6 md:px-7 lg:px-10 lg:py-9"} ${pathname === "/dashboard/basic-chat" ? "flex flex-col overflow-hidden" : ""}`}
        >
          <div
            className={`${pathname === "/dashboard/basic-chat" ? "flex-1 w-full h-full flex flex-col" : "max-w-7xl mx-auto"}`}
          >
            {children}
          </div>
        </div>
      </main>
    </div>
  );
}
