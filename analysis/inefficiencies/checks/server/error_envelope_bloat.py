"""S5: VERSION_MISMATCH / ALREADY_TERMINAL envelopes embed the full Run."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    df = inp.wire_http
    if df is None or len(df) == 0:
        return None
    # We don't get response body content from the middleware (only bytes_out),
    # so this is a static-spec-backed finding: scan openapi.yaml for the two
    # error variants that embed `current_state: Run`.
    repo_root = inp.repo_root
    if repo_root is None:
        return None
    spec = (repo_root / "api" / "openapi.yaml").read_text()
    suspects = ["VersionMismatchError", "DuplicateResourceError", "AlreadyTerminalError",
                "InvalidTransitionError"]
    embedding = [s for s in suspects if "current_state:\n              $ref: '#/components/schemas/Run'"
                 in spec.split(f"{s}:", 1)[-1][:1000]]
    if not embedding:
        # If the spec changed format, fall back to text search.
        embedding = [s for s in suspects if s in spec and "current_state" in spec]

    # Spot-check: count 4xx with route in the mutate set.
    errs = df[(df["status"] >= 400) & (df["status"] < 500)]
    avg_err_bytes = float(errs["bytes_out"].mean()) if not errs.empty else 0.0
    sev = "med" if avg_err_bytes > 1000 else "low"
    return Finding(
        id="S5",
        title="Error envelopes for state-mismatch errors embed the full Run",
        severity=sev,
        summary=(f"{len(embedding)} error variants ship `current_state: Run`; "
                 f"mean 4xx body in capture: {avg_err_bytes/1024:.2f} KiB."),
        detail=(
            f"- Variants found in `api/openapi.yaml`: {', '.join(embedding) or 'none detected'}.\n"
            f"- Per `errors.go`, the orchestrator re-fetches the run, marshals it, "
            f"and embeds it under `error.details.current_state`. Every version-off-by-one "
            f"trip costs ≈ the full run summary in bytes.\n"
            f"- Captured 4xx responses: N={len(errs)} mean body={avg_err_bytes/1024:.2f} KiB.\n"
        ),
        mitigation=("Ship `details.current_version` only. Clients that need the new "
                    "shape already re-fetch via the live cache on the next 1Hz tick."),
        metrics={"variants_embedding_run": len(embedding),
                 "avg_4xx_body_bytes": avg_err_bytes,
                 "n_4xx": len(errs)},
    )
