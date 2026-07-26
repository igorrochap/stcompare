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

Omitted Schemathesis settings inherit the defaults shown above. Required values
are validated after command-line overrides are applied. The effective
configuration must have:

- A non-empty schema location.
- An absolute HTTP or HTTPS base URL.
- A non-empty reports directory.
- At least one Schemathesis worker.
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

Candidate responses and comparison reports are written under the candidate
campaign directory:

```text
reports/gpt5.6/replay.har.json
reports/gpt5.6/comparison.json
reports/gpt5.6/comparison.md
```

`comparison.json` uses a versioned, stable schema for automation, while
`comparison.md` presents the same evidence for review. Both reports include:

- Total replayed interactions.
- Baseline Schemathesis problem counts from individual JUnit `failure` and
  `error` elements.
- Candidate latency per interaction plus minimum, maximum, and average latency
  in milliseconds.
- Exact status-code transition counts such as `200 -> 404`.
- A disclosure that problem-level outcomes are unavailable until Schemathesis
  problems are correlated with replay interactions.
- An `interactions` entry for every replayed interaction with the original
  request, candidate target URL, baseline response when recorded, candidate
  response, and bodies and headers needed to reproduce the request.

When the baseline JUnit report is absent, the problem count is explicitly
unknown rather than inferred from HTTP status codes. A malformed present JUnit
report fails comparison setup before candidate traffic begins. Likewise, a HAR
entry without a recorded baseline response is reported as unknown and is not
included in exact status-transition counts.

These first-pass reports preserve raw evidence and transitions. Semantic outcome
labels such as fixed, regressed, or still failing are not inferred yet.

The current `interactions` collection is therefore raw interaction evidence and
can include healthy unchanged transitions such as `200 -> 200`. It is not yet
the problem-centric result set used to judge fix effectiveness. The planned
classification layer will retain all traffic in `replay.har.json` while limiting
detailed comparison findings to baseline Schemathesis problems, candidate
regressions, and material or inconclusive changes.

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
