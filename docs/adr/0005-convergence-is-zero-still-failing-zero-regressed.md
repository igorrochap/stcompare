# Convergence is zero still_failing and zero regressed, tolerating inconclusive

The agent loop's stop condition — reported as `converged` in the comparison —
is: **no baseline Schemathesis problem is `still_failing` and no interaction is
`regressed`.** It deliberately *tolerates* `inconclusive` problems. Converged is
not `fix_rate == 100%`, and it is not a claim that every baseline problem was
positively confirmed fixed.

## Why not `fix_rate == 100%`

`fix_rate` is `fixed / evaluable`, and *evaluable* excludes `inconclusive`,
`unevaluable`, `uncorrelated`, and `ambiguous` problems, and says nothing about
`regressed` traffic. An agent looping until `fix_rate` hit 100% could declare
victory while the candidate had introduced brand-new 5xx regressions and while
dozens of problems sat unresolved in the excluded buckets. `fix_rate` is an
honest *reporting* metric; it is the wrong *control signal* for a loop.

## Why tolerate `inconclusive`

An agent acts by editing endpoint code. It can act on a `still_failing` problem
or a `regressed` interaction — both carry concrete reproduction evidence. It
cannot resolve an `inconclusive` problem by editing endpoint code: inconclusive
is an evidence/campaign-quality gap (missing exercise evidence, generated-resource
precondition loss), a different kind of work. Blocking the loop on inconclusives
would make it spin forever on cases it structurally cannot fix.

## Keeping the signal honest

Because Converged tolerates unverified problems, the compact agent view always
carries an `unverified` block — `inconclusive`, `uncorrelated`, `ambiguous`,
`unevaluable` counts — beside `converged`. A converged candidate may still hold
problems in those buckets, and the caller must be able to say "Converged, but N
problems remain unverified" rather than implying a clean bill of health.

## Consequences

- The compact agent view carries `converged`, the deciding counts
  (`still_failing`, `regressed`), progress counts (`fixed`), and the
  `unverified` residual. The actionable list contains only `still_failing`
  problems and `regressed` interactions — the two things an agent can act on —
  flat and stable-sorted (regressions first, then by operation), each with a
  stable identity so a caller can distinguish "same item still stuck" from "new
  item introduced by a fix".
- All five check categories (`server_error`, `negative_data_rejection`,
  `positive_data_acceptance`, `response_schema_conformance`,
  `status_code_conformance`) are evaluated, so Converged spans every category —
  not server errors alone.
- The exit code of `compare --format agent` is derived from `converged`
  (`0` converged, `2` not), keeping one source of truth.
