/**
 * Operator-gate hook. Mutation buttons that act on backend state
 * (cancel, retry, resolve, submit, chaos) consume this so the UI's
 * "actions disabled" promise in ConnectionBanner is actually
 * enforced.
 *
 * Without this, the banner copy ("Operator actions are disabled
 * until the browser reconnects") was a lie: every button stayed
 * clickable and operators got confusing 503/409 responses instead
 * of a clear "you can't act right now."
 *
 * The gate fires when ConnectionState is anything other than "OK":
 *   - OFFLINE: browser thinks it's offline; nothing will reach the
 *     orchestrator.
 *   - LIVE_PAUSED: poll has been failing for >2s; the operator's
 *     view is stale and acting on it risks a stale-state mutation.
 *   - DEGRADED_MODE: last mutation hit 503; orchestrator is
 *     reconciling and will refuse mutations anyway.
 *
 * The server-side guards (version checks, state machine) remain the
 * real safety net. This is the UI's promise that those guards
 * shouldn't fire because of a known-stale view.
 */
import { useConnection } from "@/lib/connection/use-connection";

export interface OperatorGate {
  disabled: boolean;
  reason: string;
}

const REASONS: Record<"OFFLINE" | "LIVE_PAUSED" | "DEGRADED_MODE", string> = {
  OFFLINE: "Browser is offline. Reconnect before acting on the run.",
  LIVE_PAUSED: "Live updates paused; the view may be stale. Wait for the connection to recover.",
  DEGRADED_MODE:
    "Orchestrator is reconciling after a restart. Mutations will be refused until it's ready.",
};

export function useOperatorGate(): OperatorGate {
  const { state } = useConnection();
  if (state === "OK") {
    return { disabled: false, reason: "" };
  }
  return { disabled: true, reason: REASONS[state] };
}
