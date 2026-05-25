# `analysis/` — deck-fleet architecture & scale analysis

A self-contained toolkit that answers, with evidence:

1. **What data is being moved where?** (routes, schemas, cadence, byte
   volumes, direction)
2. **Is the architecture correct/optimal at the design scale (100 decks)?**
3. **Where does it break as we scale to 1 k → 10 k executors?**

Everything analysis-owned lives under `deck-fleet/analysis/`. No files
are added to `backend/`, `frontend/src/`, or `frontend/e2e/`.

## Layout

```
analysis/
├── static/         Pillar 1 — parse openapi.yaml + grep cadences → contract.md
├── wire/           Pillar 2A — JSON middleware logs from a live demo run
├── console/        Pillar 2B — Playwright-driven HAR + trace + heap per scenario
├── loadgen/        Pillar 3a — Python async synthetic-executor fleet
├── scale/          Pillar 3b/c/d — psutil watcher + sweep harness + plots
├── inefficiencies/ Pillar 2C+3e — checklist runner: 12 server + 8 console anti-patterns
└── REPORT.md       Pillar 4 — final stitched analysis for the debrief
```

## Prerequisites

- Go 1.22+ and a built backend: `make build` from the repo root (produces
  `backend/bin/{orchestrator,executor,supervisor}`).
- Node 20+ and `frontend/node_modules` (run `cd frontend && npm install`)
  for the console pillar.
- `uv` for Python (https://docs.astral.sh/uv/).
- Python 3.11+.

Install Python deps (one-time):

```sh
cd analysis && uv sync
```

Install Playwright (one-time):

```sh
make -C analysis console-install
cd analysis && npx playwright install --with-deps chromium
```

## Running the pillars

| Goal | Command |
|---|---|
| Static contract from spec + code | `make -C analysis static` |
| Live wire capture (4-deck stack, ~60s) | `make -C analysis wire-capture wire-analyze` |
| Console scenarios (4 default specs) | `make -C analysis console-capture console-analyze` |
| 100-deck console scenario | `make -C analysis console-capture-hundred` |
| Standalone loadgen smoke (DECKS=100) | `make -C analysis loadgen DECKS=100` |
| Scale sweep (default 10,100,1000) | `make -C analysis scale-sweep` |
| Larger sweep (10 → 10k) | `make -C analysis scale-sweep SIZES=10,100,1000,5000,10000` |
| Scale charts | `make -C analysis scale-plot` |
| Inefficiency scan against current corpora | `make -C analysis scan` |
| Everything (slow!) | `make -C analysis all` |

## Where the data lands

| Pillar | Output dir (gitignored) | Tracked outputs |
|---|---|---|
| Static | — | `static/contract.md` |
| Wire | `wire/runs/<timestamp>/{logs,…}` | — (per-run; rendered into REPORT) |
| Console | `console/runs/<scenario>/{network.har,trace.zip,heap-*.heapsnapshot,metrics.*}` | — |
| Scale | `scale/runs/N=<N>/{orchestrator.log,process-samples.csv,runs.json,…}` and `scale/runs/{*.png,breaking-point.md}` | `scale/runs/breaking-point.md` |
| Inefficiencies | — | `inefficiencies/inefficiencies.md` |
| Report | — | `REPORT.md` |

## Key design decisions

- **JSON middleware logs over mitmproxy.** Orchestrator's `Logging`
  middleware already captures `method/path/status/duration/bytes_in/bytes_out/request_id`
  per request. Setting `LOG_FORMAT=json` (cleanenv overrides yaml) makes
  the existing log stream the wire capture. mitmproxy adds setup without
  new signal.
- **Synthetic loadgen, not 10 000 real executors.** Each real executor is a
  process with its own SQLite + HTTP server; laptop ceiling ~500–1500.
  The synthetic loadgen impersonates N decks from one Python process,
  using the *real* `/executor/*` protocol. Single-process ceiling is
  ~5k–8k decks (CPython); sweep harness shards via subprocesses above
  that. We honestly document where the loadgen itself becomes the
  bottleneck instead of pretending the orchestrator did.
- **Playwright with init-script monkey-patching, not FE source edits.**
  Instrumentation lives in `console/init-scripts/` and is injected via
  `page.addInitScript`. Zero diff to `frontend/src/`.
- **One inefficiency check per file.** New checks add a file and one line
  in `inefficiencies/scan.py`. Each check is given the full corpus
  (wire + console + scale) and decides whether it has enough data to
  emit a `Finding`.

## Reproducibility

Every output traces back to a re-runnable command. `inefficiencies.md`
and `REPORT.md` cite the run dirs they were generated from. To
regenerate from scratch:

```sh
make build            # from repo root
make -C analysis all  # 30-60 min on a laptop depending on sweep sizes
make -C analysis report  # then edit REPORT.md if needed
```
