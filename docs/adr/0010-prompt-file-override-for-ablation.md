# A `prompt.file` override loads an external task prompt for ablation

[ADR-0007](0007-benchmark-task-prompt-is-fixed-and-versioned.md) fixed the
benchmark task prompt: a single canonical template, embedded in the binary,
owned by `stbench`, and identified in every record. It also anticipated "an
explicit override for prompt-ablation experiments" without specifying one. This
ADR materializes that override.

A new `stbench.prompt.file` config field (and matching `--prompt-file` flag)
points at an external Go `text/template`. When set, `stbench` renders *that*
template each iteration instead of the embedded `prompt.md`, using the same
`{Prompt, ComparisonView}` template data. When unset — the default — nothing
changes: the embedded prompt is used exactly as before.

```yaml
stbench:
  prompt:
    id: ablation-terse
    version: "1"
    file: prompts/ablation-terse.md   # optional; empty/absent = embedded default
```

The override replaces the *instruction*, not the delivery envelope; ADR-0007's
split between the `stbench`-owned task and the adapter-owned envelope is
untouched.

## Why

The research needs to ask whether prompt wording moves benchmark outcomes. That
question is unanswerable while the only prompt is compiled into the binary:
testing a variant would mean rebuilding `stbench`, and the record would still
claim the embedded prompt's hash. An explicit, first-class override lets a
researcher run the same candidate under a different instruction and get records
that honestly say so.

The tension with ADR-0007 is fairness: if custom prompts were the easy default,
the benchmark would drift back into measuring prompt-engineering skill. Two
things keep that from happening. First, the override is off by default and never
scaffolded by `stbench init`, so the canonical embedded prompt remains the path
of least resistance. Second — and this is the load-bearing guarantee — a run
that uses a custom file records `prompt.hash` as the SHA-256 of *that file's
actual content*, not the embedded template's. The record cannot silently
masquerade as a default-prompt run.

Cross-run comparability itself stays a **convention**, exactly as it is today.
`stbench` does not, and after this change still does not, enforce that two runs
share a prompt identity — `prompt.id`/`version` are already freely settable with
no validation, and records simply *carry* the identity for analysis to honor. We
deliberately add no enforcement here: no forcing a distinct `id`, no refusing
`stbench-default@2` alongside a custom file, no gating in code. Adding
enforcement only on the custom-prompt path would invent a guarantee that exists
nowhere else in `stbench` and contradict its convention-based posture.

## Considered Options

- **Record the file's hash, add no enforcement** (chosen) — honest provenance
  with zero new machinery; comparability stays the analyst's job, consistent
  with how `id`/`version` already work.
- **Force a distinct `prompt.id` / refuse the default identity with a custom
  file** — rejected: new enforcement on one path only, inconsistent with the
  rest of the tool, and redundant once the hash is honest.
- **A warning banner declaring records non-comparable** — rejected as
  over-warning for the same reason; a quiet one-line notice suffices.
- **Config-file-relative path resolution** — rejected: every other stbench path
  (`source_dir`, lifecycle hooks) resolves against the working directory, and a
  one-field exception would surprise operators. `prompt.file` resolves against
  cwd like the rest.
- **Naive string substitution instead of `text/template`** — rejected: throws
  away a working engine and the shared `{Prompt, ComparisonView}` data for no
  gain.

## Consequences

- `prompt.file` is off by default; the embedded prompt remains canonical and
  `stbench init` does not scaffold the field.
- The flag `--prompt-file` overrides the YAML field, mirroring `--prompt-id` /
  `--prompt-version`.
- A custom file is loaded and validated **before the loop starts**: it must
  exist, parse as a Go `text/template`, and reference `.ComparisonView` (a
  prompt without the actionable view would run an expensive no-op loop). Any
  failure aborts the run with a clear error.
- When a custom prompt is active, `stbench` prints a one-line notice at run start
  and records `prompt.hash` as the hash of the file's content. With no override,
  behavior and output are byte-for-byte what they were before.
- Runs remain comparable only when their recorded prompt identity matches; this
  stays a convention enforced by analysis, not by `stbench`. See
  [ADR-0007](0007-benchmark-task-prompt-is-fixed-and-versioned.md).
