import { useMemo } from "react";
import { cn } from "@/lib/cn";
import { layoutDag, DAG_LAYOUT_DEFAULTS, type PositionedNode } from "@/lib/dag-layout";
import type { ParsedDag, ParsedDeckJob } from "@/lib/dag-validate";

interface DagPreviewProps {
  parsed: ParsedDag | undefined;
  className?: string;
}

const { nodeWidth: NODE_W, nodeHeight: NODE_H } = DAG_LAYOUT_DEFAULTS;

/**
 * Pre-submit DAG preview. Same Kahn layout as DagViewer but stripped of
 * status coloring, selection, and click handlers since these jobs don't
 * exist yet on the orchestrator. Renders an empty-state hint when nothing
 * has been parsed.
 */
export function DagPreview({ parsed, className }: DagPreviewProps) {
  const jobs = useMemo(() => parsed?.deck_jobs ?? [], [parsed]);
  const { nodes, width, height } = useMemo(() => layoutDag(jobs), [jobs]);
  const positions = useMemo(() => {
    const m = new Map<string, PositionedNode<ParsedDeckJob>>();
    for (const n of nodes) m.set(n.job.id, n);
    return m;
  }, [nodes]);

  if (jobs.length === 0) return null;

  return (
    <div
      className={cn("overflow-auto rounded-panel border border-line bg-surface-warm", className)}
    >
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className="block"
        aria-label={parsed ? `Preview of ${parsed.id}` : "DAG preview"}
      >
        <defs>
          <marker
            id="arrowhead-preview"
            markerWidth="6"
            markerHeight="6"
            refX="5"
            refY="3"
            orient="auto"
          >
            <path d="M0 0 L6 3 L0 6 Z" fill="#9b9696" />
          </marker>
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
              return (
                <path
                  key={`${depId}->${n.job.id}`}
                  d={`M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`}
                  fill="none"
                  stroke="#9b9696"
                  strokeWidth={1.4}
                  markerEnd="url(#arrowhead-preview)"
                />
              );
            }),
          )}
        </g>

        <g>
          {nodes.map((n) => (
            <g
              key={n.job.id}
              transform={`translate(${n.x}, ${n.y})`}
              role="group"
              aria-label={`Job ${n.job.id} on ${n.job.deck_id}`}
            >
              <rect
                width={NODE_W}
                height={NODE_H}
                rx={10}
                fill="#ffffff"
                stroke="#d9d6d2"
                strokeWidth={1.2}
              />
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
                {n.job.deck_id} · {n.job.steps.length} step{n.job.steps.length === 1 ? "" : "s"}
              </text>
            </g>
          ))}
        </g>
      </svg>
    </div>
  );
}
