import { emitSignal } from "@/lib/connection/signals";

/**
 * Admin API client. One route today (POST /api/admin/restart).
 *
 * The orchestrator returns 202 then exits; a supervisor (`make demo` in
 * dev, systemd / k8s in prod) is responsible for relaunching it.
 *
 * We emit a `degraded` connection signal on 202 so the console flashes
 * the DEGRADED_MODE banner across the brief window where the server is
 * down + reconciling on respawn. This is the only path where the client
 * has prior knowledge that the server is about to be unavailable — the
 * usual LIVE_PAUSED signal would otherwise win since polls just stop
 * arriving without an explicit 503.
 */
export async function restartOrchestrator(): Promise<void> {
  const resp = await fetch(`/api/admin/restart`, {
    method: "POST",
    headers: { Accept: "application/json" },
  });
  if (!resp.ok) {
    const body = await resp.text().catch(() => "");
    throw new Error(`restart orchestrator: ${resp.status} ${body}`);
  }
  emitSignal("degraded");
}
