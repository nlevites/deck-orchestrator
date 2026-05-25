import { defineConfig, devices } from "@playwright/test";
import path from "node:path";
import os from "node:os";
import { fileURLToPath } from "node:url";

// Ports must match config/supervisor.e2e.yaml and vite proxy.
const ORCH_PORT = 18080;
const SUP_PORT = 18090;
const VITE_PORT = 15173;
const DECKS = 4;
const STATE_DIR = process.env.DFO_E2E_DIR ?? path.join(os.tmpdir(), "dfo-e2e");

// ESM: frontend/package.json is "type":"module" — no injected __dirname.
const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const frontendDir = path.resolve(here, "..");

/**
 * Hermetic e2e stack (supervisor + Vite on distinct ports).
 * workers=1; per-test isolation via unique run IDs + chaos afterEach.
 */
export default defineConfig({
  testDir: "./specs",
  fullyParallel: false,
  workers: 1,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI
    ? [["github"], ["html", { outputFolder: "../playwright-report", open: "never" }]]
    : [["list"], ["html", { outputFolder: "../playwright-report", open: "never" }]],
  outputDir: "../test-results",
  use: {
    baseURL: `http://localhost:${VITE_PORT}`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    actionTimeout: 8_000,
    navigationTimeout: 15_000,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: [
    {
      // Probe supervisor /health — orphaned orchestrator on :18080 would fool reuseExistingServer.
      command: `./backend/bin/supervisor -config config/supervisor.e2e.yaml -clean`,
      cwd: repoRoot,
      env: {
        STATE_DIR,
        ORCH_DB_PATH: `${STATE_DIR}/orchestrator.db`,
        EXECUTOR_COUNT: String(DECKS),
      },
      url: `http://localhost:${SUP_PORT}/health`,
      timeout: 45_000,
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
      timeout: 45_000,
      reuseExistingServer: !process.env.CI,
      stdout: "pipe",
      stderr: "pipe",
    },
  ],
  globalSetup: "./global-setup.ts",
});
