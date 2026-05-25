"""C7: per-resource refetch leakage — useQuery with real queryFn."""
from __future__ import annotations

import re

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    repo_root = inp.repo_root
    if repo_root is None:
        return None
    src_dir = repo_root / "frontend" / "src"
    if not src_dir.exists():
        return None
    # Grep useQuery(...{ queryFn: ... }) and exclude the throw-pattern the
    # docs say is fine ("queryFn: throw" or "queryFn: () => { throw ... }").
    real_queryfns: list[str] = []
    for path in src_dir.rglob("*.ts*"):
        text = path.read_text(errors="ignore")
        for m in re.finditer(r"useQuery\s*\(\s*\{[^}]*queryFn\s*:\s*([^,}]+)", text):
            body = m.group(1).strip()
            if "throw" in body:
                continue
            real_queryfns.append(f"{path.relative_to(repo_root)}: queryFn={body[:60]}")
    if not real_queryfns:
        return None
    sev = "med" if len(real_queryfns) > 5 else "low"
    return Finding(
        id="C7",
        title="useQuery() calls with real queryFn (deviation from live-cache design)",
        severity=sev,
        summary=f"{len(real_queryfns)} `useQuery` call sites declare a non-throw queryFn.",
        detail=(
            "- README claims the live `/api/state` poll replaces all per-resource refetches.\n"
            "- These call sites declare an actual fetcher — each is a regression / "
            "intentional bypass that bypasses the live cache:\n"
            + "\n".join(f"  - {r}" for r in real_queryfns[:20])
            + ("\n  - … and "
               f"{len(real_queryfns) - 20} more" if len(real_queryfns) > 20 else "")
            + "\n"
        ),
        mitigation=("Audit each: if the data is in the live cache, switch to "
                    "`queryFn: throw` + cache-read; if not (e.g., supervisor data), "
                    "document the exception."),
        metrics={"n_real_queryfns": len(real_queryfns)},
    )
