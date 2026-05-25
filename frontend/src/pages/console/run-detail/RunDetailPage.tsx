import { useMemo, useRef, useState } from "react";
import { useLocation, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Card } from "@/components/primitives/Card";
import { CancelRunConfirmModal } from "@/components/console/CancelRunConfirmModal";
import { DagViewer } from "@/components/console/DagViewer";
import { JobAttemptList } from "@/components/console/JobAttemptList";
import { ActivityDrawer } from "@/components/console/run-detail/ActivityDrawer";
import { AttentionPanel } from "@/components/console/run-detail/AttentionPanel";
import { RunHero } from "@/components/console/run-detail/RunHero";
import { apiKeys } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import { criticalPathIds } from "@/lib/dag-topo";
import { useLiveRunState } from "@/lib/live";
import type { Event, Run } from "@/lib/api-types";

/**
 * Single-page run-detail surface. Replaces the four-tab layout
 * (Run/Deck/Events/Resolve) the operator console used to ship.
 *
 * Zone order top-to-bottom — designed so the operator's eye lands on
 * the next action they need to take, in priority order:
 *
 *   1. RunHero — status, duration, progress strip, single primary CTA.
 *   2. AttentionPanel — only mounts if there's something pending
 *      operator action; collapses entirely on healthy runs.
 *   3. DAG canvas — topology with critical-path highlight, click-to-
 *      select wired into the timeline below.
 *   4. Job timeline — topological order, inline deck heartbeat,
 *      collapsible attempt history.
 *
 * Forensics (event tail) live in a right-side drawer reachable from
 * the hero "Activity" toggle. The legacy `/runs/:id/events` URL
 * deep-links open the drawer (see App router redirect).
 */
export function RunDetailPage() {
  const { id } = useParams<{ id: string }>();
  const location = useLocation();
  useLiveRunState(id);

  const runQ = useQuery<Run | undefined>({
    queryKey: apiKeys.run(id ?? ""),
    queryFn: cacheOnlyQueryFn,
    enabled: !!id,
    staleTime: Infinity,
  });

  // Deep-link: /runs/:id/events opens the activity drawer on first
  // mount. We intentionally don't react to subsequent path changes —
  // closing the drawer should not bounce the URL.
  const initialDrawerFromPath = useMemo(
    () => location.pathname.endsWith("/events"),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );
  const [drawerOpen, setDrawerOpen] = useState(initialDrawerFromPath);
  const [selectedJobId, setSelectedJobId] = useState<string | undefined>();
  const [showCancel, setShowCancel] = useState(false);
  const [resolveOpenFor, setResolveOpenFor] = useState<string | null>(null);
  const [retryOpenFor, setRetryOpenFor] = useState<string | null>(null);

  const attentionRef = useRef<HTMLDivElement | null>(null);
  const jobsRef = useRef<HTMLDivElement | null>(null);

  // Track unread events while the drawer is closed: count any event
  // received after the last "drawer was open" moment.
  const eventsQ = useQuery<Event[]>({
    queryKey: id ? apiKeys.eventsForRun(id) : ["events", "run", "?"],
    queryFn: cacheOnlyQueryFn,
    enabled: !!id,
    staleTime: Infinity,
  });
  // Tracked as state (not a ref) so render stays pure. Snapshot
  // lastReadCount when the drawer opens (adjust-during-render pattern).
  const eventsLen = eventsQ.data?.length ?? 0;
  const [lastReadCount, setLastReadCount] = useState<number>(eventsLen);
  const [prevDrawerOpen, setPrevDrawerOpen] = useState(drawerOpen);
  if (drawerOpen !== prevDrawerOpen) {
    setPrevDrawerOpen(drawerOpen);
    if (drawerOpen) {
      setLastReadCount(eventsLen);
    }
  }
  const unread = drawerOpen ? 0 : Math.max(0, eventsLen - lastReadCount);

  if (!id || runQ.isLoading) {
    return (
      <div className="mx-auto max-w-container-content page-x py-10">
        <div className="text-[14px] text-ink-muted">Loading run…</div>
      </div>
    );
  }
  const run = runQ.data;
  if (!run) {
    return (
      <div className="mx-auto max-w-container-content page-x py-10">
        <p className="text-[14px] text-ink-muted">
          Run <span className="font-mono">{id}</span> not found.
        </p>
      </div>
    );
  }

  const critical = criticalPathIds(run.deck_jobs);

  const scrollToAttention = () => {
    attentionRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div className="mx-auto max-w-container-content page-x py-8 lg:py-10">
      <RunHero
        run={run}
        unreadEventCount={unread}
        onResolve={() => {
          const first = run.deck_jobs.find((j) => j.status === "AMBIGUOUS");
          if (first) {
            setResolveOpenFor(first.id);
            scrollToAttention();
          }
        }}
        onRetry={() => {
          const first = run.deck_jobs.find((j) => j.status === "FAILED");
          if (first) {
            setRetryOpenFor(first.id);
            scrollToAttention();
          }
        }}
        onCancel={() => setShowCancel(true)}
        onToggleActivity={() => setDrawerOpen((v) => !v)}
        activityOpen={drawerOpen}
      />

      <CancelRunConfirmModal open={showCancel} onClose={() => setShowCancel(false)} run={run} />

      <div ref={attentionRef} className="mt-6">
        <AttentionPanel
          run={run}
          resolveJobIdToOpen={resolveOpenFor}
          retryJobIdToOpen={retryOpenFor}
          onModalsConsumed={() => {
            setResolveOpenFor(null);
            setRetryOpenFor(null);
          }}
        />
      </div>

      <section className="mt-8 flex flex-col gap-3">
        <div className="flex items-baseline justify-between gap-3">
          <h2 className="text-[16px] font-semibold tracking-sub text-ink">DAG</h2>
          <span className="font-mono text-[11px] tracking-nav text-ink-sub">
            {critical.size > 1 ? "highlighted: critical path" : "click a node to focus a job"}
          </span>
        </div>
        <DagViewer
          run={run}
          selectedJobId={selectedJobId}
          onSelectJob={(jobId) => {
            setSelectedJobId(jobId);
            jobsRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
          }}
          highlightCriticalPath
        />
      </section>

      <section ref={jobsRef} className="mt-8 flex flex-col gap-3">
        <h2 className="text-[16px] font-semibold tracking-sub text-ink">Jobs</h2>
        {run.deck_jobs.length === 0 ? (
          <Card className="px-5 py-6 text-[13px] text-ink-muted">No deck jobs in this run.</Card>
        ) : (
          <JobAttemptList
            runId={run.id}
            jobs={run.deck_jobs}
            selectedJobId={selectedJobId}
            onSelectJob={setSelectedJobId}
            criticalPathIds={critical}
          />
        )}
      </section>

      <ActivityDrawer runId={run.id} open={drawerOpen} onClose={() => setDrawerOpen(false)} />
    </div>
  );
}
