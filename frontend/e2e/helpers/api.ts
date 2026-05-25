import type { components } from "../../src/api/gen";

type S = components["schemas"];
export type Run = S["Run"];
export type RunSummary = S["RunSummary"];
export type Deck = S["Deck"];
export type DeckJob = S["DeckJob"];
export type DagSubmission = S["DagSubmission"];
export type Event = S["Event"];
export type StateSnapshot = S["StateSnapshot"];
export type ChaosState = S["ChaosState"];
export type ChaosPatch = S["ChaosPatch"];
export type AttemptOutcome = S["AttemptOutcome"];
export type RunStatus = S["RunStatus"];
export type DeckJobStatus = S["DeckJobStatus"];
export type EventKind = S["EventKind"];
export type ErrorCode = S["ErrorCode"];

const ORCH_URL = process.env.DFO_E2E_ORCH_URL?.replace(/\/health$/, "") ?? "http://localhost:18080";

export class ApiError extends Error {
  readonly status: number;
  readonly code: ErrorCode | "UNKNOWN";
  readonly details?: Record<string, unknown>;
  constructor(
    message: string,
    status: number,
    code: ErrorCode | "UNKNOWN",
    details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const init: RequestInit = {
    method: opts.method ?? "GET",
    headers: opts.body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    signal: opts.signal,
  };
  const resp = await fetch(`${ORCH_URL}${path}`, init);
  if (resp.status === 204) return undefined as T;
  const text = await resp.text();
  if (resp.ok) {
    return text ? (JSON.parse(text) as T) : (undefined as T);
  }
  let code: ErrorCode | "UNKNOWN" = "UNKNOWN";
  let message = `${resp.status} ${resp.statusText}`;
  let details: Record<string, unknown> | undefined;
  try {
    const parsed = JSON.parse(text) as {
      error?: { code?: ErrorCode; message?: string; details?: Record<string, unknown> };
    };
    if (parsed.error?.code) code = parsed.error.code;
    if (parsed.error?.message) message = parsed.error.message;
    if (parsed.error?.details) details = parsed.error.details;
  } catch {
    // non-JSON error body — use status text
  }
  throw new ApiError(message, resp.status, code, details);
}

export async function submitRun(dag: DagSubmission): Promise<Run> {
  return request<Run>("/api/runs", { method: "POST", body: dag });
}

export async function getRun(id: string): Promise<Run> {
  return request<Run>(`/api/runs/${encodeURIComponent(id)}`);
}

export async function listRuns(): Promise<RunSummary[]> {
  const body = await request<{ runs: RunSummary[] }>("/api/runs");
  return body.runs;
}

export async function listDecks(): Promise<Deck[]> {
  const body = await request<{ decks: Deck[] }>("/api/decks");
  return body.decks;
}

export async function cancelRun(runId: string, expectedVersion: number): Promise<Run> {
  return request<Run>(`/api/runs/${encodeURIComponent(runId)}/cancel`, {
    method: "POST",
    body: { expected_version: expectedVersion },
  });
}

export async function retryJob(
  runId: string,
  jobId: string,
  expectedVersion: number,
): Promise<Run> {
  return request<Run>(
    `/api/runs/${encodeURIComponent(runId)}/jobs/${encodeURIComponent(jobId)}/retry`,
    { method: "POST", body: { expected_version: expectedVersion } },
  );
}

export async function resolveJob(
  runId: string,
  jobId: string,
  expectedVersion: number,
  resolution: AttemptOutcome,
  operatorNote?: string,
): Promise<Run> {
  return request<Run>(
    `/api/runs/${encodeURIComponent(runId)}/jobs/${encodeURIComponent(jobId)}/resolve`,
    {
      method: "POST",
      body: {
        expected_version: expectedVersion,
        resolution,
        ...(operatorNote ? { operator_note: operatorNote } : {}),
      },
    },
  );
}

export async function getChaos(deckId: string): Promise<ChaosState> {
  return request<ChaosState>(`/api/decks/${encodeURIComponent(deckId)}/chaos`);
}

export async function patchChaos(deckId: string, patch: ChaosPatch): Promise<ChaosState> {
  return request<ChaosState>(`/api/decks/${encodeURIComponent(deckId)}/chaos`, {
    method: "POST",
    body: patch,
  });
}

export async function resetChaos(deckId: string): Promise<ChaosState> {
  return request<ChaosState>(`/api/decks/${encodeURIComponent(deckId)}/chaos/reset`, {
    method: "POST",
  });
}

export async function crashDeck(deckId: string): Promise<void> {
  await request(`/api/decks/${encodeURIComponent(deckId)}/chaos/crash`, { method: "POST" });
}

export async function restartOrchestrator(): Promise<void> {
  await request("/api/admin/restart", { method: "POST" });
}

/** SIGKILL escape hatch via the supervisor's /kill route. */
export async function killOrchestrator(): Promise<void> {
  const supUrl =
    process.env.DFO_E2E_SUP_URL ??
    ORCH_URL.replace(/:1[78]080$/, ":18090").replace(/:8080$/, ":8090");
  const resp = await fetch(`${supUrl}/supervisor/orchestrator/kill`, { method: "POST" });
  if (!resp.ok && resp.status !== 202) {
    throw new Error(`killOrchestrator: ${resp.status} ${resp.statusText}`);
  }
}

export async function getState(sinceSeq = 0): Promise<StateSnapshot> {
  return request<StateSnapshot>(`/api/state?since_seq=${sinceSeq}`);
}

/** Reset demo-deck chaos in afterEach; swallow 502s while supervisor respawns. */
export async function resetAllChaos(
  deckIds = ["deck-1", "deck-2", "deck-3", "deck-4"],
): Promise<void> {
  await Promise.all(
    deckIds.map((id) =>
      resetChaos(id).catch(() => {
        // executor unreachable mid-respawn
      }),
    ),
  );
}

/** Poll /health until 200 — post-restart readiness. */
export async function waitForOrchestratorHealth(deadlineMs = 15_000): Promise<void> {
  const stop = Date.now() + deadlineMs;
  while (Date.now() < stop) {
    try {
      const resp = await fetch(`${ORCH_URL}/health`);
      if (resp.ok) return;
    } catch {
      // tcp closed during restart
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`orchestrator /health did not return 200 within ${deadlineMs}ms`);
}

/**
 * HEALTHY alone is stale for ~1s post-crash while the port is unbound;
 * chaos GET via the orchestrator proxy confirms the executor is reachable.
 */
export async function waitForDecksHealthy(
  ids = ["deck-1", "deck-2", "deck-3", "deck-4"],
  deadlineMs = 15_000,
): Promise<void> {
  const stop = Date.now() + deadlineMs;
  while (Date.now() < stop) {
    try {
      const decks = await listDecks();
      const seen = new Map(decks.map((d) => [d.id, d.last_known_health]));
      const allHealthy = ids.every((id) => seen.get(id) === "HEALTHY");
      if (allHealthy) {
        // 502 until port binds after respawn
        const probes = await Promise.all(
          ids.map((id) =>
            getChaos(id)
              .then(() => true)
              .catch(() => false),
          ),
        );
        if (probes.every(Boolean)) return;
      }
    } catch {
      // orchestrator briefly unreachable mid-restart
    }
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error(
    `decks ${ids.join(",")} did not all return HEALTHY (+ chaos reachable) within ${deadlineMs}ms`,
  );
}

// Supervisor on :18090 — direct setup/teardown, faster than UI-driven lifecycle.

const SUP_URL = process.env.DFO_E2E_SUP_URL ?? "http://localhost:18090";

export type ProcessState = "Starting" | "Running" | "Stopped" | "Crashing" | "FatalConfig";

export interface SupervisorProcessEntry {
  label: string;
  kind: "orchestrator" | "executor";
  deck_id?: string;
  port?: number;
  state: ProcessState;
  policy: "always" | "never";
  pid?: number;
}

export interface SupervisorProcessesResponse {
  orchestrator?: SupervisorProcessEntry;
  executors: SupervisorProcessEntry[];
}

async function supRequest<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const init: RequestInit = {
    method: opts.method ?? "GET",
    headers: opts.body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  };
  const resp = await fetch(`${SUP_URL}${path}`, init);
  if (resp.status === 204) return undefined as T;
  const text = await resp.text();
  if (resp.ok) {
    return text ? (JSON.parse(text) as T) : (undefined as T);
  }
  throw new Error(`supervisor ${path}: ${resp.status} ${text}`);
}

export async function listSupervisorProcesses(): Promise<SupervisorProcessesResponse> {
  return supRequest<SupervisorProcessesResponse>("/supervisor/processes");
}

export async function attachExecutorViaSupervisor(
  deckId: string,
  freshState = false,
): Promise<void> {
  await supRequest("/supervisor/executors", {
    method: "POST",
    body: { deck_id: deckId, fresh_state: freshState },
  });
}

export async function detachExecutorViaSupervisor(deckId: string): Promise<void> {
  await supRequest(`/supervisor/executors/${encodeURIComponent(deckId)}`, {
    method: "DELETE",
  });
}

/** Poll supervisor ProcessTable until deck_id is absent. */
export async function waitForExecutorAbsent(deckId: string, deadlineMs = 5_000): Promise<void> {
  const stop = Date.now() + deadlineMs;
  while (Date.now() < stop) {
    try {
      const list = await listSupervisorProcesses();
      if (!list.executors.some((e) => e.deck_id === deckId)) return;
    } catch {
      // supervisor blip
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`supervisor still has ${deckId} attached after ${deadlineMs}ms`);
}

export async function waitForExecutorState(
  deckId: string,
  want: ProcessState | ProcessState[],
  deadlineMs = 8_000,
): Promise<void> {
  const wantSet = new Set(Array.isArray(want) ? want : [want]);
  const stop = Date.now() + deadlineMs;
  let lastState: string | undefined;
  while (Date.now() < stop) {
    try {
      const list = await listSupervisorProcesses();
      const e = list.executors.find((x) => x.deck_id === deckId);
      lastState = e?.state;
      if (e && wantSet.has(e.state)) return;
    } catch {
      // supervisor blip
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(
    `supervisor ${deckId} did not reach ${[...wantSet].join("|")} within ${deadlineMs}ms (last=${lastState ?? "absent"})`,
  );
}
