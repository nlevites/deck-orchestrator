/**
 * Single-writer regression test for the C4 fix. Pre-fix, both the
 * global `useLiveState` and the run-scoped `useLiveRunState` called
 * `applyEvent` against the same shared caches with independent
 * cursors, so on any /runs/:id/* route a JOB_RUNNING event landed
 * twice — `job.version` and `run.version` inflated by 2 instead of 1,
 * and operator modal `expected_version` snapshots went stale,
 * producing spurious 409 VERSION_MISMATCH even with no second
 * operator.
 *
 * The fix moves useLiveRunState to bootstrap-only: it polls
 * /api/runs/{id}/state with since_seq=0 every tick and replaces the
 * run-detail + run-scoped events caches wholesale. The global
 * useLiveState remains the sole writer for `applyEvent`.
 *
 * This test pins that contract: useLiveRunState must not import
 * applyEvent. A regression that re-introduces event application here
 * fails the test before the version-inflation bug ships.
 */
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const HOOK_PATH = resolve(__dirname, "use-live-run-state.ts");

describe("useLiveRunState single-writer contract", () => {
  it("does not import applyEvent (pinned by C4 fix)", () => {
    const source = readFileSync(HOOK_PATH, "utf8");
    expect(source).not.toMatch(/from ["']@\/lib\/live\/apply-event["']/);
    expect(source).not.toMatch(/applyEvent\(/);
  });

  it("does not call applyEvent under any name", () => {
    const source = readFileSync(HOOK_PATH, "utf8");
    expect(source).not.toMatch(/import\s+\{[^}]*\bapplyEvent\b[^}]*\}/);
  });
});
