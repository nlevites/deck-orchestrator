import { useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { ChevronRight, Code2, ExternalLink, Play, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Card } from "@/components/primitives/Card";
import { Modal } from "@/components/primitives/Modal";
import { PageHeader } from "@/components/console/PageHeader";
import { DagPreview } from "@/components/console/DagPreview";
import { ApiError, apiKeys, submitRun } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { useToast } from "@/lib/toasts/use-toast";
import { SAMPLE_DAGS, type SampleCategory, type SampleDag } from "@/lib/samples";
import type { DagSubmission, Deck, Run } from "@/lib/api-types";
import { validateDag } from "@/lib/dag-validate";
import { cn } from "@/lib/cn";

/**
 * The Submit page. The DAG preview is the primary surface; JSON editing
 * lives behind an "Edit JSON" modal so the operator's attention stays on
 * the topology they're about to submit.
 *
 * Validation runs every keystroke. UNHEALTHY decks downgrade to warnings
 * since the orchestrator will still accept the run and let it wait in
 * READY. Duplicate-id 409s from the orchestrator (which is idempotent on
 * submission id by design — see openapi.yaml §POST /api/runs) surface
 * inline with a "Submit as <suffixed-id>" recovery action instead of a
 * dead-end toast.
 */
export function SubmitRunPage() {
  const [text, setText] = useState<string>("");
  const [selectedSample, setSelectedSample] = useState<string | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [conflict, setConflict] = useState<ConflictState | null>(null);
  const { data: decks = [] } = useQuery<Deck[]>({
    queryKey: apiKeys.decks,
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
  });
  const navigate = useNavigate();
  const toast = useToast();
  const gate = useOperatorGate();

  const validation = useMemo(() => validateDag(text, decks), [text, decks]);

  const submitMutation = useMutation({
    mutationFn: (dag: DagSubmission) => submitRun(dag),
    onSuccess: (run) => {
      setConflict(null);
      toast.push({
        kind: "success",
        title: "Run submitted",
        body: `Tracking ${run.id}`,
      });
      navigate(`/runs/${encodeURIComponent(run.id)}`);
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === "DUPLICATE_RESOURCE") {
        const existing = (err.details?.current_state as Run | undefined) ?? undefined;
        const existingId = existing?.id ?? validation.parsed?.id ?? "";
        setConflict({
          existingId,
          suggestedId: suggestId(existingId),
        });
        return;
      }
      if (err instanceof ApiError) {
        toast.push({
          kind: "error",
          title: titleForError(err),
          body: err.message,
          timeoutMs: 8000,
        });
        return;
      }
      toast.push({
        kind: "error",
        title: "Submit failed",
        body: err instanceof Error ? err.message : String(err),
        timeoutMs: 8000,
      });
    },
  });

  const clearConflict = () => {
    if (conflict) setConflict(null);
  };

  const handlePick = (sampleId: string) => {
    const sample = SAMPLE_DAGS.find((s) => s.id === sampleId);
    if (!sample) return;
    setText(JSON.stringify(sample.json, null, 2));
    setSelectedSample(sampleId);
    clearConflict();
  };

  const handleChange = (value: string) => {
    setText(value);
    if (selectedSample) setSelectedSample(null);
    clearConflict();
  };

  const handleClear = () => {
    setText("");
    setSelectedSample(null);
    clearConflict();
  };

  const handleSubmit = () => {
    if (!validation.ok || !validation.parsed) return;
    submitMutation.mutate(validation.parsed as DagSubmission);
  };

  const handleResubmitAs = () => {
    if (!conflict) return;
    const rewritten = rewriteDagId(text, conflict.suggestedId);
    if (!rewritten) return;
    setText(rewritten.text);
    setConflict(null);
    submitMutation.mutate(rewritten.dag);
  };

  const valid = validation.ok;
  const empty = text.trim() === "";
  const issueCount = validation.errors.length + validation.warnings.length;
  const hasErrors = validation.errors.length > 0;

  return (
    <div className="mx-auto max-w-container-content page-x py-8 lg:py-10">
      <PageHeader
        title="New run"
        body="Pick a sample DAG or paste your own to start a new run. We validate shape, cycles, dependencies, and deck references in the browser before sending; the orchestrator runs the authoritative check too."
      />

      <div className="mt-8 grid grid-cols-1 gap-6 xl:grid-cols-[1fr_360px]">
        <Card className="flex flex-col self-start p-0">
          <div className="flex items-center justify-between border-b border-line px-5 py-3.5">
            <div className="flex min-w-0 flex-col">
              <span className="text-eyebrow font-mono uppercase text-ink-sub">
                {selectedSample
                  ? `Sample · ${SAMPLE_DAGS.find((s) => s.id === selectedSample)?.label ?? ""}`
                  : empty
                    ? "New DAG"
                    : "Custom DAG"}
              </span>
              <span className="mt-0.5 truncate text-[15px] font-medium tracking-sub text-ink">
                {validation.parsed?.id ?? "Pick a sample or paste JSON"}
              </span>
            </div>
            <div className="flex items-center gap-3">
              {!empty && issueCount > 0 && (
                <span
                  className={cn(
                    "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium tracking-nav",
                    hasErrors
                      ? "bg-[#fbe7e3] text-status-failed"
                      : "bg-[#f7eadb] text-status-ambiguous",
                  )}
                >
                  {`${issueCount} issue${issueCount === 1 ? "" : "s"}`}
                </span>
              )}
              <Button variant="secondary" onClick={() => setEditorOpen(true)}>
                <Code2 size={14} />
                Edit JSON
              </Button>
              {!empty && (
                <button
                  type="button"
                  onClick={handleClear}
                  aria-label="Clear DAG"
                  title="Clear"
                  className="inline-flex h-9 w-9 items-center justify-center rounded-pill text-ink-nav transition-colors hover:bg-line/60 hover:text-ink"
                >
                  <Trash2 size={14} strokeWidth={1.7} />
                </button>
              )}
            </div>
          </div>

          {valid && validation.parsed && (
            <div className="px-5 py-5">
              <DagPreview parsed={validation.parsed} />
            </div>
          )}

          {conflict && (
            <ConflictBanner
              conflict={conflict}
              onResubmit={handleResubmitAs}
              onOpenExisting={() => navigate(`/runs/${encodeURIComponent(conflict.existingId)}`)}
              onDismiss={() => setConflict(null)}
              pending={submitMutation.isPending}
            />
          )}

          <div className="flex items-center justify-end border-t border-line bg-surface-subtle px-5 py-3">
            <Button
              onClick={handleSubmit}
              disabled={!valid || submitMutation.isPending || gate.disabled}
              title={gate.reason || undefined}
            >
              <Play size={14} />
              {submitMutation.isPending ? "Submitting…" : "Submit run"}
            </Button>
          </div>
        </Card>

        <div className="flex flex-col gap-4">
          <SamplePicker onPick={handlePick} selectedId={selectedSample} />
        </div>
      </div>

      {editorOpen && (
        <EditJsonModal
          initialText={text}
          onClose={() => setEditorOpen(false)}
          onApply={(next) => {
            handleChange(next);
            setEditorOpen(false);
          }}
          decks={decks}
        />
      )}
    </div>
  );
}

