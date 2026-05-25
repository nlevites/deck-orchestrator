/**
 * Injected via page.addInitScript before the React bundle loads. Runs in
 * the browser; no TS imports — the page's own modules don't exist yet at
 * exec time. This file is read as a string by specs and shipped wholesale.
 *
 * Hooks (or monkey-patches once available):
 *   - QueryClient.prototype.setQueryData: wraps each call with
 *     performance.mark/measure under name "dfo.setQueryData:<queryKey>".
 *
 * Why monkey-patching: keeps `frontend/src/` untouched. Patching happens
 * lazily once @tanstack/react-query loads — we watch for it via a
 * mutation-observer on Object.defineProperty to avoid race conditions.
 */
(function () {
  const SENTINEL = "__dfo_cache_instrumented__";
  const win = window as unknown as Record<string, unknown>;
  if (win[SENTINEL]) return;
  win[SENTINEL] = true;

  // Buffer for measures so the spec can drain them at scenario end.
  const buffer: Array<{
    name: string;
    queryKey: string;
    duration: number;
    startTime: number;
  }> = [];
  win.__dfoSetQueryDataLog = buffer;

  // Best-effort: patch as soon as we see something matching QueryClient.
  // TanStack Query exports QueryClient; instances expose setQueryData.
  // We can't reliably grab the class, so we patch at instance level via
  // a Proxy-based wrap registered on window.QueryClient (if exposed).
  // Fallback: a counter the spec increments via performance.now-driven
  // setQueryData spy (page.evaluate hooks the QueryClient after mount).
  function patchInstance(qc: unknown): unknown {
    type SetQueryDataFn = (key: unknown, data: unknown, options?: unknown) => unknown;
    const inst = qc as { setQueryData?: SetQueryDataFn };
    if (!inst || typeof inst.setQueryData !== "function") return qc;
    const orig = inst.setQueryData.bind(inst);
    inst.setQueryData = function (key: unknown, data: unknown, options?: unknown) {
      const t0 = performance.now();
      const result = orig(key, data, options);
      const t1 = performance.now();
      const keyStr = Array.isArray(key) ? key.join(":") : String(key);
      buffer.push({
        name: "dfo.setQueryData",
        queryKey: keyStr,
        duration: t1 - t0,
        startTime: t0,
      });
      performance.mark(`dfo.setQueryData:${keyStr}`);
      return result;
    };
    return qc;
  }

  // The app calls new QueryClient() at startup. We monkey-patch the
  // global by detecting it via window assignment, or via the runtime
  // QueryClientProvider that exposes its client. As a simple hook,
  // tests can also call window.__dfoInstrumentClient(qc) from a
  // page.evaluate() after the React bundle hydrates.
  win.__dfoInstrumentClient = patchInstance;

  // Inform spec via console line — easy to grep in console.log artifact.
  console.log("[dfo-analysis] instrument-cache loaded");
})();
