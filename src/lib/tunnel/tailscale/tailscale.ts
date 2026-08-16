import fs from "fs";
import path from "path";
import os from "os";
import crypto from "crypto";
import { execSync, exec, spawn } from "child_process";
import { promisify } from "util";
import { execWithPassword } from "@/mitm/dns/dnsConfig";
import { DATA_DIR } from "@/lib/dataDir";

const execAsync = promisify(exec);

const BIN_DIR = path.join(DATA_DIR, "bin");
const IS_MAC = os.platform() === "darwin";
const IS_LINUX = os.platform() === "linux";
const IS_WINDOWS = os.platform() === "win32";
const TAILSCALE_BIN = path.join(
  BIN_DIR,
  IS_WINDOWS ? "tailscale.exe" : "tailscale",
);

// Custom socket for userspace-networking mode (no root required)
const TAILSCALE_DIR = path.join(DATA_DIR, "tailscale");
export const TAILSCALE_SOCKET = path.join(TAILSCALE_DIR, "tailscaled.sock");
const SOCKET_FLAG = IS_WINDOWS ? [] : ["--socket", TAILSCALE_SOCKET];

// System daemon socket (sudo install: apt/snap/systemd) — read-only status detection
const SYSTEM_TAILSCALE_SOCKET = IS_WINDOWS
  ? null
  : "/var/run/tailscale/tailscaled.sock";
const SYSTEM_SOCKET_FLAG = SYSTEM_TAILSCALE_SOCKET
  ? ["--socket", SYSTEM_TAILSCALE_SOCKET]
  : [];

// Well-known Windows install path
const WINDOWS_TAILSCALE_BIN = "C:\\Program Files\\Tailscale\\tailscale.exe";

// Common Unix install paths to probe synchronously (system tailscale)
const UNIX_TAILSCALE_CANDIDATES = [
  "/usr/local/bin/tailscale",
  "/opt/homebrew/bin/tailscale",
  "/usr/sbin/tailscale", // apt package on Debian/Ubuntu
  "/usr/bin/tailscale",
  "/snap/bin/tailscale", // Snap package
];

// ─── Cache + background refresh (avoid blocking event loop on dead daemon) ──
const PROBE_TTL_MS = 10000;
const PROBE_TIMEOUT_MS = 1500;

const binCache: { value: string | null | undefined; fetchedAt: number; refreshing: boolean } = { value: undefined, fetchedAt: 0, refreshing: false };
const runningCache = { value: false, fetchedAt: 0, refreshing: false };
const loggedInCache = { value: false, fetchedAt: 0, refreshing: false };
const funnelUrlCache: {
  value: string | null;
  port: number | string | null;
  fetchedAt: number;
  refreshing: boolean;
} = {
  value: null,
  port: null,
  fetchedAt: 0,
  refreshing: false,
};

function fallbackBin(): string | null {
  if (fs.existsSync(TAILSCALE_BIN)) return TAILSCALE_BIN;
  if (IS_WINDOWS && fs.existsSync(WINDOWS_TAILSCALE_BIN))
    return WINDOWS_TAILSCALE_BIN;
  if (!IS_WINDOWS)
    return UNIX_TAILSCALE_CANDIDATES.find((p) => fs.existsSync(p)) || null;
  return null;
}

function bgRefreshBin() {
  if (binCache.refreshing) return;
  binCache.refreshing = true;
  const cmd = IS_WINDOWS
    ? "where tailscale 2>nul"
    : "which tailscale 2>/dev/null";
  execAsync(cmd, {
    windowsHide: true,
    timeout: PROBE_TIMEOUT_MS,
    env: { ...process.env, PATH: EXTENDED_PATH },
  })
    .then(({ stdout }) => {
      const sys = stdout.trim();
      binCache.value = sys || fallbackBin();
    })
    .catch(() => {
      binCache.value = fallbackBin();
    })
    .finally(() => {
      binCache.fetchedAt = Date.now();
      binCache.refreshing = false;
    });
}

