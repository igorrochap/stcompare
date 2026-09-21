# The local adapter's tool contract closes false-success loops

The bundled local-model adapter is a faithful measurement scaffold under
[ADR-0011](0011-local-adapter-is-a-faithful-measurement-scaffold.md). A scaffold
defect let a model burn its whole turn budget on `read_file` calls that
"succeeded" without ever changing what the model saw. This ADR fixes the tool
contract so that an unproductive call is either rejected or bounded, and it
records two choices a future reader would otherwise mistake for bugs.

## Evidence

Five runs on the same D4R `mastodon#28381` checkout, two unrelated model
families, default adapter settings, all ending in `terminal_state=adapter_error`
with `edit_attempts=0` or a single wrong-line edit:

- `gpt-oss:120b-cloud`, run `run-20260921T141309.268577000Z-4ce043aace56864f22c3a41b`:
  turns 7–60 re-read `app/models/account.rb` (597 lines) with `max_bytes: 2000`
  and invented `line_start`/`line_end` climbing to 8800/9000. All 54 results
  were byte-identical (the first 2000 bytes of the file); the content of turn 8
  and turn 58 diff clean.
- `gpt-oss:120b-cloud`, run `run-20260921T152147.909462000Z-74a888fdeac684356513ee3d`:
  turns 31–60 re-read `app/controllers/api/base_controller.rb` (164 lines) with
  `line_start` 4600→6600, stuck at 6600 for the final 13 turns.
- `nemotron-3-ultra:cloud`, run `run-20260921T195419.951505000Z-801f14dff28bf7e8a88b6ab8`:
  turns 33–60 re-read `config/routes.rb` (22,475 bytes) with an invented
  `offset` climbing 100→11000, receiving the identical first 5000 bytes 28
  times.

The first bad call in every run happened before any History Elision, so the
elision policy of [ADR-0014](0014-assistant-messages-are-immutable-history-in-the-local-adapter.md)
is not the cause. Two scaffold gaps compound:

1. `execute_tool` selected known arguments by name and ignored the rest. The
   tool schema declares `additionalProperties: false`, and the schema is sent
   on every turn, but nothing enforced it. A hallucinated pagination argument
   therefore produced `ok: true` with unchanged content instead of an error.
2. The repeat-breaker (`STBENCH_ADAPTER_MAX_REPEATS`) counted only turns whose
   every call failed *and* whose `(tool, arguments)` matched the previous turn.
   A climbing offset changed the arguments every turn, and the calls succeeded,
   so the breaker could never fire.

## Decisions

**Unsupported Arguments fail the call.** Every tool call is validated against
the declared schema at dispatch. A call with a key outside the schema returns a
structured `unsupported_argument` error that names the offending keys and the
supported set. Lenient type coercion (`max_bytes: "2000"`) stays: it has never
stranded a model. This is the enabling change, not a trade-off; it makes the
schema the model already receives true.

**`read_file` pages by line while the cap stays in bytes.** The tool gains
`offset` (1-based line) and `limit` (line count) and reports `total_lines` and
`next_offset` (null at end of file). `max_bytes` remains a byte ceiling on the
returned payload with `truncated: true`. Lines were chosen over bytes for the
window because a byte window can cut a line in half, and `str_replace` works on
exact text: a model that copies `old_string` across a byte boundary gets a
`no match` error, which is itself a loop trigger. Line units also make end of
file legible to the model (`offset 8400` against `total_lines 597`) and match
the affordance of the Codex/Claude Code baseline's `Read` tool, which is the
ADR-0011 parity test. The byte cap keeps a single multi-megabyte line from
overrunning the context. Two jobs, two units.

**An Unproductive Repeat is defined by results, not arguments.** A turn's
identity for the repeat-breaker is the ordered list of `(tool name, result as
sent to the model)`. Arguments are excluded on purpose. Identical arguments
already yield identical results, so the old rule is subsumed; changing
arguments with an unchanging result is exactly the pathology the old rule
missed. Legitimate progress always changes a result: a page forward returns
different content, a second identical `str_replace` fails with `no match`, a
`list_files` after `write_file` lists the new path. `STBENCH_ADAPTER_MAX_REPEATS`
keeps its name and default of 4; its meaning becomes "consecutive Unproductive
Repeats before the adapter stops the iteration".

## Considered Options

- **Leave `read_file` unpaged and rely on rejection alone** — rejected. A model
  whose only reading strategy is windowed would be told "no" without an
  alternative, and the baseline agent can page any file trivially.
- **Byte-based `offset`** — rejected for the edit-boundary and legibility
  reasons above, despite matching `max_bytes`.
- **Key the repeat-breaker on `(tool, arguments, result)`** — rejected. It
  never fires on a climbing offset, which is the observed failure.
- **Window-of-N repeat detection (A, B, A, B)** — deferred. No run has shown
  it; a consecutive-turn comparison is enough for the observed data.
- **Ship the changes off by default** — rejected. The current default is the
  measurement error; an off-by-default fix keeps producing `adapter_error`
  records that read as model incapability.

## Consequences

- Local-model records from before and after this ADR are not comparable and
  must not be pooled. Runs that ended in `adapter_error` with no edit under the
  old contract are inconclusive, not negative capability results.
- The adapter reports its identity and version through the adapter response so
  that a record states which contract produced it.
- The adapter records an Adapter Stop event in the audit so that a researcher
  can tell a run the adapter cut short from a run the model ended.
- Turn-limit exhaustion still ends in `adapter_error`. Reclassifying it is a
  separate decision.
