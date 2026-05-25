/**
 * Shared fixture for the analysis console specs. Each scenario inherits:
 *   - HAR recording into runs/<scenario>/network.har
 *   - Three init-scripts loaded (cache, fetch, visibility instruments)
 *   - Periodic metrics.ndjson sampler (1Hz)
 *   - Three heap snapshots (start/mid/end) saved per scenario
 *
 * Specs supply only the scenario name + the workflow body.
 */
import { test as base, expect, BrowserContext, Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const initScriptsDir = path.resolve(here, "../init-scripts");
const runsRoot = path.resolve(here, "../runs");

const INIT_SCRIPTS = [
  "instrument-cache.ts",
  "instrument-fetch.ts",
  "instrument-visibility.ts",
] as const;

export type ScenarioFixtures = {
  scenarioName: string;
  scenarioDir: string;
  contextWithCapture: BrowserContext;
  instrumentedPage: Page;
};

function loadInitScripts(): string {
  // Concatenate as plain text — these are IIFEs, not modules.
  return INIT_SCRIPTS.map((name) =>
    fs.readFileSync(path.join(initScriptsDir, name), "utf-8"),
  ).join("\n;\n");
}

export const test = base.extend<ScenarioFixtures>({
  scenarioName: ["unnamed", { option: true }],
  scenarioDir: async ({ scenarioName }, use) => {
    const dir = path.join(runsRoot, scenarioName);
    fs.mkdirSync(dir, { recursive: true });
    await use(dir);
  },
  contextWithCapture: async ({ browser, scenarioDir }, use) => {
    const ctx = await browser.newContext({
      recordHar: { path: path.join(scenarioDir, "network.har"), content: "embed" },
      // Tracing is on per playwright.config; we also start it programmatically
      // so we get per-scenario zip names alongside the HAR.
    });
    await ctx.tracing.start({
      screenshots: true,
      snapshots: true,
      sources: false,
    });
    const initScript = loadInitScripts();
    await ctx.addInitScript({ content: initScript });
    await use(ctx);
    await ctx.tracing.stop({ path: path.join(scenarioDir, "trace.zip") });
    await ctx.close();
  },
  instrumentedPage: async ({ contextWithCapture, scenarioDir }, use) => {
    const page = await contextWithCapture.newPage();

    // Capture console.log lines for the C1 'instrument-* loaded' sentinels
    // and any errors the app throws under our scenarios.
    const consoleLog: string[] = [];
    page.on("console", (msg) => {
      consoleLog.push(`[${msg.type()}] ${msg.text()}`);
    });
    page.on("pageerror", (err) => {
      consoleLog.push(`[pageerror] ${err.message}`);
    });

    // Persist console log + drain the instrumentation buffers at teardown.
    await use(page);

    fs.writeFileSync(path.join(scenarioDir, "console.log"), consoleLog.join("\n"));
    const drained = await page
      .evaluate(() => ({
        fetches: (window as unknown as { __dfoFetchLog?: unknown[] }).__dfoFetchLog ?? [],
        visibility:
          (window as unknown as { __dfoVisibilityLog?: unknown[] }).__dfoVisibilityLog ?? [],
        setQueryData:
          (window as unknown as { __dfoSetQueryDataLog?: unknown[] }).__dfoSetQueryDataLog ?? [],
        memory: ((): unknown => {
          const p = (
            performance as unknown as {
              memory?: { usedJSHeapSize: number; totalJSHeapSize: number };
            }
          ).memory;
          return p ? { used: p.usedJSHeapSize, total: p.totalJSHeapSize } : null;
        })(),
      }))
      .catch(() => ({ fetches: [], visibility: [], setQueryData: [], memory: null }));
    fs.writeFileSync(path.join(scenarioDir, "metrics.json"), JSON.stringify(drained, null, 2));
  },
});

export { expect };

/**
 * Take a V8 heap snapshot via CDP and write it to disk. Tag with a phase
 * label so the analyzer can plot heap drift across the scenario.
 */
export async function takeHeapSnapshot(page: Page, scenarioDir: string, phase: string) {
  const cdp = await page.context().newCDPSession(page);
  const chunks: string[] = [];
  cdp.on("HeapProfiler.addHeapSnapshotChunk", (e) => chunks.push(e.chunk));
  await cdp.send("HeapProfiler.takeHeapSnapshot", { reportProgress: false });
  await cdp.detach();
  fs.writeFileSync(path.join(scenarioDir, `heap-${phase}.heapsnapshot`), chunks.join(""));
}

/**
 * Sample performance.memory and the page's main-thread metrics at 1Hz
 * for `durationMs`. Writes to metrics.ndjson incrementally so a crashed
 * scenario still leaves partial data.
 */
export async function sampleMetrics(
  page: Page,
  scenarioDir: string,
  durationMs: number,
  intervalMs = 1000,
) {
  const out = path.join(scenarioDir, "metrics.ndjson");
  const stream = fs.createWriteStream(out, { flags: "a" });
  const end = Date.now() + durationMs;
  while (Date.now() < end) {
    const snap = await page
      .evaluate(() => {
        const p = (
          performance as unknown as {
            memory?: { usedJSHeapSize: number; totalJSHeapSize: number };
          }
        ).memory;
        return {
          ts: Date.now(),
          jsHeapUsed: p?.usedJSHeapSize ?? null,
          jsHeapTotal: p?.totalJSHeapSize ?? null,
          docVisibility: document.visibilityState,
          fetchCount:
            ((window as unknown as { __dfoFetchLog?: unknown[] }).__dfoFetchLog ?? []).length,
          setQueryDataCount:
            ((window as unknown as { __dfoSetQueryDataLog?: unknown[] }).__dfoSetQueryDataLog ?? [])
              .length,
        };
      })
      .catch(() => null);
    if (snap) stream.write(JSON.stringify(snap) + "\n");
    await new Promise((r) => setTimeout(r, intervalMs));
  }
  stream.end();
}
