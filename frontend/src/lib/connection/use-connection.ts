/**
 * `useConnection` lives in its own file so [ConnectionContext.tsx](./ConnectionContext.tsx)
 * remains HMR-pure (react-refresh/only-export-components — Fast Refresh
 * only works when a component file exports only components).
 */
import { useContext } from "react";
import { ConnectionCtx, type ConnectionInfo } from "./connection-ctx";

export function useConnection(): ConnectionInfo {
  const ctx = useContext(ConnectionCtx);
  if (ctx === undefined) {
    throw new Error("useConnection must be used inside a <ConnectionProvider>");
  }
  return ctx;
}
