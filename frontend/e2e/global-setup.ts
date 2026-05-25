import type { FullConfig } from "@playwright/test";

/**
 * Redundant /health poll after webServer boot — confirms serving state before fixtures run.
 * State wipe is owned by the supervisor's -clean flag; reuseExistingServer keeps prior state intentionally.
 */
async function globalSetup(_config: FullConfig): Promise<void> {
  const orchUrl = process.env.DFO_E2E_ORCH_URL ?? "http://localhost:18080/health";
  const deadline = Date.now() + 30_000;
  let lastErr: unknown;
  while (Date.now() < deadline) {
    try {
      const resp = await fetch(orchUrl);
      if (resp.ok) return;
      lastErr = new Error(`HTTP ${resp.status}`);
    } catch (e) {
      lastErr = e;
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(
    `globalSetup: orchestrator /health never returned 200 within 30s (${String(lastErr)})`,
  );
}

export default globalSetup;
