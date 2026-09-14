# The agent view lists problem groups and is bounded by evidence caps

The compact `--format agent` view ([ADR-0004](0004-fix-loop-lives-outside-stcompare.md))
was meant to keep each loop iteration's payload "small and roughly constant".
It was not: it listed one entry per still-failing case, so its size was linear
in the campaign's problem count. A real run (`--max-examples 500`, all phases)
produced 3192 entries and a 866 KB rendered prompt (~377k tokens), which
overflowed even a 256k-context model before the agent could act — and nearly
all of that was repetition: only 30 distinct route templates and 54 distinct
(route, check category, message, status) combinations were present.

We decided that the evaluator — not the runner, the campaign config, or the
agent — owns the size bound. The view now lists **Problem Groups**: every
actionable case is folded into the group that shares its kind, operation
template (resolved against the OpenAPI contract, falling back to the concrete
path when no template resolves), check category, one-line message, and
baseline/candidate status pair. Each group carries a `count`, up to three
sample `refs` into `comparison.json`, and one deterministically chosen
evidence `sample` (concrete operation, request and response bodies truncated
to a fixed 512-byte cap, and up to five check-specific detail lines such as
schema validation errors). The view is therefore bounded by
*groups × fixed cap*, never by traffic volume. This is
[ADR-0002](0002-comparison-reports-are-problem-centric.md)'s problem-centric
principle applied to the agent contract.

## Considered Options

- **Runner-side budgeting** (`stbench` slices the per-case list to a byte or
  token budget per iteration) — rejected. It changes the task from "fix the
  candidate" to "fix these N", muddies stall detection, and the agent never
  sees the whole picture. Uniform across agents, but it moves a judgement about
  *what the problem is* out of the evaluator.
- **Lower `--max-examples`** — rejected as the fix. It changes the benchmark,
  not the instrument, and the per-case redundancy remains at every scale.
- **Agent pulls detail on demand** (a query tool or a `show-ref` command
  instead of pushing evidence) — rejected. It is fair only if every adapter
  exposes the same tool, and it taxes weak tool-users — the scaffold confound
  [ADR-0011](0011-local-adapter-is-a-faithful-measurement-scaffold.md) exists
  to remove.
- **Refs only, no inline evidence** — rejected. The local adapter's
  `read_file` cannot reach interaction *n* inside a multi-megabyte
  `comparison.json`, while a shell-capable cloud agent can; refs alone would
  give cloud agents evidence local agents cannot obtain. A small inline sample
  gives every agent the same clue.
- **Coarser key (route + category only)** — rejected. It merges "undocumented
  400" with "undocumented 422" on the same route, which are usually different
  fixes, and costs almost nothing to keep apart (52 vs 54 groups on the run
  above).
- **Per-run evidence cap knob** — rejected. A fixed constant keeps every
  run's view comparable; a knob would be one more ablation dimension every
  record must carry.

## Consequences

- The agent view schema is bumped (`1` → `2`) as a breaking change; `actionable`
  keeps its name but each entry is a Problem Group. There is no v1 output mode.
  Benchmark records produced under view v1 and v2 are **not poolable**; the
  record carries the agent-view schema version so the regime is recoverable.
- The task prompt wording changes to explain `count`, `sample`, and `refs`, so
  the default prompt is re-versioned per
  [ADR-0007](0007-benchmark-task-prompt-is-fixed-and-versioned.md).
- Progress and stall detection keep their current meaning: they track the
  *case* total (`counts.still_failing + counts.regressed`), not the number of
  groups, so a partial fix on a large route still counts as progress. A group
  `id` is stable per *problem*, not per case: a fix that changes a group's
  status pair produces a new group id, which is the correct reading ("a
  different problem now").
- Groups are ordered regressions first, then by operation template, category,
  and status — never by `count` — so same-route work stays adjacent and the
  view does not editorialize on priority.
- The per-case duplicate-id defect in v1 (`hash(kind, ref, case_id)` collided
  when one case carried two problems of the same category) is moot: a group id
  is unique by construction.
- The view is bounded, not constant: an API with hundreds of routes can still
  render a large prompt. `stbench` records the rendered prompt size and offers
  an opt-in byte cap that ends a run with a distinct terminal state before the
  adapter is invoked; that guard is a runner knob, not part of this decision.
