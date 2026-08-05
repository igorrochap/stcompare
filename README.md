# stcompare

`stcompare` is a command-line tool for defining repeatable Schemathesis baseline
and candidate campaigns. Its goal is to make API comparison runs auditable by
keeping schemas, targets, deterministic generation settings, reports, and named
campaigns in a version-controlled YAML file.

The current implementation provides the configuration foundation, campaign
execution flow, and first comparison replay: initialize, load, validate,
override, inspect an effective configuration, print the Schemathesis command for
a named campaign, run a named campaign with isolated reports plus metadata, and
replay a baseline HAR transcript against a candidate API while recording
candidate responses.

## Requirements

- Go 1.25 or later

## Build

Build a local binary from the repository root:

```sh
go build -o stcompare ./cmd/stcompare
go build -o stbench ./cmd/stbench
```

The examples below assume the binary is available as `stcompare`; use
`./stcompare` when running the local build directly.

To build both binaries and install them onto your `PATH` (so `stcompare` and
`stbench` are runnable from any repository), run:

```sh
./scripts/install.sh
```

By default this installs to `$GOBIN` (or `$(go env GOPATH)/bin` if `GOBIN` is
unset); pass `--dir <path>` to install elsewhere.

Commands can also be run without keeping a binary:

```sh
go run ./cmd/stcompare --help
```

## Configuration

By default, `stcompare` reads and writes `stcompare.yaml` in the current working
directory. Use the global `--config` flag to select another path.

Initialize the default configuration:

```sh
stcompare config init
```

Initialization refuses to replace an existing file. Use `--force` only when an
existing configuration should be overwritten:

```sh
stcompare config init --force
```

Initialize a configuration at an explicit path:

```sh
stcompare --config config/benchmark.yaml config init
```

The generated configuration is:

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
  sonnet5:
    kind: candidate
```

The `campaigns` mapping declares named report identities. A `baseline` campaign
represents reference artifacts, while `candidate` campaigns represent isolated
implementations to compare with that reference. The sample campaign names can
be replaced with names appropriate to the project.

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

## Inspect Effective Configuration

Load, validate, and print the effective configuration:

```sh
stcompare config show
```

Inspect another configuration file:

```sh
stcompare --config config/benchmark.yaml config show
```

Common settings can be overridden for inspection without modifying the YAML
file:

```sh
stcompare config show \
  --schema api/openapi.yaml \
  --base-url http://localhost:9090 \
  --reports-dir comparison-reports \
  --seed 4242 \
  --workers 1
```

Omitted Schemathesis and comparison settings inherit the defaults shown above.
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

## Preview Campaign Commands

Print the Schemathesis command for a configured campaign without running it:

```sh
stcompare campaign command baseline
```

With the default configuration, the generated command is:

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

The campaign name must exist in the configuration and must be a safe path
segment made from letters, numbers, dots, underscores, or hyphens.

Common settings can be overridden for preview without editing the YAML file:

```sh
stcompare campaign command baseline \
  --schema api/openapi.yaml \
  --base-url http://localhost:9090 \
  --reports-dir comparison-reports \
  --seed 4242 \
  --workers 1
```

Additional Schemathesis arguments from `schemathesis.extra_args` are appended to
the generated command. Report format and report path options are owned by
`stcompare`; extra arguments cannot override `--report`,
`--report-junit-path`, `--report-vcr-path`, `--report-har-path`, or
`--report-ndjson-path`.

## Run Campaigns

Execute a configured campaign:

```sh
stcompare campaign run baseline
```

The command runs the same generated `st run ...` argv shown by
`stcompare campaign command <campaign>`. Reports are written under the isolated
campaign directory:

```text
reports/baseline/
  junit.xml
  campaign.vcr.yaml
  campaign.har.json
  campaign.ndjson
  metadata.yaml
