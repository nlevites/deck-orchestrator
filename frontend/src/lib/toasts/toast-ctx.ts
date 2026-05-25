/**
 * Bare React context for the toast queue. Lives in its own file
 * (separate from both the ToastProvider component and the `useToast`
 * hook) so Fast Refresh can preserve component state — the
 * react-refresh plugin treats `createContext()` next to a component
 * export as HMR-unsafe.
 */
import { createContext } from "react";

export type ToastKind = "success" | "info" | "warning" | "error";

export interface Toast {
  id: string;
  kind: ToastKind;
  title: string;
  body?: string;
  action?: { label: string; onClick: () => void };
  timeoutMs?: number;
}

export interface ToastApi {
  toasts: Toast[];
  push: (toast: Omit<Toast, "id">) => string;
  dismiss: (id: string) => void;
}

export const ToastCtx = createContext<ToastApi | undefined>(undefined);
