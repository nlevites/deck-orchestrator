import type { RunStateSnapshot, StateSnapshot } from "@/lib/api-types";

export async function fetchState(sinceSeq: number, signal?: AbortSignal): Promise<StateSnapshot> {
  const resp = await fetch(`/api/state?since_seq=${sinceSeq}`, {
    method: "GET",
    headers: { Accept: "application/json" },
    signal,
  });
  if (!resp.ok) {
    const body = await safeBody(resp);
    throw new Error(`fetchState ${sinceSeq}: ${resp.status} ${body}`);
  }
  return (await resp.json()) as StateSnapshot;
}

export async function fetchRunState(
  runId: string,
  sinceSeq: number,
  signal?: AbortSignal,
): Promise<RunStateSnapshot> {
  const resp = await fetch(`/api/runs/${encodeURIComponent(runId)}/state?since_seq=${sinceSeq}`, {
    method: "GET",
    headers: { Accept: "application/json" },
    signal,
  });
  if (!resp.ok) {
    const body = await safeBody(resp);
    throw new Error(`fetchRunState ${runId} ${sinceSeq}: ${resp.status} ${body}`);
  }
  return (await resp.json()) as RunStateSnapshot;
}

async function safeBody(resp: Response): Promise<string> {
  try {
    return await resp.text();
  } catch {
    return "";
  }
}
