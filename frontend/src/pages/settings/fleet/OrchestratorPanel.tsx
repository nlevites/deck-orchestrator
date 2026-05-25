import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Power, RotateCcw, Square } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Card } from "@/components/primitives/Card";
import { Modal } from "@/components/primitives/Modal";
import { TimeAgo } from "@/components/primitives/TimeAgo";
import { apiKeys } from "@/lib/api";
import {
  restartOrchestratorViaSupervisor,
  startOrchestrator,
  stopOrchestrator,
  type ProcessesResponse,
} from "@/lib/api/supervisor";
import { useToast } from "@/lib/toasts/use-toast";
import { ProcessStatePill } from "./ProcessStatePill";

interface OrchestratorPanelProps {
  processes?: ProcessesResponse;
  loading: boolean;
}

export function OrchestratorPanel({ processes, loading }: OrchestratorPanelProps) {
  const orch = processes?.orchestrator;
  const [restartOpen, setRestartOpen] = useState(false);
  const toast = useToast();
  const qc = useQueryClient();

  const restart = useMutation({
    mutationFn: restartOrchestratorViaSupervisor,
    onSuccess: () => {
      toast.push({
        kind: "warning",
        title: "Orchestrator restarting",
        body: "Mutations will refuse for a few seconds while it comes back up and reconciles.",
        timeoutMs: 6000,
      });
      // The runs/decks caches are populated by useLiveState; the next
      // 1s poll picks up the orchestrator's post-restart view. Only
      // the supervisor query (a real fetched query) is invalidated.
      void qc.invalidateQueries({ queryKey: apiKeys.supervisor });
      setRestartOpen(false);
    },
    onError: (err) =>
      toast.push({
        kind: "error",
        title: "Restart failed",
        body: err instanceof Error ? err.message : "Unknown error.",
      }),
  });

  const stop = useMutation({
    mutationFn: stopOrchestrator,
    onSuccess: () => qc.invalidateQueries({ queryKey: apiKeys.supervisor }),
    onError: (err) =>
      toast.push({
        kind: "error",
        title: "Stop failed",
        body: err instanceof Error ? err.message : "Unknown error.",
      }),
  });

  const start = useMutation({
    mutationFn: startOrchestrator,
    onSuccess: () => qc.invalidateQueries({ queryKey: apiKeys.supervisor }),
    onError: (err) =>
      toast.push({
        kind: "error",
        title: "Start failed",
        body: err instanceof Error ? err.message : "Unknown error.",
      }),
  });

  if (loading && !orch) {
    return <Card className="p-4 text-[13px] text-ink-muted">Loading supervisor state…</Card>;
  }
  if (!orch) {
    return (
      <Card className="p-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="text-[15px] font-semibold tracking-sub text-ink">Orchestrator</h2>
            <p className="mt-1 text-[12.5px] text-ink-muted">
              Supervisor unreachable. Is <code className="font-mono">backend/bin/supervisor</code>{" "}
              running? Try <code className="font-mono">make demo</code>.
            </p>
          </div>
        </div>
      </Card>
    );
  }

  const isRunning = orch.state === "Running" || orch.state === "Starting";

  return (
    <>
      <Card className="p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex items-center gap-3">
              <h2 className="text-[15px] font-semibold tracking-sub text-ink">Orchestrator</h2>
              <ProcessStatePill state={orch.state} />
            </div>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 font-mono text-[12px] tracking-nav text-ink-muted">
              {orch.pid ? <span>PID {orch.pid}</span> : null}
              {orch.port ? <span>:{orch.port}</span> : null}
              {orch.started_at && isRunning ? (
                <span>
                  up <TimeAgo timestamp={orch.started_at} />
                </span>
              ) : null}
              {orch.last_exit_reason && !isRunning ? <span>{orch.last_exit_reason}</span> : null}
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {isRunning ? (
              <>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => setRestartOpen(true)}
                  disabled={restart.isPending}
                >
                  <RotateCcw size={13} />
                  Restart gracefully
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => stop.mutate()}
                  disabled={stop.isPending}
                >
                  <Square size={13} />
                  Stop
                </Button>
              </>
            ) : (
              <Button
                variant="primary"
                size="sm"
                onClick={() => start.mutate()}
                disabled={start.isPending}
              >
                <Power size={13} />
                Start
              </Button>
            )}
          </div>
        </div>
      </Card>
      <Modal
        open={restartOpen}
        onClose={() => setRestartOpen(false)}
        title="Restart orchestrator"
        size="sm"
        footer={
          <>
            <Button
              variant="ghost"
              onClick={() => setRestartOpen(false)}
              disabled={restart.isPending}
            >
              Cancel
            </Button>
            <Button variant="danger" onClick={() => restart.mutate()} disabled={restart.isPending}>
              <RotateCcw size={14} />
              {restart.isPending ? "Restarting…" : "Restart"}
            </Button>
          </>
        }
      >
        <p className="text-[13px] leading-5 text-ink-muted">
          The orchestrator drains in-flight requests and exits. The supervisor relaunches it; during
          the gap, every <code className="font-mono">POST /api/*</code> returns 503 DEGRADED_MODE
          while it reconciles in-flight work against each executor&apos;s state.
        </p>
      </Modal>
    </>
  );
}
