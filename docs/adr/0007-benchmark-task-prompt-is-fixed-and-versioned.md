# The benchmark task prompt is fixed, versioned, and owned by `stbench`

`stbench` owns a single canonical **task prompt** — the instruction that tells an
agent what to do with the actionable problem list. It is fixed and versioned,
not written per adapter. The prompt is rendered with the compact
`agentreport.View` and handed to the adapter; the adapter only **delivers** it in
its agent's required envelope (system/user split, tool-use framing, a local
model's chat template) and must not rewrite the task. The prompt's identity
(version or content hash) is recorded in every benchmark record.

## Why

This is the same fairness argument that put orchestration in a neutral runner
(ADR-0004): if each adapter author wrote their own prompt, the benchmark would
measure prompt-engineering skill per adapter rather than model ability — a
well-prompted weak model could beat a poorly-prompted strong one. Holding the
task instruction uniform across all agents keeps results attributable to the
agent. Splitting "instruction" (uniform, `stbench`-owned) from "delivery
envelope" (agent-specific, adapter-owned) lets local and cloud agents receive
the *same task* in whatever format each needs.

## Considered Options

- **Caller-supplied prompt each run** — rejected as the default: flexible, but it
  makes it trivial to accidentally run two agents under different instructions
  and produce non-comparable numbers. Kept only as an explicit override for
  prompt-ablation experiments.
- **Adapter-owned prompt** — rejected: reintroduces the harness confound the
  neutral runner exists to remove.

## Consequences

- The default prompt is fixed and versioned; runs are comparable only when their
  recorded prompt identity matches. An override exists for deliberate ablation.
- `stbench` renders the prompt from the versioned template plus the compact view
  and passes the rendered instruction to the adapter alongside the view.
- The benchmark record carries the template identity, each rendered-instruction
  hash and text, and the raw adapter response, so any run's fairness and agent
  output are auditable after the fact.
- The agent's problem source on every iteration — including the first — is the
  compact `--format agent` view from `compare`, never `junit.xml` or the raw
  VCR/HAR/NDJSON transcripts. See ADR-0004 (compare-first) and the `stbench`
  spec.
