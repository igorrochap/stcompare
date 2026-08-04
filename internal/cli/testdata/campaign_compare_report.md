# Campaign comparison

## Summary

- Total interactions: 2
- Baseline problems: 3 (source: `reports/baseline/junit.xml`)
- Problem count basis: JUnit reports deduplicated Schemathesis problems. Structured evidence records every failing case from VCR/NDJSON when available, but no structured problem count is available in this report.
- Problem bucket sums: evaluable = fixed + still_failing + inconclusive; total = evaluable + unevaluable + uncorrelated + ambiguous. Every extracted Schemathesis problem is assigned to exactly one bucket. Only evaluable problems receive fixed, still_failing, or evaluable inconclusive counts; unevaluable, uncorrelated, and ambiguous problems carry not_evaluated outcomes with a reason on the problem entry.
- Baseline problem outcomes: total 0, evaluable 0, fixed 0, still failing 0, inconclusive 0, unevaluable 0, uncorrelated 0, ambiguous 0
- Fix rate: unavailable (0 evaluable baseline problems). Problems fixed among evaluable baseline problems in this comparison. It excludes uncorrelated, ambiguous, and unevaluable baseline problems; counts Schemathesis problems rather than distinct defects; and is comparable only for the same baseline and report schema version.
- Traffic classifications: total 2, success unchanged 0, changed 2, regressed 0
- Candidate latency: minimum 4 ms, maximum 10 ms, average 7 ms
- Exact status transitions:
  - `200 -> 404`: 1
  - `500 -> 200`: 1
- Baseline campaign: `baseline`
- Candidate campaign: `gpt5.6`
- Candidate base URL: `%[1]s`

> Baseline Schemathesis problems are unavailable: Baseline Schemathesis problems could not be extracted from structured evidence.

## Comparison policy

- Missing resource statuses: `404`, `410`
- Precondition heuristics: none
- Normalization defaults: enabled

## Schema validation

- Validator: `github.com/getkin/kin-openapi/openapi3filter`
- Validator version: `v0.145.0`
- Supported OpenAPI versions: `3.0, 3.1, 3.2`
- Status code conformance: explicit status codes and range codes such as 2XX count as documented; default responses do not document every status
- Contract limitation: `schema_contract_unavailable`

## Findings

### Finding 1: `POST http://baseline.invalid/widgets?dryRun=true`

- Candidate target: `%[1]s/widgets?dryRun=true`
- Classification: `changed`
- Latency: 4 ms
- Status transition: `200 -> 404`

#### Request headers

```text
A-Request: first
Content-Type: application/json
Z-Request: last
```

#### Request body

```text
{"name":"widget"}
```

#### Baseline response: `200`

Headers:

```text
Content-Type: application/json
X-Baseline-A: first
X-Baseline-Z: last
```

Body:

```text
{"id":"widget","state":"available"}
```

#### Candidate response: `404`

Headers:

```text
Content-Length: 28
Content-Type: application/problem+json
Date: Mon, 02 Jan 2006 15:04:05 GMT
X-Candidate-A: first
X-Candidate-Z: last
```

Body:

```text
{"error":"widget not found"}
```

### Finding 2: `GET http://baseline.invalid/missing`

- Candidate target: `%[1]s/missing`
- Classification: `changed`
- Latency: 10 ms
- Status transition: `500 -> 200`

#### Request headers

```text
A-Request: first
Z-Request: last
```

#### Request body

_Empty._

#### Baseline response: `500`

Headers:

```text
X-Baseline-A: first
X-Baseline-Z: last
```

Body:

```text
{"error":"baseline unavailable"}
```

#### Candidate response: `200`

Headers:

```text
Content-Length: 36
Content-Type: application/json
Date: Mon, 02 Jan 2006 15:04:05 GMT
X-Candidate-A: first
X-Candidate-Z: last
```

Body:

```text
{"id":"missing","state":"available"}
```
