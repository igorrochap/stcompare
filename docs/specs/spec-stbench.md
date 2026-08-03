# Spec: `stbench` — neutral loop runner and benchmark record

## Problem Statement

The research this tool supports measures how well a variety of coding agents —
cloud agents and locally-run open-source models — can fix a candidate API until
it converges against a frozen baseline. The `stcompare` agent contract (separate
spec) gives any agent a way to *evaluate* a candidate in one shot, but nothing
drives the **loop**, and if each agent's own harness drove it, the study would
measure *agent-plus-harness*, not the agent: a cloud agent's sophisticated
self-looping versus a local model's thin scaffold is a confound, and some
minimal scaffolds cannot self-loop at all. The research also needs comparable
**cost and effort** metrics per run (wall-clock time, iterations, tokens) that
no single agent reports uniformly.

There is also no uniform, repeatable way to: assert the baseline precondition,
reset the candidate to a clean state each iteration (APIs are state-sensitive,
so a dirty candidate corrupts replay outcomes), stop sensibly when an agent
stops making progress, and record the outcome in a machine-readable form for
downstream analysis.

## Solution

A second binary, `stbench`, that owns the loop **identically for every agent**,
so results are attributable to the agent. It is a pure consumer of `stcompare`'s
public CLI contract (it `os/exec`s the `stcompare` binary and reads the compact
`--format agent` JSON plus exit code — it never links the evaluator's internals;
see ADR-0004, ADR-0006). For each run it:

1. Asserts the baseline campaign exists (a precondition, not a loop step).
2. Repeats: reset the candidate to a clean state, rebuild, restart, wait
   healthy; run `stcompare campaign compare <candidate> --format agent`; branch
   on the exit code —
   - `0` (Converged) → stop, success.
   - `2` (not converged) → hand the compact actionable list to the agent via a
     per-agent **adapter**, which edits the candidate source and reports its
     token usage; then loop.
   - `1` (tool error) → stop, surface.
3. Terminates the loop on convergence, on a **stall** (the actionable count
   stops dropping), or on a **max-iteration** cap.
4. Emits one **benchmark record** per `(agent, candidate, run)` capturing
   terminal state, iteration count, a per-phase time breakdown, known token
   totals plus the number of iterations with unknown usage, and the final
   convergence counts.

The candidate lifecycle (reset/build/start/health) is uniform and therefore
owned by `stbench` via configuration; only the "work the fixes" step varies per
agent and is isolated behind the adapter. This keeps orchestration constant
across agents so the benchmark measures the model, not its harness.

## User Stories

1. As a researcher, I want one loop driver that behaves identically for every
   agent, so that my results reflect the agent's fixing ability, not its
   harness's looping ability.
2. As a researcher, I want to run a benchmark for a named candidate and agent
   with a single command, so that I can script sweeps across many agents.
3. As a researcher, I want `stbench` to assert the baseline campaign exists
   before starting, so that a missing baseline fails fast instead of producing
   meaningless iterations.
4. As a researcher, I want the candidate reset to a clean state before every
   `compare`, so that state left over from a prior replay does not corrupt the
   outcome of the next one.
5. As a researcher, I want `stbench` to rebuild and restart the candidate after
   the agent edits it, so that each `compare` runs against the agent's latest
   code, not a stale build.
6. As a researcher, I want the loop to stop as soon as the candidate converges,
   so that I do not waste time or tokens once the goal is met.
7. As a researcher, I want the loop to stop when progress stalls (the actionable
   count stops dropping over a configurable window), so that a genuinely hard or
   unfixable problem does not spin forever.
8. As a researcher, I want a hard max-iteration cap, so that pathological cases
   the stall detector misses still terminate.
9. As a researcher, I want a tool error from `stcompare` (exit `1`) to stop the
   run and be surfaced, so that a broken config is never retried as if it were
   an unconverged candidate.
10. As a researcher, I want each run to emit a machine-readable benchmark record,
    so that I can aggregate and compare results across agents and candidates.
11. As a researcher, I want the record to include a per-phase time breakdown
    (agent fix, candidate reset/rebuild/restart, compare) and total wall-clock,
    so that I can separate model latency from harness overhead.
12. As a researcher, I want the record to include the iteration count and the
    terminal state (converged, stalled, max-iterations, tool error, adapter
    error), so that I know how and why each run ended.
13. As a researcher, I want known token usage recorded as `{input, output,
    total}` while unknown iterations are counted explicitly, so that partial
    data is retained without collapsing "unknown" into "zero" or overstating
    an aggregate.
14. As a researcher, I want the record to carry model and hardware metadata I
    provide, so that I can read latency and token numbers fairly across cloud
    and local runs.
