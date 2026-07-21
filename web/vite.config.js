import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:20128",
      "/v1": "http://127.0.0.1:20128",
      "/v1beta": "http://127.0.0.1:20128",
      "/codex": "http://127.0.0.1:20128",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
