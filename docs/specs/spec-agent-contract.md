# Spec: Agent-facing convergence contract for `campaign compare`

## Problem Statement

The tool is meant to be the measurement instrument for a cross-agent benchmark:
coding agents — cloud agents and locally-run open-source models alike — should
be able to fix a candidate API in a loop, re-running `stcompare campaign compare`
until every baseline problem the candidate can be held to is resolved. Today
that loop is impractical for an agent to drive:

- `campaign compare` **always exits `0`**, whether the candidate is perfect,
  still broken, or the run failed. An agent has no cheap, deterministic signal
  for "am I done?" versus "iterate again" versus "stop, something is wrong."
- The only machine-readable output, `comparison.json`, is an **audit-grade
  document**: every finding carries full request/response headers and bodies,
  latency stats, status-transition histograms, normalization-policy disclosure,
  and explanatory prose. An agent re-reading it every iteration burns its
  context window fast, and almost none of it is relevant to deciding *what to
  fix next*.
- There is no single field that answers "is this candidate done?" — the reader
  must reason across `fix_rate`, the problem-outcome buckets, and the traffic
  `regressed` count to work it out, and the obvious shortcut (`fix_rate ==
  100%`) is misleading because it ignores regressions and unverified problems.

## Solution

Give `campaign compare` an agent-facing contract with three parts:

1. **A canonical convergence verdict in `comparison.json`.** A new top-level
   `converged` boolean plus the counts that decide it, so "done?" is a single
   read and the reasoning behind it is auditable in the same document. This is a
   report schema version bump (10 → 11).

2. **A compact `--format agent` view on stdout.** A small, roughly
   constant-size JSON projection containing only what an agent needs to decide
   convergence and act on the next fix — with a pointer back into the full
   `comparison.json` for on-demand detail. Human "wrote <path>" lines move to
   stderr in this mode; all four on-disk artifacts are still written unchanged.

3. **Convergence-derived exit codes.** `0` = Converged, `2` = ran fine but not
   converged (iterate), `1` = tool error (stop and surface). The loop's control
   flow becomes `stcompare … && done || iterate`, with tool errors never
   mistaken for "not converged."

A **Converged** comparison (see `CONTEXT.md`) is one where no baseline problem
is `still_failing` and no interaction is `regressed`. It deliberately tolerates
`inconclusive` problems, and the contract stays honest about that by always
reporting the residual unverified counts. See ADR-0004 and ADR-0005.

## User Stories

1. As a coding agent, I want a single `converged` boolean in the comparison, so
   that I can decide whether to stop or iterate with one read instead of
   reasoning across several metrics.
2. As a coding agent, I want `campaign compare` to exit `0` when the candidate
   has converged, so that my loop can branch on the exit code alone without
   parsing output.
3. As a coding agent, I want `campaign compare` to exit `2` when the candidate
   ran fine but has not converged, so that I know to iterate rather than stop.
4. As a coding agent, I want `campaign compare` to exit `1` only on genuine tool
   errors (bad config, missing baseline, malformed artifact), so that I stop and
   surface the problem instead of looping forever against a broken setup.
5. As a coding agent, I want a `--format agent` mode that prints a compact JSON
   summary to stdout, so that I get the convergence verdict and my next actions
   in the tool result I already receive, without an extra file read.
6. As a coding agent, I want the compact view to stay roughly the same size
   regardless of how large the candidate's response bodies are, so that my
   context window is not blown out by one big payload.
7. As a coding agent, I want the compact view to list only the items I can act
   on — `still_failing` problems and `regressed` interactions — so that I am not
   handed problems I cannot fix by editing endpoint code.
8. As a coding agent, I want each actionable item to name its check category,
   its operation (`METHOD /path`), its observed-vs-expected status, and a
   one-line message, so that I can usually identify the fix without opening the
   full report.
9. As a coding agent, I want each actionable item to carry a `ref` pointer into
   `comparison.json`, so that I can pull the full request/response detail for the
   one item I am working on when the summary is not enough.
10. As a coding agent, I want the actionable list sorted deterministically —
    regressions first, then clustered by operation — so that I see newly-broken
    behavior first and can fix same-endpoint items together.
11. As a coding agent, I want each actionable item to have a stable identity
    across iterations, so that a driver can tell "same item still stuck" from "a
    new item my last fix introduced."
