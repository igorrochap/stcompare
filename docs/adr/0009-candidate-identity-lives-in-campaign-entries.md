# Candidate identity lives in campaign entries; the `stbench:` block is fixed infra

What a candidate *is* — its `agent`, `model`, `effort`, and `adapter` type — lives
in that candidate's `campaigns.<name>` entry. The `stbench:` block holds only
what is constant across every candidate on the same machine: `source_dir`,
`hardware`, `reuse_process`, the fixed `prompt` identity
([ADR-0007](0007-benchmark-task-prompt-is-fixed-and-versioned.md)), the
`lifecycle` hooks, and a new `adapters:` map from type name to adapter command.
Running a different candidate means selecting a different campaign
(`stbench run <candidate>`) or adding a campaign entry — nothing in the
`stbench:` block moves.

```yaml
stbench:
  source_dir: candidate-src
  hardware: "RTX 4090 / 64GB"     # one machine; declared once (TODO: auto-detect)
  reuse_process: true
  prompt: { id: ..., version: ... }
  lifecycle: { build: ..., start: ..., stop: ..., health_url: ... }
  adapters:                        # type -> command, defined once
    local:  python adapters/local_model_adapter.py
    remote: python adapters/anthropic_adapter.py

campaigns:
  baseline:
    kind: baseline                 # ground truth — carries no identity fields
  sonnet5-high:
    kind: candidate
    agent:  claude-code
    model:  sonnet-5
    effort: high                   # identity, not baked into the name
    adapter: remote                # references stbench.adapters.remote
  sonnet5-low:
    kind: candidate
    agent:  claude-code
    model:  sonnet-5
    effort: low
    adapter: remote
```

The candidate-selecting CLI flags (`--agent`, `--model`, `--adapter`,
`--hardware`) are **removed**. `stbench run <candidate>` reads identity from the
campaign entry; the surviving flags are execution-only knobs (`--reuse-process`,
`--emit-scorecard`, the various timeouts, `--max-iterations`, `--stall-window`).
The `record_path` field is **removed** and the record path is derived
deterministically from the campaign name (`reports/<candidate>/…`), the same
`config.CampaignReportDir` join stcompare already uses for comparison artifacts
([ADR-0008](0008-scorecard-joins-comparison-and-record-via-decoupled-package.md)).

## Why

The `stbench:` block previously duplicated candidate identity that could *also*
be passed as CLI flags, so the same fact had two sources and an unstated
precedence between them. Worse, `record_path` was a hand-edited field: run a
`kimi-k3` campaign while the field still read `sonnet5.json` and the record was
silently written under the wrong name, quietly corrupting a study whose entire
purpose is trustworthy, comparable records. Deriving the path from the campaign
that actually ran makes that mislabeling *structurally impossible* rather than a
discipline the operator has to maintain.

Moving identity next to the campaign it describes gives one source of truth and
keeps the fairness posture of the neutral harness
([ADR-0004](0004-fix-loop-lives-outside-stcompare.md)): every axis that makes two
runs comparable-or-not — agent, model, effort, hardware, prompt identity — is
captured as structured, recorded config, not as an ephemeral flag an operator
might forget. `effort` is treated as identity, not a run-time knob: `sonnet5-high`
and `sonnet5-low` are distinct candidates so the scorecard can compare them as
first-class rows.

## Considered Options

- **Interactive picker after `stbench run`** — rejected. one of the possible targets are local models in overnight, scripted, unattended runs; a prompt that
  blocks on a human choosing an agent is hostile to that primary use. The
  positional `stbench run <candidate>` already selects a candidate
  non-interactively and reproducibly.
- **Keep the identity flags as overrides "for convenience"** — rejected. It
  rebuilds the exact two-sources-of-truth ambiguity this decision exists to
  remove, now with a precedence rule nobody remembers.
- **Keep `record_path` as a manual field** — rejected. It is the direct cause of
  the mislabeled-record failure above; a derived path removes the failure mode
  entirely.
- **Auto-detect `hardware` now** — deferred, not rejected. Go's standard library
  resolves CPU/arch/OS/RAM deterministically but has no GPU detection; the
  accelerator that dominates local-model timing requires shelling out to
  `nvidia-smi` or parsing PCI IDs, which is fragile across multi-GPU hosts,
  missing drivers, and containers. For a single-machine study `hardware` is
  declared once in `stbench:`; auto-detection is left as a later decision.
- **Adapter path per candidate** — rejected in favor of a `stbench.adapters` map
  keyed by type. A candidate names a *type* (`adapter: local`); the command lives
  once in the map. Candidates that share an adapter no longer repeat its path.

## Consequences

- The config schema is reshaped, not migrated: there are no existing users, so
  the deprecated-alias path used for the earlier `candidate`→`campaign` rename is
  not repeated here.
- Load-time validation becomes strict and fails fast: a candidate's `adapter`
  must resolve to a key in `stbench.adapters`; a `kind: candidate` must carry the
  identity fields; a `kind: baseline` must not.
- Both scaffolders must emit the new shape. `stcompare config init`
  (`config.Default` / `WriteDefault`) must produce campaign entries that carry
  identity and an `stbench.adapters` map. `stbench init` must additionally
  **write its block into `stcompare.yaml`** rather than only creating the
  lifecycle scripts and printing a stanza to stdout, and that block must reflect
  this schema (no `record_path`, `adapters` map, `hardware` in `stbench:`).
- Known limitation: with `hardware` declared once in `stbench:`, a `remote`
  (hosted-API) candidate inherits the harness machine's label even though
  inference did not run there. This is a non-issue for the local-model study but
  must be revisited if remote candidates are ever compared on cost or latency.
