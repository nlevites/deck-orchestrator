/**
 * Module-level event bus for connection-state signals.
 *
 * Decouples non-React emitters (the `request<T>` helper, the
 * `useLiveState` / `useLiveRunState` polling hooks) from the React
 * `ConnectionProvider` consumer. Emitters call `emitSignal(...)` from
 * anywhere; the provider subscribes once on mount and updates its
 * derived state.
 *
 * Three signals today:
 *   - "poll-ok"  — `useLiveState` completed a global `/api/state`
 *     fetch successfully. The provider bumps `lastPollOkAt`; the
 *     absence of this signal for the LIVE_PAUSED_THRESHOLD_MS window
 *     flips state → LIVE_PAUSED. ONLY the global hook emits this --
 *     run-scoped polls must not, otherwise navigating to /runs/:id
 *     could mask a global-poll failure (the global cache freezes but
 *     the banner stays clean because the run-scoped poll is fine).
 *   - "run-poll-ok" — `useLiveRunState` completed a successful run-
 *     scoped fetch. Currently informational; not consumed by the
 *     provider but kept distinct so future per-page health surfaces
 *     can subscribe.
 *   - "degraded" — `request<T>` saw a 503 with `code: DEGRADED_MODE`.
 *     The provider sets a sliding `degradedUntilAt = now + window`;
 *     while `now < degradedUntilAt` the state is DEGRADED_MODE.
 *
 * Strict-mode safe: `subscribe` returns its own cleanup that removes
 * the exact listener reference it added, so React's double-mount in
 * development doesn't leak handlers.
 */

export type ConnectionSignal = "poll-ok" | "run-poll-ok" | "degraded";

type Listener = (s: ConnectionSignal) => void;

const listeners = new Set<Listener>();

export function emitSignal(s: ConnectionSignal): void {
  listeners.forEach((l) => l(s));
}

export function subscribe(l: Listener): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}
