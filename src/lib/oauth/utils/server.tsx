import http from "http";
import type { AddressInfo } from "net";
import { URL } from "url";
import {
  CODEX_CONFIG,
  TRAE_CONFIG,
  WINDSURF_CONFIG,
  ZED_HOSTED_CONFIG,
} from "../constants/oauth";

export interface LocalServerResult {
  server: http.Server;
  port: number;
  close: () => void;
}

export interface CodexSessionData {
  codeVerifier: string;
  redirectUri: string;
  status: "pending" | "done" | "error";
  createdAt: number;
  connectionId?: string;
  email?: string | null;
  error?: string;
}

export interface TraeSessionData {
  state: string;
  status: "pending" | "done" | "error";
  createdAt: number;
  connectionId?: string;
  email?: string | null;
  error?: string;
}

export interface WindsurfSessionData {
  state: string;
  status: "pending" | "done" | "error";
  createdAt: number;
  connectionId?: string;
  email?: string | null;
  error?: string;
}

export interface ZedSessionData {
  state: string;
  codeVerifier: string;
  status: "pending" | "done" | "error";
  createdAt: number;
  connectionId?: string;
  email?: string | null;
  error?: string;
}

export interface ProxyStartResult {
  success: boolean;
  reason?: string;
  port?: number | null;
  callbackUrl?: string;
}

// Loopback origin guard for local callback proxies.
// Legit OAuth redirects are top-level navigations (no `Origin` header); a cross-site
// page issuing `fetch(..., {mode:"no-cors"})` to scan + hit 127.0.0.1 always sends
// `Origin: https://attacker`. Reject any non-loopback Origin to block login-CSRF.
function isLoopbackOrigin(origin?: string): boolean {
  if (!origin) return true; // navigation redirect — allow
  return /^http:\/\/(127\.0\.0\.1|localhost)(:\d+)?$/.test(origin);
}

/**
 * Start a local HTTP server to receive OAuth callback
 * @param {Function} onCallback - Called with query params when callback received
 * @param {number} fixedPort - Optional fixed port number (default: random)
 * @returns {Promise<{server: http.Server, port: number, close: Function}>}
 */
