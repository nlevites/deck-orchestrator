/**
 * Client-side DAG validation. The orchestrator will run an authoritative
 * validation pass server-side too (see deck-fleet/API.md §submit and
 * DATA_MODEL.md), but doing it in the UI lets the operator catch shape
 * mistakes, cycles, and bad deck refs before hitting submit.
 *
 * We surface every error we can find, not just the first, so the operator
 * sees the whole picture in one pass.
 */

import type { Deck } from "@/lib/api-types";

export interface DagValidation {
  ok: boolean;
  parsed?: ParsedDag;
  errors: ValidationError[];
  warnings: ValidationError[];
}

export interface ValidationError {
  code:
    | "JSON_INVALID"
    | "SHAPE_INVALID"
    | "EMPTY_DAG"
    | "DUPLICATE_JOB_ID"
    | "MISSING_DEP"
    | "SELF_DEP"
    | "CYCLE_DETECTED"
    | "UNKNOWN_DECK"
    | "EMPTY_STEPS"
    | "DECK_BUSY"
    | "DECK_UNHEALTHY";
  message: string;
  /** Optional path into the DAG, e.g. `deck_jobs[2].depends_on[0]`. */
  path?: string;
}

export interface ParsedDag {
  id: string;
  deck_jobs: ParsedDeckJob[];
}

export interface ParsedDeckJob {
  id: string;
  deck_id: string;
  depends_on: string[];
  steps: { type: string; description: string }[];
}

const isString = (v: unknown): v is string => typeof v === "string" && v.length > 0;

function asArray<T>(v: unknown): T[] | undefined {
  return Array.isArray(v) ? (v as T[]) : undefined;
}

function parseShape(raw: unknown, errors: ValidationError[]): ParsedDag | undefined {
  if (typeof raw !== "object" || raw === null) {
    errors.push({ code: "SHAPE_INVALID", message: "Top-level value must be an object." });
    return undefined;
  }
  const obj = raw as Record<string, unknown>;
  const id = obj.id;
  if (!isString(id)) {
    errors.push({
      code: "SHAPE_INVALID",
      message: "`id` is required and must be a non-empty string.",
      path: "id",
    });
  }
  const jobs = asArray<unknown>(obj.deck_jobs);
  if (!jobs) {
    errors.push({
      code: "SHAPE_INVALID",
      message: "`deck_jobs` is required and must be an array.",
      path: "deck_jobs",
    });
    return undefined;
  }
  const parsedJobs: ParsedDeckJob[] = [];
  jobs.forEach((j, i) => {
    if (typeof j !== "object" || j === null) {
      errors.push({
        code: "SHAPE_INVALID",
        message: "Each deck_job must be an object.",
        path: `deck_jobs[${i}]`,
      });
      return;
    }
    const job = j as Record<string, unknown>;
    const jid = job.id;
    if (!isString(jid)) {
      errors.push({
        code: "SHAPE_INVALID",
        message: "`id` is required on each deck_job.",
        path: `deck_jobs[${i}].id`,
      });
    }
    const deckId = job.deck_id;
    if (!isString(deckId)) {
      errors.push({
        code: "SHAPE_INVALID",
        message: "`deck_id` is required on each deck_job.",
        path: `deck_jobs[${i}].deck_id`,
      });
    }
    const deps = asArray<unknown>(job.depends_on);
    if (!deps) {
      errors.push({
        code: "SHAPE_INVALID",
        message: "`depends_on` must be an array (use [] for no dependencies).",
        path: `deck_jobs[${i}].depends_on`,
      });
    }
    const steps = asArray<unknown>(job.steps);
    if (!steps) {
      errors.push({
        code: "SHAPE_INVALID",
        message: "`steps` must be an array.",
        path: `deck_jobs[${i}].steps`,
      });
    }
    if (steps && steps.length === 0) {
      errors.push({
        code: "EMPTY_STEPS",
        message: "Each deck_job must have at least one step.",
        path: `deck_jobs[${i}].steps`,
      });
    }
    if (isString(jid) && isString(deckId)) {
      parsedJobs.push({
        id: jid,
        deck_id: deckId,
        depends_on: (deps ?? []).filter(isString) as string[],
        steps: (steps ?? [])
          .filter((s): s is Record<string, unknown> => typeof s === "object" && s !== null)
          .map((s) => ({
            type: isString(s.type) ? s.type : "unknown",
            description: isString(s.description) ? s.description : "",
          })),
      });
    }
  });
  if (!isString(id)) return undefined;
  return { id, deck_jobs: parsedJobs };
}

