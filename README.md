# stcompare

`stcompare` runs repeatable Schemathesis campaigns and compares a candidate API
with a recorded baseline. Schemas, targets, deterministic generation settings,
reports, and named campaigns live in a version-controlled YAML file, making each
comparison reproducible and auditable.

The repository also includes `stbench`, an optional runner that repeatedly asks
a coding agent to address comparison findings until the candidate converges or
the run reaches a stopping condition.

## Table of contents

- [How it works](#how-it-works)
- [Quick start](#quick-start)
- [Installation](#installation)
- [Usage guide](#usage-guide)
  - [1. Initialize the configuration](#1-initialize-the-configuration)
  - [2. Configure the campaigns](#2-configure-the-campaigns)
  - [3. Inspect the effective configuration](#3-inspect-the-effective-configuration)
  - [4. Preview a campaign command](#4-preview-a-campaign-command)
  - [5. Record the baseline](#5-record-the-baseline)
  - [6. Compare a candidate](#6-compare-a-candidate)
- [Configuration reference](#configuration-reference)
- [Comparison behavior and reports](#comparison-behavior-and-reports)
- [Benchmark loops with stbench](#benchmark-loops-with-stbench)
- [Build a benchmark scorecard](#build-a-benchmark-scorecard)
- [Caveats and troubleshooting](#caveats-and-troubleshooting)
- [Development](#development)

## How it works

```text
baseline API + OpenAPI schema
        |
        | stcompare campaign run baseline
        v
Schemathesis reports + frozen schema + HAR transcript
        |
        | start the candidate API
        | stcompare campaign compare <candidate>
        v
replayed responses + JSON/Markdown/HTML comparison reports
```

A campaign name is an artifact identity, not a process definition. You are
responsible for starting the baseline or candidate API at `base_url` before the
corresponding command runs. `stcompare` records the baseline with Schemathesis,
then replays its ordered HAR requests against the candidate and grades the
result using the recorded problem evidence.

## Quick start

This example assumes the baseline API is already running at
`http://localhost:8080` and exposes a local `openapi.json` schema.

```sh
# Build and initialize.
go build -o stcompare ./cmd/stcompare
./stcompare config init

# Check the generated settings and the underlying Schemathesis command.
./stcompare config show
./stcompare campaign command baseline

# Record the running baseline API.
./stcompare campaign run baseline

# Stop the baseline, start the candidate on the same URL, then compare it.
./stcompare campaign compare gpt5.6
```

Review `reports/gpt5.6/comparison.html` for the scorecard,
`comparison.md` for a portable review, or `comparison.json` for automation.
A comparison that found actionable failures still writes all reports and exits
with status `2`; this is a comparison result, not a tool failure.

## Installation

### Requirements

- Schemathesis available as `st`, or `uvx` available so `stcompare` can fall
  back to `uvx schemathesis` when running a campaign.
- A reachable baseline or candidate API for commands that generate or replay
  traffic.

Install the latest prebuilt `stcompare` and `stbench` binaries on Linux or
macOS without installing Go:

```sh
curl -fsSL https://raw.githubusercontent.com/igorrochap/stcompare/main/scripts/install.sh | sh
```

The installer verifies the release checksum and writes both binaries to
`~/.local/bin` by default. Select another directory or a specific release with:

```sh
curl -fsSL https://raw.githubusercontent.com/igorrochap/stcompare/main/scripts/install.sh \
  | sh -s -- --dir /usr/local/bin --version v1.2.3
```

Make sure the selected directory is on `PATH`. The examples below assume the
binary is available as `stcompare`.

To build from source instead, install Go 1.25 or later and run from the
repository root:

```sh
go build -o stcompare ./cmd/stcompare
go build -o stbench ./cmd/stbench
```

Commands can also be run without keeping a binary:

```sh
go run ./cmd/stcompare --help
```

## Usage guide

### 1. Initialize the configuration

By default, `stcompare` reads and writes `stcompare.yaml` in the current working
directory. Use the global `--config` flag to select another path.

Initialize the default configuration:

```sh
stcompare config init
```

Use `--force` only when the existing configuration should be overwritten:

```sh
stcompare config init --force
```

Initialize a configuration at an explicit path:

```sh
stcompare --config config/benchmark.yaml config init
```

Initialization refuses to replace an existing file. `--force` overwrites it, so
use that option only when losing the current configuration is intentional.

### 2. Configure the campaigns

Edit `stcompare.yaml` so `schema` identifies the schema used to generate the
baseline, `base_url` points at the currently running API, and the `campaigns`
mapping contains exactly one baseline plus one or more candidates. The generated
file is a complete starting point:

```yaml
schema: openapi.json
# Optional: a candidate-owned spec URL or workspace path used during compare.
# candidate_spec: /openapi.json
base_url: http://localhost:8080
reports_dir: reports
schemathesis:
  seed: 12345
  workers: 1
  generation_deterministic: true
  generation_database: none
  reports:
    - junit
    - vcr
    - har
    - ndjson
  output_sanitize: false
  output_truncate: false
  extra_args: []
comparison:
  missing_resource_statuses:
    - 404
    - 410
  precondition_heuristics: []
  normalization:
    default_rules: true
    body_fields: []
    headers: []
campaigns:
  baseline:
    kind: baseline
  gpt5.6:
    kind: candidate
    agent: codex
    model: gpt-5.6
    effort: high
    adapter: coding-agent
stbench:
  hardware: harness-machine
  adapters:
    coding-agent: python examples/stbench/coding_agent_adapter.py
  adapter_timeout: 30m
  reuse_process: false
  source_dir: .
  stcompare_binary: stcompare
  prompt:
    id: stbench-default
    version: "2"
  lifecycle:
    stop: .local/stbench/stop.sh
    reset: .local/stbench/reset.sh
    build: .local/stbench/build.sh
    start: .local/stbench/start.sh
    command_timeout: 30m
    health_url: http://localhost:8080/health
    health_timeout: 30s
    health_interval: 100ms
  max_iterations: 100
  stall_window: 2
```

Replace the sample candidate names with stable identifiers for the
implementations being compared. Names may contain letters, numbers, dots,
underscores, and hyphens. Candidate Campaign identity consists of `agent`,
`model`, `effort`, and an `adapter` type that resolves through
`stbench.adapters`; a Baseline Campaign has none of those fields. The
`stcompare config init` command scaffolds this complete shape, including the
candidate identity and `stbench:` settings shown above.

If the candidate publishes its own OpenAPI document, set `candidate_spec` to an
HTTP(S) URL, an absolute workspace path, or an endpoint path such as
`/openapi.json`. Candidate contract evidence is optional, but improves
status-code grading.

### 3. Inspect the effective configuration

Load, validate, and print the settings that a command will use:

```sh
stcompare config show
```

Configuration flags are temporary overrides; they do not modify the YAML file:

```sh
stcompare --config config/benchmark.yaml config show \
  --schema api/openapi.yaml \
  --candidate-spec /openapi.json \
  --base-url http://localhost:9090 \
  --reports-dir comparison-reports \
  --seed 4242 \
  --workers 1
```

The same overrides are available to campaign commands. Validation happens
after the overrides are applied.

### 4. Preview a campaign command

Inspect the exact Schemathesis invocation without generating traffic:

```sh
stcompare campaign command baseline
```

With the default configuration, this prints a command equivalent to:

```sh
st run openapi.json \
  --url http://localhost:8080 \
  --workers 1 \
  --seed 12345 \
  --generation-deterministic \
  --report junit,vcr,har,ndjson \
  --report-junit-path reports/baseline/junit.xml \
  --report-vcr-path reports/baseline/campaign.vcr.yaml \
  --report-har-path reports/baseline/campaign.har.json \
  --report-ndjson-path reports/baseline/campaign.ndjson \
  --output-sanitize false \
  --output-truncate false
```

Arguments in `schemathesis.extra_args` are appended. Report formats and output
paths belong to `stcompare`, so extra arguments cannot override `--report` or
any `--report-*-path` option.

### 5. Record the baseline

Start the baseline API at `base_url`, then run:

```sh
stcompare campaign run baseline
```

On completion, the baseline directory contains:

```text
reports/baseline/
  campaign.har.json
  campaign.ndjson
  campaign.vcr.yaml
  junit.xml
  metadata.yaml
  schema.snapshot
```

Existing campaign directories are protected. To deliberately replace a
baseline, pass `--force`:

```sh
stcompare campaign run baseline --force
```

By default, artifacts from an aborted Schemathesis run are removed. Preserve
partial output for debugging with `--keep-failed`; it must not be treated as a
completed campaign:

```sh
stcompare campaign run baseline --keep-failed
```

`stcompare` executes `st` directly when available and otherwise tries
`uvx schemathesis`. Shell aliases are not visible to it. To select an explicit
command:

```sh
STCOMPARE_SCHEMATHESIS_COMMAND="uvx schemathesis" \
  stcompare campaign run baseline
```

### 6. Compare a candidate

Stop the baseline, start the candidate API at the configured `base_url`, and
replay the baseline transcript:

```sh
stcompare campaign compare gpt5.6
```

To compare against a temporary target without editing the file:

```sh
stcompare campaign compare gpt5.6 --base-url http://localhost:9090
```

The candidate directory receives the raw replay and three report formats:

```text
reports/gpt5.6/
  replay.har.json
  comparison.json
  comparison.md
  comparison.html
```

For agent or CI integrations, request the compact machine-readable view on
standard output:

```sh
stcompare campaign compare gpt5.6 --format agent
```

Comparison exit statuses are part of the public automation contract:

| Status | Meaning |
| ---: | --- |
| `0` | Converged: no actionable remaining failure or regression. |
| `1` | Tool or setup error, such as invalid configuration or malformed input. |
| `2` | Valid comparison completed, but the candidate did not converge. |

## Configuration reference

### Baseline and candidate contracts

When a baseline campaign runs successfully, `stcompare` stores the exact
generation schema as `reports/<baseline>/schema.snapshot`. Comparisons never
use the mutable top-level `schema` as the candidate contract. Set the optional
`candidate_spec` to an HTTP(S) URL, an absolute workspace file, or an endpoint
path such as `/openapi.json`; when it is omitted, status-code grading uses the
candidate's observed behavior only. If an explicitly configured candidate spec
cannot be fetched or parsed, the comparison reports an inconclusive contract
limitation rather than falling back to the shared generation schema.
Baseline problem identities remain pinned to the VCR/NDJSON/JUnit artifacts
produced during that baseline run; the snapshot is loaded and recorded in
comparison provenance for the frozen baseline contract and is never used as
the candidate contract.

### Validation rules

Omitted Schemathesis and comparison settings inherit the generated defaults.
Required values are validated after command-line overrides are applied. The
effective configuration must have:

- A non-empty schema location.
- An absolute HTTP or HTTPS base URL.
- A non-empty reports directory.
- At least one Schemathesis worker.
- Only `401`, `403`, `404`, or `410` entries in
  `comparison.missing_resource_statuses`. An explicit empty list is valid and
  disables generated-resource precondition-loss classification.
- A unique, non-empty `name`, a non-empty `method`, and a non-empty valid
  regular expression in `path_pattern` for every precondition heuristic.
  Methods are accepted in any case because compare-time matching is
  case-insensitive.
- A non-empty `name` and `field_name` for every
  `comparison.normalization.body_fields` rule.
- A non-empty `name` and `header_name` for every
  `comparison.normalization.headers` rule. Header names match
  case-insensitively.
- At least one campaign.
- A `baseline` or `candidate` kind for every campaign.

There must be exactly one baseline campaign. A campaign name must be a safe path
segment made from letters, numbers, dots, underscores, or hyphens.

`metadata.yaml` records the effective command, `stcompare` and Schemathesis
versions, timestamp, configuration path, campaign identity, effective settings,
and command-line overrides.

## Comparison behavior and reports

### Replay behavior

Replay the baseline HAR transcript against a candidate API:

```sh
stcompare campaign compare gpt5.6
```

The command discovers the single campaign whose kind is `baseline` and reads
its transcript from:

```text
<reports_dir>/<baseline-campaign>/campaign.har.json
```

Requests are replayed sequentially in HAR entry order against the effective
`base_url`. Replay rewrites only the target base URL. The original request path,
query string, and percent encoding are preserved. Semantic request headers are
copied, while stale transport-managed headers such as `Host`, `Content-Length`,
`Transfer-Encoding`, `Connection`, and `Accept-Encoding` are dropped or
recomputed. Plain HAR `postData.text` request bodies are sent as recorded.
Unsupported `postData.encoding` values fail during baseline setup before any
candidate request is sent or response-log path is created.

### Generated-resource precondition loss

Comparison heuristics identify replays that may not have reached the baseline
behavior because candidate-side state created during the original campaign is
missing. They affect only `campaign compare` classification and reporting; they
do not change Schemathesis command generation, campaign execution, or replayed
requests.

For example, a candidate may return `404` when replaying a request for a widget
whose ID was generated during the baseline campaign:

```yaml
comparison:
  missing_resource_statuses:
    - 403
    - 404
    - 410
  precondition_heuristics:
    - name: generated-widget
      method: GET
      path_pattern: ^/widgets/[0-9a-f]+$
```

The default missing-resource statuses are `404` and `410`. Add `401` or `403`
when an API uses those statuses to represent missing generated state. A supplied
list replaces the defaults, and `[]` disables this classification.

A heuristic matches only when the recorded baseline response is `2xx`, the
candidate returns a configured missing-resource status, and the method and path
match. Method matching is case-insensitive. `path_pattern` is evaluated against
the decoded URL path only, excluding the host and query. Heuristics are
evaluated in configuration order, and the first match is recorded.

Stronger reproduction evidence takes precedence over precondition heuristics. A
candidate `5xx` response is handled first for every problem category and is
never classified as precondition loss. For a correlated Schemathesis
server-error problem, candidate `5xx` remains `still_failing`.

### Dynamic response normalization

Response normalization is used when replay compares baseline and candidate
response bodies for exercise evidence. It produces derived responses plus
deterministic disclosure of configured rules; it does not mutate HAR, replay
logs, or reproduction evidence.

Default rules are enabled by `comparison.normalization.default_rules: true`.
They mask JSON body fields named `id`, `uuid`, `created_at`, `updated_at`, and
`timestamp`, and the response `Date` header, using stable placeholders such as
`<normalized:generated-id>` and `<normalized:timestamp>`.

Add body-field or header rules for API-specific dynamic values:

```yaml
comparison:
  normalization:
    default_rules: true
    body_fields:
      - name: request-id
        field_name: request_id
    headers:
      - name: server-version
        header_name: server
```

JSON bodies are normalized field-wise, including matching fields nested in
objects or arrays. Empty bodies and absent bodies remain distinct. Bodies that
do not parse as JSON are left unchanged and disclosed as unparseable rather
than treated as equivalent opaque text.

### Report contents

Candidate responses and comparison reports are written under the candidate
campaign directory:

```text
reports/gpt5.6/replay.har.json
reports/gpt5.6/comparison.json
reports/gpt5.6/comparison.md
reports/gpt5.6/comparison.html
```

`comparison.json` uses a versioned, stable schema for automation, while
`comparison.md` and `comparison.html` present the same evidence for review.
Schema version 5 adds
exercise-evidence-backed fixed outcomes for server-error problems and records
normalization policy in the comparison provenance while keeping Schemathesis
problem outcomes separate from replay traffic classifications. The reports
include:

- Total replayed interactions.
- The effective ordered comparison policy. JSON records it in the top-level
  `comparison` object, including response normalization settings, and Markdown
  renders the same statuses, heuristics, and normalization rules in a
  `Comparison policy` section.
- Individual baseline Schemathesis problems with their check name, message,
  evidence source, recorded case ID, reproduction context, and correlated
  interaction number when available.
- Problems classified through a precondition heuristic are `inconclusive` with
  `outcome_reason: "generated_resource_precondition_loss"` and
  `matched_precondition_heuristic: "<name>"`. Markdown shows the same reason and
  matched heuristic.
- Correlated server-error problems are `fixed` only when replay records explicit
  exercise evidence: operation/path identity, semantic response-body agreement
  after configured normalization, and no matched precondition-loss heuristic.
  No single signal is sufficient on its own; all three signals are jointly
  necessary and sufficient for replay to classify the problem as fixed.
  When candidate or baseline response bodies are unavailable, or semantic
  agreement is missing, the problem remains `inconclusive` with
  `outcome_reason: "exercise_evidence_missing"`. Markdown and JSON record the
  available `exercise_evidence` signals next to the outcome.
- A baseline problem aggregate count from JUnit `failure`/`error` elements
  when that artifact is present, plus a separate extracted structured-problem
  count when structured evidence is available. Structured VCR and NDJSON
  evidence likewise treat Schemathesis `failure` and `error` checks as
  problems, while `success`, `skip`, and `interrupted` remain recognized
  non-problem statuses. The two count values remain visible independently so
  extraction discrepancies are auditable.
- Problem outcome totals for extracted baseline Schemathesis problems:
  `fixed`, `still_failing`, and `inconclusive`, plus separate total,
  `evaluable`, `unevaluable`, `uncorrelated`, and `ambiguous` counts. Every
  extracted problem falls into exactly one top-level bucket: evaluable,
  unevaluable, uncorrelated, or ambiguous. The outcome totals always sum to the
  evaluable count, and the top-level buckets always sum to total. A problem is
  `evaluable` when it is correlated to a replay interaction and the comparison
  has evidence for an outcome. Correlated problems whose check category is not
  yet supported are `unevaluable`, not inconclusive. Check-specific evaluation
  covers the Schemathesis server-error, negative-data-rejection,
  positive-data-acceptance, response-schema-conformance, and
  status-code-conformance checks; only uncategorized checks remain
  `unevaluable`. Generated-resource
  precondition-loss evidence can make a correlated problem of any supported
  check category evaluable and inconclusive. A correlated `not_a_server_error`
  problem remains `still_failing` when replay also returns a 5xx response. A
  replayed non-server-error problem on a replayed 5xx response remains outside
  the evaluable denominator. A replayed non-5xx response is `fixed` only when
  the recorded exercise evidence shows replay reached the relevant behavior;
  absence of contrary evidence remains `inconclusive`.
- The baseline-problem `fix_rate` is computed as fixed problems divided by
  evaluable baseline problems. JSON states the denominator count and basis
  (`evaluable_baseline_problems`) alongside the percentage; Markdown prints the
  same numerator and denominator next to the rate. When there are zero evaluable
  problems, the report emits an unavailable rate instead of `0%`, so this is
  distinct from a genuine `0/N` result. The rate counts Schemathesis problems,
  not distinct underlying defects; it measures only the evaluable subset, not
  all baseline problems; and it is comparable across candidates only when they
  share a baseline campaign and report schema version.
- Traffic classification totals for replay interactions: `success_unchanged`,
  `changed`, and `regressed`. A candidate 5xx response is a `regressed` finding
  when the baseline response was not already a server error and no corresponding
  baseline Schemathesis problem explains it. A candidate 5xx response is never
  counted as `success_unchanged`, so a baseline server error that persists is
  reported as `changed` rather than as healthy traffic.
- Candidate latency per finding plus minimum, maximum, and average in
  milliseconds across all replayed interactions.
- Exact status-code transition counts such as `200 -> 404`.
- A baseline-problem availability state. Markdown shows an unavailable
  disclosure only when the JSON `baseline_problems_available` value is `false`.
- A `findings` entry for every reportable traffic finding with the original
  request, candidate target URL, baseline response when recorded, candidate
  response, classification, and bodies and headers needed to reproduce the
  request. Healthy unchanged traffic is counted as `success_unchanged`, remains
  available in `replay.har.json`, and is omitted from detailed comparison
  findings. A persistent server error already explained by a correlated baseline
  Schemathesis problem is likewise omitted, because that problem carries the
  finding and its outcome; the interaction remains in `replay.har.json`.

### Evidence and correlation

HAR remains the mandatory ordered replay transcript and the correlation anchor.
Problems are matched to HAR entries only through the recorded
`X-Schemathesis-TestCaseId`; method, URL, and operation name are never treated
as unique keys. Problem evidence uses this deterministic precedence:

1. VCR checks and interaction IDs.
2. NDJSON recorder checks and interactions.
3. Structured JUnit failure text, but only when every counted JUnit
   `failure`/`error` has been extracted.

All present artifacts are parsed before replay, including lower-precedence
sources. A malformed HAR, VCR, NDJSON, or JUnit artifact therefore fails
comparison setup before candidate traffic or output creation. Missing VCR,
NDJSON, and JUnit artifacts are optional, and selection falls through to the
next source. Problem evidence is unavailable only when no complete structured
source is available. A valid selected artifact with no failed checks instead
produces an available state with a zero count and an empty `problems` array.

Problems without a matching HAR case ID remain in `problems` with
`interaction: null`; they are not discarded or assigned by request similarity.
When the same recorded case ID appears on multiple HAR entries, the problem is
reported with `correlation_status: "ambiguous"` and no `interaction` number
rather than being guessed onto one replay interaction.
Likewise, a HAR entry without a recorded baseline response is reported as
unknown and is not included in exact status-transition counts.

These reports preserve correlated problem evidence and raw status transitions
without treating status transitions as problem outcomes. Equal HTTP status codes
do not hide correlated baseline problems, and a status change alone never proves
that a baseline Schemathesis problem was fixed. A `fixed` outcome means replay
exercised the recorded semantic response path and the server-error check no
longer failed; it does not prove every campaign path or all related stateful
preconditions were exhaustively retested.

Schemathesis exits with status `1` when it finds API failures; `stcompare`
treats that as a completed campaign and keeps the generated reports plus
metadata. When Schemathesis aborts before completing the campaign, `stcompare`
removes a newly-created campaign directory and does not write success metadata.
Forced runs against an existing directory leave existing files in place if
Schemathesis aborts.

## Benchmark loops with stbench

`stbench` owns the neutral fix loop and invokes `stcompare` through its public
CLI contract. Candidate identity belongs to each Candidate Campaign, while the
`stbench:` block contains the fixed harness configuration shared by candidates
on the same machine:

```yaml
campaigns:
  baseline:
    kind: baseline
  sonnet5-high:
    kind: candidate
    agent: claude-code
    model: sonnet-5
    effort: high
    adapter: remote
  sonnet5-low:
    kind: candidate
    agent: claude-code
    model: sonnet-5
    effort: low
    adapter: remote
stbench:
  hardware: RTX 4090 / 64GB
  adapters:
    local: python /absolute/path/to/examples/stbench/local_model_adapter.py
    remote: python /absolute/path/to/examples/stbench/coding_agent_adapter.py --timeout 1800
  adapter_timeout: 30m
  reuse_process: false
  source_dir: .
  stcompare_binary: stcompare
  prompt:
    id: stbench-default
    version: "2"
  lifecycle:
    stop: .local/stbench/stop.sh
    reset: .local/stbench/reset.sh
    build: .local/stbench/build.sh
    start: .local/stbench/start.sh
    command_timeout: 30m
    health_url: http://localhost:8080/health
    health_timeout: 30s
    health_interval: 100ms
  max_iterations: 100
  stall_window: 2
```

Run the loop with:

```sh
stbench --config stcompare.yaml run sonnet5-high
```

In `stbench run <candidate>`, the positional argument selects a Candidate
Campaign and reads its `agent`, `model`, `effort`, and adapter type from that
campaign entry. `effort` is a
first-class identity axis: `sonnet5-high` and `sonnet5-low` are distinct
candidates and appear as separate scorecard rows even though they use the same
agent and model. Run flags are limited to execution settings such as source and
binary paths, lifecycle commands and timeouts, process reuse, prompt identity,
iteration and stall limits, heartbeat cadence, the base URL, and scorecard
emission. Explicit execution flags override the `stbench:` values, which
override documented defaults; the `--base-url` override is applied before
configuration validation.

The benchmark record path is not configurable. It is derived from the selected
candidate as `reports/<candidate>/benchmark-record.json` (under the configured
`reports_dir`), so a run cannot accidentally be saved under another
candidate's name.

To scaffold the lifecycle hooks from the API repository root, run:

```sh
stbench init
```

For a new project, `stcompare config init` creates the complete new-shape
configuration with identity on campaign entries, an adapter map, and harness
hardware. For an existing `stcompare.yaml` that does not yet contain an
`stbench:` block, `stbench init` creates executable `stop.sh`, `reset.sh`,
`build.sh`, and `start.sh` stubs in the repository-local `.local/stbench/`
directory and writes the matching block directly into the configuration. Each
API keeps its own adapter lifecycle setup. Set `STBENCH_STATE_DIR` to choose an
external state directory for a deliberate override; repository-local overrides
must use `.local/stbench`.
`stbench init` also adds `.local/stbench/` to `.gitignore` if it is not already
covered. It is non-destructive: it refuses to overwrite lifecycle files or to
modify a configuration that already has an `stbench:` block (including one
created by `stcompare config init`). Replace the no-op
commands with the API's commands and keep adapter support files outside the API
repository. The generated `stop` hook is safe to run before the first iteration,
when no API process exists. The `reset` hook must clean per-iteration runtime
state without reverting source files, because source changes are the agent's
progress.

Before each comparison, `stbench` runs stop, optional reset, build, start, and
health polling. `stop` may be called when nothing is running and should be
idempotent. `reset` is for runtime state only; it must not run commands such as
`git checkout .` that erase source changes. `build` prepares the candidate, and
`start` must launch a long-running candidate process. After `start` returns,
`stbench` polls `health_url` until it receives a `2xx` response or the health
timeout expires; `health_interval` controls the delay between polls. The
candidate must listen on the host and effective port declared by `base_url`,
and `lifecycle.health_url` must use that same host and port. An omitted port
means 80 for HTTP or 443 for HTTPS. Configuration validation reports a mismatch
before the benchmark starts; the health URL may use a different path.
`adapter_timeout` bounds each adapter invocation and
`lifecycle.command_timeout` bounds each lifecycle hook; both default to 30
minutes and can also be supplied as `--adapter-timeout` and
`--command-timeout`. Timed-out commands are terminated as process groups and
produce an adapter or lifecycle error. The adapter runs with `source_dir` as
its working directory, while its own adapter files and lifecycle harness stay
outside that tree. It receives one
JSON object on stdin and must write one JSON object to stdout:

```json
{"agent":"codex","model":"gpt-5","effort":"high","hardware":"local-machine","instruction":"...","view":{"schema_version":"1", "actionable":[]}}
```

`agent`, `model`, and `effort` come from the selected Candidate Campaign;
`hardware` comes from the `stbench:` harness configuration. Together they are
the adapter's execution metadata. Adapter-specific flags are optional explicit
overrides; do not duplicate these values in the adapter command by default.

`reuse_process` is opt-in and off by default. When enabled, stbench asks the
adapter during preflight whether it supports a line-delimited request/response
session. A supporting adapter keeps its process alive and returns one JSON
result per request; an adapter that does not advertise support automatically
falls back to the cold one-shot invocation. Every reused turn receives the
same fresh metadata, rendered instruction, and compact view as the cold path.
This is process reuse, not context carry: stbench never sends prior prompts,
reasoning, tool transcripts, or session history. Context carry is intentionally
out of scope because it would confound agent comparisons and can overflow the
small context windows targeted by local-model studies. Each request is still
bounded by `adapter_timeout`, and the benchmark record's `process_reuse`
field reports whether reuse was actually negotiated.

Before the first comparison, `stbench` automatically runs a preflight smoke
test: stop, optional reset, build, start, health check, and stop again. It then
sends the adapter a no-op request with `"preflight": true`; a compatible
adapter returns an `ok` result without invoking its model or editing the
candidate. Any failed preflight phase is reported before iteration 1 or a
comparison begins.

Reusable adapters set `reuse_process: true` in their successful preflight
result and read one newline-delimited JSON request at a time. The reference
adapters remain stateless by default and therefore return `reuse_process:
false`; enabling the runner option is a verified no-op for those adapters.

The result is `{ "status": "ok"|"error", "message": "...", "response":
"<raw model response>", "tokens": { "input": 1, "output": 2, "total": 3 } |
null, "reuse_process": false }`. The adapter edits the candidate in place;
unknown token usage must be reported as `null`. The command writes the
versioned benchmark record to the selected Candidate Campaign's derived report
path; its `tokens` field sums known usage, while
`unknown_token_iterations` counts fix iterations that reported `null`. If no
iteration reports token usage, `tokens` remains `null`. The command exits `0`
on convergence, `2` on a stalled or capped run, and `1` on tool, adapter, or
lifecycle errors.

### Adapter examples

The adapter is a delivery boundary, not the benchmark loop. `stbench` renders
one fixed task instruction and compact `--format agent` view per iteration; the
adapter passes that instruction through its agent-specific envelope, edits the
candidate in place, and returns the protocol result. It does not read raw
campaign artifacts, rewrite the task, or decide when to iterate.

Three reference adapters are provided in
[`examples/stbench/README.md`](examples/stbench/README.md):

- `local_model_adapter.py` is the first-class on-prem path. It talks to an
  OpenAI-compatible local inference server and gives the model confined
  read/write/command tools, so source is edited in place without a cloud
  dependency or repository snapshot. Select its adapter type on the Candidate
  Campaign, keep harness hardware in `stbench:`, and pass URL, timeout, and turn
  limit as adapter-command options; keep only an optional API key in
  `STBENCH_LOCAL_MODEL_API_KEY`.
- `coding_agent_adapter.py` is the first-class engineering path for an
  installed Codex or Claude Code CLI. Set `agent`, `model`, `effort`, and the
  adapter type on the Candidate Campaign, then map that type to the command in
  `stbench.adapters`; no runner code changes are needed.
- `adapter.py` is the explicit cloud fallback. It snapshots tracked source
  below `source_dir`; it excludes the repository-local `.local/stbench`
  and `.local/stcompare` control-plane paths from snapshots and patches. It
  requests a unified diff, validates it with `git apply --check`, and applies
  it. Keep the adapter and `_protocol.py` outside the API
  repository; keep model and effort on the Candidate Campaign and hardware in
  `stbench:`, then pass endpoint, timeout, and snapshot limit as adapter-command
  options;
  keep `OPENAI_API_KEY` as an environment credential. The
  example documents its cloud, repository-size, and patch-format limitations
  up front; it is not the recommended on-prem path.

Map an adapter type to any of these commands in `stbench.adapters`, then select
that type in each Candidate Campaign without changing the loop or lifecycle
configuration. The examples use only Python's standard library.

Because `hardware` is declared once for the harness, a hosted or remote
candidate inherits the harness machine's hardware label even though inference
runs elsewhere. This is acceptable for the current single-machine local-model
study, but must be revisited before comparing remote candidates on cost or
latency.

## Build a benchmark scorecard

Join the final traffic comparison with the benchmark record produced by
`stbench run` automatically:

```sh
stbench --config stcompare.yaml run gpt5.6 --emit-scorecard
```

This writes `scorecard.html` to the candidate's configured report directory.
Scorecard generation is best-effort: a missing comparison or builder failure
prints a warning without changing the benchmark's terminal state or exit code.
Without `--emit-scorecard`, no scorecard subprocess runs.

To build the same artifact manually:

```sh
stcompare scorecard build \
  --comparison reports/gpt5.6/comparison.json \
  --record reports/gpt5.6/benchmark-record.json \
  --out reports/gpt5.6/scorecard.html
```

The self-contained HTML includes every section from `comparison.html` plus a
Benchmark Run section with the agent, model, iteration count, total and
agent-fix durations, and token usage. All three paths are required. Missing or
malformed inputs fail without writing the output file; a record whose token
usage is `null` is shown explicitly as not reported.

## Caveats and troubleshooting

- **Replay does not recreate baseline state.** Requests are sent in recorded
  order, but databases, generated identifiers, authentication sessions, clocks,
  and external services may differ. Configure precondition heuristics and
  normalization rules for known sources of nondeterminism; treat
  `inconclusive` as unverified, not fixed.
- **The candidate process is external.** `campaign compare` does not build,
  start, stop, or health-check the candidate. Start it yourself at `base_url`,
  or use `stbench` when lifecycle automation is required.
- **A candidate campaign run is not required for comparison.** The candidate
  name selects an output identity. Comparison consumes the baseline artifacts
  and talks directly to the running candidate.
- **Reports may contain sensitive data.** HAR, VCR, NDJSON, metadata, request
  headers, and bodies can contain credentials or customer data. The generated
  configuration disables Schemathesis output sanitization and truncation for
  audit fidelity. Protect `reports_dir`, review artifacts before sharing, and
  do not commit them unless that is intentional.
- **Exit code `2` is expected for findings.** CI wrappers should distinguish a
  completed non-converged comparison (`2`) from a tool failure (`1`).
  Schemathesis status `1` likewise means it found API failures; `stcompare`
  retains that completed campaign.
- **`--force` can destroy a good baseline.** Campaign runs protect existing
  directories by default. If a forced rerun aborts, pre-existing files remain
  and may be mixed with partial output. Prefer a new campaign identity or back
  up the report directory before replacing important evidence.
- **Comparison outputs are replaceable.** Repeating `campaign compare` for the
  same candidate writes to the same `replay.har.json` and `comparison.*` paths.
  Copy or rename a result before another run when historical reports matter.
- **Configured candidate specs must be usable.** A missing or invalid
  `candidate_spec` produces an inconclusive contract limitation; it never falls
  back to the baseline generation schema as if that were the candidate's
  contract.
- **Baseline artifacts are validated together.** A malformed present HAR, VCR,
  NDJSON, or JUnit file stops comparison before candidate traffic is sent.
  Optional evidence files may be absent, but `campaign.har.json` is mandatory.
- **Shell aliases do not select Schemathesis.** Install `st`, make `uvx`
  available, or set `STCOMPARE_SCHEMATHESIS_COMMAND` to an executable command.
  Use `stcompare campaign command <name>` to inspect the generated invocation.

## Development

Run the test suite, race detector, and static checks:

```sh
go test ./...
go test -race ./...
go vet ./...
scripts/install_test.sh
```

Pull requests and pushes to `main` run formatting, ShellCheck, module-file,
package-boundary, vet, test, and race-detector checks. Only after every check
passes does CI cross-compile release archives for Linux and macOS on amd64 and
arm64.

Successful `main` builds use Conventional Commit messages to create semantic
versions and GitHub Releases. `feat` commits produce minor releases, `fix` and
`perf` commits produce patch releases, and breaking changes produce major
releases. Each release contains both binaries for every supported platform and
a checksum manifest consumed by the installer.

### Optional benchmark verification

The default test suite stays fast and dependency-free. It does not require
Schemathesis, a network, or an external service.

To verify the full benchmark pipeline against a real Schemathesis installation,
run the opt-in integration target:

```sh
STCOMPARE_RUN_E2E_BENCHMARK=1 go test ./integration -run TestOptionalEndToEndBenchmarkVerification -count=1
```

Prerequisites:

- `st` on `PATH`, `uvx` on `PATH`, or
  `STCOMPARE_SCHEMATHESIS_COMMAND="uvx schemathesis"` or another explicit
  Schemathesis command.
- Enough time for two Schemathesis campaigns plus replay and report generation;
  on a warm local installation this is expected to take seconds to about a
  minute.

When `STCOMPARE_RUN_E2E_BENCHMARK` is unset, the test reports an explicit skip.
When no usable Schemathesis command is available, it also reports an explicit
skip with the missing prerequisite. The target uses temporary directories and
local HTTP servers, so it leaves no report directories, ports, or processes
behind after the test exits.

The verification service is defined inside the integration test. The baseline
service deliberately returns a schema-invalid `201` response for `POST
/widgets`; the candidate service returns a schema-valid response for the same
operation. The test runs baseline capture, candidate capture, HAR replay, and
comparison report generation as one flow, then asserts that the produced JSON
report extracted baseline problems, correlated at least one to replay traffic,
and assigned at least one problem outcome. Campaign metadata is also checked for
the recorded Schemathesis version so version drift is visible.