15. As a researcher, I want the record to include the final convergence counts
    (`still_failing`, `regressed`, and the `unverified` residual), so that even
    a non-converged run's ending state is captured.
16. As an integrator adding a new agent, I want a documented adapter protocol
    (how `stbench` invokes the agent and how the agent returns token usage), so
    that I can plug in a cloud agent or a local model without changing
    `stbench`.
17. As an integrator, I want to write an adapter in any language, so that
    Python-based local-model scaffolds are first-class, not second-class.
18. As an integrator, I want the adapter to receive the compact actionable list
    and to edit the candidate source in place, so that the "fix" step has
    exactly the context `stcompare` produced and nothing more.
19. As a maintainer, I want `stbench` to talk to `stcompare` only through its
    published CLI and the public `agentreport`/`benchrecord` packages, so that
    the runner dogfoods the same contract external adapters depend on and stays
    decoupled from the evaluator's internals.
20. As a maintainer, I want the loop logic testable without real subprocesses,
    services, or agents, so that the default test suite stays fast and
    dependency-free.
21. As a maintainer, I want an opt-in end-to-end integration test that exercises
    the real binary against fake services and a stub adapter, so that the wiring
    is verified without burdening the default suite.
22. As a researcher, I want a stalled or capped run to still record the remaining
    actionable items and which were "stuck" versus newly introduced, so that I
    can see what the agent failed to fix.

## Implementation Decisions

**New binary, module boundaries (per ADR-0006):**

- `cmd/stbench/main.go` — thin entrypoint; parses config/flags, calls the loop
  driver, maps the result to an exit code.
- `internal/bench/…` — the runner: loop driver, stall/termination logic,
  candidate-lifecycle orchestration, adapter invocation, record assembly. Lives
  in `internal/`; imports the public contract packages and `os/exec`; does
  **not** import `internal/comparison`.
- `benchrecord` — public, dependency-free package: the benchmark-record Go
  structs and a `schema_version`. Downstream analysis reads this schema.
- Consumes `agentreport` (public) to parse the compact `--format agent` JSON.

**Loop driver seam (dependency injection, mirroring
`comparison.Dependencies{Now}`):**

```
bench.Run(config, deps) (benchrecord.Record, error)

deps:
  Comparator  // runs `stcompare … compare --format agent`, returns parsed
              // agentreport.View + exit code (0/2/1)
  Candidate   // Reset, Build, Start, WaitHealthy, Stop  (candidate lifecycle)
  Adapter     // Fix(view) -> token usage {input,output,total|null}, edits
              // candidate source in place
  Now func() time.Time  // injected clock for the time breakdown
```

The three collaborators are interfaces so the loop is tested against fakes; the
real implementations are thin `os/exec`/HTTP wrappers covered by the opt-in
integration test.

**Candidate lifecycle (owned by `stbench`, configured, uniform across agents):**

- Configuration supplies the commands/hooks for `stop`, `reset` (optional),
  `build`, `start`, and a health check (URL to poll + timeout). Per iteration
  the runner runs: stop → reset → build → start → wait-healthy → compare. If any
  lifecycle step fails, the run ends with a distinct terminal state and the
  record captures which phase failed.
- The candidate base URL is taken from the existing `stcompare` config /
  `--base-url` override and passed through to `compare`.
- `base_url` and `lifecycle.health_url` must use the same candidate host and
  effective port. An omitted port means 80 for HTTP or 443 for HTTPS. Config
  validation reports a mismatch before the benchmark starts; the health URL may
  use a different path.

**Task prompt (fixed, versioned, owned by `stbench`; see ADR-0007):**

- `stbench` owns a single canonical **task prompt template** — the instruction
  that tells the agent what to do with the actionable list. It is fixed and
  versioned, not written per adapter, because a per-adapter prompt would turn
  prompt-engineering skill into a confound and defeat the point of a neutral
  runner (same fairness argument as ADR-0004).
