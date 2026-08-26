# The bundled local-model adapter is a faithful measurement scaffold

[ADR-0004](0004-fix-loop-lives-outside-stcompare.md) fixed the load-bearing
principle: the benchmark measures *the agent*, not the agent-plus-its-harness,
and the bundled `examples/stbench/local_model_adapter.py` is the "minimal
scaffold" that lets a locally-run open-source model drive the loop over the CLI
contract. This ADR is about keeping that scaffold *honest*: three defects in it
were biasing outcomes for reasons that have nothing to do with the model's
ability, so the benchmark was partly measuring the scaffold. We remove them.

The evidence that forced this: a 7B local model (`qwen2.5-coder`) driven through
the adapter produced no useful edit and stalled — it hallucinated tool names,
ran "verification" commands the task explicitly forbids, and truncated a
controller to a broken stub. The *same model*, handed the *same* single change
as a concrete, self-contained request, produced a correct, complete edit. The
gap was the scaffold, not (only) the model. Three decisions close it.

## 1. Sampling is explicit and deterministic by default — and separate from `effort`

The adapter sends no sampling parameters, so it inherits the inference server's
default temperature (~0.7). A small model driving an autonomous tool loop is
erratic there; at greedy decoding it is steady. We add an explicit
`temperature` knob (a campaign-level config field plus a `--temperature`
adapter flag) and **default it to 0** for reproducibility.

Temperature is deliberately **not** sourced from the campaign's `effort` field.
`effort` denotes *reasoning/agentic effort* — that is how the baseline uses it
(`luna-high`) — and temperature is an orthogonal *decoding* parameter.
Overloading `effort` to carry temperature would break its shared meaning across
adapters and make records uninterpretable ("was `effort: high` more reasoning or
hotter sampling?"). The two stay independent knobs.

## 2. Edits are targeted, not whole-file rewrites

The adapter's only edit primitive is `write_file`, which replaces an entire
file. Real coding agents — including the Codex-based adapter the baseline runs
on — edit by *patch*. Whole-file rewrite is the wrong primitive here for three
reasons: it forces the model to re-emit ~100 correct lines to change three (the
observed truncation and incidental damage); it inflates every turn's transcript
with full file bodies, so context balloons and a request eventually exceeds the
adapter timeout mid-run; and it hands the local model a strictly weaker editing
affordance than the baseline, so the comparison measures scaffold, not model.

The adapter gains an exact-match `str_replace`-style edit tool (`old_string` →
`new_string`, unique-match, with a clear error the model can recover from on a
missing or ambiguous match). `write_file` remains only for creating new files.
This mirrors the baseline's editing affordance and keeps each turn's payload
small.

## 3. The system prompt is task-neutral

The prompt must describe the agent's identity, the tools it has, and the basic
"make your edits and stop" loop — nothing more. It must not coach *methodology*
("map each failing operation to its file", "fix every affected controller") and
must never carry *domain hints* about the fix. The stcompare report delivered in
the user turn is the sole source of what to do. This is the same fairness
guarantee as [ADR-0010](0010-prompt-file-override-for-ablation.md): the moment
the scaffold's prompt starts helping, the benchmark drifts into measuring
prompt-engineering instead of the model. Task-specific instruction, when a
researcher wants it, belongs in the versioned, hash-recorded `prompt.file`
override — never smuggled into the adapter envelope (see
[ADR-0007](0007-benchmark-task-prompt-is-fixed-and-versioned.md)).

## Considered Options

- **Reuse `effort` as the temperature source** — rejected. It conflates two
  orthogonal concepts and destroys the field's cross-adapter meaning; a
  dedicated knob costs almost nothing.
- **Keep whole-file `write_file` as the only editor** — rejected. Error surface,
  transcript/context blow-up leading to timeouts, and an unfair handicap versus
  the patch-based baseline.
- **Unified-diff as the patch format** — rejected as the primitive. A small
  model emits an exact scoped `old_string`/`new_string` pair far more reliably
  than a well-formed hunk with correct line numbers; diff parsing would add its
  own failure mode. `str_replace` is the robust middle ground.
- **A more helpful, coaching system prompt** — rejected. It measurably improves
  a weak model precisely by doing the model's job for it, which is the outcome
  we are trying *not* to contaminate.

## Consequences

- New defaults: `temperature: 0`; `str_replace` is the primary editor and
  `write_file` is for new files; the canonical embedded prompt is task-neutral.
- These changes alter measured outcomes, so **local-model records from before
  and after this ADR are not comparable** and must not be pooled. The effective
  sampling temperature and the adapter's editing affordance are part of a run's
  provenance; records should make the temperature recoverable so a reader can
  tell which regime a run belongs to.
- The `str_replace` tool needs a precise failure contract (no match / multiple
  matches → a structured error), or a small model will loop trying to guess why
  an edit "did nothing."
- Reliable tool-call handling for servers that emit calls as text rather than
  structured `tool_calls` is assumed by this scaffold; it is a precondition for
  any of the above to take effect, not a change introduced here.
- This ADR governs the bundled `local_model_adapter.py` only. The Codex-based
  `coding_agent_adapter.py` already edits by patch and manages its own sampling;
  it is unaffected.