// Sync getter: returns cached value, triggers background refresh if stale
export function getTailscaleBin(): string | null {
  if (Date.now() - binCache.fetchedAt > PROBE_TTL_MS) bgRefreshBin();
  // First call: synchronously probe common install paths (no exec, no event-loop block)
  if (binCache.value === undefined) {
    if (fs.existsSync(TAILSCALE_BIN)) binCache.value = TAILSCALE_BIN;
    else if (IS_WINDOWS && fs.existsSync(WINDOWS_TAILSCALE_BIN))
      binCache.value = WINDOWS_TAILSCALE_BIN;
    else if (!IS_WINDOWS) {
      const found = UNIX_TAILSCALE_CANDIDATES.find((p) => fs.existsSync(p));
      binCache.value = found || null;
    } else binCache.value = null;
  }
  return binCache.value ?? null;
}

export function isTailscaleInstalled(): boolean {
  return getTailscaleBin() !== null;
}

/** Build tailscale CLI args with custom socket (no root needed) */
function tsArgs(...args: string[]): string[] {
  return [...SOCKET_FLAG, ...args];
}

// Async strict probe: authoritative, awaitable (never blocks event loop). Updates cache.
export async function isTailscaleLoggedInStrict() {
  const bin = getTailscaleBin();
  if (!bin) return false;
  try {
    const { stdout } = await execAsync(
      `"${bin}" ${SOCKET_FLAG.join(" ")} status --json`,
      {
        windowsHide: true,
        env: { ...process.env, PATH: EXTENDED_PATH },
        timeout: 5000,
      },
    );
    const json = JSON.parse(stdout);
    // BackendState=Running + Self.Online=true → device still exists in tailnet
    const loggedIn =
      json.BackendState === "Running" && json.Self?.Online === true;
    loggedInCache.value = loggedIn;
    loggedInCache.fetchedAt = Date.now();
    return loggedIn;
  } catch {
    return false;
  }
}

function bgRefreshLoggedIn() {
  if (loggedInCache.refreshing) return;
  const bin = getTailscaleBin();
  if (!bin) {
    loggedInCache.value = false;
    loggedInCache.fetchedAt = Date.now();
    return;
  }
  loggedInCache.refreshing = true;
  // Dual-socket aware: probe custom socket first, then system socket
  probeStatusAsync(bin)
    .then((json) => {
      loggedInCache.value =
        !!json && json.BackendState === "Running" && json.Self?.Online === true;
    })
    .catch(() => {
      loggedInCache.value = false;
    })
    .finally(() => {
      loggedInCache.fetchedAt = Date.now();
      loggedInCache.refreshing = false;
    });
}

// Probe `status --json` over custom then system socket. Resolves parsed JSON or null. Never blocks event loop.
async function probeStatusAsync(bin: string) {
  for (const socketArgs of [SOCKET_FLAG, SYSTEM_SOCKET_FLAG]) {
    try {
      const { stdout } = await execAsync(
        `"${bin}" ${socketArgs.join(" ")} status --json`,
        {
          windowsHide: true,
          env: { ...process.env, PATH: EXTENDED_PATH },
          timeout: PROBE_TIMEOUT_MS,
        },
      );
      return JSON.parse(stdout);
    } catch {
      /* try next socket */
    }
  }
  return null;
}

// Sync getter: never blocks; returns last known state, refreshes in background
export function isTailscaleLoggedIn() {
  if (Date.now() - loggedInCache.fetchedAt > PROBE_TTL_MS) bgRefreshLoggedIn();
  return loggedInCache.value;
}

function bgRefreshRunning() {
  if (runningCache.refreshing) return;
  const bin = getTailscaleBin();
  if (!bin) {
    runningCache.value = false;
    runningCache.fetchedAt = Date.now();
    return;
  }
  runningCache.refreshing = true;
  execAsync(`"${bin}" ${SOCKET_FLAG.join(" ")} funnel status --json`, {
    windowsHide: true,
    timeout: PROBE_TIMEOUT_MS,
  })
    .then(({ stdout }) => {
      try {
        const json = JSON.parse(stdout);
        runningCache.value = Object.keys(json.AllowFunnel || {}).length > 0;
      } catch {
        runningCache.value = false;
      }
    })
    .catch(() => {
      runningCache.value = false;
    })
    .finally(() => {
      runningCache.fetchedAt = Date.now();
      runningCache.refreshing = false;
    });
}

// Sync getter: never blocks; returns last known state, refreshes in background
export function isTailscaleRunning() {
  if (Date.now() - runningCache.fetchedAt > PROBE_TTL_MS) bgRefreshRunning();
  return runningCache.value;
}

