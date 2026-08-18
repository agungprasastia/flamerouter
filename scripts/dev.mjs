import { spawn, execSync } from "node:child_process";
import http from "node:http";
import process from "node:process";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import crypto from "node:crypto";

const backendPort = process.env.BACKEND_PORT || "20130";
const frontendPort = process.env.PORT || "20129";
const dataDir = process.env.DATA_DIR || path.join(os.homedir(), ".flamerouter");

// Ensure JWT secret is shared across both Go backend and Next.js frontend
let jwtSecret = process.env.JWT_SECRET;
if (!jwtSecret) {
  const secretFile = path.join(dataDir, "jwt-secret");
  try {
    jwtSecret = fs.readFileSync(secretFile, "utf8").trim();
  } catch (_) {}
  if (!jwtSecret) {
    jwtSecret = crypto.randomBytes(32).toString("hex");
    try {
      fs.mkdirSync(dataDir, { recursive: true });
      fs.writeFileSync(secretFile, jwtSecret, { mode: 0o600 });
    } catch (_) {}
  }
}

// Clean up any stale process before starting
if (process.platform === "win32") {
  try {
    execSync("taskkill /F /IM flamerouter.exe /IM air.exe", { stdio: "ignore" });
  } catch (_) {}
}

console.log("\x1b[36m%s\x1b[0m", `[FlameRouter] Starting Go Gateway on port ${backendPort}...`);

// Check if air is installed for hot reload
let useAir = false;
try {
  execSync("air -v", { stdio: "ignore" });
  useAir = true;
} catch (_) {}

// 1. Spawn Go backend process
const sharedEnv = {
  ...process.env,
  DATA_DIR: dataDir,
  JWT_SECRET: jwtSecret,
};
const goEnv = { ...sharedEnv, PORT: backendPort };
const goProc = useAir
  ? spawn("air", ["-c", ".air.toml"], {
      stdio: ["inherit", "inherit", "inherit"],
      env: goEnv,
      shell: true,
    })
  : spawn("go", ["run", "./cmd/flamerouter", "serve"], {
      stdio: ["inherit", "inherit", "inherit"],
      env: goEnv,
      shell: true,
    });

if (useAir) {
  console.log("\x1b[35m%s\x1b[0m", "[FlameRouter] Air hot-reload enabled for Go backend.");
}

let nextProc = null;

function cleanup() {
  if (nextProc && !nextProc.killed) {
    try {
      if (process.platform === "win32") {
        spawn("taskkill", ["/pid", nextProc.pid.toString(), "/f", "/t"], { stdio: "ignore" });
      } else {
        nextProc.kill("SIGINT");
      }
    } catch (_) {}
  }
  if (goProc && !goProc.killed) {
    console.log("\x1b[33m%s\x1b[0m", "\n[FlameRouter] Stopping Go Gateway backend...");
    try {
      if (process.platform === "win32") {
        spawn("taskkill", ["/pid", goProc.pid.toString(), "/f", "/t"], { stdio: "ignore" });
      } else {
        goProc.kill("SIGINT");
      }
    } catch (_) {}
  }
  process.exit();
}

process.on("SIGINT", cleanup);
process.on("SIGTERM", cleanup);
process.on("exit", cleanup);

goProc.on("error", (err) => {
  console.error("\x1b[31m%s\x1b[0m", `[FlameRouter Backend Error] Failed to start Go process: ${err.message}`);
});

goProc.on("exit", (code) => {
  if (code && code !== 0) {
    console.error("\x1b[31m%s\x1b[0m", `[FlameRouter Backend Exit] Go process exited with code ${code}`);
  }
});

// 2. Poll backend health check
function waitForBackend(retries = 30) {
  return new Promise((resolve) => {
    const check = () => {
      const req = http.get(`http://127.0.0.1:${backendPort}/api/health`, (res) => {
        if (res.statusCode === 200) {
          resolve(true);
        } else if (retries-- > 0) {
          setTimeout(check, 300);
        } else {
          resolve(false);
        }
      });
      req.on("error", () => {
        if (retries-- > 0) {
          setTimeout(check, 300);
        } else {
          resolve(false);
        }
      });
    };
    check();
  });
}

// 3. Start Next.js frontend once backend is ready or after small delay
async function startFrontend() {
  await waitForBackend();
  console.log("\x1b[32m%s\x1b[0m", `[FlameRouter] Backend is ready! Starting Next.js UI on http://localhost:${frontendPort}...`);

  nextProc = spawn("npx", ["next", "dev", "--webpack", "--port", frontendPort], {
    stdio: "inherit",
    env: { ...sharedEnv, PORT: frontendPort, BACKEND_URL: `http://127.0.0.1:${backendPort}` },
    shell: true,
  });

  nextProc.on("exit", (code) => {
    cleanup();
    process.exit(code || 0);
  });
}

startFrontend();
