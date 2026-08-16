import type { ReactNode } from "react";
import { DashboardLayout } from "@/shared/components";

interface DashboardRootLayoutProps {
  children: ReactNode;
}

export default function DashboardRootLayout({ children }: DashboardRootLayoutProps) {
  return <DashboardLayout>{children}</DashboardLayout>;
}
