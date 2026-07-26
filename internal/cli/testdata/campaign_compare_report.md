# Campaign comparison

## Summary

- Total interactions: 2
- Baseline problems: 3 (source: `reports/baseline/junit.xml`)
- Candidate latency: minimum 4 ms, maximum 10 ms, average 7 ms
- Exact status transitions:
  - `200 -> 404`: 1
  - `500 -> 200`: 1
- Baseline campaign: `baseline`
- Candidate campaign: `gpt5.6`
- Candidate base URL: `%[1]s`

## Interaction evidence

### Interaction 1: `POST http://baseline.invalid/widgets?dryRun=true`

- Candidate target: `%[1]s/widgets?dryRun=true`
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

### Interaction 2: `GET http://baseline.invalid/missing`

- Candidate target: `%[1]s/missing`
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