export function startLocalServer(
  onCallback: (params: Record<string, string>) => void,
  fixedPort: number | null = null,
): Promise<LocalServerResult> {
  return new Promise((resolve, reject) => {
    const server = http.createServer((req, res) => {
      const url = new URL(req.url || "/", "http://localhost");

      if (url.pathname === "/callback" || url.pathname === "/auth/callback") {
        const params = Object.fromEntries(url.searchParams);

        // Send success response to browser with auto-close attempt
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Authentication Successful</title>
  <style>
    body { font-family: system-ui; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f5f5f5; }
    .container { text-align: center; padding: 2rem; background: white; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
    .success { color: #22c55e; font-size: 3rem; }
    h1 { margin: 1rem 0; }
    p { color: #666; }
    #countdown { font-weight: bold; }
  </style>
</head>
<body>
  <div class="container">
    <div class="success">&#10003;</div>
    <h1>Authentication Successful</h1>
    <p id="message">Closing in <span id="countdown">3</span> seconds...</p>
  </div>
  <script>
    let count = 3;
    const countdown = document.getElementById("countdown");
    const message = document.getElementById("message");
    const timer = setInterval(() => {
      count--;
      countdown.textContent = count;
      if (count <= 0) {
        clearInterval(timer);
        window.close();
        setTimeout(() => {
          message.textContent = "Please close this tab manually.";
        }, 500);
      }
    }, 1000);
  </script>
</body>
</html>`);

        // Call callback with params
        onCallback(params);
      } else {
        res.writeHead(404);
        res.end("Not found");
      }
    });

    // Listen on fixed port or find available port
    const portToUse = fixedPort || 0;
    server.listen(portToUse, "127.0.0.1", () => {
      const addr = server.address() as AddressInfo | null;
      const port = addr ? addr.port : portToUse;
      resolve({
        server,
        port,
        close: () => server.close(),
      });
    });

    server.on("error", (err: Error & { code?: string }) => {
      if (err.code === "EADDRINUSE" && fixedPort) {
        reject(
          new Error(
            `Port ${fixedPort} is already in use. Please close other applications using this port.`,
          ),
        );
      } else {
        reject(err);
      }
    });
  });
}

export interface CallbackWaiter extends Promise<Record<string, string>> {
  __onCallback?: (params: Record<string, string>) => void;
}

/**
 * Wait for callback with timeout
 * @param {number} timeoutMs - Timeout in milliseconds
 * @returns {Promise<Object>} - Callback params
 */
export function waitForCallback(timeoutMs = 300000): CallbackWaiter {
  let onCallbackHandler: ((params: Record<string, string>) => void) | undefined;
  const promise = new Promise<Record<string, string>>((resolve, reject) => {
    let resolved = false;

    const timeout = setTimeout(() => {
      if (!resolved) {
        resolved = true;
        reject(new Error("Authentication timeout"));
      }
    }, timeoutMs);

    const onCallback = (params: Record<string, string>) => {
      if (!resolved) {
        resolved = true;
        clearTimeout(timeout);
        resolve(params);
      }
    };

    onCallbackHandler = onCallback;
  }) as CallbackWaiter;

  promise.__onCallback = onCallbackHandler;
  return promise;
}

// Singleton proxy server for Codex OAuth callback on fixed port
let codexProxyServer: http.Server | null = null;
let codexProxyTimeout: NodeJS.Timeout | null = null;

const CODEX_PROXY_TIMEOUT_MS = 300000; // 5 minutes
const CODEX_PORT =
  (CODEX_CONFIG as unknown as { fixedPort?: number })?.fixedPort || 1455;

// Pending exchange sessions keyed by state — used by server-side exchange mode
const pendingExchanges = new Map<string, CodexSessionData>();

/**
 * Register a pending exchange session for server-side mode.
 * Modal client calls this before opening popup.
 */
export function registerCodexSession({
  state,
  codeVerifier,
  redirectUri,
}: {
  state: string;
  codeVerifier: string;
  redirectUri: string;
}): boolean {
  if (!state || !codeVerifier || !redirectUri) return false;
  pendingExchanges.set(state, {
    codeVerifier,
    redirectUri,
    status: "pending",
    createdAt: Date.now(),
  });
  return true;
}

/**
 * Read session status (modal polls this).
 */
export function getCodexSessionStatus(state: string): CodexSessionData | null {
  return pendingExchanges.get(state) || null;
}

/**
 * Clear a session (called after modal consumes status).
 */
export function clearCodexSession(state: string): void {
  pendingExchanges.delete(state);
}

function escapeHtml(str: unknown): string {
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function renderCodexResultPage(success: boolean, message: string): string {
  const color = success ? "#22c55e" : "#ef4444";
  const icon = success ? "&#10003;" : "&#10007;";
  const title = success ? "Authentication Successful" : "Authentication Failed";
  const safeMessage = escapeHtml(message);
  return `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>${title}</title>
<style>body{font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#f5f5f5}.c{text-align:center;padding:2rem;background:#fff;border-radius:8px;box-shadow:0 2px 10px rgba(0,0,0,.1)}.i{color:${color};font-size:3rem}h1{margin:1rem 0}p{color:#666}</style>
</head><body><div class="c"><div class="i">${icon}</div><h1>${title}</h1><p>${safeMessage}</p><p>Closing in <span id="cd">3</span>s...</p>
<script>let n=3;const c=document.getElementById("cd");const t=setInterval(()=>{n--;c.textContent=n;if(n<=0){clearInterval(t);window.close();}},1000);</script>
</div></body></html>`;
}

/**
 * Start Codex proxy on fixed port 1455.
 * Mode A (server-side): if any session was registered, proxy auto-exchanges + saves DB.
 * Mode B (channel fallback): if no session, proxy 302 redirects to app port for legacy channel-based flow.
 */
export function startCodexProxy(appPort: number | string): Promise<ProxyStartResult> {
  return new Promise((resolve) => {
    if (codexProxyServer) {
      resolve({ success: true });
      return;
    }

    const server = http.createServer(async (req, res) => {
      const url = new URL(req.url || "/", "http://localhost");

      if (url.pathname !== "/callback" && url.pathname !== "/auth/callback") {
        res.writeHead(404);
        res.end("Not found");
        return;
      }

      const code = url.searchParams.get("code");
      const state = url.searchParams.get("state");
      const errorParam = url.searchParams.get("error");
      const session = state ? pendingExchanges.get(state) : null;

      // Mode A: server-side exchange (session registered)
      if (session) {
        try {
          if (errorParam) {
            throw new Error(
              url.searchParams.get("error_description") || errorParam,
            );
          }
          if (!code) throw new Error("No authorization code received");

          // Lazy import to avoid circular deps
          const { exchangeTokens } = await import("../providers");
          const { createProviderConnection } = await import("@/models");

          const tokenData = (await exchangeTokens(
            "codex",
            code,
            session.redirectUri,
            session.codeVerifier,
            state ?? undefined,
          )) as Record<string, unknown> & { expiresIn?: number };
          const connection = (await createProviderConnection({
            provider: "codex",
            authType: "oauth",
            ...tokenData,
            expiresAt:
              typeof tokenData.expiresIn === "number"
                ? new Date(Date.now() + tokenData.expiresIn * 1000).toISOString()
                : null,
            testStatus: "active",
          })) as { id?: string; email?: string | null };

          session.status = "done";
          session.connectionId = connection.id;
          session.email = connection.email;

          res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
          res.end(renderCodexResultPage(true, "You can close this window."));
        } catch (err: unknown) {
          const message = err instanceof Error ? err.message : String(err);
          session.status = "error";
          session.error = message;
          res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
          res.end(renderCodexResultPage(false, message));
        } finally {
          stopCodexProxy();
        }
        return;
      }

      // Mode B: legacy channel fallback — 302 redirect to app /callback
      const redirectUrl = `http://localhost:${appPort}/callback${url.search}`;
      res.writeHead(302, { Location: redirectUrl });
      res.end();
      stopCodexProxy();
    });

    server.listen(CODEX_PORT, "127.0.0.1", () => {
      codexProxyServer = server;
      codexProxyTimeout = setTimeout(
        () => stopCodexProxy(),
        CODEX_PROXY_TIMEOUT_MS,
      );
      resolve({ success: true });
    });

    server.on("error", (err: Error & { code?: string }) => {
      if (err.code === "EADDRINUSE") {
        resolve({ success: false, reason: "port_busy" });
      } else {
        resolve({ success: false, reason: err.message });
      }
    });
  });
}

/**
 * Stop the Codex proxy server and cleanup
 */
export function stopCodexProxy(): void {
  if (codexProxyTimeout) {
    clearTimeout(codexProxyTimeout);
    codexProxyTimeout = null;
  }
  if (codexProxyServer) {
    codexProxyServer.close();
    codexProxyServer = null;
  }
}

// ───────────────────────────────────────────────────────────────────────────
// xAI fixed-port proxy on 127.0.0.1:56121
// Same shape as the Codex proxy. Kept as a parallel implementation rather than
// generalizing the Codex one to keep the codex hot-path byte-equivalent.
// ───────────────────────────────────────────────────────────────────────────

let xaiProxyServer: http.Server | null = null;
let xaiProxyTimeout: NodeJS.Timeout | null = null;
const XAI_PROXY_TIMEOUT_MS = 300000; // 5 minutes
const XAI_PROXY_PORT = 56121;
const xaiPendingExchanges = new Map<string, CodexSessionData>();

export function registerXaiSession({
  state,
  codeVerifier,
  redirectUri,
}: {
  state: string;
  codeVerifier: string;
  redirectUri: string;
}): boolean {
  if (!state || !codeVerifier || !redirectUri) return false;
  xaiPendingExchanges.set(state, {
    codeVerifier,
    redirectUri,
    status: "pending",
    createdAt: Date.now(),
  });
  return true;
}

export function getXaiSessionStatus(state: string): CodexSessionData | null {
  return xaiPendingExchanges.get(state) || null;
}

export function clearXaiSession(state: string): void {
  xaiPendingExchanges.delete(state);
}

function renderXaiResultPage(success: boolean, message: string): string {
  return renderCodexResultPage(success, message);
}

/**
 * Start xAI proxy on fixed port 56121.
 * Mode A (server-side): if any session was registered, proxy auto-exchanges + saves DB.
 * Mode B (channel fallback): if no session, proxy 302 redirects to app port.
 */
export function startXaiProxy(appPort: number | string): Promise<ProxyStartResult> {
  return new Promise((resolve) => {
    if (xaiProxyServer) {
      resolve({ success: true });
      return;
    }

    const server = http.createServer(async (req, res) => {
      const url = new URL(req.url || "/", "http://localhost");
      if (url.pathname !== "/callback" && url.pathname !== "/auth/callback") {
        res.writeHead(404);
        res.end("Not found");
        return;
      }

      const code = url.searchParams.get("code");
      const state = url.searchParams.get("state");
      const errorParam = url.searchParams.get("error");
      const session = state ? xaiPendingExchanges.get(state) : null;

      // Mode A: server-side exchange
      if (session) {
        try {
          if (errorParam) {
            throw new Error(
              url.searchParams.get("error_description") || errorParam,
            );
          }
          if (!code) throw new Error("No authorization code received");

          const { exchangeTokens } = await import("../providers");
          const { createProviderConnection } = await import("@/models");

          const tokenData = (await exchangeTokens(
            "xai",
            code,
            session.redirectUri,
            session.codeVerifier,
            state ?? undefined,
          )) as Record<string, unknown> & { expiresIn?: number };
          const connection = (await createProviderConnection({
            provider: "xai",
            authType: "oauth",
            ...tokenData,
            expiresAt:
              typeof tokenData.expiresIn === "number"
                ? new Date(Date.now() + tokenData.expiresIn * 1000).toISOString()
                : null,
            testStatus: "active",
          })) as { id?: string; email?: string | null };

          session.status = "done";
          session.connectionId = connection.id;
          session.email = connection.email;

          res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
          res.end(renderXaiResultPage(true, "You can close this window."));
        } catch (err: unknown) {
          const message = err instanceof Error ? err.message : String(err);
          session.status = "error";
          session.error = message;
          res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
          res.end(renderXaiResultPage(false, message));
        } finally {
          stopXaiProxy();
        }
        return;
      }

      // Mode B: legacy fallback redirect
      const redirectUrl = `http://localhost:${appPort}/callback${url.search}`;
      res.writeHead(302, { Location: redirectUrl });
      res.end();
      stopXaiProxy();
    });

    server.listen(XAI_PROXY_PORT, "127.0.0.1", () => {
      xaiProxyServer = server;
      xaiProxyTimeout = setTimeout(() => stopXaiProxy(), XAI_PROXY_TIMEOUT_MS);
      resolve({ success: true });
    });

    server.on("error", (err: Error & { code?: string }) => {
      if (err.code === "EADDRINUSE") {
        resolve({ success: false, reason: "port_busy" });
      } else {
        resolve({ success: false, reason: err.message });
      }
    });
  });
}

export function stopXaiProxy(): void {
  if (xaiProxyTimeout) {
    clearTimeout(xaiProxyTimeout);
    xaiProxyTimeout = null;
  }
  if (xaiProxyServer) {
    xaiProxyServer.close();
    xaiProxyServer = null;
  }
}

// ───────────────────────────────────────────────────────────────────────────
// Trae dynamic-port proxy. Singleton session (one connect at a time per provider).
// Callback path = /callback with params refreshToken + loginHost.
// ───────────────────────────────────────────────────────────────────────────

let traeProxyServer: http.Server | null = null;
let traeProxyTimeout: NodeJS.Timeout | null = null;
let traeProxyPort: number | null = null;
let traeSession: TraeSessionData | null = null;

export function registerTraeSession({ state }: { state: string }): boolean {
  if (!state) return false;
  traeSession = { state, status: "pending", createdAt: Date.now() };
  return true;
}
export function getTraeSessionStatus(state?: string): TraeSessionData | null {
  if (!traeSession) return null;
  if (state && traeSession.state !== state) return null;
  return traeSession;
}
export function clearTraeSession(state?: string): void {
  if (!state || (traeSession && traeSession.state === state))
    traeSession = null;
}

export function startTraeProxy(): Promise<ProxyStartResult> {
  return new Promise((resolve) => {
    if (traeProxyServer) {
      resolve({
        success: true,
        port: traeProxyPort,
        callbackUrl: `http://127.0.0.1:${traeProxyPort}${TRAE_CONFIG.callbackPath}`,
      });
      return;
    }
    const server = http.createServer(async (req, res) => {
      const url = new URL(req.url || "/", "http://localhost");
      if (
        url.pathname !== TRAE_CONFIG.callbackPath &&
        url.pathname !== "/auth/callback"
      ) {
        res.writeHead(404);
        res.end("Not found");
        return;
      }
      const session = traeSession;
      if (!session) {
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, "No active Trae login session"));
        return;
      }
      // Anti-CSRF: reject cross-origin fetches (legit redirects send no Origin),
      // and reject state mismatch when state is present.
      if (!isLoopbackOrigin(req.headers.origin)) {
        res.writeHead(403, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, "Cross-origin callback rejected"));
        return;
      }
      const cbState = url.searchParams.get("state");
      if (cbState && session.state && cbState !== session.state) {
        session.status = "error";
        session.error = "Trae callback state mismatch";
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, session.error));
        stopTraeProxy();
        return;
      }
      // Pass the raw callback query to exchangeTokens → parseTraeCallback
      const rawCallback = `${url.pathname}?${url.searchParams.toString()}`;
      try {
        const { exchangeTokens } = await import("../providers");
        const { createProviderConnection } = await import("@/models");
        const tokenData = (await exchangeTokens(
          "trae",
          rawCallback,
          "",
          "",
          session.state,
        )) as Record<string, unknown> & { expiresIn?: number };
        const connection = (await createProviderConnection({
          provider: "trae",
          authType: "oauth",
          ...tokenData,
          expiresAt:
            typeof tokenData.expiresIn === "number"
              ? new Date(Date.now() + tokenData.expiresIn * 1000).toISOString()
              : null,
          testStatus: "active",
        })) as { id?: string; email?: string | null };
        session.status = "done";
        session.connectionId = connection.id;
        session.email = connection.email;
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(true, "You can close this window."));
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : String(err);
        session.status = "error";
        session.error = message;
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, message));
      } finally {
        stopTraeProxy();
      }
    });
    server.listen(0, "127.0.0.1", () => {
      traeProxyServer = server;
      const addr = server.address() as AddressInfo | null;
      traeProxyPort = addr ? addr.port : null;
      traeProxyTimeout = setTimeout(
        () => stopTraeProxy(),
        TRAE_CONFIG.oauthTimeoutMs,
      );
      resolve({
        success: true,
        port: traeProxyPort,
        callbackUrl: `http://127.0.0.1:${traeProxyPort}${TRAE_CONFIG.callbackPath}`,
      });
    });
    server.on("error", (err: Error) =>
      resolve({ success: false, reason: err.message }),
    );
  });
}

