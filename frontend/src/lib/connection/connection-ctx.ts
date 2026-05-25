/**
 * Bare React context for the connection state. Lives in its own file
 * (separate from both the Provider component and the `useConnection`
 * hook) so Fast Refresh can preserve component state — the
 * react-refresh plugin treats `createContext()` next to a component
 * export as HMR-unsafe.
 */
import { createContext } from "react";

export type ConnectionState =
  | "OK"
  | "OFFLINE" // navigator.onLine === false
  | "LIVE_PAUSED" // no successful /api/state poll within ConnectionProvider's threshold
  | "DEGRADED_MODE"; // last mutation returned 503 with code DEGRADED_MODE (sliding window)

export interface ConnectionInfo {
  state: ConnectionState;
  lastSyncAt: string;
  override: ConnectionState | null;
  setOverride: (state: ConnectionState | null) => void;
}

export const ConnectionCtx = createContext<ConnectionInfo | undefined>(undefined);
