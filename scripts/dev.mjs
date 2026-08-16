import { spawn } from "node:child_process";
import http from "node:http";
import process from "node:process";

const backendPort = process.env.BACKEND_PORT || "20130";
const frontendPort = process.env.PORT || "20129";

console.log("\x1b[36m%s\x1b[0m", `[FlameRouter] Starting Go Gateway on port ${backendPort}...`);

// 1. Spawn Go backend process
const goEnv = { ...process.env, PORT: backendPort };
const goProc = spawn("go", ["run", "./cmd/flamerouter", "serve"], {
  stdio: ["inherit", "inherit", "inherit"],
  env: goEnv,
  shell: true,
});

function cleanup() {
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

  const nextProc = spawn("npx", ["next", "dev", "--webpack", "--port", frontendPort], {
    stdio: "inherit",
    env: { ...process.env, PORT: frontendPort, BACKEND_URL: `http://127.0.0.1:${backendPort}` },
    shell: true,
  });

  nextProc.on("exit", (code) => {
    cleanup();
    process.exit(code || 0);
  });
}

startFrontend();
