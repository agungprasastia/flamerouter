"use client";

import { useEffect, type ReactNode } from "react";
import { usePathname } from "next/navigation";
import { initRuntimeI18n, reloadTranslations } from "./runtime";

interface RuntimeI18nProviderProps {
  children: ReactNode;
}

export function RuntimeI18nProvider({ children }: RuntimeI18nProviderProps) {
  const pathname = usePathname();

  useEffect(() => {
    initRuntimeI18n();
  }, []);

  // Re-process DOM when route changes
  useEffect(() => {
    if (pathname) {
      // Double RAF to ensure React has committed changes to DOM
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          reloadTranslations();
        });
      });
    }
  }, [pathname]);

  return <>{children}</>;
}