// Async strict probe for hot user-initiated paths (enable/connect flow).
// Awaitable, never blocks event loop; updates cache as a side effect.
export async function isTailscaleRunningStrict() {
  const bin = getTailscaleBin();
  if (!bin) return false;
  try {
    const { stdout } = await execAsync(
      `"${bin}" ${SOCKET_FLAG.join(" ")} funnel status --json`,
      {
        windowsHide: true,
        timeout: PROBE_TIMEOUT_MS,
      },
    );
    const json = JSON.parse(stdout);
    const running = Object.keys(json.AllowFunnel || {}).length > 0;
    runningCache.value = running;
    runningCache.fetchedAt = Date.now();
    return running;
  } catch {
    return false;
  }
}

// Check if a system-level tailscaled is running (uses system socket, not FlameRouter's custom one).
export function isSystemDaemonRunning() {
  if (
    IS_WINDOWS ||
    !SYSTEM_TAILSCALE_SOCKET ||
    !fs.existsSync(SYSTEM_TAILSCALE_SOCKET)
  )
    return false;
  const bin = getTailscaleBin();
  if (!bin) return false;
  try {
    const out = execSync(
      `"${bin}" ${SYSTEM_SOCKET_FLAG.join(" ")} status --json`,
      {
        encoding: "utf8",
        windowsHide: true,
        env: { ...process.env, PATH: EXTENDED_PATH },
        timeout: PROBE_TIMEOUT_MS,
      },
    );
    return JSON.parse(out).BackendState === "Running";
  } catch {
    return false;
  }
}

function bgRefreshFunnelUrl(port?: number | string | null) {
  if (funnelUrlCache.refreshing) return;
  const bin = getTailscaleBin();
  if (!bin) return;
  funnelUrlCache.refreshing = true;
  execAsync(`"${bin}" ${SOCKET_FLAG.join(" ")} status --json`, {
    windowsHide: true,
    timeout: PROBE_TIMEOUT_MS,
  })
    .then(({ stdout }) => {
      try {
        const json = JSON.parse(stdout);
        const dnsName = json.Self?.DNSName?.replace(/\.$/, "");
        funnelUrlCache.value = dnsName ? `https://${dnsName}` : null;
      } catch {
        /* keep prev */
      }
    })
    .catch(() => {
      /* keep prev */
    })
    .finally(() => {
      funnelUrlCache.port = port ?? null;
      funnelUrlCache.fetchedAt = Date.now();
      funnelUrlCache.refreshing = false;
    });
}

/** Get actual funnel URL from Self.DNSName (sync, authoritative — avoids hostname-conflict suffix). */
function getActualFunnelUrl(): string | null {
  const bin = getTailscaleBin();
  if (!bin) return null;
  try {
    const out = execSync(`"${bin}" ${SOCKET_FLAG.join(" ")} status --json`, {
      encoding: "utf8",
      windowsHide: true,
      env: { ...process.env, PATH: EXTENDED_PATH },
      timeout: 5000,
    });
    const json = JSON.parse(out);
    const dnsName = json.Self?.DNSName?.replace(/\.$/, "");
    return dnsName ? `https://${dnsName}` : null;
  } catch {
    return null;
  }
}

/** Get funnel URL from tailscale status (cached, non-blocking) */
export function getTailscaleFunnelUrl(port?: number | string | null): string | null {
  if (
    Date.now() - funnelUrlCache.fetchedAt > PROBE_TTL_MS ||
    funnelUrlCache.port !== port
  ) {
    bgRefreshFunnelUrl(port);
  }
  return funnelUrlCache.value;
}

/**
 * Install tailscale.
 * - macOS + brew: brew install tailscale (no sudo needed)
 * - macOS no brew: download .pkg then sudo installer -pkg
 * - Linux: fetch install.sh, pipe to sudo -S sh via stdin
 * - Windows: download MSI via UAC-elevated PowerShell
 */
export async function installTailscale(
  sudoPassword?: string,
  hostname?: string,
  onProgress?: (msg: string) => void,
) {
  const log = onProgress || (() => {});
  if (IS_WINDOWS) {
    await installTailscaleWindows(log);
    return { success: true };
  }
  if (IS_MAC) await installTailscaleMac(sudoPassword, log);
  else await installTailscaleLinux(sudoPassword, log);

  log("Starting daemon...");
  await startDaemonWithPassword(sudoPassword);
  log("Logging in...");
  return startLogin(hostname);
}