interface ConflictState {
  existingId: string;
  suggestedId: string;
}

interface ConflictBannerProps {
  conflict: ConflictState;
  onResubmit: () => void;
  onOpenExisting: () => void;
  onDismiss: () => void;
  pending: boolean;
}

function ConflictBanner({
  conflict,
  onResubmit,
  onOpenExisting,
  onDismiss,
  pending,
}: ConflictBannerProps) {
  return (
    <div className="border-t border-[#fbe7e3] bg-[#fff3f1] px-5 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-status-failed/80">
            Duplicate run id
          </span>
          <span className="mt-0.5 text-[13px] leading-5 text-ink">
            <span className="font-mono">{conflict.existingId}</span> already exists. Submit as{" "}
            <span className="font-mono">{conflict.suggestedId}</span>?
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" onClick={onOpenExisting}>
            <ExternalLink size={14} />
            Open existing
          </Button>
          <Button onClick={onResubmit} disabled={pending}>
            <RefreshCw size={14} />
            Submit as {conflict.suggestedId}
          </Button>
          <button
            type="button"
            onClick={onDismiss}
            className="text-[12px] tracking-nav text-ink-nav hover:text-ink"
          >
            Dismiss
          </button>
        </div>
      </div>
    </div>
  );
}

interface EditJsonModalProps {
  initialText: string;
  onClose: () => void;
  onApply: (next: string) => void;
  decks: Deck[];
}

