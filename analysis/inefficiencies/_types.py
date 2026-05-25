"""Shared types for the inefficiency scanner."""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Literal


Severity = Literal["low", "med", "high"]


@dataclass
class Finding:
    id: str                  # "S1", "C4", etc — stable across reports
    title: str
    severity: Severity
    summary: str             # one-liner for the worst-first table
    detail: str = ""         # multi-line markdown body
    mitigation: str = ""     # one-line proposed fix
    metrics: dict = field(default_factory=dict)  # numeric backing data


@dataclass
class Inputs:
    """Bag of corpora available to each check; check returns None if N/A."""
    wire_http: object | None = None   # pandas DataFrame from wire/analyze.py
    scenario_log: list[dict] = field(default_factory=list)
    console_runs: dict = field(default_factory=dict)
        # {scenario_name: {"har": [...], "metrics_final": {...},
        #                  "metrics_ndjson": DataFrame}}
    scale_runs: dict = field(default_factory=dict)
        # {N: {"http": DataFrame, "samples": DataFrame, "meta": dict}}
    repo_root: object | None = None  # pathlib.Path
