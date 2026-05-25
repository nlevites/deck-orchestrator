import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@/lib/cn";
import { colors } from "@/styles/tokens";
import { layoutDag, DAG_LAYOUT_DEFAULTS, type PositionedNode } from "@/lib/dag-layout";
import { criticalPathIds } from "@/lib/dag-topo";
import { apiKeys } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import { jobBlockReason } from "@/lib/ui-derive";
import type { Deck, DeckJob, DeckJobStatus, Run } from "@/lib/api-types";

interface DagViewerProps {
  run: Run;
  selectedJobId?: string;
  onSelectJob?: (id: string) => void;
  className?: string;
  highlightCriticalPath?: boolean;
}

const { nodeWidth: NODE_W, nodeHeight: NODE_H } = DAG_LAYOUT_DEFAULTS;

function nodeFill(s: DeckJobStatus): string {
  switch (s) {
    case "PENDING":
      return "#f7f6f6";
    case "READY":
      return "#f4ece1";
    case "DISPATCHED":
      return "#e7eef9";
    case "RUNNING":
      return "#e0effb";
    case "COMPLETED":
      return "#e7f1ea";
    case "FAILED":
      return "#fbe7e3";
    case "AMBIGUOUS":
      return "#f7eadb";
    case "CANCELLED":
      return "#f1f1f1";
  }
}

function nodeStroke(s: DeckJobStatus): string {
  switch (s) {
    case "PENDING":
      return "#e4e4e4";
    case "READY":
      return colors.status.ready;
    case "DISPATCHED":
      return colors.status.dispatched;
    case "RUNNING":
      return colors.status.running;
    case "COMPLETED":
      return colors.status.completed;
    case "FAILED":
      return colors.status.failed;
    case "AMBIGUOUS":
      return colors.status.ambiguous;
    case "CANCELLED":
      return colors.status.cancelled;
  }
}

function stepLabel(job: DeckJob): string {
  if (job.status === "RUNNING") {
    const done = job.last_completed_step ?? 0;
    const total = job.total_steps ?? job.steps.length;
    if (total > 1) {
      return `step ${done}/${total}`;
    }
  }
  return job.status.toLowerCase();
}

/**
 * "Upstream-blocked" = an unfinished job whose at least one upstream is FAILED,
 * AMBIGUOUS, or CANCELLED. The orchestrator won't mark it READY until
 * the upstream is resolved.
 *
 * "Deck-blocked" = a READY job whose target deck is unhealthy or slot-held.
 * The orchestrator is intentionally withholding dispatch.
 *
 * Both render with the blocked-stripes overlay and a "blocked" status label
 * in the node so the operator's first glance identifies the stall.
 */
function blockedSet(jobs: DeckJob[], decksById: Map<string, Deck>): Set<string> {
  const byId = new Map(jobs.map((j) => [j.id, j]));
  const BLOCKING: ReadonlySet<DeckJobStatus> = new Set(["FAILED", "AMBIGUOUS", "CANCELLED"]);
  const FINISHED: ReadonlySet<DeckJobStatus> = new Set(["COMPLETED", "CANCELLED"]);
  const out = new Set<string>();
  for (const j of jobs) {
    if (FINISHED.has(j.status)) continue;
    if (j.status === "FAILED" || j.status === "AMBIGUOUS") continue;
    for (const dep of j.depends_on) {
      const upstream = byId.get(dep);
      if (upstream && BLOCKING.has(upstream.status)) {
        out.add(j.id);
        break;
      }
    }
    if (!out.has(j.id) && jobBlockReason(j, decksById.get(j.deck_id)) !== null) {
      out.add(j.id);
    }
  }
  return out;
}