export function stopTraeProxy(): void {
  if (traeProxyTimeout) {
    clearTimeout(traeProxyTimeout);
    traeProxyTimeout = null;
  }
  if (traeProxyServer) {
    traeProxyServer.close();
    traeProxyServer = null;
  }
  traeProxyPort = null;
}

// ───────────────────────────────────────────────────────────────────────────
// Windsurf dynamic-port proxy. Singleton session.
// Callback path = /windsurf-auth-callback with params access_token (firebase JWT) + state.
// ───────────────────────────────────────────────────────────────────────────

let windsurfProxyServer: http.Server | null = null;
let windsurfProxyTimeout: NodeJS.Timeout | null = null;
let windsurfProxyPort: number | null = null;
let windsurfSession: WindsurfSessionData | null = null;

export function registerWindsurfSession({ state }: { state: string }): boolean {
  if (!state) return false;
  windsurfSession = { state, status: "pending", createdAt: Date.now() };
  return true;
}
export function getWindsurfSessionStatus(state?: string): WindsurfSessionData | null {
  if (!windsurfSession) return null;
  if (state && windsurfSession.state !== state) return null;
  return windsurfSession;
}
export function clearWindsurfSession(state?: string): void {
  if (!state || (windsurfSession && windsurfSession.state === state))
    windsurfSession = null;
}

