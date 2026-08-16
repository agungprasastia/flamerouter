"use client";

import React, { useEffect, ReactNode } from "react";
import useThemeStore from "@/store/themeStore";

export function ThemeProvider({ children }: { children: ReactNode }) {
  const { initTheme } = useThemeStore();

  useEffect(() => {
    initTheme();
  }, [initTheme]);

  return <>{children}</>;
}
