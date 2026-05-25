import { test, expect } from "../../fixtures/stack";
import {
  cycleDag,
  danglingDepDag,
  unknownDeckDag,
  duplicateJobIdDag,
} from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { SubmitRunPage } from "../../pages/SubmitRunPage";
import { ApiError } from "../../helpers/api";

// Frontend validation codes in Edit JSON modal; server-side covered by integration suite.
test.describe("DAG validation: invalid submissions surface inline", () => {
  test("cycle: CYCLE_DETECTED error renders, submit disabled", async ({ page }, testInfo) => {
    const sub = new SubmitRunPage(page);
    await sub.goto();
    await sub.openEditor();
    await sub.fillJson(cycleDag(runIdFor(testInfo, "cycle")));
    await expect(sub.validationItem("CYCLE_DETECTED")).toBeVisible();
    await sub.applyAndClose();
    await expect(sub.submitButton()).toBeDisabled();
  });

  test("dangling dep: MISSING_DEP error renders, submit disabled", async ({ page }, testInfo) => {
    const sub = new SubmitRunPage(page);
    await sub.goto();
    await sub.openEditor();
    await sub.fillJson(danglingDepDag(runIdFor(testInfo, "dang")));
    await expect(sub.validationItem("MISSING_DEP")).toBeVisible();
    await sub.applyAndClose();
    await expect(sub.submitButton()).toBeDisabled();
  });

  test("unknown deck: UNKNOWN_DECK error renders, submit disabled", async ({ page }, testInfo) => {
    const sub = new SubmitRunPage(page);
    await sub.goto();
    await sub.openEditor();
    await sub.fillJson(unknownDeckDag(runIdFor(testInfo, "unk")));
    await expect(sub.validationItem("UNKNOWN_DECK")).toBeVisible();
    await sub.applyAndClose();
    await expect(sub.submitButton()).toBeDisabled();
  });

  test("duplicate job id: DUPLICATE_JOB_ID error renders, submit disabled", async ({
    page,
  }, testInfo) => {
    const sub = new SubmitRunPage(page);
    await sub.goto();
    await sub.openEditor();
    await sub.fillJson(duplicateJobIdDag(runIdFor(testInfo, "dupid")));
    await expect(sub.validationItem("DUPLICATE_JOB_ID")).toBeVisible();
    await sub.applyAndClose();
    await expect(sub.submitButton()).toBeDisabled();
  });
});

// Direct API bypass — backend rejects with DAG_VALIDATION_FAILED.
test("backend rejects DAG with cycle: DAG_VALIDATION_FAILED via direct API", async ({
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "cycle-be");
  let caught: ApiError | undefined;
  try {
    await api.submitRun(cycleDag(runId));
  } catch (e) {
    if (e instanceof ApiError) caught = e;
    else throw e;
  }
  expect(caught, "cycle DAG must be rejected").toBeTruthy();
  expect(caught!.code).toBe("DAG_VALIDATION_FAILED");
});
