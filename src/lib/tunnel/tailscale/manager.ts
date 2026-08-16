import { loadState, generateShortId } from "../shared/state";
import {
  startFunnel,
  stopFunnel,
  isTailscaleRunning,
  isTailscaleRunningStrict,
  isTailscaleLoggedIn,
  isTailscaleLoggedInStrict,
  startLogin,
  startDaemonWithPassword,
  provisionCert,
} from "./tailscale";
import { waitForHealth } from "./healthCheck";
import { getSettings, updateSettings } from "@/lib/localDb";
import {
  getCachedPassword,
  loadEncryptedPassword,
  initDbHooks,
} from "@/mitm/manager";

initDbHooks(getSettings, updateSettings);

interface TailscaleServiceState {
  cancelToken: { cancelled: boolean };
  spawnInProgress: boolean;
  lastRestartAt: number;
  activeLocalPort: number | string | null;
}

const svc: TailscaleServiceState = {
  cancelToken: { cancelled: false },
  spawnInProgress: false,
  lastRestartAt: 0,
  activeLocalPort: null,
};

export function getTailscaleService() {
  return svc;
}
export function isTailscaleReconnecting(): boolean {
  return svc.spawnInProgress;
}

function throwIfCancelled(token: { cancelled: boolean }): void {
  if (token.cancelled) throw new Error("tailscale cancelled");
}

export interface EnableTailscaleResult {
  success: boolean;
  needsLogin?: boolean;
  authUrl?: string | null;
  funnelNotEnabled?: boolean;
  enableUrl?: string | null;
  error?: string;
  tunnelUrl?: string;
}

export async function enableTailscale(localPort: number | string = 20129): Promise<EnableTailscaleResult> {
  console.log(`[Tailscale] enable start (port=${localPort})`);
  svc.cancelToken = { cancelled: false };
  svc.activeLocalPort = localPort;
  svc.spawnInProgress = true;
  const token = svc.cancelToken;

  try {
    const sudoPass =
      getCachedPassword() || (await loadEncryptedPassword()) || "";
    await startDaemonWithPassword(sudoPass);
    console.log("[Tailscale] daemon ready");
    throwIfCancelled(token);

    const existing = loadState<{ shortId?: string }>();
    const shortId = existing?.shortId || generateShortId();
    const tsHostname = shortId;

    const loggedIn = await isTailscaleLoggedInStrict();
    console.log(`[Tailscale] loggedIn=${loggedIn}`);
    if (!loggedIn) {
      const loginResult = (await startLogin(tsHostname)) as { authUrl?: string | null };
      if (loginResult?.authUrl) {
        console.log(`[Tailscale] needs login, authUrl=${loginResult.authUrl}`);
        return {
          success: false,
          needsLogin: true,
          authUrl: loginResult.authUrl,
        };
      }
      console.log("[Tailscale] login resolved alreadyLoggedIn");
    }
    throwIfCancelled(token);

    stopFunnel();
    let result: { funnelNotEnabled?: boolean; enableUrl?: string | null; tunnelUrl?: string } | undefined;
    try {
      console.log("[Tailscale] starting funnel");
      result = (await startFunnel(localPort)) as { funnelNotEnabled?: boolean; enableUrl?: string | null; tunnelUrl?: string };
    } catch (e: unknown) {
      const err = e as Error;
      console.error(`[Tailscale] funnel error: ${err.message}`);
      // Daemon not logged in / not ready → auto-trigger login flow so user stays in-app
      if (
        /NoState|unexpected state|not logged in|Logged ?out|NeedsLogin/i.test(
          err.message || "",
        )
      ) {
        console.log("[Tailscale] retry via startLogin");
        const loginResult = (await startLogin(tsHostname)) as { authUrl?: string | null };
        if (loginResult?.authUrl)
          return {
            success: false,
            needsLogin: true,
            authUrl: loginResult.authUrl,
          };
      }
      throw e;
    }
    throwIfCancelled(token);

    if (result?.funnelNotEnabled) {
      console.log(
        `[Tailscale] funnel not enabled, enableUrl=${result.enableUrl}`,
      );
      return {
        success: false,
        funnelNotEnabled: true,
        enableUrl: result.enableUrl,
      };
    }

    // Strict probe: bypass cache so we don't false-negative on first invocation
    if (
      !(await isTailscaleLoggedInStrict()) ||
      !(await isTailscaleRunningStrict())
    ) {
      console.error("[Tailscale] strict probe failed (device removed?)");
      stopFunnel();
      return {
        success: false,
        error:
          "Tailscale not connected. Device may have been removed. Please re-login.",
      };
    }

    if (!result?.tunnelUrl) {
      throw new Error("Tailscale funnel did not produce a URL");
    }

    await updateSettings({
      tailscaleEnabled: true,
      tailscaleUrl: result.tunnelUrl,
    });
    console.log(`[Tailscale] funnel up: ${result.tunnelUrl}`);

    // Provision TLS cert so Funnel can serve HTTPS (non-fatal if fails)
    const hostname = new URL(result.tunnelUrl).hostname;
    await provisionCert(hostname);

    // Verify funnel serves /api/health — timeout is non-fatal (DNS may still be propagating)
    let reachableNow = false;
    try {
      await waitForHealth(result.tunnelUrl, token);
      reachableNow = true;
    } catch (he: unknown) {
      const hErr = he as Error;
      if (!hErr.message.startsWith("Health check timeout")) throw he;
      console.warn(
        `[Tailscale] health check timed out, will retry via watchdog`,
      );
    }
    console.log(`[Tailscale] enable success (reachable=${reachableNow})`);
    return { success: true, tunnelUrl: result.tunnelUrl };
  } catch (e: unknown) {
    const err = e as Error;
    console.error(`[Tailscale] enable error: ${err.message}`);
    throw e;
  } finally {
    svc.spawnInProgress = false;
  }
}

export async function disableTailscale(): Promise<{ success: boolean }> {
  console.log("[Tailscale] disable");
  svc.cancelToken.cancelled = true;
  stopFunnel();
  await updateSettings({ tailscaleEnabled: false, tailscaleUrl: "" });
  return { success: true };
}

export interface TailscaleStatusResult {
  enabled: boolean;
  settingsEnabled: boolean;
  tunnelUrl: string;
  running: boolean;
  loggedIn: boolean;
}

export async function getTailscaleStatus(): Promise<TailscaleStatusResult> {
  const settings = await getSettings();
  const settingsEnabled = settings.tailscaleEnabled === true;
  const tunnelUrl = (settings.tailscaleUrl as string) || "";
  // Skip probes entirely when disabled; check login before running (device removed = not logged in)
  const loggedIn = settingsEnabled ? isTailscaleLoggedIn() : false;
  const running = loggedIn ? isTailscaleRunning() : false;
  return {
    enabled: settingsEnabled && running,
    settingsEnabled,
    tunnelUrl,
    running,
    loggedIn,
  };
}
