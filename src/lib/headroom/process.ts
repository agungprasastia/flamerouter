import fs from "fs";
import path from "path";
import { spawn } from "child_process";
import { DATA_DIR } from "@/lib/dataDir";
import {
  findHeadroomBinary,
  findPython310,
  HEADROOM_COMPRESSION_EXTRAS,
  EXTRA_MARKERS,
  getInstalledHeadroomExtras,
} from "./detect";

const HEADROOM_DIR = path.join(DATA_DIR, "headroom");
const PID_FILE = path.join(HEADROOM_DIR, "proxy.pid");
const LOG_FILE = path.join(HEADROOM_DIR, "proxy.log");
const INSTALL_LOG_FILE = path.join(HEADROOM_DIR, "install.log");
const DEFAULT_PORT = 8787;
const STARTUP_TIMEOUT_MS = 8000;
function ensureDir(): void {
  if (!fs.existsSync(HEADROOM_DIR))
    fs.mkdirSync(HEADROOM_DIR, { recursive: true });
}

function readPid(): number | null {
  try {
    if (fs.existsSync(PID_FILE))
      return parseInt(fs.readFileSync(PID_FILE, "utf8"), 10);
  } catch {
    /* ignore */
  }
  return null;
}

function writePid(pid: number): void {
  ensureDir();
  fs.writeFileSync(PID_FILE, String(pid));
}

function clearPid(): void {
  try {
    if (fs.existsSync(PID_FILE)) fs.unlinkSync(PID_FILE);
  } catch {
    /* ignore */
  }
}