export function startWindsurfProxy(): Promise<ProxyStartResult> {
  return new Promise((resolve) => {
    if (windsurfProxyServer) {
      resolve({
        success: true,
        port: windsurfProxyPort,
        callbackUrl: `http://127.0.0.1:${windsurfProxyPort}${WINDSURF_CONFIG.callbackPath}`,
      });
      return;
    }
    const server = http.createServer(async (req, res) => {
      const url = new URL(req.url || "/", "http://localhost");
      if (url.pathname !== WINDSURF_CONFIG.callbackPath) {
        res.writeHead(404);
        res.end("Not found");
        return;
      }
      const session = windsurfSession;
      if (!session) {
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(
          renderCodexResultPage(false, "No active Windsurf login session"),
        );
        return;
      }
      // Anti-CSRF: reject cross-origin fetches, and require state present + matching.
      if (!isLoopbackOrigin(req.headers.origin)) {
        res.writeHead(403, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, "Cross-origin callback rejected"));
        return;
      }
      const cbState = url.searchParams.get("state");
      if (!cbState || !session.state || cbState !== session.state) {
        session.status = "error";
        session.error = "Windsurf callback state mismatch";
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, session.error));
        stopWindsurfProxy();
        return;
      }
      const rawCallback = `${url.pathname}?${url.searchParams.toString()}`;
      try {
        const { exchangeTokens } = await import("../providers");
        const { createProviderConnection } = await import("@/models");
        const tokenData = (await exchangeTokens(
          "windsurf",
          rawCallback,
          "",
          "",
          session.state,
        )) as Record<string, unknown>;
        const connection = (await createProviderConnection({
          provider: "windsurf",
          authType: "api_key",
          ...tokenData,
          testStatus: "active",
        })) as { id?: string; email?: string | null };
        session.status = "done";
        session.connectionId = connection.id;
        session.email = connection.email;
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(true, "You can close this window."));
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : String(err);
        session.status = "error";
        session.error = message;
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, message));
      } finally {
        stopWindsurfProxy();
      }
    });
    server.listen(0, "127.0.0.1", () => {
      windsurfProxyServer = server;
      const addr = server.address() as AddressInfo | null;
      windsurfProxyPort = addr ? addr.port : null;
      windsurfProxyTimeout = setTimeout(
        () => stopWindsurfProxy(),
        WINDSURF_CONFIG.oauthTimeoutMs,
      );
      resolve({
        success: true,
        port: windsurfProxyPort,
        callbackUrl: `http://127.0.0.1:${windsurfProxyPort}${WINDSURF_CONFIG.callbackPath}`,
      });
    });
    server.on("error", (err: Error) =>
      resolve({ success: false, reason: err.message }),
    );
  });
}

