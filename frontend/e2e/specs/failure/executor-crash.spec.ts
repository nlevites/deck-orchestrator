import { test, expect } from "../../fixtures/stack";
import { linearDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForJobStatus, waitForRunStatus } from "../../fixtures/time";

// Failure mode #3b: crash mid-RUNNING — terminal-or-resolvable, exactly one attempt.
test("executor crash mid-run: terminal-or-resolvable, no duplicate dispatch", async ({
  page,
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "crash");

  await api.patchChaos("deck-2", { hang: true });
  await submit(linearDag(runId));
  await waitForJobStatus(runId, "j2", "RUNNING", { timeout: 8_000 });

  await api.crashDeck("deck-2").catch(() => {});

  // Outbox replay vs reconciler timing — both COMPLETED and AMBIGUOUS are valid.
  await waitForJobStatus(runId, "j2", ["COMPLETED", "FAILED", "AMBIGUOUS"], {
    timeout: 15_000,
  });

  const run = await api.getRun(runId);
  const j2 = run.deck_jobs.find((j) => j.id === "j2")!;
  expect((j2.recent_attempts ?? []).length, "j2 attempts").toBe(1);
  if (j2.status === "AMBIGUOUS") {
    await api.resolveJob(runId, "j2", j2.version, "FAILED");
    await waitForRunStatus(runId, "FAILED", { timeout: 5_000 });
  }

  await page.goto(`/runs/${encodeURIComponent(runId)}`);
  const terminalLabel = j2.status === "COMPLETED" ? /Completed|Running/ : /Failed|Cancelled/;
  await expect(page.getByText(terminalLabel).first()).toBeVisible();
});
