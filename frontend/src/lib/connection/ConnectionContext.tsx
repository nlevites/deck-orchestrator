import { useEffect, useMemo, useState } from "react";
import { ConnectionCtx, type ConnectionInfo, type ConnectionState } from "./connection-ctx";
import { subscribe } from "./signals";

/**
 * Four operator-visible connection states from ARCHITECTURE.md §7.1.
 * The states are deliberately distinct because conflating them erodes
 * operator trust the first time a "stale" deck self-recovers after the
 * operator had treated it as failed.
 *
 * State derivation (highest priority first):
 *   1. `override`         — dev-only URL/page setter for screenshots.
 *   2. `OFFLINE`          — navigator.onLine === false.
 *   3. `DEGRADED_MODE`    — request.ts saw 503 DEGRADED_MODE within the
 *                            last DEGRADED_WINDOW_MS (sliding window).
 *   4. `LIVE_PAUSED`      — no successful /api/state poll observed within
 *                            the last LIVE_PAUSED_THRESHOLD_MS.
 *   5. `OK`               — none of the above.
 *
 * Emitters (see [./signals.ts](./signals.ts)):
 *   - request.ts          emits "degraded" on 503 ApiError(DEGRADED_MODE).
 *   - useLiveState        emits "poll-ok" on every successful fetchState
 *                         (the only signal that bumps lastPollOkAt).
 *   - useLiveRunState     emits "run-poll-ok" -- informational, not
 *                         consumed here. Splitting them prevents a
 *                         healthy run-scoped poll from masking a
 *                         failing global poll.
 *
 * The provider runs a TICK_INTERVAL_MS heartbeat so the LIVE_PAUSED /
 * DEGRADED_MODE thresholds re-evaluate even when no signals arrive — the
 * derived state must transition on time, not only on input events.
 *
 * The context object itself lives in [./connection-ctx.ts](./connection-ctx.ts)
 * and the `useConnection` consumer hook in
 * [./use-connection.ts](./use-connection.ts) so this file is HMR-pure
 * (react-refresh/only-export-components).
 */

// Two missed 1s polls → flip to LIVE_PAUSED. Tight enough to be
// demo-visible right after a `kill -9`, loose enough that a single
// slow tick doesn't flicker the banner.
const LIVE_PAUSED_THRESHOLD_MS = 2_000;
// Auto-clear DEGRADED_MODE after this many ms with no new 503. The
// orchestrator's degraded window in practice is well under a second
// (startup reconciliation); 3s gives the operator time to read the
// banner before it disappears.
const DEGRADED_WINDOW_MS = 3_000;
// Re-render cadence for time-based transitions. 500ms gives ~0.5s
// resolution on "now - lastPollOkAt", which is finer than the 2s
// threshold so the banner appears within ~2.5s of an outage.
const TICK_INTERVAL_MS = 500;

/**
 * Read a dev-only override from the URL query string so screenshots can
 * trigger every banner without flipping the network or killing the server.
 *
 *   /?connection=offline       → OFFLINE
 *   /?connection=live          → LIVE_PAUSED
 *   /?connection=degraded      → DEGRADED_MODE
 *   /?connection=ok            → OK (default)
 *
 * Override wins over real signals. Without an override, the provider
 * derives state from navigator.onLine + the signal bus (see signals.ts).
 */
function readUrlOverride(): ConnectionState | null {
  if (typeof window === "undefined") return null;
  const value = new URLSearchParams(window.location.search).get("connection");
  if (!value) return null;
  switch (value.toLowerCase()) {
    case "offline":
      return "OFFLINE";
    case "live":
    case "live-paused":
    case "paused":
      return "LIVE_PAUSED";
    case "degraded":
    case "degraded-mode":
      return "DEGRADED_MODE";
    case "ok":
      return "OK";
    default:
      return null;
  }
}

export function ConnectionProvider({ children }: { children: React.ReactNode }) {
  const [browserOnline, setBrowserOnline] = useState(
    typeof navigator === "undefined" ? true : navigator.onLine,
  );
  const [override, setOverride] = useState<ConnectionState | null>(() => readUrlOverride());

  useEffect(() => {
    const handleOnline = () => setBrowserOnline(true);
    const handleOffline = () => setBrowserOnline(false);
    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);
    return () => {
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
    };
  }, []);

  const [lastPollOkAt, setLastPollOkAt] = useState(() => Date.now());
  const [degradedUntilAt, setDegradedUntilAt] = useState(0);

  useEffect(() => {
    return subscribe((s) => {
      if (s === "poll-ok") {
        setLastPollOkAt(Date.now());
      } else if (s === "degraded") {
        setDegradedUntilAt(Date.now() + DEGRADED_WINDOW_MS);
      }
    });
  }, []);

  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), TICK_INTERVAL_MS);
    return () => clearInterval(t);
  }, []);

  // Bump `lastSyncAt` whenever any input that affects the derived state
  // changes. Done in render via a stored previous-value snapshot per
  // React's "adjusting state when props/state change" pattern — this
  // avoids both the impure Date.now() during render AND the useEffect →
  // setState cascade flagged by react-hooks/set-state-in-effect.
  const [lastSyncAt, setLastSyncAt] = useState(() => new Date().toISOString());
  const [prevInputs, setPrevInputs] = useState({
    override,
    browserOnline,
    lastPollOkAt,
    degradedUntilAt,
  });
  if (
    prevInputs.override !== override ||
    prevInputs.browserOnline !== browserOnline ||
    prevInputs.lastPollOkAt !== lastPollOkAt ||
    prevInputs.degradedUntilAt !== degradedUntilAt
  ) {
    setPrevInputs({ override, browserOnline, lastPollOkAt, degradedUntilAt });
    setLastSyncAt(new Date().toISOString());
  }

  const value = useMemo<ConnectionInfo>(() => {
    let state: ConnectionState = "OK";
    if (override !== null) {
      state = override;
    } else if (!browserOnline) {
      state = "OFFLINE";
    } else if (now < degradedUntilAt) {
      state = "DEGRADED_MODE";
    } else if (now - lastPollOkAt > LIVE_PAUSED_THRESHOLD_MS) {
      state = "LIVE_PAUSED";
    }
    return { state, lastSyncAt, override, setOverride };
  }, [override, browserOnline, now, degradedUntilAt, lastPollOkAt, lastSyncAt]);

  return <ConnectionCtx.Provider value={value}>{children}</ConnectionCtx.Provider>;
}