const EXTENDED_PATH = `/usr/local/bin:/opt/homebrew/bin:/usr/sbin:/usr/bin:/bin:/snap/bin:${process.env.PATH || ""}`;

function hasBrew(): boolean {
  try {
    execSync("which brew", {
      stdio: "ignore",
      windowsHide: true,
      env: { ...process.env, PATH: EXTENDED_PATH },
    });
    return true;
  } catch {
    return false;
  }
}

async function installTailscaleMac(sudoPassword: string | undefined, log: (msg: string) => void): Promise<void> {
  if (hasBrew()) {
    log("Installing via Homebrew...");
    await new Promise<void>((resolve, reject) => {
      const child = spawn("brew", ["install", "tailscale"], {
        stdio: ["ignore", "pipe", "pipe"],
        windowsHide: true,
        env: { ...process.env, PATH: EXTENDED_PATH },
      });
      child.stdout.on("data", (d: Buffer) => {
        const line = d.toString().trim();
        if (line) log(line);
      });
      child.stderr.on("data", (d: Buffer) => {
        const line = d.toString().trim();
        if (line) log(line);
      });
      child.on("close", (c) => {
        if (c === 0) resolve();
        else reject(new Error(`brew install failed (code ${c})`));
      });
      child.on("error", reject);
    });
    return;
  }

  // No brew: download .pkg and install via sudo installer
  const pkgUrl = "https://pkgs.tailscale.com/stable/tailscale-latest.pkg";
  const pkgPath = path.join(os.tmpdir(), "tailscale.pkg");

  log("Downloading Tailscale package...");
  await new Promise<void>((resolve, reject) => {
    const child = spawn(
      "curl",
      ["-fL", "--progress-bar", pkgUrl, "-o", pkgPath],
      {
        stdio: ["ignore", "pipe", "pipe"],
        windowsHide: true,
      },
    );
    child.stderr.on("data", (d: Buffer) => {
      const line = d.toString().trim();
      if (line) log(line);
    });
    child.on("close", (c) => {
      if (c === 0) resolve();
      else reject(new Error("Download failed"));
    });
    child.on("error", reject);
  });

  log("Installing package (requires sudo)...");
  await execWithPassword(
    `installer -pkg "${pkgPath}" -target /`,
    sudoPassword || "",
  );
  try {
    fs.unlinkSync(pkgPath);
  } catch {}
}

async function installTailscaleLinux(sudoPassword: string | undefined, log: (msg: string) => void): Promise<void> {
  log("Running Tailscale install script...");
  const scriptPath = path.join(os.tmpdir(), "tailscale-install.sh");

  await new Promise<void>((resolve, reject) => {
    const child = spawn(
      "curl",
      ["-fsSL", "https://tailscale.com/install.sh", "-o", scriptPath],
      {
        stdio: ["ignore", "pipe", "pipe"],
        windowsHide: true,
      },
    );
    child.on("close", (c) => {
      if (c === 0) resolve();
      else reject(new Error("Failed to download install script"));
    });
    child.on("error", reject);
  });

  await execWithPassword(`sh "${scriptPath}"`, sudoPassword || "");
  try {
    fs.unlinkSync(scriptPath);
  } catch {}
}

async function installTailscaleWindows(log: (msg: string) => void): Promise<void> {
  const msiUrl = "https://pkgs.tailscale.com/stable/tailscale-setup-latest.exe";
  const installerPath = path.join(os.tmpdir(), "tailscale-setup.exe");

  log("Downloading Tailscale installer...");
  const psDownload = `
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -Uri "${msiUrl}" -OutFile "${installerPath}" -UseBasicParsing
  `;
  try {
    execSync(
      `powershell -NonInteractive -WindowStyle Hidden -Command "${psDownload.replace(/\n/g, " ")}"`,
      {
        windowsHide: true,
        timeout: 120000,
      },
    );
  } catch (e: unknown) {
    const err = e as Error;
    throw new Error(`Download failed: ${err.message}`);
  }

  log("Installing Tailscale (elevation prompt may appear)...");
  const psInstall = `Start-Process -FilePath "${installerPath}" -ArgumentList "/install /quiet /norestart" -Verb RunAs -Wait`;
  try {
    execSync(`powershell -NonInteractive -Command "${psInstall}"`, {
      timeout: 180000,
    });
  } finally {
    try {
      fs.unlinkSync(installerPath);
    } catch {}
  }
}

