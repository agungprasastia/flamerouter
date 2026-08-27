"use client";

import { useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";

export type OAuthCallbackStatus = "processing" | "success" | "done" | "manual";

/**
 * Custom hook to handle OAuth callback search params and relay authentication details
 * via postMessage (opener), BroadcastChannel, or localStorage fallback.
 */
export function useOAuthCallback() {
  const searchParams = useSearchParams();
  const [status, setStatus] = useState<OAuthCallbackStatus>("processing");

  useEffect(() => {
    const code = searchParams.get("code");
    const token = searchParams.get("token");
    const state = searchParams.get("state");
    const error = searchParams.get("error");
    const errorDescription = searchParams.get("error_description");

    const callbackData = {
      code,
      token,
      state,
      error,
      errorDescription,
      fullUrl: window.location.href,
    };

    let relayed = false;

    // Trusted origins that may receive this callback. The OAuth code/state
    // must only be relayed to the dashboard window we expect to be the opener
    // (same origin) or the Codex helper that listens on a fixed loopback port.
    // Any other origin is treated as hostile (drive-by attacker that opened
    // the popup against the well-known redirect_uri to phish the code).
    const expectedOrigins = [
      window.location.origin, // Same origin (for most providers)
      "http://localhost:20129",
      "http://localhost:20130",
      "http://127.0.0.1:20129",
      "http://127.0.0.1:20130",
      "http://localhost:1455", // Codex specific port
    ];

    // Method 1: postMessage to opener (popup mode)
    // Send once per expected origin. The browser delivers the message only
    // when the opener's origin matches the targetOrigin we pass — using "*"
    // here would leak the code/state to any opener (e.g. an attacker page
    // that opened this URL in a popup), so iterate over the allowlist.
    if (window.opener) {
      for (const origin of expectedOrigins) {
        try {
          window.opener.postMessage(
            { type: "oauth_callback", data: callbackData },
            origin,
          );
          relayed = true;
        } catch (e) {
          console.log("postMessage failed:", e);
        }
      }
    }

    // Method 2: BroadcastChannel (same origin tabs)
    try {
      const channel = new BroadcastChannel("oauth_callback");
      channel.postMessage(callbackData);
      channel.close();
      relayed = true;
    } catch (e) {
      console.log("BroadcastChannel failed:", e);
    }

    // Method 3: localStorage event (fallback)
    try {
      localStorage.setItem(
        "oauth_callback",
        JSON.stringify({ ...callbackData, timestamp: Date.now() }),
      );
      relayed = true;
    } catch (e) {
      console.log("localStorage failed:", e);
    }

    if (!(code || token || error)) {
      setTimeout(() => setStatus("manual"), 0);
      return;
    }

    setStatus("success");
    setTimeout(() => {
      window.close();
      setTimeout(() => setStatus("done"), 500);
    }, 1500);
  }, [searchParams]);

  return { status };
}
