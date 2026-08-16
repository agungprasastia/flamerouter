"use client";

import PropTypes from "prop-types";
import { Flame } from "lucide-react";
import ThemeToggle from "../ThemeToggle";

export default function AuthLayout({ children }) {
  return (
    <div className="relative flex min-h-[100dvh] items-center justify-center overflow-x-hidden bg-bg px-5 py-16 text-text-main selection:bg-primary/20 selection:text-primary sm:px-8">
      <div className="absolute top-5 right-5 sm:top-8 sm:right-8">
          <ThemeToggle className="border border-border bg-surface" />
      </div>

      <main className="w-full max-w-[420px]">
        <div className="mb-10 flex items-center gap-3">
          <span className="flex size-9 items-center justify-center rounded-[6px] bg-brand-600 text-white">
            <Flame size={19} strokeWidth={1.75} aria-hidden="true" />
          </span>
          <div>
            <p className="font-semibold tracking-tight">FlameRouter</p>
            <p className="text-xs text-text-muted">Local control plane</p>
          </div>
        </div>
        {children}
      </main>
    </div>
  );
}

AuthLayout.propTypes = {
  children: PropTypes.node.isRequired,
};
