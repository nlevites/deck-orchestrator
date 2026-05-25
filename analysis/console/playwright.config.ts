/**
 * Pillar 2B — console access-pattern capture.
 *
 * Mirrors the structure of `frontend/e2e/playwright.config.ts` but on a
 * distinct port range (12xxx) so analysis runs don't collide with
 * `make e2e` (18xxx) or `make e2e-scale` (16xxx) if someone has those
 * running. Stack composition: supervisor + 4 executors (default) or
 * 100 (set DFO_ANALYSIS_DECKS=100) + Vite.
 *
 * Specs do NOT assert correctness — they drive realistic operator
 * workflows and dump HAR/trace/heap artifacts under console/runs/.
 */
import { defineConfig, devices } from "@playwright/test";
import path from "node:path";
import os from "node:os";
import { fileURLToPath } from "node:url";

const ORCH_PORT = Number(process.env.DFO_ANALYSIS_ORCH_PORT ?? 12080);
const SUP_PORT = Number(process.env.DFO_ANALYSIS_SUP_PORT ?? 12090);
const EXEC_PORT_BASE = Number(process.env.DFO_ANALYSIS_EXEC_PORT_BASE ?? 13000);
const VITE_PORT = Number(process.env.DFO_ANALYSIS_VITE_PORT ?? 12173);
const DECKS = Number(process.env.DFO_ANALYSIS_DECKS ?? 4);
const STATE_DIR =
  process.env.DFO_ANALYSIS_STATE_DIR ?? path.join(os.tmpdir(), "dfo-analysis-console");

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const frontendDir = path.resolve(repoRoot, "frontend");

export default defineConfig({
  testDir: "./specs",
  fullyParallel: false,
  workers: 1,
  timeout: 60 * 60_000, // long-lived spec runs 30 min
  expect: { timeout: 15_000 },
  retries: 0,
  reporter: [["list"], ["html", { outputFolder: "./report", open: "never" }]],
  outputDir: "./test-results",
  use: {
    baseURL: `http://localhost:${VITE_PORT}`,
    // Tracing is started manually in _fixture.ts so each scenario's
    // trace.zip lives in console/runs/<scenario>/. The config-level
    // `trace` option would collide with the manual start.
    video: "retain-on-failure",
    screenshot: "only-on-failure",
    actionTimeout: 10_000,
    navigationTimeout: 20_000,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: [
    {
      command: `./backend/bin/supervisor -config config/supervisor.e2e.yaml -clean`,
      cwd: repoRoot,
      env: {
        STATE_DIR,
        LISTEN_ADDR: `:${SUP_PORT}`,
        ORCH_HTTP_PORT: String(ORCH_PORT),
        // HTTP_ADDR overrides the orchestrator's yaml-pinned bind addr
        // (supervisor.e2e.yaml -> config/e2e.yaml -> http.addr: :18080).
        // Without this, the orchestrator child happily ignores our port
        // choice and tries to bind :18080.
        HTTP_ADDR: `:${ORCH_PORT}`,
        HTTP_CORS_ALLOWED_ORIGINS: `http://localhost:${VITE_PORT}`,
        ORCH_DB_PATH: `${STATE_DIR}/orchestrator.db`,
        EXEC_PORT_BASE: String(EXEC_PORT_BASE),
        EXEC_ORCH_URL: `http://localhost:${ORCH_PORT}`,
        EXECUTOR_COUNT: String(DECKS),
        FLEET_SIZE: String(Math.max(DECKS, 16)),
        // JSON logs so we can correlate with the wire pillar if someone
        // wants to cross-reference a console scenario against orchestrator
        // ndjson from the same run.
        LOG_FORMAT: "json",
      },
      url: `http://localhost:${SUP_PORT}/health`,
      timeout: 60_000,
      reuseExistingServer: !process.env.CI,
      stdout: "pipe",
      stderr: "pipe",
    },
    {
      command: `npm run dev -- --port ${VITE_PORT} --strictPort --host 127.0.0.1`,
      cwd: frontendDir,
      env: {
        DFO_ORCHESTRATOR_URL: `http://localhost:${ORCH_PORT}`,
        DFO_SUPERVISOR_URL: `http://localhost:${SUP_PORT}`,
      },
      url: `http://localhost:${VITE_PORT}`,
      timeout: 60_000,
      reuseExistingServer: !process.env.CI,
      stdout: "pipe",
      stderr: "pipe",
    },
  ],
});

// Re-exported so specs can grab the orchestrator URL without re-parsing env.
export const STACK = {
  orchestratorUrl: `http://localhost:${ORCH_PORT}`,
  supervisorUrl: `http://localhost:${SUP_PORT}`,
  viteUrl: `http://localhost:${VITE_PORT}`,
  decks: DECKS,
};