export async function downloadTailscaleUserspace(log?: (msg: string) => void): Promise<void> {
  const logFn = log || (() => {});
  const binDir = BIN_DIR;
  fs.mkdirSync(binDir, { recursive: true });

  const arch = os.arch() === "arm64" ? "arm64" : "amd64";
  const osType = IS_MAC ? "darwin" : "linux";
  const tarUrl = `https://pkgs.tailscale.com/stable/tailscale_latest_${osType}_${arch}.tgz`;
  const tarPath = path.join(os.tmpdir(), "tailscale.tgz");

  logFn(`Downloading userspace Tailscale (${osType}/${arch})...`);
  await new Promise<void>((resolve, reject) => {
    const child = spawn("curl", ["-fL", tarUrl, "-o", tarPath], {
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });
    child.on("close", (c) => {
      if (c === 0) resolve();
      else reject(new Error("Failed to download Tailscale binary"));
    });
    child.on("error", reject);
  });

  logFn("Extracting binary...");
  const extractDir = path.join(os.tmpdir(), `ts-extract-${Date.now()}`);
  fs.mkdirSync(extractDir, { recursive: true });
  try {
    execSync(`tar -xzf "${tarPath}" -C "${extractDir}" --strip-components=1`, {
      windowsHide: true,
    });
    for (const bin of ["tailscale", "tailscaled"]) {
      const src = path.join(extractDir, bin);
      const dst = path.join(binDir, bin);
      if (fs.existsSync(src)) {
        fs.copyFileSync(src, dst);
        fs.chmodSync(dst, 0o755);
      }
    }
  } finally {
    try {
      fs.unlinkSync(tarPath);
      fs.rmSync(extractDir, { recursive: true, force: true });
    } catch {}
  }
  logFn("Tailscale binaries installed to ~/.flamerouter/bin");
}

function fixDirOwnership(dir: string): void {
  if (IS_WINDOWS) return;
  try {
    const uid = process.getuid ? process.getuid() : null;
    const gid = process.getgid ? process.getgid() : null;
    if (uid !== null && gid !== null && fs.existsSync(dir)) {
      fs.chownSync(dir, uid, gid);
      for (const entry of fs.readdirSync(dir)) {
        try {
          fs.chownSync(path.join(dir, entry), uid, gid);
        } catch {}
      }
    }
  } catch {}
}

// Self-heal: if state dir/files were previously created by root (e.g. legacy sudo daemon),
// reclaim ownership recursively so the user-mode daemon can read/write state files.
async function ensureUserOwnedDir(dir: string): Promise<void> {
  try {
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
      return;
    }
    const uid = process.getuid ? process.getuid() : null;
    const gid = process.getgid ? process.getgid() : null;
    if (uid === null || gid === null) return;

    // Walk dir + all entries to find any non-user-owned items
    const needsChown = (() => {
      const stack = [dir];
      while (stack.length) {
        const cur = stack.pop();
        if (!cur) continue;
        try {
          const st = fs.statSync(cur);
          if (st.uid !== uid) return true;
          if (st.isDirectory()) {
            for (const name of fs.readdirSync(cur))
              stack.push(path.join(cur, name));
          }
        } catch {
          /* ignore */
        }
      }
      return false;
    })();

    if (!needsChown) return;

    // Try direct chown first (works if already owned). Fallback to passwordless sudo.
    try {
      execSync(`chown -R ${uid}:${gid} "${dir}"`, {
        stdio: "ignore",
        timeout: 3000,
      });
    } catch {
      try {
        execSync(`sudo -n chown -R ${uid}:${gid} "${dir}"`, {
          stdio: "ignore",
          timeout: 3000,
        });
      } catch {
        /* ignore */
      }
    }
  } catch {
    /* ignore */
  }
}