export function stopWindsurfProxy(): void {
  if (windsurfProxyTimeout) {
    clearTimeout(windsurfProxyTimeout);
    windsurfProxyTimeout = null;
  }
  if (windsurfProxyServer) {
    windsurfProxyServer.close();
    windsurfProxyServer = null;
  }
  windsurfProxyPort = null;
}

// ───────────────────────────────────────────────────────────────────────────
// Zed RSA native-app proxy. Singleton session.
// Callback: GET http://127.0.0.1:<port>/?user_id=...&access_token=<RSA-encrypted>
// The proxy decrypts the access token using the private key stored in session.codeVerifier.
// ───────────────────────────────────────────────────────────────────────────

let zedProxyServer: http.Server | null = null;
let zedProxyTimeout: NodeJS.Timeout | null = null;
let zedProxyPort: number | null = null;
let zedSession: ZedSessionData | null = null;

export function registerZedSession({
  state,
  codeVerifier,
}: {
  state: string;
  codeVerifier: string;
}): boolean {
  if (!state || !codeVerifier) return false;
  zedSession = {
    state,
    codeVerifier,
    status: "pending",
    createdAt: Date.now(),
  };
  return true;
}
export function getZedSessionStatus(state?: string): ZedSessionData | null {
  if (!zedSession) return null;
  if (state && zedSession.state !== state) return null;
  return zedSession;
}
export function clearZedSession(state?: string): void {
  if (!state || (zedSession && zedSession.state === state)) zedSession = null;
}

