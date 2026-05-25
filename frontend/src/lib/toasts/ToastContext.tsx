import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { AlertTriangle, CheckCircle2, Info, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { ToastCtx, type Toast, type ToastKind } from "./toast-ctx";

let nextToastId = 0;

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const push = useCallback((toast: Omit<Toast, "id">) => {
    const id = `toast-${++nextToastId}`;
    setToasts((prev) => [...prev, { id, timeoutMs: 5000, ...toast }]);
    return id;
  }, []);

  return (
    <ToastCtx.Provider value={{ toasts, push, dismiss }}>
      {children}
      <ToastViewport toasts={toasts} dismiss={dismiss} />
    </ToastCtx.Provider>
  );
}

interface ToastViewportProps {
  toasts: Toast[];
  dismiss: (id: string) => void;
}

function ToastViewport({ toasts, dismiss }: ToastViewportProps) {
  if (toasts.length === 0) return null;
  return createPortal(
    <div
      aria-live="polite"
      className="fixed bottom-4 right-4 z-[110] flex w-full max-w-[420px] flex-col gap-2"
    >
      {toasts.map((t) => (
        <ToastItem key={t.id} toast={t} onDismiss={() => dismiss(t.id)} />
      ))}
    </div>,
    document.body,
  );
}

const kindIcon: Record<ToastKind, typeof CheckCircle2> = {
  success: CheckCircle2,
  info: Info,
  warning: AlertTriangle,
  error: AlertTriangle,
};

const kindClasses: Record<ToastKind, { ring: string; text: string }> = {
  success: { ring: "border-status-completed/20 bg-[#f1f8f4]", text: "text-status-completed" },
  info: { ring: "border-accent-link/20 bg-[#eef3fb]", text: "text-accent-link" },
  warning: { ring: "border-status-ambiguous/20 bg-[#fff7ec]", text: "text-status-ambiguous" },
  error: { ring: "border-status-failed/20 bg-[#fff3f1]", text: "text-status-failed" },
};

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: () => void }) {
  useEffect(() => {
    if (!toast.timeoutMs) return;
    const t = setTimeout(onDismiss, toast.timeoutMs);
    return () => clearTimeout(t);
  }, [toast.timeoutMs, onDismiss]);

  const Icon = kindIcon[toast.kind];
  const classes = kindClasses[toast.kind];

  return (
    <div
      role="status"
      className={cn(
        "flex items-start gap-3 rounded-card border bg-surface p-3 shadow-card",
        classes.ring,
      )}
    >
      <span className={cn("mt-0.5", classes.text)}>
        <Icon size={16} strokeWidth={1.7} />
      </span>
      <div className="flex flex-1 flex-col gap-1">
        <div className="text-[13px] font-semibold tracking-nav text-ink">{toast.title}</div>
        {toast.body && (
          <div className="text-[12px] leading-[1.45] tracking-nav text-ink-muted">{toast.body}</div>
        )}
        {toast.action && (
          <button
            type="button"
            onClick={() => {
              toast.action?.onClick();
              onDismiss();
            }}
            className={cn(
              "mt-1 self-start text-[12px] font-semibold tracking-nav underline-offset-4 hover:underline",
              classes.text,
            )}
          >
            {toast.action.label}
          </button>
        )}
      </div>
      <button
        type="button"
        onClick={onDismiss}
        className="text-ink-nav transition-colors hover:text-ink"
        aria-label="Dismiss notification"
      >
        <X size={14} strokeWidth={1.7} />
      </button>
    </div>
  );
}