/** Check if running daemon uses TUN mode (Funnel TLS requires TUN). */
function isDaemonTunMode() {
  try {
    const ps = execSync(`pgrep -af "tailscaled.*${TAILSCALE_SOCKET}"`, {
      encoding: "utf8",
      timeout: 2000,
    }).trim();
    if (!ps) return null;
    return !ps.includes("--tun=userspace-networking");
  } catch {
    return null;
  }
}

/** Daemon process alive (independent of funnel state) — mirrors cloudflared PID check semantic. */
export function isDaemonAlive() {
  return isDaemonTunMode() !== null;
}

/**
 * Start tailscaled.
 * - With sudoPassword: TUN mode (root) → Funnel TLS works
 * - Without: userspace-networking fallback (no sudo, but Funnel TLS unstable)
 * State always lives in ~/.flamerouter/tailscale/ via --statedir.
 */
/**
 * Start tailscaled.
 * - With sudoPassword: TUN mode (root) → Funnel TLS works
 * - Without: userspace-networking fallback (no sudo, but Funnel TLS unstable)
 * State always lives in ~/.flamerouter/tailscale/ via --statedir.
 */
export async function startDaemonWithPassword(sudoPassword?: string): Promise<void> {
  if (IS_WINDOWS) {
    const bin = getTailscaleBin();
    console.log("[Tailscale] win: net start Tailscale");
    try {
      execSync("net start Tailscale", {
        stdio: "ignore",
        windowsHide: true,
        timeout: 10000,
      });
    } catch {
      /* may need admin, or already running */
    }
    if (!bin) return;
    for (let i = 0; i < 20; i++) {
      try {
        const out = execSync(`"${bin}" status --json`, {
          encoding: "utf8",
          windowsHide: true,
          timeout: 2000,
        });
        const j = JSON.parse(out) as { BackendState?: string; Self?: { Online?: boolean } };
        if (j.BackendState && j.BackendState !== "NoState") {
          console.log(
            `[Tailscale] win: BackendState=${j.BackendState} after ${i * 500}ms`,
          );
          return;
        }
      } catch {
        /* daemon not ready */
      }
      await new Promise((r) => setTimeout(r, 500));
    }
    console.log("[Tailscale] win: BackendState still NoState after poll");
    return;
  }

  const currentMode = isDaemonTunMode();
  const wantTun = sudoPassword ? true : currentMode === true;

  if (currentMode !== null && currentMode === wantTun) {
    try {
      const bin = getTailscaleBin() || "tailscale";
      execSync(`"${bin}" ${SOCKET_FLAG.join(" ")} status --json`, {
        stdio: "ignore",
        windowsHide: true,
        env: { ...process.env, PATH: EXTENDED_PATH },
        timeout: 3000,
      });
      return;
    } catch {
      /* unresponsive, restart below */
    }
  }

  try {
    execSync(`pkill -9 -f "tailscaled.*${TAILSCALE_SOCKET}"`, {
      stdio: "ignore",
      timeout: 3000,
    });
  } catch {
    /* ignore */
  }
  if (sudoPassword) {
    try {
      await execWithPassword(
        `pkill -9 -f "tailscaled.*${TAILSCALE_SOCKET}"`,
        sudoPassword,
      );
    } catch {
      /* ignore */
    }
  } else {
    try {
      execSync(`sudo -n pkill -9 -f "tailscaled.*${TAILSCALE_SOCKET}"`, {
        stdio: "ignore",
        timeout: 3000,
      });
    } catch {
      /* ignore */
    }
  }
  await new Promise((r) => setTimeout(r, 1500));

  fixDirOwnership(TAILSCALE_DIR);

  const tailscaledBin = IS_MAC ? "/usr/local/bin/tailscaled" : "tailscaled";
  const daemonArgs = [
    `--socket=${TAILSCALE_SOCKET}`,
    `--statedir=${TAILSCALE_DIR}`,
  ];
  if (!wantTun) daemonArgs.push("--tun=userspace-networking");

  if (wantTun) {
    const child = spawn("sudo", ["-S", tailscaledBin, ...daemonArgs], {
      detached: true,
      stdio: ["pipe", "ignore", "ignore"],
      cwd: os.tmpdir(),
      env: { ...process.env, PATH: EXTENDED_PATH },
    });
    child.stdin?.write(`${sudoPassword}\n`);
    child.stdin?.end();
    child.unref();
  } else {
    const child = spawn(tailscaledBin, daemonArgs, {
      detached: true,
      stdio: "ignore",
      cwd: os.tmpdir(),
      env: { ...process.env, PATH: EXTENDED_PATH },
    });
    child.unref();
  }

  await new Promise((r) => setTimeout(r, 3000));
}

