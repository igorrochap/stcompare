# stcompare

`stcompare` is a command-line tool for defining repeatable Schemathesis baseline
and candidate campaigns. Its goal is to make API comparison runs auditable by
keeping schemas, targets, deterministic generation settings, reports, and named
campaigns in a version-controlled YAML file.

The current implementation provides the configuration foundation: initialize,
load, validate, override, and inspect an effective configuration. Campaign
execution and baseline replay commands are not implemented yet.

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

## Development

Run the test suite, race detector, and static checks:

```sh
go test ./...
go test -race ./...
go vet ./...
```
