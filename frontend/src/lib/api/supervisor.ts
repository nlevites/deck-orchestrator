/**
 * Supervisor API client.
 *
 * The supervisor is a dev-only sidecar service (port 8090 by default,
 * proxied through Vite at /supervisor/*) that owns OS-level lifecycle
 * of the orchestrator + per-deck executor processes. The Settings >
 * Fleet Management page reads its inventory via these calls and
 * cross-joins with /api/decks (orchestrator-side health) to render
 * the fleet grid.
 *
 * Wire shapes mirror api/supervisor.openapi.yaml. We don't generate
 * TypeScript types from that file today — the surface is small and
 * dev-only, and codegen drift is the larger risk.
 */

export type ProcessKind = "orchestrator" | "executor";

export type ProcessState = "Starting" | "Running" | "Stopped" | "Crashing" | "FatalConfig";

export interface ProcessEntry {
  label: string;
  kind: ProcessKind;
  deck_id?: string;
  port?: number;
  db_path?: string;
  args?: string[];
  env?: string[];
  log_path?: string;
  policy: "always" | "never";
  state: ProcessState;
  pid?: number;
  started_at?: string;
  last_exit_code?: number | null;
  last_exit_reason?: string;
}

export interface ProcessesResponse {
  orchestrator?: ProcessEntry;
  executors: ProcessEntry[];
}

const BASE = "/supervisor";

async function call<T>(
  path: string,
  init?: { method?: "GET" | "POST" | "DELETE"; body?: unknown },
): Promise<T | undefined> {
  const opts: RequestInit = {
    method: init?.method ?? "GET",
    headers: { Accept: "application/json" },
  };
  if (init?.body !== undefined) {
    opts.body = JSON.stringify(init.body);
    (opts.headers as Record<string, string>)["Content-Type"] = "application/json";
  }
  const resp = await fetch(`${BASE}${path}`, opts);
  if (!resp.ok) {
    const text = await resp.text().catch(() => "");
    throw new SupervisorError(`supervisor ${path}: ${resp.status} ${text}`, resp.status);
  }
  if (resp.status === 204) return undefined;
  const text = await resp.text();
  if (!text) return undefined;
  return JSON.parse(text) as T;
}

export class SupervisorError extends Error {
  readonly status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = "SupervisorError";
    this.status = status;
  }
}

export async function listSupervisorProcesses(): Promise<ProcessesResponse> {
  return (await call<ProcessesResponse>("/processes")) ?? { executors: [] };
}

export async function restartOrchestratorViaSupervisor(): Promise<void> {
  await call("/orchestrator/restart", { method: "POST" });
}

export async function stopOrchestrator(): Promise<void> {
  await call("/orchestrator/stop", { method: "POST" });
}

export async function startOrchestrator(): Promise<void> {
  await call("/orchestrator/start", { method: "POST" });
}

export async function attachExecutor(
  deckId: string,
  freshState = false,
): Promise<ProcessEntry | undefined> {
  return await call<ProcessEntry>("/executors", {
    method: "POST",
    body: { deck_id: deckId, fresh_state: freshState },
  });
}

export async function stopExecutor(deckId: string): Promise<void> {
  await call(`/executors/${encodeURIComponent(deckId)}/stop`, { method: "POST" });
}

export async function startExecutor(deckId: string): Promise<void> {
  await call(`/executors/${encodeURIComponent(deckId)}/start`, { method: "POST" });
}

export async function restartExecutor(deckId: string): Promise<void> {
  await call(`/executors/${encodeURIComponent(deckId)}/restart`, { method: "POST" });
}

export async function detachExecutor(deckId: string): Promise<void> {
  await call(`/executors/${encodeURIComponent(deckId)}`, { method: "DELETE" });
}
