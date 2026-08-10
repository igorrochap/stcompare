# Scorecard joins comparison output and benchmark record via a decoupled package

`stcompare scorecard build --comparison --record --out` and `stbench run
--emit-scorecard` together surface a benchmark record's cost information
(time to fix, tokens spent) on the same at-a-glance page as the traffic
comparison scorecard ([ADR-0003](0003-comparison-emits-html-scorecard.md)),
without teaching either half of the codebase about the other's data model.

`internal/scorecard` is a new package that parses a `comparison.json` and a
`benchmark-record.json` and renders their join as `scorecard.html`. It reuses
`comparison.RenderHTML` to produce the traffic sections — fix rate, regression
count, baseline problem breakdown, traffic classifications, problem lists —
and inserts a new "Benchmark Run" section ahead of them (agent/model identity,
total and agent-fix time, iteration count, token usage, with an explicit "not
reported" state when the record's `Tokens` is nil). `scorecard.html` is
therefore a strict superset of `comparison.html`; `comparison.html` itself is
unmodified and remains the traffic-only artifact.

`internal/comparison` gains no new fields, imports, or awareness of
`benchrecord` as a result of this feature. `internal/bench` and `benchrecord`
gain no awareness of `internal/comparison`'s report shape either — `stbench
run --emit-scorecard` invokes `stcompare scorecard build` as a subprocess
after writing its own record, the same process-boundary pattern it already
uses to invoke `campaign compare` for every iteration
([ADR-0004](0004-fix-loop-lives-outside-stcompare.md)). It derives the
comparison artifact's path with the existing `config.CampaignReportDir`
helper — the same join stcompare's own CLI uses — rather than a second,
driftable copy of that logic.

## Considered Options

- **Add `TimeMS`/`Tokens` fields directly to `internal/comparison`'s `Report`
  struct** — rejected. It would make the core traffic-comparison scorecard
  depend on stbench's record shape, contradicting ADR-0003's framing of that
  page as a pure traffic summary.
- **A merge command living inside stbench that imports `internal/comparison`'s
  types directly** — rejected for the same coupling reason, just pointed in
  the other direction: whichever package imports the other's internals ties
  the fix-loop-agnostic evaluator (ADR-0004) to a specific downstream
  reporting concern.
- **Reimplement the traffic section from raw `comparison.json` inside
  `internal/scorecard` instead of reusing `comparison.RenderHTML`** —
  rejected. It would mean maintaining two HTML templates for the same
  fix-rate/regression data, which will drift as `internal/comparison`'s
  renderer evolves.
- **Make `--record` optional on `scorecard build`, degrading to
  comparison-only output** — rejected. A record-less scorecard would just be a
  redundant copy of `comparison.html` under a different name; `campaign
  compare` already produces that artifact.

## Consequences

- `internal/scorecard` is the only package in the module that knows about
  both `comparison.Report` and `benchrecord.Record`. It depends
  one-directionally on `internal/comparison` (via the exported
  `comparison.RenderHTML`); `internal/comparison` has no reciprocal
  dependency. Future changes must not have `internal/comparison` import
  `benchrecord` or `internal/scorecard`, and must not have `internal/bench`
  import `internal/comparison` directly — the subprocess boundary between
  `stbench` and the `stcompare` binary is deliberate, not missing wiring to be
  "cleaned up."
- `stbench run --emit-scorecard` failing to produce a scorecard (for example,
  because no `comparison.json` exists yet) only logs a warning on stderr; it
  never turns an otherwise-successful benchmark run into a failed one, and the
  run's exit code is unaffected.
- Output is HTML only for now. A `scorecard.json` or `scorecard.md` would need
  their own decision if a machine-readable joined artifact is ever needed
  elsewhere; nothing here precludes adding one later since the join logic and
  the rendering are already separated (`scorecard.Build` parses and writes,
  `scorecard.Render` produces the HTML string).
