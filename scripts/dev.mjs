import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const rootDir = path.resolve(__dirname, "..");
const openSseDir = path.resolve(rootDir, "open-sse");

const port = process.env.PORT || "20228";
const flamerouterPort = process.env.FLAMEROUTER_PORT || "20229";

// 1. Start Rust engine
console.log(`[FlameRouter] Starting Rust open-sse engine on port ${flamerouterPort}...`);
const rustProc = spawn("cargo", ["run", "--release"], {
  cwd: openSseDir,
  stdio: "inherit",
  env: {
    ...process.env,
    PORT: flamerouterPort,
  },
});

// 2. Start Next.js dev server
console.log(`[FlameRouter] Starting Next.js gateway on port ${port}...`);
const nextProc = spawn("npx", ["next", "dev", "--webpack", "--port", port], {
  cwd: rootDir,
  stdio: "inherit",
  env: {
    ...process.env,
    PORT: port,
    FLAMEROUTER_PORT: flamerouterPort,
    FLAMEROUTER_URL: `http://127.0.0.1:${flamerouterPort}`,
  },
});

function cleanup() {
  if (rustProc && !rustProc.killed) {
    rustProc.kill("SIGINT");
  }
  if (nextProc && !nextProc.killed) {
    nextProc.kill("SIGINT");
  }
  process.exit(0);
}

process.on("SIGINT", cleanup);
process.on("SIGTERM", cleanup);
process.on("exit", cleanup);
