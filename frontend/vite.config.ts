import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "node:path";

// Per TECH_STACK.md §11: dev server proxies /api/* and /executor/* to the
// orchestrator (default http://localhost:8080) so the frontend can use
// relative URLs and we avoid CORS entirely during local development.
// /supervisor/* additionally proxies to the dev-only supervisor sidecar
// (default http://localhost:8090) for the Fleet Management page.
const ORCHESTRATOR_URL = process.env.DFO_ORCHESTRATOR_URL ?? "http://localhost:8080";
const SUPERVISOR_URL = process.env.DFO_SUPERVISOR_URL ?? "http://localhost:8090";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  server: {
    port: 5173,
    strictPort: false,
    open: false,
    proxy: {
      "/api": {
        target: ORCHESTRATOR_URL,
        changeOrigin: true,
      },
      "/executor": {
        target: ORCHESTRATOR_URL,
        changeOrigin: true,
      },
      "/events": {
        target: ORCHESTRATOR_URL,
        changeOrigin: true,
        ws: false,
      },
      "/supervisor": {
        target: SUPERVISOR_URL,
        changeOrigin: true,
      },
    },
  },
  test: {
    // jsdom so components / DOM-touching hooks can be exercised. Pure-logic
    // tests don't need it but pay only a small startup cost.
    environment: "jsdom",
    globals: true,
    setupFiles: ["src/test/setup.ts"],
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
    // Keep Playwright's `e2e/**/*.spec.ts` out of Vitest — they're driven
    // by playwright.config.ts, not vitest.
    exclude: ["e2e/**", "node_modules/**", "dist/**"],
    css: false,
  },
});
