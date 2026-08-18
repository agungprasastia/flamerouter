import type { NextRequest } from "next/server";
import { proxy as dashboardProxy } from "./dashboardGuard";

export default async function proxy(request: NextRequest) {
  return dashboardProxy(request);
}

export const config = {
  matcher: [
    "/((?!api|v1|v1beta|codex|_next/static|_next/image|favicon\\.ico|favicon\\.svg).*)",
  ],
};
