# Single-module, two-binary layout; boundary by communication, not by a wall

The evaluator (`stcompare`) and the benchmark orchestrator (`stbench`) live as
two thin `main`s in **one** Go module. The evaluator/orchestrator boundary from
[ADR-0004](0004-fix-loop-lives-outside-stcompare.md) is enforced by *how the
runner communicates* — it `os/exec`s the `stcompare` binary and reads stdout +
exit code, exactly as an external agent would — and by clear package
organization, **not** by a compiler-level wall.

## Layout

```
stcompare/                 (one module, go.mod at root)
├── agentreport/           public, zero-dependency: compact agent-JSON structs,
│                          schema version, exit-code constants
├── benchrecord/           public, zero-dependency: benchmark-record schema
├── cmd/
│   ├── stcompare/main.go  thin
│   └── stbench/main.go    thin
└── internal/
    ├── comparison/        evaluator (unchanged from today's layout)
    ├── config/
    ├── cli/
    └── bench/             runner: orchestration, adapter exec, timing, record
```

- The runner (`internal/bench`) imports the public contract packages and
  `os/exec`; it does **not** import `internal/comparison`.
- The wire format is shared as pure types: the evaluator imports
  `agentreport`/`benchrecord` to *produce* JSON, the runner imports them to
  *parse* it, and external non-Go adapters use the documented JSON schema.
  Sharing a versioned wire-format type is not logic coupling — communication is
  still subprocess + stdout.

## Considered Options

- **Separate module for the runner** (nested `go.mod` + `go.work`) — the
  canonical Go hard wall, and a separate module genuinely *cannot* import
  another's `internal/`. Rejected as premature: for a two-binary internal tool
  it buys a compile-time guarantee against a mistake a reviewer catches in
  seconds, at the cost of permanent multi-module tooling friction
  (cross-module `go test`, workspace files, release complexity).
- **Relocate the evaluator's guts under `cmd/stcompare/internal/`** so a sibling
  binary cannot import them (the `cmd/go/internal/...` pattern) — rejected: it
  is compile-enforced but uncommon for *mutual* exclusion between first-party
  binaries, and it forces churn on tested, working evaluator code for a wall we
  do not yet need.
- **Root `internal/` + a depguard lint rule from day one** — rejected as the
  *foundation* only because there is no CI yet, so the rule would enforce
  nothing until CI exists. Adopted instead as a cheap later *ratchet* (see
  Consequences).

## Consequences

- Zero churn to the existing evaluator packages; no multi-module tooling tax.
- The boundary that matters is behavioural: the runner links none of the
  evaluator's logic, so it dogfoods the same public CLI contract external
  adapters depend on.
- Codebase tidiness comes from clear package responsibilities and thin `main`s,
  not from a structural wall. `internal/bench/*` vs `internal/comparison/*` is a
  self-evident split.
- Enforcement is a ratchet applied when free: once CI is built (an explicit
  separate follow-up), add a depguard rule forbidding `internal/bench` from
  importing `internal/comparison`. Escalate to a separate module only if the
  runner ever ships independently or grows a second consumer.
