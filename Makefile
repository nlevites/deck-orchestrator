.PHONY: help \
        gen gen-api gen-api-go gen-api-ts gen-sql \
        lint lint-go lint-frontend lint-frontend-typecheck \
        lint-frontend-eslint lint-frontend-format lint-go-fix \
        build build-backend build-frontend \
        test test-backend test-frontend cover cover-backend \
        check-codegen check \
        demo demo-large \
        e2e e2e-headed e2e-ui e2e-install

help:
	@echo "Test:"
	@echo "  test                       Run backend + frontend unit tests"
	@echo "  test-backend               Backend Go unit tests (excludes integration suite)"
	@echo "  test-frontend              Frontend Vitest suite"
	@echo "  test-integration           Run the integration suite (real orchestrator + executors in-process)"
	@echo "  test-integration-cover     Same as test-integration with coverage attribution"
	@echo ""
	@echo "Codegen:"
	@echo "  gen            Regenerate API types + SQL queries"
	@echo "  gen-api        Regenerate Go + TypeScript types from api/openapi.yaml"
	@echo "  gen-api-go     Regenerate Go types only"
	@echo "  gen-api-ts     Regenerate TypeScript types only"
	@echo "  gen-sql        Regenerate sqlc query bindings from backend/sql/{orchestrator,executor}/queries"
	@echo ""
	@echo "Lint:"
	@echo "  lint                       All linters (Go + frontend)"
	@echo "  lint-go                    gofmt + go vet + golangci-lint"
	@echo "  lint-frontend              All frontend linters (typecheck + eslint + prettier)"
	@echo "  lint-frontend-typecheck    TypeScript typecheck (src + e2e)"
	@echo "  lint-frontend-eslint       ESLint on src + e2e"
	@echo "  lint-frontend-format       Prettier --check"
	@echo "  lint-go-fix                Run Go linters with auto-fix where supported"
	@echo ""
	@echo "Build:"
	@echo "  build          Build backend + frontend"
	@echo "  build-backend  Compile orchestrator + executor binaries to backend/bin/"
	@echo "  build-frontend Vite production build"
	@echo ""
	@echo "Demo:"
	@echo "  demo                       Boot dev stack (orchestrator + 3 executors, fleet_size=100)"
	@echo "  demo-large                 Boot dev stack with 25 prelaunched executors (fleet_size=100)"
	@echo ""
	@echo "CI gate:"
	@echo "  check          Run check-codegen + lint + build (what CI runs)"
	@echo "  check-codegen  Verify generated code is in sync with api/openapi.yaml"
	@echo ""
	@echo "E2E:"
	@echo "  e2e            Build + run the Playwright suite headless against a hermetic stack"
	@echo "  e2e-headed     Same, with Chromium visible (dev only)"
	@echo "  e2e-ui         Open the Playwright UI runner"
	@echo "  e2e-install    Install Chromium for Playwright (first-time setup)"

# --- Codegen ----------------------------------------------------------------

gen: gen-api gen-sql

gen-api: gen-api-go gen-api-ts

gen-api-go:
	cd backend && go tool oapi-codegen -config ../api/oapi-codegen.yaml ../api/openapi.yaml

gen-api-ts:
	cd frontend && npm run gen:api

gen-sql:
	cd backend && go tool sqlc generate

# --- Lint -------------------------------------------------------------------

lint: lint-go lint-frontend

lint-go:
	@cd backend && diff=$$(gofmt -l . 2>&1); if [ -n "$$diff" ]; then echo "gofmt: files need formatting:"; echo "$$diff"; exit 1; fi
	cd backend && go vet ./...
	cd backend && go tool golangci-lint run ./...

# Split into three subtargets so CI can run them as separate steps
# (each with its own log line) without losing the dev-friendly
# "make lint-frontend" entry point.
lint-frontend: lint-frontend-typecheck lint-frontend-eslint lint-frontend-format

lint-frontend-typecheck:
	cd frontend && npm run typecheck
	cd frontend && npx tsc --noEmit -p e2e/tsconfig.json

lint-frontend-eslint:
	cd frontend && npm run lint

lint-frontend-format:
	cd frontend && npm run format:check

lint-go-fix:
	cd backend && gofmt -w .
	cd backend && go tool golangci-lint run --fix ./...

# --- Build ------------------------------------------------------------------

build: build-backend build-frontend