// process.kill throws if pid is dead — use this to probe.
export function isPidAlive(pid?: number | null): boolean {
  if (!pid || typeof pid !== "number") return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

export function getManagedPid(): number | null {
  const pid = readPid();
  return pid && isPidAlive(pid) ? pid : null;
}

export interface ExtrasProxyArgsOptions {
  codeAware?: boolean;
  kompress?: boolean;
}

function extrasProxyArgs({ codeAware, kompress }: ExtrasProxyArgsOptions = {}): string[] {
  const args: string[] = [];
  if (codeAware) args.push("--code-aware");
  if (kompress === false) args.push("--disable-kompress");
  return args;
}

export interface StartHeadroomProxyOptions {
  port?: number | string;
  codeAware?: boolean;
  kompress?: boolean;
}

export async function startHeadroomProxy({
  port = DEFAULT_PORT,
  codeAware = false,
  kompress = true,
}: StartHeadroomProxyOptions = {}): Promise<{ pid: number; alreadyRunning: boolean }> {
  const safePort =
    Number(port) > 0 && Number(port) < 65536 ? Number(port) : DEFAULT_PORT;
  const binary = findHeadroomBinary();
  if (!binary) {
    const err = new Error("Headroom CLI not installed") as Error & { code?: string };
    err.code = "NOT_INSTALLED";
    throw err;
  }

  const existing = getManagedPid();
  if (existing) return { pid: existing, alreadyRunning: true };

  ensureDir();
  // spawn stdio requires fd numbers, not WriteStream objects.
  const outFd = fs.openSync(LOG_FILE, "a");

  const args = [
    "proxy",
    "--port",
    String(safePort),
    ...extrasProxyArgs({ codeAware, kompress }),
  ];

  const child = spawn(binary, args, {
    stdio: ["ignore", outFd, outFd],
    detached: true,
    windowsHide: true,
    env: { ...process.env },
  });

  if (!child.pid) {
    fs.closeSync(outFd);
    const err = new Error("Failed to spawn headroom proxy") as Error & { code?: string };
    err.code = "SPAWN_FAILED";
    throw err;
  }

  child.unref();
  writePid(child.pid);

  // Wait until the process either stays alive briefly (success) or exits fast (failure).
  await new Promise<void>((resolve, reject) => {
    const startupTimer = setTimeout(() => {
      if (isPidAlive(child.pid)) resolve();
      else
        reject(
          new Error("headroom proxy exited during startup — see proxy.log"),
        );
    }, STARTUP_TIMEOUT_MS);

    child.once("exit", (code) => {
      clearTimeout(startupTimer);
      clearPid();
      fs.closeSync(outFd);
      const e = new Error(
        `headroom proxy exited early (code=${code}) — see proxy.log`,
      ) as Error & { code?: string };
      e.code = "EARLY_EXIT";
      reject(e);
    });
  });

  // Close parent's copy of the fd; child retains its own after unref.
  fs.closeSync(outFd);

  return { pid: child.pid, alreadyRunning: false };
}

export function stopHeadroomProxy(): { stopped: boolean; pid?: number; reason?: string } {
  const pid = getManagedPid();
  if (!pid) return { stopped: false, reason: "not_running" };
  try {
    process.kill(pid, "SIGTERM");
    // Give it a moment, then force if still alive.
    setTimeout(() => {
      if (isPidAlive(pid)) {
        try {
          process.kill(pid, "SIGKILL");
        } catch {
          /* already gone */
        }
      }
    }, 2000);
    clearPid();
    return { stopped: true, pid };
  } catch (e: unknown) {
    clearPid();
    const err = new Error(`Failed to stop headroom proxy: ${(e as Error).message}`) as Error & { code?: string };
    err.code = "STOP_FAILED";
    throw err;
  }
}

export async function restartHeadroomProxy(opts: StartHeadroomProxyOptions = {}): Promise<{ pid: number; alreadyRunning: boolean }> {
  const pid = getManagedPid();
  if (pid) {
    try {
      process.kill(pid, "SIGTERM");
    } catch {
      /* already gone */
    }
    for (let i = 0; i < 30 && isPidAlive(pid); i++) {
      await new Promise((r) => setTimeout(r, 100));
    }
    if (isPidAlive(pid)) {
      try {
        process.kill(pid, "SIGKILL");
      } catch {
        /* already gone */
      }
      await new Promise((r) => setTimeout(r, 300));
    }
    clearPid();
  }
  return startHeadroomProxy(opts);
}

export function getHeadroomLogTail(maxLines = 200): string {
  try {
    if (!fs.existsSync(LOG_FILE)) return "";
    const content = fs.readFileSync(LOG_FILE, "utf8");
    const lines = content.split(/\r?\n/).filter(Boolean);
    return lines.slice(-maxLines).join("\n");
  } catch {
    return "";
  }
}

export async function installHeadroomExtras(extras: readonly string[] | string[] = []) {
  const requested = Array.isArray(extras)
    ? extras.filter((e) => (HEADROOM_COMPRESSION_EXTRAS as readonly string[]).includes(e))
    : [];
  const py = findPython310();
  if (!py) {
    const err = new Error("Python >= 3.10 not found") as Error & { code?: string };
    err.code = "NO_PYTHON";
    throw err;
  }
  if (!findHeadroomBinary()) {
    const err = new Error(
      "headroom-ai not installed (run `pip install headroom-ai[proxy]` first)",
    ) as Error & { code?: string };
    err.code = "NOT_INSTALLED";
    throw err;
  }
  const extrasList = ["proxy", ...requested].join(",");
  const spec = `headroom-ai[${extrasList}]`;
  const args = ["-m", "pip", "install", "--upgrade", spec];

  ensureDir();
  const outFd = fs.openSync(INSTALL_LOG_FILE, "w");
  const child = spawn(py, args, {
    stdio: ["ignore", outFd, outFd],
    windowsHide: true,
    env: { ...process.env },
  });

  return new Promise((resolve, reject) => {
    child.once("error", (e) => {
      fs.closeSync(outFd);
      reject(e);
    });
    child.once("exit", (code) => {
      fs.closeSync(outFd);
      if (code === 0) {
        const status = getInstalledHeadroomExtras(py);
        resolve({ success: true, code, spec, ...status, extras: requested });
      } else {
        const err = new Error(
          `pip install exited with code=${code} — see headroom/install.log`,
        ) as Error & { code?: string };
        err.code = "INSTALL_FAILED";
        reject(err);
      }
    });
  });
}

export async function uninstallHeadroomExtras(extras: readonly string[] | string[] = []) {
  const requested = Array.isArray(extras)
    ? (extras.filter((e) => (HEADROOM_COMPRESSION_EXTRAS as readonly string[]).includes(e)) as (typeof HEADROOM_COMPRESSION_EXTRAS)[number][])
    : [];
  const py = findPython310();
  if (!py) {
    const err = new Error("Python >= 3.10 not found") as Error & { code?: string };
    err.code = "NO_PYTHON";
    throw err;
  }
  const pkgs = [...new Set(requested.flatMap((e) => EXTRA_MARKERS[e] || []))];
  if (pkgs.length === 0) {
    const err = new Error("No valid extras to remove") as Error & { code?: string };
    err.code = "INVALID_EXTRAS";
    throw err;
  }
  const args = ["-m", "pip", "uninstall", "-y", ...pkgs];

  ensureDir();
  const outFd = fs.openSync(INSTALL_LOG_FILE, "w");
  const child = spawn(py, args, {
    stdio: ["ignore", outFd, outFd],
    windowsHide: true,
    env: { ...process.env },
  });

  return new Promise((resolve, reject) => {
    child.once("error", (e) => {
      fs.closeSync(outFd);
      reject(e);
    });
    child.once("exit", (code) => {
      fs.closeSync(outFd);
      if (code === 0) {
        const status = getInstalledHeadroomExtras(py);
        resolve({
          success: true,
          code,
          removed: pkgs,
          ...status,
          extras: requested,
        });
      } else {
        const err = new Error(
          `pip uninstall exited with code=${code} — see headroom/install.log`,
        ) as Error & { code?: string };
        err.code = "UNINSTALL_FAILED";
        reject(err);
      }
    });
  });
}

export function getInstallLogTail(maxLines = 15): string {
  try {
    if (!fs.existsSync(INSTALL_LOG_FILE)) return "";
    const lines = fs
      .readFileSync(INSTALL_LOG_FILE, "utf8")
      .split(/\r?\n/)
      .filter(Boolean);
    return lines.slice(-maxLines).join("\n");
  } catch {
    return "";
  }
}
