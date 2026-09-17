# Assistant messages are immutable history in the local adapter

The bundled local-model adapter is a faithful measurement scaffold under
[ADR-0011](0011-local-adapter-is-a-faithful-measurement-scaffold.md). Its
History Elision policy must not rewrite information produced by the model into
content that the model can imitate on a later turn.

## Decision

The adapter keeps every assistant message exactly as returned by the model for
the whole run. This includes `str_replace` `old_string` and `new_string`
arguments, `write_file` `content` arguments, and any content echoed in the
assistant message.

History Elision remains enabled by default, but applies only to successful
`read_file` results from turns older than the current turn. It replaces the
result's `content` field with `[read_file content elided from history]` and
preserves the result's other fields. A `read_file` result from the current turn
keeps its full content.

`STBENCH_ADAPTER_NO_COMPACT=1` keeps every `read_file` result verbatim. Its
meaning is limited to the `read_file` result policy; assistant messages are
always immutable history.

The audit's per-turn `input` continues to capture the exact model input after
History Elision, as required by
[ADR-0012](0012-benchmark-audit-capture-is-required-research-evidence.md).

## Evidence

In run `run-20260917T154126.496078000Z-9d7c59fc02b231cc8c4ebc13`, using
`nemotron-3-super:cloud` on `bibliothek` with default settings, the model's
reasoning at turn 21 contained the correct `old_string`, and the file content
was present verbatim in its input. The emitted `str_replace` call instead used
`[edit content elided from history]` for both `old_string` and `new_string`,
because two earlier successful edit calls had been rewritten that way in the
assistant history. Every edit from turn 21 through turn 40 failed; the audit
recorded 176 calls with the marker arguments, and the run ended in
`adapter_error`.

The same campaign with `STBENCH_ADAPTER_NO_COMPACT=1` converged in two
iterations with zero failed tool calls. The model re-read the information it
needed, showing that removing `read_file` payloads was not the harmful part of
the policy.

## Considered Options

- **Rewrite assistant edit arguments with a placeholder** — rejected. The
  model can copy the marker into its next tool call, as the audit evidence
  demonstrates.
- **Truncate or summarise the argument** — rejected. Any rewritten argument is
  still model-visible and imitable, and neither form preserves the exact action
  the model returned.
- **Use budget-triggered History Elision** — rejected. It introduces a second,
  changing policy boundary and still rewrites assistant actions when a request
  grows large. No budget-triggered or keep-last-N variant is part of this
  scaffold.

## Consequences

- Assistant tool-call arguments and echoed content remain available verbatim in
  every later model input, so the model sees its own actions as immutable
  history.
- Default History Elision still bounds older `read_file` payloads by replacing
  only their content field. The current turn remains complete, and the opt-out
  environment variable preserves all `read_file` content.
- Full assistant arguments, including complete created-file bodies, can increase
  request size over a long run. That cost is accepted to keep the scaffold from
  biasing the model through rewritten actions.
- Audit consumers can reconstruct the exact post-History-Elision input for each
  turn, including the distinction between immutable assistant messages and
  elided older `read_file` results.