/** Best-effort: ensure daemon running (used for login flow) */
function ensureDaemon(): void {
  startDaemonWithPassword("").catch(() => {});
}

/** Read AuthURL from `tailscale status --json` (Win exposes it there, not stdout). */
function getAuthUrlFromStatus(): string | null {
  const bin = getTailscaleBin();
  if (!bin) return null;
  try {
    const out = execSync(`"${bin}" ${SOCKET_FLAG.join(" ")} status --json`, {
      encoding: "utf8",
      windowsHide: true,
      timeout: 2000,
    });
    const j = JSON.parse(out) as { AuthURL?: string };
    if (j.AuthURL) return j.AuthURL;
    return null;
  } catch {
    return null;
  }
}

export interface StartLoginResult {
  authUrl?: string;
  alreadyLoggedIn?: boolean;
}

export function startLogin(hostname?: string): Promise<StartLoginResult> {
  const bin = getTailscaleBin();
  if (!bin) return Promise.reject(new Error("Tailscale not installed"));

  return new Promise((resolve, reject) => {
    ensureDaemon();

    if (isTailscaleLoggedIn()) {
      resolve({ alreadyLoggedIn: true });
      return;
    }

    const args = tsArgs("up", "--accept-routes");
    if (hostname) args.push(`--hostname=${hostname}`);
    const child = spawn(bin, args, {
      stdio: ["ignore", "pipe", "pipe"],
      detached: true,
      windowsHide: true,
    });

    let resolved = false;
    let output = "";

    const parseAuthUrl = (text: string): string | null => {
      const match = text.match(
        /https:\/\/login\.tailscale\.com\/a\/[a-zA-Z0-9]+/,
      );
      return match ? match[0] ?? null : null;
    };

    const finishWithUrl = (url: string, source: string) => {
      if (resolved) return;
      resolved = true;
      clearTimeout(timeout);
      clearInterval(statusPoll);
      console.log(`[Tailscale] login authUrl detected (${source})`);
      child.unref();
      resolve({ authUrl: url });
    };

    const statusPoll = setInterval(() => {
      if (resolved) return;
      const url = getAuthUrlFromStatus();
      if (url) finishWithUrl(url, "status");
    }, 500);

    const timeout = setTimeout(() => {
      if (resolved) return;
      resolved = true;
      clearInterval(statusPoll);
      child.unref();
      const url = parseAuthUrl(output) || getAuthUrlFromStatus();
      if (url) resolve({ authUrl: url });
      else reject(new Error("tailscale up timed out without auth URL"));
    }, 15000);

    const handleData = (data: Buffer | string) => {
      output += data.toString();
      const url = parseAuthUrl(output);
      if (url) finishWithUrl(url, "stdout");
    };

    child.stdout?.on("data", handleData);
    child.stderr?.on("data", handleData);

    child.on("error", (err) => {
      if (resolved) return;
      resolved = true;
      clearTimeout(timeout);
      clearInterval(statusPoll);
      console.error(`[Tailscale] login spawn error: ${err.message}`);
      reject(err);
    });

    child.on("exit", (code) => {
      if (resolved) return;
      console.log(`[Tailscale] login exit code=${code}`);
      const url = parseAuthUrl(output) || getAuthUrlFromStatus();
      if (url) {
        finishWithUrl(url, "exit");
        return;
      }
      if (isTailscaleLoggedIn()) {
        resolved = true;
        clearTimeout(timeout);
        clearInterval(statusPoll);
        resolve({ alreadyLoggedIn: true });
        return;
      }
    });
  });
}

export interface StartFunnelResult {
  tunnelUrl?: string;
  funnelNotEnabled?: boolean;
  enableUrl?: string;
}