// Mounted only while the editor is open (parent guards with `editorOpen
// && <EditJsonModal />`). That keeps the draft state initialized from
// `initialText` on each open without an effect-driven re-sync.
function EditJsonModal({ initialText, onClose, onApply, decks }: EditJsonModalProps) {
  const [draft, setDraft] = useState(initialText);
  const validation = useMemo(() => validateDag(draft, decks), [draft, decks]);
  const empty = draft.trim() === "";

  return (
    <Modal
      open={true}
      onClose={onClose}
      title="Edit DAG JSON"
      eyebrow="DAG source"
      size="lg"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => onApply(draft)}>Apply</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          spellCheck={false}
          placeholder='{"id":"my-run","deck_jobs":[{"id":"prep","deck_id":"deck-1","depends_on":[],"steps":[{"type":"prepare","description":"…"}]}]}'
          className="min-h-[340px] resize-y rounded-panel border border-line bg-surface-warm px-4 py-3 font-mono text-[12px] leading-[1.55] text-ink outline-none placeholder:text-ink-sub focus:border-ink"
        />
        <div className="flex items-center justify-between">
          <span className="text-[12px] tracking-nav text-ink-muted">
            {empty
              ? "Paste JSON to validate."
              : validation.ok
                ? `${validation.parsed?.deck_jobs.length ?? 0} deck_jobs parsed.`
                : `${validation.errors.length} issue${validation.errors.length === 1 ? "" : "s"} found.`}
          </span>
          <span
            className={cn(
              "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium tracking-nav",
              empty
                ? "bg-line text-ink-nav"
                : validation.ok
                  ? "bg-[#e7f1ea] text-status-completed"
                  : "bg-[#fbe7e3] text-status-failed",
            )}
          >
            {empty ? "—" : validation.ok ? "valid" : "invalid"}
          </span>
        </div>
        {(validation.errors.length > 0 || validation.warnings.length > 0) && (
          <ul className="flex max-h-[200px] flex-col gap-1.5 overflow-auto">
            {validation.errors.map((e, i) => (
              <li
                key={`err-${i}`}
                className="rounded-md border border-[#fbe7e3] bg-[#fff3f1] px-3 py-1.5 text-[12px] leading-5 text-status-failed"
              >
                <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-status-failed/80">
                  {e.code}
                  {e.path && <span className="text-status-failed/70"> · {e.path}</span>}
                </span>
                <div className="text-ink">{e.message}</div>
              </li>
            ))}
            {validation.warnings.map((w, i) => (
              <li
                key={`warn-${i}`}
                className="rounded-md border border-[#f7eadb] bg-[#fff7ec] px-3 py-1.5 text-[12px] leading-5 text-status-ambiguous"
              >
                <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-status-ambiguous/80">
                  {w.code}
                  {w.path && <span className="text-status-ambiguous/70"> · {w.path}</span>}
                </span>
                <div className="text-ink">{w.message}</div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Modal>
  );
}

interface SamplePickerProps {
  onPick: (id: string) => void;
  selectedId: string | null;
}

const CATEGORY_ORDER: { key: SampleCategory; label: string }[] = [
  { key: "topology", label: "Topologies" },
  { key: "validation", label: "Validation cases" },
  { key: "stress", label: "Stress tests" },
];

function groupByCategory(dags: SampleDag[]): Record<SampleCategory, SampleDag[]> {
  const groups: Record<SampleCategory, SampleDag[]> = {
    topology: [],
    validation: [],
    stress: [],
  };
  for (const dag of dags) groups[dag.category].push(dag);
  return groups;
}

function SamplePicker({ onPick, selectedId }: SamplePickerProps) {
  const groups = groupByCategory(SAMPLE_DAGS);
  // Single-open accordion: at most one section expanded at a time.
  // Topologies open by default; clicking another section closes the
  // current one. Picking a sample snaps the accordion to its category
  // (so a programmatic pick stays visible) but doesn't lock it — the
  // user can still toggle other sections after the snap.
  const [openKey, setOpenKey] = useState<SampleCategory | null>("topology");
  const selectedCategory = selectedId
    ? SAMPLE_DAGS.find((s) => s.id === selectedId)?.category
    : undefined;
  // Derive-during-render to snap the accordion when selection changes,
  // without an effect (effects cause cascading re-renders per React docs).
  const [lastSyncedCategory, setLastSyncedCategory] = useState<SampleCategory | undefined>(
    selectedCategory,
  );
  if (selectedCategory && selectedCategory !== lastSyncedCategory) {
    setLastSyncedCategory(selectedCategory);
    setOpenKey(selectedCategory);
  }
  const toggle = (key: SampleCategory) => {
    setOpenKey((prev) => (prev === key ? null : key));
  };
  return (
    <Card className="flex flex-col p-0">
      <div className="flex items-center justify-between border-b border-line px-4 py-2.5">
        <span className="text-eyebrow font-mono uppercase text-ink-sub">Sample DAGs</span>
        <span className="font-mono text-[11px] tracking-nav text-ink-sub">
          {SAMPLE_DAGS.length}
        </span>
      </div>
      {CATEGORY_ORDER.map(({ key, label }, idx) => {
        const items = groups[key];
        if (items.length === 0) return null;
        const open = openKey === key;
        return (
          <section key={key} className={idx > 0 ? "border-t border-line" : undefined}>
            <button
              type="button"
              onClick={() => toggle(key)}
              aria-expanded={open}
              className="flex w-full items-center justify-between bg-surface-subtle px-4 py-2 text-left transition-colors hover:bg-line/40"
            >
              <span className="flex items-center gap-2">
                <ChevronRight
                  size={12}
                  strokeWidth={2}
                  className={cn(
                    "text-ink-nav transition-transform duration-150 ease-out-soft",
                    open && "rotate-90",
                  )}
                />
                <span className="text-eyebrow font-mono uppercase text-ink-sub">{label}</span>
              </span>
              <span className="font-mono text-[10px] tracking-nav text-ink-sub">
                {items.length}
              </span>
            </button>
            {open && (
              <ul className="divide-y divide-line">
                {items.map((s) => {
                  const active = selectedId === s.id;
                  return (
                    <li key={s.id}>
                      <button
                        type="button"
                        onClick={() => onPick(s.id)}
                        className={cn(
                          "flex w-full flex-col gap-0.5 px-4 py-2.5 text-left transition-colors duration-150 ease-out-soft",
                          active ? "bg-surface-warm" : "hover:bg-surface-subtle",
                        )}
                      >
                        <div className="flex min-w-0 items-center gap-2">
                          <span className="flex-1 truncate text-[13.5px] font-semibold tracking-sub text-ink">
                            {s.label}
                          </span>
                          {s.badge && (
                            <span
                              className={cn(
                                "rounded-sm px-1.5 py-0.5 font-mono text-[9.5px] tracking-nav",
                                s.expectInvalid
                                  ? "bg-[#fbe7e3] text-status-failed"
                                  : "bg-line text-ink-sub",
                              )}
                            >
                              {s.badge}
                            </span>
                          )}
                        </div>
                        <span
                          className="line-clamp-1 text-[12px] tracking-nav text-ink-nav"
                          title={s.description}
                        >
                          {s.description}
                        </span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </section>
        );
      })}
    </Card>
  );
}

function titleForError(err: ApiError): string {
  switch (err.code) {
    case "DUPLICATE_RESOURCE":
      return "Run id already exists";
    case "DAG_VALIDATION_FAILED":
      return "DAG validation failed";
    case "SCHEMA_VIOLATION":
    case "INVALID_JSON":
      return "Malformed request";
    case "PAYLOAD_TOO_LARGE":
      return "DAG too large";
    case "DEGRADED_MODE":
      return "Orchestrator degraded";
    default:
      return "Submit failed";
  }
}

/**
 * Generate a 6-char `[a-z0-9]` suffix appended to the existing id.
 * Strips any trailing `-xxxxxx` suffix already present so successive
 * retries don't pile up suffixes (`linear-pipeline-abc123-def456-...`).
 */
function suggestId(existingId: string): string {
  const trimmed = existingId.replace(/-[a-z0-9]{6}$/i, "");
  return `${trimmed}-${randomSuffix()}`;
}

function randomSuffix(): string {
  const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789";
  const bytes = new Uint8Array(6);
  crypto.getRandomValues(bytes);
  let out = "";
  for (const b of bytes) out += alphabet[b % alphabet.length];
  return out;
}

/**
 * Re-write the top-level `id`, preserving formatting when possible.
 */
function rewriteDagId(text: string, newId: string): { text: string; dag: DagSubmission } | null {
  try {
    const parsed = JSON.parse(text) as DagSubmission;
    parsed.id = newId;
    return { text: JSON.stringify(parsed, null, 2), dag: parsed };
  } catch {
    return null;
  }
}
