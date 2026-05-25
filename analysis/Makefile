# Analysis toolkit entrypoints. All targets are re-runnable; raw outputs
# land under analysis/{wire,console,scale}/runs/ (gitignored). Tracked
# artifacts (contract.md, inefficiencies.md, REPORT.md, breaking-point.md)
# are checked in for the debrief.
#
# Run from repo root via `make -C analysis <target>`, or just inside this
# directory.

.PHONY: help install \
        static \
        wire-capture wire-analyze \
        console-install console-capture console-capture-hundred console-analyze \
        loadgen \
        scale-sweep scale-plot \
        scan \
        report \
        all clean

REPO_ROOT := $(abspath $(CURDIR)/..)
UV := uv run --project $(CURDIR)

help:
	@echo "Targets:"
	@echo "  install               install Python deps via uv"
	@echo "  static                regenerate static/contract.md"
	@echo "  wire-capture          run wire capture (~60s, needs make build)"
	@echo "  wire-analyze          analyze latest wire run"
	@echo "  console-install       npm install @playwright/test"
	@echo "  console-capture       run 4 console scenarios (steady, burst, reconnect, multi-tab)"
	@echo "  console-capture-hundred  run 100-deck scenario"
	@echo "  console-analyze       analyze captured console runs"
	@echo "  loadgen DECKS=100     standalone loadgen, no sweep harness"
	@echo "  scale-sweep SIZES=10,100  run scale sweeps"
	@echo "  scale-plot            plot scale-sweep artifacts"
	@echo "  scan                  run inefficiency scan against all corpora"
	@echo "  report                regenerate REPORT.md from current artifacts"
	@echo "  all                   static + wire + console + scale + scan + report"
	@echo "  clean                 wipe runs/ subdirs (keeps code + tracked artifacts)"

install:
	$(UV) python -c "import sys; print('uv venv ready, python', sys.version)"

# ---- Pillar 1 ----

static:
	cd $(REPO_ROOT) && $(UV) python -m analysis.static.extract

# ---- Pillar 2A ----

wire-capture:
	cd $(REPO_ROOT) && bash $(CURDIR)/wire/capture.sh

wire-analyze:
	cd $(REPO_ROOT) && $(UV) python -m analysis.wire.analyze

# ---- Pillar 2B ----

console-install:
	cd $(CURDIR) && npm install

console-capture:
	cd $(CURDIR) && npx playwright test --config console/playwright.config.ts \
	    console/specs/steady-state.spec.ts \
	    console/specs/event-burst.spec.ts \
	    console/specs/reconnect.spec.ts \
	    console/specs/multi-tab.spec.ts

console-capture-hundred:
	cd $(CURDIR) && DFO_ANALYSIS_DECKS=100 \
	    npx playwright test --config console/playwright.config.ts \
	    console/specs/hundred-deck-fleet.spec.ts

console-analyze:
	cd $(REPO_ROOT) && $(UV) python -m analysis.console.analyze

# ---- Pillar 3 ----

DECKS ?= 100
loadgen:
	cd $(REPO_ROOT) && $(UV) python -m analysis.loadgen \
	    --decks $(DECKS) \
	    --out $(CURDIR)/loadgen/runs/N=$(DECKS)/loadgen.ndjson \
	    --duration 60

SIZES ?= 10,100,1000
scale-sweep:
	cd $(REPO_ROOT) && $(UV) python -m analysis.scale.sweep \
	    --sizes $(SIZES) --duration 120

scale-plot:
	cd $(REPO_ROOT) && $(UV) python -m analysis.scale.plot

# ---- Inefficiencies ----

scan:
	cd $(REPO_ROOT) && $(UV) python -m analysis.inefficiencies.scan

# ---- Final report ----

report:
	@echo "REPORT.md is hand-curated; regenerate static, scan, plots first:"
	@echo "  make static scan scale-plot console-analyze wire-analyze"
	@echo "Then edit analysis/REPORT.md to incorporate latest numbers."

# ---- Cross-cutting ----

all: static wire-capture wire-analyze console-capture console-analyze \
     scale-sweep scale-plot scan
	@echo "Pillars 1-4 done. Edit REPORT.md to publish."

clean:
	rm -rf wire/runs console/runs console/test-results console/report \
	    scale/runs loadgen/runs
	@echo "wiped runs/. Tracked artifacts (contract.md, inefficiencies.md, REPORT.md) preserved."