- Each iteration `stbench` renders the template with the compact
  `agentreport.View` and passes the **rendered instruction** to the adapter
  alongside the view. The adapter only **delivers** it in its agent's required
  envelope (system/user split, tool-use framing, a local model's chat template);
  it must not rewrite the task.
- The prompt is fixed by default and overridable via config **only** for
  deliberate prompt-ablation experiments. The prompt identity (version or content
  hash) is recorded in the benchmark record, and two runs are comparable only
  when their recorded prompt identity matches. The record also stores a hash of
  each exact rendered instruction sent to the adapter.

**Adapter protocol (language-agnostic, per-agent):**

- `stbench` invokes the configured adapter command with the candidate source
  directory as its working directory.
- The adapter implementation and lifecycle harness are control-plane files,
  not candidate source. `stbench init` stores generated lifecycle scripts and
  the benchmark record in the repository-local `.local/stbench` control-plane
  directory by default, and prints configuration paths for that directory.
  Adapters exclude `.local/stbench` and `.local/stcompare` from source views and
  edits; reference adapter implementations should remain outside the source
  tree.
- The `agent`, `model`, and `hardware` metadata from the stbench configuration,
  the rendered task instruction, and the compact `agentreport.View` (the
  actionable list and counts) are passed to the adapter on **stdin** as JSON.
  The request metadata is the source of truth for adapter execution; an
  adapter may override it only through an explicit adapter option.
  This compact view is the agent's problem source on **every** iteration,
  including the first — never `junit.xml` or the raw VCR/HAR/NDJSON transcripts
  (those are internal evidence only; see Further Notes).
- The adapter edits the candidate source **in place** and writes a small result
  JSON to **stdout**: `{ "tokens": {"input": N, "output": N, "total": N} | null,
  "response": "<raw model response>", "status": "ok" | "error",
  "message": "…" }`. A non-zero adapter exit or `status: "error"` ends the
  run as `adapter_error`; the response text is retained for audit.
- Before the first comparison, `stbench` sends a no-op preflight request with
  `"preflight": true`. The adapter must execute its command, return an `ok`
  result, and exit without invoking a model or editing the candidate. The
  request does not include an instruction or compact view.
- The adapter — not `stbench` — is responsible for capturing tokens from its
  agent (provider `usage` for cloud/API agents; inference-server counts or a
  local tokenizer for local models). Unknown → `tokens: null`.

**Termination and stall detection:**

- **Converged** — `compare` exits `0`. Terminal state `converged`.
- **Stall** — the total actionable count does not strictly decrease across a
  configurable window of consecutive iterations (default window small, e.g. 2).
  Item identity uses the stable `id` from `agentreport` so the record can mark
  which items were stuck versus newly introduced. Terminal state `stalled`.
- **Max iterations** — a hard cap; terminal state `max_iterations`.
- **Tool error** — `compare` exits `1`; terminal state `tool_error`.
- **Adapter error** / **lifecycle error** — terminal states `adapter_error` /
  `lifecycle_error`.

**Benchmark record (`benchrecord`, one per run):**

```
{
  "schema_version": "...",
  "agent": "...", "model": "...", "hardware": "...",   // provided by the caller
  "prompt": { "id": "...", "version": "...", "hash": "..." },
                                                        // task-prompt identity (ADR-0007)
  "prompt_instructions": ["..."],                     // rendered instruction per agent fix
  "rendered_prompt_hashes": ["..."],                   // hash of each exact instruction
  "agent_responses": ["..."],                          // raw adapter/model response per fix
  "candidate": "...", "baseline": "...",
  "started_at": "...", "ended_at": "...",
  "iterations": N,
  "terminal_state": "converged" | "stalled" | "max_iterations"
                    | "tool_error" | "adapter_error" | "lifecycle_error",
  "time_ms": { "total": N, "agent_fix": N, "candidate_reset": N, "compare": N },
  "tokens": { "input": N, "output": N, "total": N } | null,
  "unknown_token_iterations": N,
  "final": {
    "converged": bool,
    "still_failing": N, "regressed": N,
    "unverified": { "inconclusive": N, "uncorrelated": N,
                    "ambiguous": N, "unevaluable": N }
  },
  "remaining_actionable": [ { id, kind, operation, stuck: bool } ]
}
```

The three per-fix arrays use the same index: instruction, rendered-instruction
hash, and raw agent response.

- `tokens` at the record level is the sum of iterations with known usage.
  `unknown_token_iterations` counts fix iterations whose usage was unknown.
  `tokens` is `null` only when no iteration reported known usage, which
  distinguishes an all-unknown run from a run with a retained partial sum.
- `remaining_actionable` is empty on a converged run.

**Configuration/CLI:** `stbench run` takes the candidate name, the agent/adapter
command, model/hardware metadata, lifecycle commands, max-iterations, stall
window, and the record output path — via an `stbench` config file and/or flags,
reusing `stcompare`'s config for schema/base-url/reports-dir.

## Testing Decisions

Good tests assert **external behavior**: the terminal state, iteration count,
the emitted benchmark record, and the phase timings — driven through injected
fakes, never by reaching into loop internals. The loop must be provable without
real subprocesses, services, or agents.

- **Loop driver (`internal/bench`, primary seam `bench.Run(config, deps)`).**
  Inject fake `Comparator`, `Candidate`, `Adapter`, and a controllable `Now`.
  Prior art: `comparison.Compare(input, Dependencies{Now})` and the fake-server
  CLI tests. Cover:
  - Converges immediately (Comparator returns exit `0` on iteration 1) →
    terminal `converged`, `iterations == 1`, adapter never invoked.
  - Iterates then converges (exit `2`, `2`, `0`) → adapter invoked for each non
    converged iteration, terminal `converged`, correct iteration count.
  - Stall (actionable count flat across the window) → terminal `stalled`,
    `remaining_actionable` marks the stuck items via stable `id`.
  - Max iterations hit before convergence → terminal `max_iterations`.
  - Tool error (exit `1`) → terminal `tool_error`, loop stops immediately.
  - Adapter error (`status: error` / non-zero exit) → terminal `adapter_error`.
  - Lifecycle failure (fake `Candidate` fails a phase) → `lifecycle_error` with
    the failing phase recorded.
  - Time breakdown sums correctly from the injected clock across phases.
  - Token aggregation: numeric usages sum, mixed numeric/`null` iterations
    retain the known sum and count the unknown iterations, and an all-`null`
    run keeps the record total `null`.
  - The embedded prompt template is identified by a content hash, and each
    rendered instruction is archived in the benchmark record.
- **Benchmark record schema (`benchrecord`).** Pure marshal/round-trip tests of
  the record shape and `schema_version`, mirroring `report_test.go`'s JSON
  assertions.
- **Opt-in end-to-end integration test** (`integration/`, prior art
  `TestOptionalEndToEndBenchmarkVerification`, gated behind an env var). Build
  both binaries; stand up fake baseline and candidate HTTP services; use a
  **stub adapter** (a tiny script that applies a canned edit and prints token
  usage); run `stbench run` and assert a converged record is produced. Skips
  cleanly when the env var is unset or Schemathesis is unavailable, and leaves
  no processes, ports, or directories behind — matching the existing e2e target.

## Out of Scope

- Any change to `stcompare`'s evaluation, replay, reporting, or the agent
  contract itself — that is the separate agent-contract spec and is a dependency
  of this one.
- Shipping concrete production integrations for specific agents. The repository
  does provide reference examples for an on-prem local-model scaffold, a
  Codex/Claude Code CLI, and a cloud API fallback; these examples do not change
  the protocol or claim to be production integrations.
- Cost ($) computation and cross-run aggregation/plotting — downstream analysis
  over the emitted records, not `stbench`'s job.
- Parallelism across multiple candidates/agents in one invocation — one run per
  invocation for now; sweeps are scripted externally.
- Building CI/CD and the depguard rule that would forbid `internal/bench` from
  importing `internal/comparison` — a separate follow-up (the boundary is upheld
  by construction meanwhile).
- Candidate provisioning/deployment beyond running the configured lifecycle
  commands (no container/orchestrator management built in).

## Further Notes

- `stbench` is a *pure consumer* of `stcompare`'s CLI. It must not import
  `internal/comparison`; it shares only the public `agentreport` and
  `benchrecord` packages. This is deliberate dogfooding of the same contract
  external adapters use (ADR-0004, ADR-0006).
- The fresh-candidate-per-iteration reset exists specifically because APIs are
  state-sensitive: the baseline HAR was recorded against baseline state, so
  replaying it against a candidate carrying state from a previous replay can
  produce misleading outcomes.
- Stall detection and the max-iteration cap both rely on facts the agent
  contract already provides: the compact counts and the stable per-item `id`.
  `stbench` adds no evaluation logic of its own.
- Glossary terms **Converged**, **Still Failing**, **Regression**, and
  **Inconclusive** are defined in `CONTEXT.md`; `stbench` and the record use
  them verbatim.
- This spec depends on the agent-contract spec landing first (contract-first
  sequencing); `stbench` cannot branch on exit codes or parse the compact view
  until that exists.
- **The agent never reads `junit.xml`.** On every iteration, including the
  first, the agent's problem source is the compact `--format agent` view from
  `compare` — which is more suitable than `junit.xml` on the axis that matters
  (compact, low-junk) *and* is correlated, actionable, and filtered to what is
  actually `still_failing` against this candidate. `junit.xml` remains a
  low-precedence internal evidence input and an audit cross-count only. Passing
  raw VCR/HAR/NDJSON to the agent is explicitly avoided (context-window junk),
  and `uncorrelated` baseline problems are deliberately kept out of the agent's
  view because they cannot be verified fixed and do not block Convergence.
  This retires the manual "hand the agent `junit.xml` on iteration 1" workaround.
