/**
 * Log document.visibilityState transitions so the C1 (backgrounded-tab
 * keeps polling) check can verify behavior. Pairs with the fetch log to
 * count poll requests that fire while the tab was hidden.
 */
(function () {
  const SENTINEL = "__dfo_visibility_instrumented__";
  const win = window as unknown as Record<string, unknown>;
  if (win[SENTINEL]) return;
  win[SENTINEL] = true;

  const log: Array<{ ts: number; state: DocumentVisibilityState }> = [];
  win.__dfoVisibilityLog = log;

  log.push({ ts: performance.now(), state: document.visibilityState });
  document.addEventListener("visibilitychange", () => {
    log.push({ ts: performance.now(), state: document.visibilityState });
  });

  console.log("[dfo-analysis] instrument-visibility loaded");
})();