build-backend:
	cd backend && go build -o bin/orchestrator ./cmd/orchestrator \
	  && go build -o bin/executor ./cmd/executor \
	  && go build -o bin/supervisor ./cmd/supervisor

build-frontend:
	cd frontend && npm run build

# --- Test -------------------------------------------------------------------

test: test-backend test-frontend

# Go unit tests. Explicitly exclude `./integration/...` so this target
# stays sub-30s — the integration suite has its own boot cost (in-process
# orchestrator + 4 executors) and runs via `test-integration`. CI runs
# them as separate jobs for parallelism + clearer failure attribution.
test-backend:
	cd backend && go test -race -count=1 $$(go list ./... | grep -v '/integration')

test-frontend:
	cd frontend && npm run test

# Integration test suite — boots the real orchestrator + executor wiring
# in-process (one httptest.Server per role) via the harness in
# backend/integration/harness_test.go. Failure scenarios from
# ARCHITECTURE.md §5.4 (orchestrator restart, executor crash, executor
# hang, network flake) are covered alongside the API/state-machine
# corpus.
test-integration:
	cd backend && go test -race -count=1 -timeout=120s ./integration/...

# Same suite plus coverage attribution back to the orchestrator and
# executor internal packages. Useful for confirming a new test
# meaningfully exercises new code paths.
test-integration-cover:
	cd backend && go test -count=1 -timeout=180s \
		-coverpkg=./internal/... -covermode=atomic \
		-coverprofile=integration.out ./integration/...
	cd backend && go tool cover -func=integration.out | tail -n 40

# Coverage runs the backend test suite with -coverprofile excluding generated
# packages and prints a per-package summary. Subagents adding tests should
# pass this locally before reporting back.
COVER_PKGS = $$(cd backend && go list ./... | grep -Ev '/(gen|migrations)$$' | tr '\n' ',' | sed 's/,$$//')
cover: cover-backend

cover-backend:
	cd backend && go test ./... -race -coverprofile=coverage.out -coverpkg=$(COVER_PKGS)
	cd backend && go tool cover -func=coverage.out | tail -n 30

# --- CI gate ----------------------------------------------------------------

# Re-run codegen and fail if the generated files moved. Catches both
# "edited openapi.yaml but forgot to regen" and "hand-edited a generated file".
# Covers API codegen (Go + TS) and SQL codegen (sqlc) in one drift gate.
check-codegen: gen
	@git diff --exit-code -- backend/internal/api/gen backend/internal/store/gen frontend/src/api/gen.ts \
	  || { echo "::error::Generated code is out of sync. Run 'make gen' locally and commit the result."; exit 1; }

check: check-codegen lint build

# --- Demo -------------------------------------------------------------------

# Boot the dev backend stack via the supervisor sidecar:
#   - orchestrator on :8080
#   - supervisor   on :8090
#   - 4 prelaunched executors (deck-1..deck-4), 100-slot fleet
# Run `npm run dev` (frontend Vite) in a separate terminal for the UI.
demo: build-backend
	rm -rf $(CURDIR)/.demo-state
	EXECUTOR_COUNT=4 $(CURDIR)/backend/bin/supervisor -config $(CURDIR)/config/supervisor.demo.yaml

# Same as `demo` but prelaunches 25 executors instead of 4 (via the
# supervisor's EXECUTOR_COUNT shorthand). Fleet size is 100
# (orchestrator default), so the remaining 75 deck slots stay
# UNATTACHED and can be attached at runtime via Settings > Fleet
# Management.
demo-large: build-backend
	rm -rf $(CURDIR)/.demo-state
	EXECUTOR_COUNT=25 $(CURDIR)/backend/bin/supervisor -config $(CURDIR)/config/supervisor.demo.yaml

# --- E2E (Playwright) -------------------------------------------------------

# The Playwright suite owns its own stack lifecycle via playwright.config.ts's
# `webServer` entries (orchestrator + 4 executors + Vite dev server, all on
# distinct e2e ports). Tests must run after `make build` so the orchestrator /
# executor binaries the webServer spawns exist.
e2e: build-backend
	cd frontend && npm run e2e

e2e-headed: build-backend
	cd frontend && npm run e2e:headed

e2e-ui: build-backend
	cd frontend && npm run e2e:ui

e2e-install:
	cd frontend && npx playwright install --with-deps chromium