export function startZedProxy(preferredPort = 0): Promise<ProxyStartResult> {
  return new Promise((resolve) => {
    if (zedProxyServer) {
      resolve({
        success: true,
        port: zedProxyPort,
        callbackUrl: `http://127.0.0.1:${zedProxyPort}/`,
      });
      return;
    }
    const server = http.createServer(async (req, res) => {
      const url = new URL(req.url || "/", "http://localhost");
      // Log path + redacted params (access_token is the RSA-encrypted credential).
      const redacted = Object.fromEntries(url.searchParams);
      for (const k of ["access_token", "user_id", "code_verifier", "state"]) {
        if (redacted[k]) redacted[k] = "<redacted>";
      }
      console.log(
        "[Zed proxy]",
        req.method,
        url.pathname,
        JSON.stringify(redacted),
      );
      if (url.pathname !== "/" && url.pathname !== "/callback") {
        res.writeHead(404);
        res.end("Not found");
        return;
      }
      const session = zedSession;
      if (!session) {
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, "No active Zed login session"));
        return;
      }
      // Anti-CSRF: Zed tokens are RSA-encrypted to our keypair so they can't be
      // forged cross-site, but still reject cross-origin fetches for defense-in-depth.
      if (!isLoopbackOrigin(req.headers.origin)) {
        res.writeHead(403, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, "Cross-origin callback rejected"));
        return;
      }
      // Pass raw callback path+query to exchangeTokens → parseZedCallbackPayload.
      // codeVerifier carries the encoded RSA private key for decryption.
      const rawCallback = url.search
        ? `${url.pathname}?${url.searchParams.toString()}`
        : url.pathname;
      try {
        const { exchangeTokens } = await import("../providers");
        const { createProviderConnection } = await import("@/models");
        const tokenData = (await exchangeTokens(
          "zed",
          rawCallback,
          "",
          session.codeVerifier,
          session.state,
        )) as Record<string, unknown>;
        const connection = (await createProviderConnection({
          provider: "zed",
          authType: "oauth",
          ...tokenData,
          testStatus: "active",
        })) as { id?: string; email?: string | null };
        session.status = "done";
        session.connectionId = connection.id;
        session.email = connection.email;
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(true, "You can close this window."));
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : String(err);
        session.status = "error";
        session.error = message;
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end(renderCodexResultPage(false, message));
      } finally {
        stopZedProxy();
      }
    });
    const tryPort = Number(preferredPort) || 0;
    server.on("error", (err: Error & { code?: string }) => {
      // If the preferred port (e.g. 58443) is busy, fall back to a random port.
      if (err.code === "EADDRINUSE" && tryPort !== 0) {
        console.log(`[Zed proxy] port ${tryPort} busy, falling back to random`);
        server.listen(0, "127.0.0.1", () => {
          zedProxyServer = server;
          const addr = server.address() as AddressInfo | null;
          zedProxyPort = addr ? addr.port : null;
          zedProxyTimeout = setTimeout(
            () => stopZedProxy(),
            ZED_HOSTED_CONFIG.oauthTimeoutMs,
          );
          console.log(`[Zed proxy] listening on random port ${zedProxyPort}`);
          resolve({
            success: true,
            port: zedProxyPort,
            callbackUrl: `http://127.0.0.1:${zedProxyPort}/`,
          });
        });
      } else {
        console.log(`[Zed proxy] listen error: ${err.message}`);
        resolve({ success: false, reason: err.message });
      }
    });
    server.listen(tryPort, "127.0.0.1", () => {
      zedProxyServer = server;
      const addr = server.address() as AddressInfo | null;
      zedProxyPort = addr ? addr.port : null;
      zedProxyTimeout = setTimeout(() => {
        console.log("[Zed proxy] timeout, stopping");
        stopZedProxy();
      }, ZED_HOSTED_CONFIG.oauthTimeoutMs);
      console.log(`[Zed proxy] listening on port ${zedProxyPort}`);
      resolve({
        success: true,
        port: zedProxyPort,
        callbackUrl: `http://127.0.0.1:${zedProxyPort}/`,
      });
    });
  });
}

export function stopZedProxy(): void {
  console.log(`[Zed proxy] stopping (port ${zedProxyPort || "-"})`);
  if (zedProxyTimeout) {
    clearTimeout(zedProxyTimeout);
    zedProxyTimeout = null;
  }
  if (zedProxyServer) {
    zedProxyServer.close();
    zedProxyServer = null;
  }
  zedProxyPort = null;
}