12. As a coding agent, I want the compact view to include progress counts
    (`fixed`, `still_failing`, `regressed`), so that I (or my driver) can detect
    a stall when the actionable count stops dropping.
13. As a coding agent, I want the compact view to include an `unverified` block
    (`inconclusive`, `uncorrelated`, `ambiguous`, `unevaluable` counts), so that
    I never mistake "Converged" for "every baseline problem is confirmed fixed."
14. As a researcher running the benchmark, I want "Converged" to mean no
    confirmed-broken behavior and no regressions across *all* evaluated check
    categories, so that a converged result is a meaningful, honest bar.
15. As a researcher, I want `converged` and its deciding counts to be canonical
    in `comparison.json`, so that the compact view and the exit code are both
    derivable from one auditable source of truth.
16. As a human reviewer, I want the default (non-`agent`) output of
    `campaign compare` to remain exactly as it is today, so that the agent mode
    does not disrupt my normal use.
17. As a human reviewer, I want all four artifacts (`replay.har.json`,
    `comparison.json`, `comparison.md`, `comparison.html`) to still be written
    in agent mode, so that the full auditable record is never sacrificed for the
    compact view.
18. As a maintainer, I want the report schema version bumped when `converged`
    and its counts are added, so that consumers can tell v11 output from v10.
19. As a maintainer, I want the exit-code decision to live in the command layer
    (not in `main`), so that it is unit-testable at the CLI seam.
20. As a benchmark integrator building a non-Go adapter, I want the compact
    `--format agent` JSON to be a documented, versioned schema, so that I can
    parse it from any language without importing Go packages.

## Implementation Decisions

**Modules built/modified (all within the `stcompare` binary; no `stbench` in
this slice):**

- `internal/comparison` — report construction and rendering.
  - Add a top-level `converged` boolean to the `report` struct emitted in
    `comparison.json`, plus the counts that decide it. `converged` is true iff
    `summary.baseline_problems.still_failing == 0` **and**
    `summary.traffic.regressed == 0`. Computed in `newReport` from data already
    present on the report; no new classification logic — every check category is
    already evaluated (`problemClassifiersByCategory`).
  - Bump `reportSchemaVersion` from `10` to `11`.
  - Add a pure projection function `report → agent view` that produces the
    compact structure. It reads only fields already on `report`.
- A public, dependency-free `agentreport` package (per ADR-0006) holding the
  compact view's Go structs, its own `schema_version` constant, and the
  exit-code constants. The evaluator imports it to produce the view; future
  consumers (`stbench`, Go adapters) import it to parse. Non-Go adapters use the
  documented JSON schema. Placing it outside `internal/` is deliberate: it is a
  published contract.
- `internal/cli` — the `campaign compare` command.
  - Add a `--format` flag accepting `agent` (default is the current human
    output). In `--format agent`: write the compact JSON to **stdout**; route
    the existing "replayed N…" / "wrote <path>" lines to **stderr**.
  - Return a **typed exit-code error** from the command so exit codes are set in
    the command layer and asserted in tests. Mapping: Converged → `0`,
    not converged → `2`, tool error (config/baseline/artifact/replay failures) →
    `1`. Non-agent (human) mode uses the same exit-code mapping so the
    convergence signal is available without opting into the compact view.
- `cmd/stcompare/main.go` — map the typed exit-code error to `os.Exit(code)`.
  Keep it dumb: no decision logic beyond reading the code off the error.

**Compact agent view shape (canonical schema owned by `agentreport`):**

```
{
  "schema_version": "<agent-view version>",
  "converged": false,
  "candidate": "<campaign name>",
  "baseline": "<campaign name>",
  "counts": {
    "fixed": 3,
    "still_failing": 2,
    "regressed": 1
  },
  "unverified": {
    "inconclusive": 5,
    "uncorrelated": 1,
    "ambiguous": 0,
    "unevaluable": 0
  },
  "actionable": [
    {
      "id": "<stable across iterations>",
      "kind": "regressed" | "still_failing",
      "check_category": "server_error",
      "operation": "POST /widgets",
      "status": { "baseline": 200, "candidate": 500 },
      "message": "<one line>",
      "ref": <interaction number into comparison.json>
    }
  ]
}
```

