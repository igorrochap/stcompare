# Comparison emits an HTML scorecard

`stcompare campaign compare` writes a fourth artifact, `comparison.html`,
alongside `replay.har.json`, `comparison.json`, and `comparison.md`. The HTML is
a deliberately lossy at-a-glance scorecard: it foregrounds the metrics that are
otherwise buried in the exhaustive per-request detail of the Markdown and JSON
reports — fix rate, regression count, and the campaign identity. Later
scorecard tickets can enrich the same page with outcome breakdowns, traffic
classifications, and collapsible problem lists. It renders from the same
in-memory `report` struct as the other renderers, so it introduces no new
comparison logic.

## Considered Options

- **Opt-in behind a `--html`/`--format` flag** — rejected because no other
  output is format-selected today; the tool always writes every artifact, and a
  flag would be speculative machinery for a file users can simply ignore.
- **An interactive investigation console** (client-side filtering, drill-down
  into every interaction) — rejected for the first version because the Markdown
  and JSON reports already serve investigation well, and the unmet need is
  seeing the shape of a comparison at a glance, not another way to browse the
  full dataset.

## Consequences

- The HTML is intentionally incomplete: it is a summary view, and the Markdown
  and JSON reports remain the complete, auditable record.
- The file is self-contained with zero external requests — inlined CSS, no
  chart library, and no JavaScript — so it stays reproducible when committed,
  moved, opened offline, or emailed.
- It is rendered with `html/template` rather than the Markdown renderer's
  hand-rolled `strings.Builder` style, because response bodies, messages, and
  URLs captured from a live candidate API must be auto-escaped in an HTML
  context.
- Degraded states (baseline problems unavailable, zero-evaluable fix rate) are
  carried through as first-class "unavailable" states rather than misleading
  zeros, matching the Markdown report's honesty.
- The `compare` command prints the HTML path as an absolute `file://` URI so it
  is clickable from the terminal; the other three artifact lines stay as
  relative paths because they are not meant to be opened in a browser.