```

By default, `stcompare` executes `st` directly. Shell aliases are not visible to
non-interactive processes. If `st` is not installed but `uvx` is available,
`stcompare` falls back to `uvx schemathesis`. To use another executable, set:

```sh
STCOMPARE_SCHEMATHESIS_COMMAND="uvx schemathesis" stcompare campaign run baseline
```

`metadata.yaml` records the effective command, `stcompare` version,
Schemathesis version, timestamp, config path, campaign name and kind, effective
settings, and command-line overrides.

Existing campaign report directories are protected by default:

```sh
stcompare campaign run baseline
# campaign report directory reports/baseline already exists; use --force to overwrite
```

Use `--force` only when replacing the outputs for that named campaign is
intentional:

```sh
stcompare campaign run baseline --force
```

When Schemathesis fails, newly-created campaign report directories are removed
and `metadata.yaml` is not written. To inspect partial Schemathesis output after
a failed run, opt in explicitly:

```sh
stcompare campaign run baseline --keep-failed
```

Preserved failed-run artifacts are partial debug output, not a completed
auditable campaign.

## Compare Campaigns

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
`base_url`. Common configuration overrides are supported, so a temporary
candidate target can be selected without editing the YAML file:

```sh
stcompare campaign compare gpt5.6 --base-url http://localhost:9090
```

Replay rewrites only the target base URL. The original request path, query
string, and percent encoding are preserved. Semantic request headers are copied,
while stale transport-managed headers such as `Host`, `Content-Length`,
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

Candidate responses and comparison reports are written under the candidate
campaign directory:

```text
reports/gpt5.6/replay.har.json
reports/gpt5.6/comparison.json
reports/gpt5.6/comparison.md
```

`comparison.json` uses a versioned, stable schema for automation, while
`comparison.md` presents the same evidence for review. Schema version 5 adds
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

## Run Benchmark Loops

`stbench` owns the neutral fix loop and invokes `stcompare` through its public
CLI contract. Add a `stbench` section to `stcompare.yaml`, or provide the same
values as flags:

```yaml
stbench:
  campaign: gpt5.6
  agent: local-agent
  model: model-name
  hardware: hardware-name
  adapter: python /absolute/path/to/examples/stbench/coding_agent_adapter.py --timeout 1800
  adapter_timeout: 30m
  # reuse_process: true
  source_dir: .
  stcompare_binary: stcompare
  record_path: .local/stbench/records/gpt5.6.json
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
stbench run --config stcompare.yaml
```

The canonical `stbench run` flags use the same names as the settings they
override: `--campaign`, `--agent`, `--model`, `--hardware`, `--source-dir`,
`--adapter`, `--adapter-timeout`,
`--stcompare-binary`, `--record-path`, `--base-url`, `--stop-command`,
`--reset-command`, `--build-command`, `--start-command`, `--command-timeout`,
`--health-url`, `--health-timeout`, `--health-interval`, `--max-iterations`,
`--stall-window`, `--prompt-id`, `--prompt-version`, and `--reuse-process`. The old short and
duplicate aliases are not accepted. Effective values follow this precedence:
explicit run flags override the `stbench` YAML section, which overrides the
documented defaults. The `--base-url` override is applied before configuration
validation.

To scaffold the lifecycle hooks from the API repository root, run:

```sh
stbench init
```

This creates executable `stop.sh`, `reset.sh`, `build.sh`, and `start.sh`
stubs in the repository-local `.local/stbench/` directory, then prints a
matching `stbench:` configuration stanza with absolute paths. Each API keeps
its own adapter lifecycle setup. Set `STBENCH_STATE_DIR` to choose an external
state directory for a deliberate override; repository-local overrides must use
`.local/stbench`.
`stbench init` also adds `.local/stbench/` to `.gitignore` if it is not already
covered. Add the printed stanza to `stcompare.yaml` and replace the no-op
commands with the API's commands. Keep adapter support files outside the API
repository. The generated `stop` hook is safe to run before the first
iteration, when no API process exists. The `reset` hook must clean per-iteration
runtime state without reverting source files, because source changes are the
agent's progress.

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
{"agent":"codex","model":"gpt-5","hardware":"local-machine","instruction":"...","view":{"schema_version":"1", "actionable":[]}}
```

`agent`, `model`, and `hardware` come from the `stbench` configuration and are
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
unknown token usage must be reported as `null`. The command writes the versioned benchmark record to
`record_path`; its `tokens` field sums known usage, while
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
  dependency or repository snapshot. Put its URL, model, hardware, timeout,
  and turn limit in the `stbench` configuration; keep only an optional API key
  in `STBENCH_LOCAL_MODEL_API_KEY`.
- `coding_agent_adapter.py` is the first-class engineering path for an
  installed Codex or Claude Code CLI. Set `agent`, `model`, and `hardware` in
  the `stbench` configuration and pass only the timeout on the `adapter:`
  command; no runner code changes are needed.
- `adapter.py` is the explicit cloud fallback. It snapshots tracked source
  below `source_dir`; it excludes the repository-local `.local/stbench`
  and `.local/stcompare` control-plane paths from snapshots and patches. It
  requests a unified diff, validates it with `git apply --check`, and applies
  it. Keep the adapter and `_protocol.py` outside the API
  repository; put its model and hardware in the `stbench` configuration and
  pass the endpoint, timeout, and snapshot limit on the `adapter:` command;
  keep `OPENAI_API_KEY` as an environment credential. The
  example documents its cloud, repository-size, and patch-format limitations
  up front; it is not the recommended on-prem path.

Point `stbench.adapter` at any of these commands without changing the
loop or lifecycle configuration. The examples use only Python's standard
library.

## Development

Run the test suite, race detector, and static checks:

```sh
go test ./...
go test -race ./...
go vet ./...
```

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
