import { test, expect } from "../../fixtures/stack";
import { linearDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForJobStatus } from "../../fixtures/time";
import * as sel from "../../helpers/selectors";

// Failure mode #3a: chaos hang → orchestrator escalates to AMBIGUOUS (DEADLINE_EXCEEDED).
test("executor hang: job surfaces as AMBIGUOUS to the operator", async ({
  page,
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "hang");
  await api.patchChaos("deck-2", { hang: true });
  await submit(linearDag(runId));

  // AttemptDeadline=4s; liveness sweep every 2s (no env knob in e2e.yaml).
  await waitForJobStatus(runId, "j2", "AMBIGUOUS", { timeout: 12_000 });

  await page.goto("/fleet");
  await expect(sel.ambiguousBanner(page)).toBeVisible();

  await page.goto(`/runs/${encodeURIComponent(runId)}/resolve`);
  await expect(page.getByText(runId).first()).toBeVisible();
  await page.goto(`/runs/${encodeURIComponent(runId)}`);
  await expect(sel.jobNode(page, "j2", "AMBIGUOUS")).toBeVisible();
});
