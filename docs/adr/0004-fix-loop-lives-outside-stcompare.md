# The fix loop lives outside stcompare, behind a harness-neutral CLI contract

`stcompare` stays a stateless *evaluator*: it runs one comparison and reports
the result. The iterative "read problems, edit the candidate, re-compare until
converged" loop lives in a separate orchestrator, never inside `stcompare`. The
two communicate only through `stcompare`'s public command-line contract — a
compact machine view on stdout plus an exit code — so that *any* agent, from a
cloud coding agent to a locally-run open-source model under a minimal scaffold,
can drive the loop by running a shell command and reading its output. This tool
is the measurement instrument for a cross-agent benchmark, and the CLI is the
lowest common denominator every agent can speak.

## The contract

`stcompare campaign compare <candidate> --format agent` emits, to stdout, a
compact JSON projection of the comparison (see [ADR-0005](0005-convergence-is-zero-still-failing-zero-regressed.md)),
and returns an exit code:

- `0` — Converged.
- `2` — Ran successfully but not converged; the caller should iterate.
- `1` — Tool error (bad config, missing baseline, malformed artifact); the
  caller should stop and surface it, never retry.

Human-oriented "wrote <path>" lines move to stderr in this mode; the full
`comparison.json` / `.md` / `.html` artifacts are still written to disk for
audit. The `converged` flag and its deciding counts are canonical in
`comparison.json`; the exit code is derived from them.

## Loop invariants (owned by the orchestrator, not by stcompare)

- **Baseline is a once-outside precondition.** `campaign run baseline` is run
  once before the loop and then frozen — it is the immutable reference, and
  re-running it mid-loop would break cross-iteration comparability. A missing
  baseline transcript is a tool error (exit `1`), not something the loop tries
  to fix by iterating.
- **Fresh candidate per iteration.** Every `compare` runs against a
  clean-state candidate (reset, rebuild, restart, wait healthy), so outcomes
  are attributable to code changes rather than state leaked from a previous
  replay.
- **Termination is the orchestrator's job.** The loop stops on convergence, on
  a stall (the actionable count stops dropping), or on a max-iteration cap.
  `stcompare` needs nothing extra for this: the compact counts plus a stable
  per-item identity are sufficient.

## Considered Options

- **`stcompare` owns the loop** (e.g. a `stcompare fix` command) — rejected.
  The loop edits source, resets and restarts the candidate service, and makes
  judgement calls; folding that into the evaluator would couple a clean,
  deterministic, testable component to an agent runtime and environment it
  should know nothing about.
- **The loop exists only as an agent-specific skill** (e.g. a Claude Code
  `SKILL.md`) — rejected as the *mechanism*. It would silently restrict the
  benchmark to agents that speak that harness, biasing the sample. Such a skill
  is fine as *one adapter* over the CLI contract, but the CLI is the load-bearing
  interface.

## Consequences

- Benchmark orchestration is a separate program (`stbench`, see
  [ADR-0006](0006-single-module-two-binary-layout.md)) that drives a uniform
  loop and invokes each agent through a thin per-agent adapter, so results
  measure the *agent*, not the agent-plus-its-harness. `stcompare` is unaware
  of it.
- Exit codes must keep "ran fine, not converged" (`2`) distinct from "tool
  failed" (`1`), or a loop would retry forever on a broken config.
- The compact stdout view exists to keep each iteration's payload small and
  roughly constant regardless of response-body sizes; the full artifacts remain
  the complete auditable record.