export async function startFunnel(port: number | string): Promise<StartFunnelResult> {
  const bin = getTailscaleBin();
  if (!bin) throw new Error("Tailscale not installed");

  try {
    execSync(`"${bin}" ${SOCKET_FLAG.join(" ")} funnel --bg reset`, {
      stdio: "ignore",
      windowsHide: true,
    });
  } catch {
    /* ignore */
  }

  return new Promise<StartFunnelResult>((resolve, reject) => {
    const child = spawn(bin, tsArgs("funnel", "--bg", `${port}`), {
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });

    let resolved = false;
    let output = "";

    const timeout = setTimeout(() => {
      if (resolved) return;
      resolved = true;
      const url = getActualFunnelUrl() || getTailscaleFunnelUrl(port);
      if (url) resolve({ tunnelUrl: url });
      else
        reject(
          new Error(
            `Tailscale funnel timed out: ${output.trim() || "no output"}`,
          ),
        );
    }, 30000);

    const parseFunnelUrl = () => getActualFunnelUrl();
    let funnelNotEnabled = false;

    const handleData = (data: Buffer | string) => {
      output += data.toString();

      if (output.includes("Funnel is not enabled")) funnelNotEnabled = true;

      if (funnelNotEnabled && !resolved) {
        const enableMatch = output.match(
          /https:\/\/login\.tailscale\.com\/[^\s]+/,
        );
        if (enableMatch) {
          resolved = true;
          clearTimeout(timeout);
          child.kill();
          resolve({ funnelNotEnabled: true, enableUrl: enableMatch[0] });
          return;
        }
      }

      const url = parseFunnelUrl();
      if (url && !resolved) {
        resolved = true;
        clearTimeout(timeout);
        resolve({ tunnelUrl: url });
      }
    };

    child.stdout?.on("data", handleData);
    child.stderr?.on("data", handleData);

    child.on("exit", (code) => {
      if (resolved) return;
      resolved = true;
      clearTimeout(timeout);
      console.log(
        `[Tailscale] funnel exit code=${code} output="${output.trim().slice(0, 200)}"`,
      );
      const url = parseFunnelUrl() || getTailscaleFunnelUrl(port);
      if (url) resolve({ tunnelUrl: url });
      else
        reject(
          new Error(`tailscale funnel failed (code ${code}): ${output.trim()}`),
        );
    });

    child.on("error", (err) => {
      if (resolved) return;
      resolved = true;
      clearTimeout(timeout);
      reject(err);
    });
  });
}

export async function provisionCert(hostname?: string | null): Promise<void> {
  const bin = getTailscaleBin();
  if (!bin || !hostname) return;
  const certsDir = path.join(TAILSCALE_DIR, "certs");
  fs.mkdirSync(certsDir, { recursive: true });
  const certFile = path.join(certsDir, `${hostname}.crt`);
  const keyFile = path.join(certsDir, `${hostname}.key`);
  try {
    await execAsync(
      `"${bin}" ${SOCKET_FLAG.join(" ")} cert --cert-file "${certFile}" --key-file "${keyFile}" "${hostname}"`,
      {
        windowsHide: true,
        env: { ...process.env, PATH: EXTENDED_PATH },
        timeout: 30000,
      },
    );
    console.log(`[Tailscale] cert provisioned for ${hostname}`);
  } catch (e: unknown) {
    const err = e as Error;
    console.warn(`[Tailscale] cert provision failed (non-fatal): ${err.message}`);
  }
}

export function stopFunnel(): void {
  const bin = getTailscaleBin();
  if (!bin) return;
  try {
    execSync(`"${bin}" ${SOCKET_FLAG.join(" ")} funnel --bg reset`, {
      stdio: "ignore",
      windowsHide: true,
    });
  } catch {
    /* ignore */
  }
}

export async function stopDaemon(sudoPassword?: string): Promise<void> {
  try {
    execSync("pkill -x tailscaled", {
      stdio: "ignore",
      windowsHide: true,
      timeout: 3000,
    });
  } catch {
    /* ignore */
  }

  try {
    execSync("pgrep -x tailscaled", {
      stdio: "ignore",
      windowsHide: true,
      timeout: 2000,
    });
  } catch {
    return;
  }

  if (!IS_WINDOWS) {
    try {
      await execWithPassword("pkill -x tailscaled", sudoPassword || "");
    } catch {
      /* ignore */
    }
  }

  try {
    if (fs.existsSync(TAILSCALE_SOCKET)) fs.unlinkSync(TAILSCALE_SOCKET);
  } catch {
    /* ignore */
  }
}