export function DagViewer({
  run,
  selectedJobId,
  onSelectJob,
  className,
  highlightCriticalPath = true,
}: DagViewerProps) {
  const { data: decks = [] } = useQuery<Deck[]>({
    queryKey: apiKeys.decks,
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
  });
  const decksById = useMemo(() => {
    const m = new Map<string, Deck>();
    for (const d of decks) m.set(d.id, d);
    return m;
  }, [decks]);

  const { nodes, width, height } = useMemo(() => layoutDag(run.deck_jobs), [run.deck_jobs]);
  const positions = useMemo(() => {
    const m = new Map<string, PositionedNode<DeckJob>>();
    for (const n of nodes) m.set(n.job.id, n);
    return m;
  }, [nodes]);
  const critical = useMemo(
    () => (highlightCriticalPath ? criticalPathIds(run.deck_jobs) : new Set<string>()),
    [run.deck_jobs, highlightCriticalPath],
  );
  const blocked = useMemo(() => blockedSet(run.deck_jobs, decksById), [run.deck_jobs, decksById]);
  const dimNonCritical = highlightCriticalPath && critical.size > 1;

  return (
    <div
      className={cn("overflow-auto rounded-panel border border-line bg-surface-warm", className)}
    >
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className="block"
        aria-label={`DAG for ${run.id}`}
      >
        <defs>
          <marker id="arrowhead" markerWidth="6" markerHeight="6" refX="5" refY="3" orient="auto">
            <path d="M0 0 L6 3 L0 6 Z" fill="#9b9696" />
          </marker>
          <marker
            id="arrowhead-critical"
            markerWidth="6"
            markerHeight="6"
            refX="5"
            refY="3"
            orient="auto"
          >
            <path d="M0 0 L6 3 L0 6 Z" fill={colors.status.running} />
          </marker>
          <pattern
            id="blocked-stripes"
            width="6"
            height="6"
            patternUnits="userSpaceOnUse"
            patternTransform="rotate(45)"
          >
            <rect width="6" height="6" fill="transparent" />
            <line
              x1="0"
              y1="0"
              x2="0"
              y2="6"
              stroke={colors.status.failed}
              strokeWidth="1.2"
              strokeOpacity="0.35"
            />
          </pattern>
        </defs>

        <g>
          {nodes.flatMap((n) =>
            n.job.depends_on.map((depId) => {
              const from = positions.get(depId);
              if (!from) return null;
              const x1 = from.x + NODE_W;
              const y1 = from.y + NODE_H / 2;
              const x2 = n.x;
              const y2 = n.y + NODE_H / 2;
              const mx = (x1 + x2) / 2;
              const onCritical = critical.has(depId) && critical.has(n.job.id);
              const dimEdge = dimNonCritical && !onCritical;
              return (
                <path
                  key={`${depId}->${n.job.id}`}
                  d={`M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`}
                  fill="none"
                  stroke={onCritical ? colors.status.running : "#9b9696"}
                  strokeWidth={onCritical ? 1.8 : 1.4}
                  strokeOpacity={dimEdge ? 0.35 : 1}
                  markerEnd={onCritical ? "url(#arrowhead-critical)" : "url(#arrowhead)"}
                />
              );
            }),
          )}
        </g>

        <g>
          {nodes.map((n) => {
            const isSelected = selectedJobId === n.job.id;
            const onCritical = critical.has(n.job.id);
            const isBlocked = blocked.has(n.job.id);
            const isLive = n.job.status === "RUNNING" || n.job.status === "DISPATCHED";
            const dim = dimNonCritical && !onCritical && !isSelected;
            return (
              <g
                key={n.job.id}
                transform={`translate(${n.x}, ${n.y})`}
                className="cursor-pointer"
                onClick={() => onSelectJob?.(n.job.id)}
                role="group"
                aria-label={`Job ${n.job.id}, status ${n.job.status}${onCritical ? " (critical path)" : ""}`}
                opacity={dim ? 0.55 : 1}
              >
                <rect
                  width={NODE_W}
                  height={NODE_H}
                  rx={10}
                  fill={nodeFill(n.job.status)}
                  stroke={isSelected ? "#272222" : nodeStroke(n.job.status)}
                  strokeWidth={isSelected ? 2 : onCritical ? 1.8 : 1.2}
                />
                {isBlocked && (
                  <rect
                    width={NODE_W}
                    height={NODE_H}
                    rx={10}
                    fill="url(#blocked-stripes)"
                    pointerEvents="none"
                  />
                )}
                {isLive && (
                  <circle
                    cx={NODE_W - 14}
                    cy={14}
                    r={4}
                    fill={colors.status.running}
                    className="animate-pulse-slow"
                  />
                )}
                <text
                  x={14}
                  y={20}
                  fontFamily="Geist Mono, ui-monospace, monospace"
                  fontSize={10}
                  fill="#6b6b6a"
                  letterSpacing="0.06em"
                >
                  {n.job.id}
                </text>
                <text
                  x={14}
                  y={40}
                  fontFamily="Geist, sans-serif"
                  fontSize={13}
                  fontWeight={600}
                  fill="#272222"
                  letterSpacing="-0.01em"
                >
                  {(n.job.steps[0]?.description ?? "(no steps)").slice(0, 28)}
                </text>
                <text x={14} y={60} fontFamily="Geist, sans-serif" fontSize={11} fill="#6b6b6a">
                  {n.job.deck_id} · {isBlocked ? "blocked" : stepLabel(n.job)}
                </text>
              </g>
            );
          })}
        </g>
      </svg>
    </div>
  );
}
