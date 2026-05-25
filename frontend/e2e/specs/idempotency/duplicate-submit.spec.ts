import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import { linearDag } from "../../helpers/dag-factory";
import { ApiError } from "../../helpers/api";

// Duplicate DAG id → 409 DUPLICATE_RESOURCE; original run unchanged.
test("duplicate submit: second submission of same DAG id returns 409 DUPLICATE_RESOURCE", async ({
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "dup");
  const dag = linearDag(runId);

  const firstRun = await submit(dag);
  expect(firstRun.id).toBe(runId);

  let caught: ApiError | undefined;
  try {
    await api.submitRun(dag);
  } catch (e) {
    if (e instanceof ApiError) caught = e;
    else throw e;
  }
  expect(caught, "second submit must throw ApiError").toBeTruthy();
  expect(caught!.code).toBe("DUPLICATE_RESOURCE");
  expect(caught!.status).toBe(409);
  const currentState = caught!.details?.["current_state"] as { id: string } | undefined;
  expect(currentState?.id).toBe(runId);

  const runs = await api.listRuns();
  const matches = runs.filter((r) => r.id === runId);
  expect(matches.length, "list returns one run per id").toBe(1);
});
