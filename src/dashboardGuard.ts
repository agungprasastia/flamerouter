import { NextRequest, NextResponse } from "next/server";
import { verifyDashboardAuthToken } from "@/lib/auth/dashboardSession";

const BACKEND_URL = process.env.BACKEND_URL || "http://127.0.0.1:20130";

async function isRequireLoginDisabled(): Promise<boolean> {
  try {
    const res = await fetch(`${BACKEND_URL}/api/settings/require-login`, {
      cache: "no-store",
    });
    if (res.ok) {
      const data = await res.json();
      return data.requireLogin === false;
    }
  } catch {
    // on error default to requiring login
  }
  return false;
}

export async function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl;

  if (pathname.startsWith("/dashboard")) {
    if (await isRequireLoginDisabled()) {
      return NextResponse.next();
    }

    const token = request.cookies.get("auth_token")?.value;
    if (token) {
      if (await verifyDashboardAuthToken(token)) {
        return NextResponse.next();
      }
    }
    return NextResponse.redirect(new URL("/login", request.url));
  }

  if (pathname === "/login") {
    if (await isRequireLoginDisabled()) {
      return NextResponse.redirect(new URL("/dashboard", request.url));
    }

    const token = request.cookies.get("auth_token")?.value;
    if (token && (await verifyDashboardAuthToken(token))) {
      return NextResponse.redirect(new URL("/dashboard", request.url));
    }
  }

  if (pathname === "/") {
    return NextResponse.redirect(new URL("/dashboard", request.url));
  }

  return NextResponse.next();
}
