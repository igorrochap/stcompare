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
```

The examples below assume the binary is available as `stcompare`; use
`./stcompare` when running the local build directly.

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
  `fixed`, `still_failing`, and `inconclusive`, plus separate total, evaluable,
  and uncorrelated counts. A problem is `evaluable` when it is correlated to a
  replay interaction and the comparison has evidence for an outcome. Existing
  check-specific evaluation covers the Schemathesis server-error check.
  Generated-resource precondition-loss evidence can make a correlated problem
  of any check category evaluable and inconclusive. The three outcome totals
  always sum to the evaluable count. A correlated `not_a_server_error` problem
  remains `still_failing` when replay also returns a 5xx response. A replayed
  non-server-error problem on a replayed 5xx response remains outside the
  evaluable denominator. A replayed non-5xx response is `fixed` only when the
  recorded exercise evidence shows replay reached the relevant behavior; absence
  of contrary evidence remains `inconclusive`.
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

## Development

Run the test suite, race detector, and static checks:

```sh
go test ./...
go test -race ./...
go vet ./...
```
