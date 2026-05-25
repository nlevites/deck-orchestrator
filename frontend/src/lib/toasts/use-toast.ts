/**
 * `useToast` lives in its own file so [ToastContext.tsx](./ToastContext.tsx)
 * remains HMR-pure (react-refresh/only-export-components — Fast Refresh
 * only works when a component file exports only components).
 */
import { useContext } from "react";
import { ToastCtx, type ToastApi } from "./toast-ctx";

export function useToast(): ToastApi {
  const ctx = useContext(ToastCtx);
  if (!ctx) throw new Error("useToast must be used within a <ToastProvider>");
  return ctx;
}
