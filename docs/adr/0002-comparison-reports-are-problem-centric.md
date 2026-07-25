# Comparison reports are problem-centric

`stcompare` preserves every replayed interaction in the response log, but its
JSON and Markdown comparison findings focus on baseline Schemathesis problems,
candidate regressions, and material changes. Healthy unchanged interactions are
counted rather than listed. Problem outcomes are kept separate from
interaction classifications because equal HTTP status codes can represent a
fixed problem, a still-failing problem, or ordinary healthy traffic, while a
status change alone does not prove that a problem was fixed.

## Consequences

- Reports maintain separate problem and traffic metrics.
- Every correlated baseline problem remains visible regardless of whether its
  status code changed.
- A complete replay log remains available for auditing interactions omitted
  from comparison findings.
- Problem-centric reporting depends on deterministic correlation between
  Schemathesis problem evidence and replay interactions.
