import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Search } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Card } from "@/components/primitives/Card";
import { PageHeader } from "@/components/console/PageHeader";
import { RunRow } from "@/components/console/RunRow";
import { apiKeys } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import type { RunStatus, RunSummary } from "@/lib/api-types";
import { cn } from "@/lib/cn";

// Operator-urgency order shared with NeedsAttentionPanel and the All-view sort.
const STATUS_FILTERS: { value: "ALL" | RunStatus; label: string }[] = [
  { value: "ALL", label: "All" },
  { value: "AMBIGUOUS", label: "Ambiguous" },
  { value: "FAILED", label: "Failed" },
  { value: "RUNNING", label: "Running" },
  { value: "COMPLETED", label: "Completed" },
  { value: "CANCELLED", label: "Cancelled" },
];

const STATUS_PRIORITY: Record<RunStatus, number> = {
  AMBIGUOUS: 0,
  FAILED: 1,
  RUNNING: 2,
  PENDING: 3,
  COMPLETED: 4,
  CANCELLED: 5,
};

export function RunsListPage() {
  const { data: runs = [] } = useQuery<RunSummary[]>({
    queryKey: apiKeys.runs,
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
  });

  const [filter, setFilter] = useState<(typeof STATUS_FILTERS)[number]["value"]>("ALL");
  const [query, setQuery] = useState("");

  const statusCounts = useMemo(() => {
    const counts: Record<RunStatus, number> = {
      PENDING: 0,
      RUNNING: 0,
      COMPLETED: 0,
      FAILED: 0,
      CANCELLED: 0,
      AMBIGUOUS: 0,
    };
    for (const r of runs) counts[r.status] = (counts[r.status] ?? 0) + 1;
    return counts;
  }, [runs]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const matched = runs.filter((r) => {
      if (filter !== "ALL" && r.status !== filter) return false;
      if (!q) return true;
      return r.id.toLowerCase().includes(q);
    });
    // Under "All", surface ambiguous + failed to the top so triage is
    // single-glance. Under a specific filter, the user already opted into
    // a status so just sort by recency.
    return [...matched].sort((a, b) => {
      if (filter === "ALL") {
        const pa = STATUS_PRIORITY[a.status] ?? 99;
        const pb = STATUS_PRIORITY[b.status] ?? 99;
        if (pa !== pb) return pa - pb;
      }
      return new Date(b.submitted_at).getTime() - new Date(a.submitted_at).getTime();
    });
  }, [runs, filter, query]);

  const hasActiveFilter = filter !== "ALL" || query.trim() !== "";
  const clearFilters = () => {
    setFilter("ALL");
    setQuery("");
  };

  return (
    <div className="mx-auto max-w-container-content page-x py-8 lg:py-10">
      <PageHeader
        title="Runs"
        actions={
          <Link to="/submit">
            <Button variant="primary" size="md">
              New run
            </Button>
          </Link>
        }
      />

      <div className="mt-6 flex flex-wrap items-center gap-3">
        <label className="relative flex min-w-[220px] flex-1 items-center md:max-w-sm">
          <Search
            size={14}
            strokeWidth={1.8}
            className="pointer-events-none absolute left-3 text-ink-nav"
          />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search run id…"
            className={cn(
              "h-9 w-full rounded-pill border border-line bg-surface pl-9 pr-3 text-[13px] tracking-nav text-ink",
              "placeholder:text-ink-sub focus:border-ink/30 focus:outline-none",
            )}
          />
        </label>
        <div className="flex flex-wrap items-center gap-1.5">
          {STATUS_FILTERS.map((opt) => {
            const count = opt.value === "ALL" ? runs.length : statusCounts[opt.value];
            const selected = filter === opt.value;
            const empty = opt.value !== "ALL" && count === 0;
            return (
              <button
                key={opt.value}
                type="button"
                onClick={() => setFilter(opt.value)}
                disabled={empty && !selected}
                aria-pressed={selected}
                className={cn(
                  "inline-flex h-7 items-center justify-center gap-1.5 rounded-pill px-3 text-[12px] font-medium tracking-nav transition-colors duration-150 ease-out-soft",
                  selected
                    ? "bg-surface text-ink ring-1 ring-inset ring-ink/25"
                    : empty
                      ? "bg-transparent text-ink-sub cursor-not-allowed"
                      : "bg-line-strong/60 text-ink-button hover:bg-line-strong hover:text-ink",
                )}
              >
                {opt.label}
                <span
                  className={cn(
                    "font-mono text-[11px] tracking-nav",
                    selected ? "text-ink-sub" : empty ? "text-ink-sub/70" : "text-ink-nav",
                  )}
                >
                  {count}
                </span>
              </button>
            );
          })}
        </div>
        {hasActiveFilter && (
          <span className="ml-auto font-mono text-[11px] tracking-nav text-ink-sub">
            Showing {filtered.length} of {runs.length}
          </span>
        )}
      </div>

      <Card className="mt-6 overflow-hidden p-0">
        {filtered.length === 0 ? (
          <EmptyState
            hasRuns={runs.length > 0}
            hasActiveFilter={hasActiveFilter}
            onClearFilters={clearFilters}
          />
        ) : (
          filtered.map((run) => <RunRow key={run.id} run={run} />)
        )}
      </Card>
    </div>
  );
}

interface EmptyStateProps {
  hasRuns: boolean;
  hasActiveFilter: boolean;
  onClearFilters: () => void;
}

function EmptyState({ hasRuns, hasActiveFilter, onClearFilters }: EmptyStateProps) {
  if (!hasRuns) {
    return (
      <div className="flex flex-col items-center gap-3 px-5 py-12 text-center">
        <h3 className="text-[16px] font-semibold tracking-sub text-ink">No runs yet</h3>
        <p className="max-w-sm text-[13px] tracking-nav text-ink-muted">
          Submit a DAG and it&apos;ll show up here as the orchestrator picks it up.
        </p>
        <Link to="/submit" className="mt-1">
          <Button variant="primary" size="md">
            Submit your first DAG
          </Button>
        </Link>
      </div>
    );
  }
  return (
    <div className="flex flex-col items-center gap-2 px-5 py-10 text-center">
      <p className="text-[14px] tracking-nav text-ink-muted">No runs match these filters.</p>
      {hasActiveFilter && (
        <button
          type="button"
          onClick={onClearFilters}
          className="text-[13px] font-medium tracking-nav text-accent-link hover:text-accent-linkAlt hover:underline"
        >
          Clear filters
        </button>
      )}
    </div>
  );
}
