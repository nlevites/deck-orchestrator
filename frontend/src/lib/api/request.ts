/**
 * Shared `fetch` wrapper for the orchestrator API.
 *
 * Every mutation in `lib/api/` goes through `request<T>` so error
 * handling stays consistent: on a non-2xx response, the body is
 * parsed as the OpenAPI `ErrorResponse` shape and thrown as an
 * `ApiError` that callers can `instanceof`-check and branch on.
 *
 * Two specific error-mapping cases worth knowing:
 *
 *   - 409 `VERSION_MISMATCH` becomes a `StateMovedError` so the
 *     existing mutation modals can keep their pre-real-backend
 *     "state moved; refresh" branch without changing any UI code.
 *   - All other codes surface as the generic `ApiError` with the
 *     code on `.code` for granular handling.
 */
import type { ErrorCode, ErrorResponse } from "@/lib/api-types";
import { emitSignal } from "@/lib/connection/signals";

export class ApiError extends Error {
  readonly code: ErrorCode | "UNKNOWN";
  readonly status: number;
  readonly details: Record<string, unknown> | undefined;
  readonly requestId: string | undefined;

  constructor(
    message: string,
    opts: {
      code: ErrorCode | "UNKNOWN";
      status: number;
      details?: Record<string, unknown>;
      requestId?: string;
    },
  ) {
    super(message);
    this.name = "ApiError";
    this.code = opts.code;
    this.status = opts.status;
    this.details = opts.details;
    this.requestId = opts.requestId;
  }
}

export class StateMovedError extends Error {
  readonly code = "STATE_MOVED" as const;
  readonly currentVersion: number;
  constructor(message: string, currentVersion: number) {
    super(message);
    this.name = "StateMovedError";
    this.currentVersion = currentVersion;
  }
}

export interface RequestOptions {
  method?: "GET" | "POST";
  body?: unknown;
  accept?: string;
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const init: RequestInit = {
    method: opts.method ?? "GET",
    headers: { Accept: opts.accept ?? "application/json" },
  };
  if (opts.body !== undefined) {
    init.body = JSON.stringify(opts.body);
    (init.headers as Record<string, string>)["Content-Type"] = "application/json";
  }
  // Wrap fetch so network failures (offline mid-request, DNS, dropped
  // connection) become ApiError("NETWORK") instead of a raw TypeError.
  // Without this, code/status-based UX paths (e.g. SubmitRunPage's
  // titleForError) silently fall through and operators see "Failed to
  // fetch" toasts. Don't emit "degraded" -- the connection layer
  // already infers offline/paused from poll signals + navigator.onLine.
  let resp: Response;
  try {
    resp = await fetch(path, init);
  } catch (err) {
    const message = err instanceof Error ? err.message : "Network request failed";
    throw new ApiError(message, { code: "UNKNOWN", status: 0, details: { kind: "NETWORK" } });
  }
  if (resp.ok) {
    if (resp.status === 204) return undefined as T;
    const text = await resp.text();
    return (text ? JSON.parse(text) : undefined) as T;
  }
  const raw = await resp.text().catch(() => "");
  let parsed: ErrorResponse | undefined;
  if (raw) {
    try {
      parsed = JSON.parse(raw) as ErrorResponse;
    } catch {
      // fall through to generic ApiError below
    }
  }
  const inner = parsed?.error as
    | { code?: ErrorCode; message?: string; request_id?: string; details?: Record<string, unknown> }
    | undefined;
  if (inner?.code === "VERSION_MISMATCH") {
    const currentVersion = numberFromDetails(inner.details, "current_version") ?? 0;
    throw new StateMovedError(inner.message ?? "State moved", currentVersion);
  }
  const code = inner?.code ?? "UNKNOWN";
  // Notify the connection bus so the banner flips to DEGRADED_MODE. The
  // backend's Degraded middleware only blocks non-GET requests during
  // startup reconciliation, so a single 503 here is a meaningful signal
  // that the orchestrator is alive-but-refusing-mutations.
  if (code === "DEGRADED_MODE") {
    emitSignal("degraded");
  }
  const message = inner?.message ?? raw ?? `${resp.status} ${resp.statusText}`;
  throw new ApiError(message, {
    code,
    status: resp.status,
    details: inner?.details,
    requestId: inner?.request_id,
  });
}

function numberFromDetails(
  details: Record<string, unknown> | undefined,
  key: string,
): number | undefined {
  if (!details) return undefined;
  const v = details[key];
  return typeof v === "number" ? v : undefined;
}