function detectCycle(jobs: ParsedDeckJob[]): string[] | undefined {
  enum Mark {
    White,
    Gray,
    Black,
  }
  const marks = new Map<string, Mark>(jobs.map((j) => [j.id, Mark.White]));
  const out = new Map<string, string[]>(jobs.map((j) => [j.id, j.depends_on]));
  let cycle: string[] | undefined;
  const stack: string[] = [];

  function visit(id: string): void {
    if (cycle) return;
    const m = marks.get(id);
    if (m === Mark.Black) return;
    if (m === Mark.Gray) {
      const idx = stack.indexOf(id);
      cycle = stack.slice(idx).concat(id);
      return;
    }
    marks.set(id, Mark.Gray);
    stack.push(id);
    for (const dep of out.get(id) ?? []) {
      if (marks.has(dep)) visit(dep);
    }
    stack.pop();
    marks.set(id, Mark.Black);
  }

  for (const j of jobs) {
    if (cycle) break;
    visit(j.id);
  }
  return cycle;
}

export function validateDag(input: string, decks: Deck[]): DagValidation {
  const errors: ValidationError[] = [];
  const warnings: ValidationError[] = [];

  if (!input.trim()) {
    return {
      ok: false,
      errors: [{ code: "JSON_INVALID", message: "Paste a DAG JSON or pick a sample." }],
      warnings,
    };
  }

  let raw: unknown;
  try {
    raw = JSON.parse(input);
  } catch (err) {
    return {
      ok: false,
      errors: [
        {
          code: "JSON_INVALID",
          message: err instanceof Error ? err.message : "JSON parse failed.",
        },
      ],
      warnings,
    };
  }

  const parsed = parseShape(raw, errors);
  if (!parsed) return { ok: false, errors, warnings };

  if (parsed.deck_jobs.length === 0) {
    errors.push({
      code: "EMPTY_DAG",
      message: "deck_jobs is empty; a DAG must have at least one job.",
      path: "deck_jobs",
    });
  }

  const jobIds = new Set<string>();
  for (const j of parsed.deck_jobs) {
    if (jobIds.has(j.id)) {
      errors.push({
        code: "DUPLICATE_JOB_ID",
        message: `Duplicate deck_job id: ${j.id}`,
        path: `deck_jobs[id=${j.id}]`,
      });
    }
    jobIds.add(j.id);
  }

  for (const j of parsed.deck_jobs) {
    for (const dep of j.depends_on) {
      if (dep === j.id) {
        errors.push({
          code: "SELF_DEP",
          message: `${j.id} depends on itself.`,
          path: `deck_jobs[id=${j.id}].depends_on`,
        });
      } else if (!jobIds.has(dep)) {
        errors.push({
          code: "MISSING_DEP",
          message: `${j.id} depends on missing job ${dep}.`,
          path: `deck_jobs[id=${j.id}].depends_on`,
        });
      }
    }
  }

  const cycle = detectCycle(parsed.deck_jobs);
  if (cycle) {
    errors.push({
      code: "CYCLE_DETECTED",
      message: `Cycle detected: ${cycle.join(" → ")}`,
    });
  }

  const deckById = new Map(decks.map((d) => [d.id, d]));
  for (const j of parsed.deck_jobs) {
    const deck = deckById.get(j.deck_id);
    if (!deck) {
      errors.push({
        code: "UNKNOWN_DECK",
        message: `${j.id} references unknown deck ${j.deck_id}.`,
        path: `deck_jobs[id=${j.id}].deck_id`,
      });
      continue;
    }
    // STATE_MACHINE.md §3.2: Dispatcher refuses dispatch into a deck that is
    // not HEALTHY. Surface as a warning at submit time so the operator
    // knows the run will sit in READY until the deck recovers, but don't
    // block submission — the deck might recover before scheduling reaches it.
    if (deck.last_known_health === "UNREACHABLE" || deck.last_known_health === "STALE") {
      warnings.push({
        code: "DECK_UNHEALTHY",
        message: `${j.deck_id} is currently ${deck.last_known_health}; ${j.id} will wait in READY until heartbeat returns.`,
        path: `deck_jobs[id=${j.id}].deck_id`,
      });
    }
  }

  return { ok: errors.length === 0, parsed, errors, warnings };
}
