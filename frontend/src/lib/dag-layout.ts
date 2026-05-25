/**
 * Shared Kahn-style topological layout for DAG visualizations.
 *
 * One column per level, jobs stacked vertically inside each column.
 * Used by both DagViewer (live run rendering) and DagPreview (pre-submit
 * preview on the Submit page).
 */

export interface LayoutJob {
  id: string;
  depends_on: string[];
}

export interface PositionedNode<J extends LayoutJob> {
  job: J;
  level: number;
  index: number;
  x: number;
  y: number;
}

export interface LayoutOptions {
  nodeWidth?: number;
  nodeHeight?: number;
  gapX?: number;
  gapY?: number;
  padding?: number;
}

export interface LayoutResult<J extends LayoutJob> {
  nodes: PositionedNode<J>[];
  width: number;
  height: number;
}

const DEFAULTS = {
  nodeWidth: 200,
  nodeHeight: 76,
  gapX: 80,
  gapY: 28,
  padding: 24,
};

export function layoutDag<J extends LayoutJob>(
  jobs: J[],
  options: LayoutOptions = {},
): LayoutResult<J> {
  const NODE_W = options.nodeWidth ?? DEFAULTS.nodeWidth;
  const NODE_H = options.nodeHeight ?? DEFAULTS.nodeHeight;
  const GAP_X = options.gapX ?? DEFAULTS.gapX;
  const GAP_Y = options.gapY ?? DEFAULTS.gapY;
  const PADDING = options.padding ?? DEFAULTS.padding;

  const byId = new Map(jobs.map((j) => [j.id, j]));
  const indeg = new Map<string, number>(
    jobs.map((j) => [j.id, j.depends_on.filter((d) => byId.has(d)).length]),
  );
  const level = new Map<string, number>();
  const queue: string[] = [];

  for (const j of jobs) {
    if ((indeg.get(j.id) ?? 0) === 0) {
      level.set(j.id, 0);
      queue.push(j.id);
    }
  }

  while (queue.length > 0) {
    const id = queue.shift();
    if (!id) break;
    const cur = level.get(id) ?? 0;
    for (const j of jobs) {
      if (j.depends_on.includes(id)) {
        const next = (indeg.get(j.id) ?? 0) - 1;
        indeg.set(j.id, next);
        level.set(j.id, Math.max(level.get(j.id) ?? 0, cur + 1));
        if (next === 0) queue.push(j.id);
      }
    }
  }

  const byLevel = new Map<number, J[]>();
  for (const j of jobs) {
    const l = level.get(j.id) ?? 0;
    const arr = byLevel.get(l) ?? [];
    arr.push(j);
    byLevel.set(l, arr);
  }

  const nodes: PositionedNode<J>[] = [];
  const maxLevel = Math.max(0, ...Array.from(byLevel.keys()));
  let maxRows = 0;

  for (let l = 0; l <= maxLevel; l++) {
    const col = byLevel.get(l) ?? [];
    maxRows = Math.max(maxRows, col.length);
    col.forEach((j, idx) => {
      nodes.push({
        job: j,
        level: l,
        index: idx,
        x: PADDING + l * (NODE_W + GAP_X),
        y: PADDING + idx * (NODE_H + GAP_Y),
      });
    });
  }

  const width = PADDING * 2 + (maxLevel + 1) * NODE_W + maxLevel * GAP_X;
  const height = PADDING * 2 + Math.max(1, maxRows) * NODE_H + Math.max(0, maxRows - 1) * GAP_Y;

  return { nodes, width, height };
}

export const DAG_LAYOUT_DEFAULTS = DEFAULTS;
