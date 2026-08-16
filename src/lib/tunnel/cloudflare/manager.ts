import { loadState, saveState, generateShortId } from "../shared/state";
import {
  spawnQuickTunnel,
  killCloudflared,
  isCloudflaredRunning,
  setUnexpectedExitHandler,
} from "./cloudflared";
import { clearPid } from "./pid";
import { waitForHealth, probeUrlAlive } from "./healthCheck";
import { WORKER_URL } from "./config";
import { getSettings, updateSettings } from "@/lib/localDb";

interface TunnelState {
  shortId: string;
  tunnelUrl?: string | null;
}

const svc: {
  cancelToken: { cancelled: boolean };
  spawnInProgress: boolean;
  lastRestartAt: number;
  activeLocalPort: number | string | null;
} = {
  cancelToken: { cancelled: false },
  spawnInProgress: false,
  lastRestartAt: 0,
  activeLocalPort: null,
};

export function getTunnelService() {
  return svc;
}
export function isTunnelManuallyDisabled(): boolean {
  return svc.cancelToken.cancelled;
}
export function isTunnelReconnecting(): boolean {
  return svc.spawnInProgress;
}

let onUnexpectedExit: (() => void) | null = null;
export function setTunnelUnexpectedExitCallback(cb: (() => void) | null): void {
  onUnexpectedExit = cb;
}

async function registerTunnelUrl(shortId: string, tunnelUrl: string): Promise<void> {
  await fetch(`${WORKER_URL}/api/tunnel/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ shortId, tunnelUrl }),
  });
}

function throwIfCancelled(token: { cancelled: boolean }): void {
  if (token.cancelled) throw new Error("tunnel cancelled");
}

export interface EnableTunnelResult {
  success: boolean;
  tunnelUrl: string;
  shortId: string;
  publicUrl: string;
  alreadyRunning?: boolean;
}

export async function enableTunnel(localPort: number | string = 20129): Promise<EnableTunnelResult> {
  console.log(`[Tunnel] enable start (port=${localPort})`);
  svc.cancelToken = { cancelled: false };
  svc.activeLocalPort = localPort;
  svc.spawnInProgress = true;
  const token = svc.cancelToken;

  try {
    if (isCloudflaredRunning()) {
      const existing = loadState<TunnelState>();
      if (existing?.tunnelUrl && existing?.shortId) {
        const publicUrl = `https://r${existing.shortId}.abc-tunnel.us`;
        // Reuse only if BOTH direct + public URL alive (avoid stale socket after network change)
        const [directOk, publicOk] = await Promise.all([
          probeUrlAlive(existing.tunnelUrl),
          probeUrlAlive(publicUrl),
        ]);
        if (directOk && publicOk) {
          console.log(`[Tunnel] already running, reuse: ${existing.tunnelUrl}`);
          return {
            success: true,
            tunnelUrl: existing.tunnelUrl,
            shortId: existing.shortId,
            publicUrl,
            alreadyRunning: true,
          };
        }
        console.log(
          `[Tunnel] stale (direct=${directOk} public=${publicOk}), respawn`,
        );
      }
    }

    killCloudflared(localPort);
    console.log("[Tunnel] killed existing cloudflared");
    throwIfCancelled(token);

    const existing = loadState<TunnelState>();
    const shortId = existing?.shortId || generateShortId();

    const onUrlUpdate = async (url: string) => {
      if (token.cancelled) return;
      console.log(`[Tunnel] url updated: ${url}`);
      await registerTunnelUrl(shortId, url);
      saveState({ shortId, tunnelUrl: url });
      await updateSettings({ tunnelEnabled: true, tunnelUrl: url });
    };

    // Register exit handler BEFORE spawn so it fires even on early exit
    setUnexpectedExitHandler(() => {
      console.warn(
        "[Tunnel] cloudflared exited unexpectedly, scheduling respawn",
      );
      if (onUnexpectedExit) onUnexpectedExit();
    });

    const { tunnelUrl } = await spawnQuickTunnel(localPort, onUrlUpdate);
    console.log(`[Tunnel] spawned: ${tunnelUrl}`);
    throwIfCancelled(token);

    const publicUrl = `https://r${shortId}.abc-tunnel.us`;
    await registerTunnelUrl(shortId, tunnelUrl);
    saveState({ shortId, tunnelUrl });
    await updateSettings({ tunnelEnabled: true, tunnelUrl });
    console.log(
      `[Tunnel] registered shortId=${shortId} publicUrl=${publicUrl}`,
    );

    // Verify publicUrl first (worker route is reliable; direct *.trycloudflare.com DNS may lag)
    await waitForHealth(publicUrl, token);
    console.log("[Tunnel] public URL healthy");
    // Direct tunnel probe is best-effort: DNS for *.trycloudflare.com can be slow/blocked
    if (!(await probeUrlAlive(tunnelUrl))) {
      console.warn(
        "[Tunnel] direct URL not reachable yet, continuing via publicUrl",
      );
    } else {
      console.log("[Tunnel] direct URL healthy");
    }

    console.log("[Tunnel] enable success");
    return { success: true, tunnelUrl, shortId, publicUrl };
  } catch (e: unknown) {
    const err = e as Error;
    // Suppress noise when spawn was deliberately killed (restart/disable superseded it)
    if (!/cloudflared killed|tunnel cancelled/.test(err.message)) {
      console.error(`[Tunnel] enable error: ${err.message}`);
    }
    throw e;
  } finally {
    svc.spawnInProgress = false;
  }
}

export async function disableTunnel(): Promise<{ success: boolean }> {
  console.log("[Tunnel] disable");
  // Abort any in-flight enable so it cannot resurrect state after we clear it
  svc.cancelToken.cancelled = true;
  setUnexpectedExitHandler(null);

  try {
    if (svc.activeLocalPort) killCloudflared(svc.activeLocalPort);
  } catch (e: unknown) {
    const err = e as Error;
    console.warn(`[Tunnel] kill warn: ${err.message}`);
  }
  clearPid();

  const state = loadState<TunnelState>();
  if (state) saveState({ shortId: state.shortId, tunnelUrl: null });

  await updateSettings({ tunnelEnabled: false, tunnelUrl: "" });
  // Force-clear flags so a subsequent enable is not blocked by a stuck spawnInProgress
  svc.spawnInProgress = false;
  svc.activeLocalPort = null;
  return { success: true };
}

export interface TunnelStatusResult {
  enabled: boolean;
  settingsEnabled: boolean;
  tunnelUrl: string;
  shortId: string;
  publicUrl: string;
  running: boolean;
}

export async function getTunnelStatus(): Promise<TunnelStatusResult> {
  const settings = await getSettings();
  const settingsEnabled = settings.tunnelEnabled === true;
  const state = loadState<TunnelState>();
  const shortId = state?.shortId || "";
  const publicUrl = shortId ? `https://r${shortId}.abc-tunnel.us` : "";
  const tunnelUrl = state?.tunnelUrl || "";

  // Lazy: skip PID probe entirely when user disabled tunnel
  const running = settingsEnabled ? isCloudflaredRunning() : false;

  return {
    enabled: settingsEnabled && running,
    settingsEnabled,
    tunnelUrl,
    shortId,
    publicUrl,
    running,
  };
}
