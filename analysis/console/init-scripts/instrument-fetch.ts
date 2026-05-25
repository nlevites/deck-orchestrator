/**
 * Wrap window.fetch with start/end timestamps + byte sizes for response
 * bodies. The HAR already records this, but having a JS-side mirror keeps
 * everything in one timebase and gives us request_id correlation that the
 * server-side NDJSON exposes via the X-Request-Id header.
 */
(function () {
  const SENTINEL = "__dfo_fetch_instrumented__";
  const win = window as unknown as Record<string, unknown>;
  if (win[SENTINEL]) return;
  win[SENTINEL] = true;

  const log: Array<{
    url: string;
    method: string;
    status: number;
    t_start: number;
    t_end: number;
    duration: number;
    request_id: string | null;
    bytes_response: number;
  }> = [];
  win.__dfoFetchLog = log;

  const orig = window.fetch.bind(window);
  window.fetch = async function (input: RequestInfo | URL, init?: RequestInit) {
    const t0 = performance.now();
    const url = typeof input === "string" ? input : (input as Request).url ?? String(input);
    const method = (init?.method ?? "GET").toUpperCase();
    try {
      const resp = await orig(input, init);
      const t1 = performance.now();
      // We need to read the body to measure size, but doing so would
      // consume it. Clone first; cheap relative to network cost.
      let bytes = 0;
      try {
        const clone = resp.clone();
        const buf = await clone.arrayBuffer();
        bytes = buf.byteLength;
      } catch {
        // body unreadable (already consumed, opaque, etc.) — skip.
      }
      log.push({
        url,
        method,
        status: resp.status,
        t_start: t0,
        t_end: t1,
        duration: t1 - t0,
        request_id: resp.headers.get("X-Request-Id"),
        bytes_response: bytes,
      });
      return resp;
    } catch (err) {
      const t1 = performance.now();
      log.push({
        url,
        method,
        status: 0,
        t_start: t0,
        t_end: t1,
        duration: t1 - t0,
        request_id: null,
        bytes_response: 0,
      });
      throw err;
    }
  };

  console.log("[dfo-analysis] instrument-fetch loaded");
})();
