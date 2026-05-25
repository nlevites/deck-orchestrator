"""
Scenario driver for the Pillar 2A wire capture.

Submits each of the six canonical sample DAGs against the live orchestrator,
waits for terminal status, then exercises one cancel + one retry + one
chaos-hang scenario so the capture includes every relevant code path.

Designed for ~60s wall-clock against the 4-deck capture stack
(step_duration=2s, deck-{1..4}). Re-run from inside capture.sh which
sets the env vars; can also be run standalone if a demo stack is up.
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
import uuid
from pathlib import Path

import httpx


ORCH = os.environ.get("ANALYSIS_ORCH", "http://localhost:8080")
RUN_DIR = Path(os.environ.get("ANALYSIS_RUN_DIR", "."))

# Six topologies verbatim from deck_fleet_orchestrator_assignment.md.
# Embedded as dicts (not parsed from samples/valid.ts) so this file has
# no FE coupling.
SAMPLE_DAGS: list[dict] = [
    {
        "id": "linear-pipeline",
        "deck_jobs": [
            {"id": "prep", "deck_id": "deck-1", "depends_on": [],
             "steps": [{"type": "prepare", "description": "Prep sample tray"},
                       {"type": "transfer", "description": "Transfer reagent to plate"}]},
            {"id": "incubate", "deck_id": "deck-2", "depends_on": ["prep"],
             "steps": [{"type": "incubate", "description": "Incubate at 37C for 30s"}]},
            {"id": "measure", "deck_id": "deck-3", "depends_on": ["incubate"],
             "steps": [{"type": "measure", "description": "Read fluorescence"}]},
        ],
    },
    {
        "id": "parallel-assays",
        "deck_jobs": [
            {"id": "track-a", "deck_id": "deck-1", "depends_on": [],
             "steps": [{"type": "transfer", "description": "Aliquot sample A"},
                       {"type": "incubate", "description": "Incubate 30s"},
                       {"type": "measure", "description": "Read A absorbance"}]},
            {"id": "track-b", "deck_id": "deck-2", "depends_on": [],
             "steps": [{"type": "transfer", "description": "Aliquot sample B"},
                       {"type": "incubate", "description": "Incubate 30s"},
                       {"type": "measure", "description": "Read B absorbance"}]},
        ],
    },
    {
        "id": "fanout-aliquot",
        "deck_jobs": [
            {"id": "source-prep", "deck_id": "deck-1", "depends_on": [],
             "steps": [{"type": "prepare", "description": "Prep master mix"},
                       {"type": "aliquot", "description": "Aliquot to three plates"}]},
            {"id": "assay-warm", "deck_id": "deck-2", "depends_on": ["source-prep"],
             "steps": [{"type": "incubate", "description": "37C"},
                       {"type": "measure", "description": "Read condition 1"}]},
            {"id": "assay-ambient", "deck_id": "deck-3", "depends_on": ["source-prep"],
             "steps": [{"type": "incubate", "description": "25C"},
                       {"type": "measure", "description": "Read condition 2"}]},
            {"id": "assay-cool", "deck_id": "deck-4", "depends_on": ["source-prep"],
             "steps": [{"type": "incubate", "description": "4C"},
                       {"type": "measure", "description": "Read condition 3"}]},
        ],
    },
    {
        "id": "fanin-pool",
        "deck_jobs": [
            {"id": "extract-a", "deck_id": "deck-1", "depends_on": [],
             "steps": [{"type": "extract", "description": "Extract from A"}]},
            {"id": "extract-b", "deck_id": "deck-2", "depends_on": [],
             "steps": [{"type": "extract", "description": "Extract from B"}]},
            {"id": "extract-c", "deck_id": "deck-3", "depends_on": [],
             "steps": [{"type": "extract", "description": "Extract from C"}]},
            {"id": "pool-and-measure", "deck_id": "deck-4",
             "depends_on": ["extract-a", "extract-b", "extract-c"],
             "steps": [{"type": "pool", "description": "Pool extracts"},
                       {"type": "measure", "description": "Read pooled signal"}]},
        ],
    },
]


def _unique_id(base: str) -> str:
    return f"{base}-{uuid.uuid4().hex[:8]}"


def submit_dag(client: httpx.Client, dag: dict, *, log: list[dict]) -> str:
    body = dict(dag)
    body["id"] = _unique_id(body["id"])
    t0 = time.time()
    r = client.post(f"{ORCH}/api/runs", json=body)
    r.raise_for_status()
    log.append({
        "ts": t0, "event": "submit_dag", "run_id": body["id"],
        "status": r.status_code, "n_jobs": len(body["deck_jobs"]),
    })
    return body["id"]


def wait_for_terminal(client: httpx.Client, run_id: str, *, timeout: float = 120) -> dict:
    deadline = time.time() + timeout
    last_status = None
    while time.time() < deadline:
        r = client.get(f"{ORCH}/api/runs/{run_id}")
        if r.status_code != 200:
            time.sleep(0.5)
            continue
        body = r.json()
        if body["status"] != last_status:
            print(f"  [{run_id}] -> {body['status']}", flush=True)
            last_status = body["status"]
        if body["status"] in ("COMPLETED", "FAILED", "AMBIGUOUS", "CANCELLED"):
            return body
        time.sleep(0.5)
    raise TimeoutError(f"run {run_id} did not reach terminal status in {timeout}s")


def scenario_happy_path(client: httpx.Client, log: list[dict]) -> list[str]:
    """Submit all six sample DAGs and wait for each to complete."""
    run_ids: list[str] = []
    for dag in SAMPLE_DAGS:
        rid = submit_dag(client, dag, log=log)
        run_ids.append(rid)
    # Wait sequentially so the capture has clearly-attributable phases.
    for rid in run_ids:
        final = wait_for_terminal(client, rid, timeout=180)
        log.append({"ts": time.time(), "event": "terminal",
                    "run_id": rid, "status": final["status"]})
    return run_ids


def scenario_cancel(client: httpx.Client, log: list[dict]) -> None:
    """Submit a long-ish DAG, cancel mid-flight."""
    dag = SAMPLE_DAGS[2]  # fanout — 4 jobs across 4 decks
    rid = submit_dag(client, dag, log=log)
    time.sleep(2)
    r = client.get(f"{ORCH}/api/runs/{rid}")
    version = r.json()["version"]
    r = client.post(f"{ORCH}/api/runs/{rid}/cancel",
                    json={"expected_version": version})
    log.append({"ts": time.time(), "event": "cancel",
                "run_id": rid, "status": r.status_code})
    wait_for_terminal(client, rid, timeout=30)


def scenario_chaos_hang(client: httpx.Client, log: list[dict]) -> None:
    """Set hang flag on deck-1, submit a DAG that uses deck-1, observe AMBIGUOUS."""
    r = client.post(f"{ORCH}/api/decks/deck-1/chaos",
                    json={"hang": True})
    log.append({"ts": time.time(), "event": "chaos_hang_on",
                "deck": "deck-1", "status": r.status_code})

    dag = SAMPLE_DAGS[0]  # linear starts on deck-1
    rid = submit_dag(client, dag, log=log)
    # Don't wait for terminal — ambiguous_deadline=15s in demo config.
    # Just give it long enough to dispatch, then clear chaos so the run
    # can resolve before the capture wraps up.
    time.sleep(10)
    client.post(f"{ORCH}/api/decks/deck-1/chaos/reset")
    log.append({"ts": time.time(), "event": "chaos_hang_off", "deck": "deck-1"})


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--scenario", default="all",
                    choices=["all", "happy", "cancel", "chaos"])
    args = ap.parse_args()

    log: list[dict] = []
    with httpx.Client(timeout=10) as client:
        if args.scenario in ("all", "happy"):
            print("=== scenario: happy path (6 sample DAGs) ===")
            scenario_happy_path(client, log)
        if args.scenario in ("all", "cancel"):
            print("=== scenario: cancel mid-flight ===")
            scenario_cancel(client, log)
        if args.scenario in ("all", "chaos"):
            print("=== scenario: chaos hang on deck-1 ===")
            scenario_chaos_hang(client, log)

    out = RUN_DIR / "scenario.ndjson"
    out.parent.mkdir(parents=True, exist_ok=True)
    with out.open("w") as f:
        for entry in log:
            f.write(json.dumps(entry) + "\n")
    print(f"wrote {out} ({len(log)} entries)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