- `actionable` contains only `still_failing` problems and `regressed`
  interactions. Sorted: `regressed` before `still_failing`, then by `operation`,
  then by `ref`. Flat, not nested.
- `id` is a stable identity derived from `(kind, ref, case_id)`; because the
  baseline HAR is frozen, it is stable across iterations.
- `ref` is the correlated interaction number in `comparison.json`. For an item
  whose baseline response is unrecorded/uncorrelated the corresponding
  status/ref fields follow the same null conventions the full report already
  uses.
- Counts and the `converged` verdict are projected from the same `report`
  values that are canonical in `comparison.json`; the two never disagree.

**Contracts / provenance:**

- `comparison.json` gains `converged` and the deciding counts (the buckets it
  already carries under `summary` remain; `converged` is the new top-level
  verdict). Schema version → `11`.
- Exit codes are a stable part of the CLI contract: `0` converged, `2` not
  converged, `1` tool error.

## Testing Decisions

Good tests here assert **external behavior**: the JSON a consumer reads, the
stdout/stderr split, and the process exit code — never internal function
wiring. Two existing seams, no new ones:

- **`newReport` (`internal/comparison`, prior art in `report_test.go`).** In
  memory, no I/O. Cover: `converged == true` when zero `still_failing` and zero
  `regressed`; `converged == false` when a `still_failing` problem remains;
  `converged == false` when a `regressed` interaction exists even with zero
  `still_failing`; `converged == true` while `inconclusive` problems are present
  (tolerated); schema version is `11`. Reuse the existing
  `newReport(reportInput{…})` construction style and the real Schemathesis
  fixtures already used by `TestNewReportCategorizesRealSchemathesisFixtureProblems`.
- **The compact projection (`report → agent view`), tested at the same
  `internal/comparison` seam** as a pure transform: actionable list contains
  only `still_failing` + `regressed`; ordering is regressions-first then by
  operation then `ref`; `unverified` counts match the report buckets; item `id`
  is stable when the same fixture is re-projected; `ref` points at the correct
  interaction; counts equal the canonical report counts.
- **The `campaign compare` command (`internal/cli`, prior art in
  `campaign_compare_report_test.go` / `campaign_compare_test.go`).** Drive the
  command end-to-end against the existing fake candidate HTTP server. Cover:
  `--format agent` writes compact JSON to stdout and the human lines to stderr;
  all four artifacts are still written to disk; default (human) mode output is
  unchanged; exit code is `0` for a converged fixture, `2` for a not-converged
  fixture, and `1` for a tool error (e.g. missing baseline transcript). Exit
  codes are asserted via the command's returned typed error, so no test needs to
  call `os.Exit`.

## Out of Scope

- The `stbench` neutral loop runner, the benchmark record (`benchrecord`), and
  any per-agent adapter — separate programs and separate specs (ADR-0004/0006).
- Candidate reset/rebuild/restart orchestration and the fresh-candidate-per-
  iteration invariant — owned by the runner, not `stcompare`.
- Stall detection and max-iteration termination — owned by the runner; this
  slice only provides the counts and stable item identity that make them
  possible.
- Token and time instrumentation — owned by the runner/adapters.
- Expanding which check categories are evaluable — all five are already
  evaluated; no change here.
- Cost ($) derivation — a downstream analysis concern.
- Building CI/CD and the depguard boundary ratchet — a separate follow-up.
- Any change to `campaign run`, replay mechanics, normalization, or the
  precondition/heuristic policy.

## Further Notes

- `converged` is intentionally **not** `fix_rate == 100%`. `fix_rate` excludes
  `inconclusive`/`unevaluable`/`uncorrelated`/`ambiguous` and ignores
  regressions, so it is an honest reporting metric but the wrong control signal.
  `fix_rate` stays exactly as it is; `converged` is the new, separate verdict.
  See ADR-0005.
- The default human output and exit-code semantics are shared: a human running
  `campaign compare` also gets exit `2` on a non-converged candidate. This is a
  visible behavior change from today's always-`0` and is intended — the exit
  code reflects the true state regardless of output format.
- Glossary: **Converged**, **Still Failing**, **Regression**, **Inconclusive**
  are defined in `CONTEXT.md`; use those terms verbatim in code and reports.
- The README's baseline-problem section was updated this session to reflect that
  all five check categories are evaluated; keep report/​doc wording consistent
  with that when touching output.
