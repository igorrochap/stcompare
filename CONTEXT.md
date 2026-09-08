# Campaign Comparison

Campaign Comparison evaluates how well a candidate API implementation addresses
problems observed during a baseline Schemathesis campaign while preserving
uncertainty when the available evidence cannot support a verdict.

## Language

**Campaign**:
A named Schemathesis execution whose configuration and artifacts form one
auditable evaluation record.

**Baseline Campaign**:
The immutable reference campaign containing the original API behavior and its
Schemathesis problems.
_Avoid_: Original run, control run

**Candidate Campaign**:
A campaign representing an implementation evaluated against the baseline.
_Avoid_: Patch run, comparison run

**Interaction**:
One recorded HTTP request and response exchange in a campaign transcript.
_Avoid_: Finding, problem

**Schemathesis Problem**:
One failed Schemathesis check associated with baseline evidence. An interaction
may have zero, one, or multiple problems.
_Avoid_: Failure, finding

**Problem Outcome**:
The evidence-backed conclusion for one baseline Schemathesis problem: fixed,
still failing, or inconclusive.
_Avoid_: Status transition, interaction classification

**Fixed**:
A problem outcome supported by evidence that the candidate no longer exhibits
the baseline problem while exercising the relevant behavior.

**Still Failing**:
A problem outcome supported by evidence that the candidate continues to exhibit
the baseline problem.

**Inconclusive**:
A problem outcome used when replay evidence cannot establish whether the
baseline problem was fixed or remains.
_Avoid_: Fixed, passed

**Interaction Classification**:
A description of candidate traffic not itself equivalent to a problem outcome,
such as success unchanged, changed, or regressed.
_Avoid_: Problem outcome

**Regression**:
A candidate problem or materially worse behavior that was not present in the
corresponding baseline evidence.

**Finding**:
A reportable conclusion about a baseline problem, regression, or material
change. Healthy unchanged interactions are not findings.
_Avoid_: Interaction, replay entry

**Status Transition**:
The baseline and candidate HTTP status codes for an interaction. It is
supporting evidence and does not by itself determine a problem outcome.
_Avoid_: Outcome, finding

**Converged**:
A comparison outcome in which no baseline Schemathesis problem is _Still Failing_
and no interaction is _Regressed_. Converged is the loop's stop condition. It
tolerates _Inconclusive_ problems, so a converged comparison may still contain
unverified problems and is not a claim that every baseline problem was positively
confirmed fixed.
_Avoid_: 100% fixed, Fix rate 100%, Passed

**Fix Quality Assessment**:
A researcher's evidence-backed judgment of whether a candidate's changes
address the underlying Schemathesis problem or merely work around its observed
failure. It is distinct from the replay-backed Problem Outcome; a Fixed outcome
alone does not establish fix quality.
_Avoid_: Problem Outcome, Fix rate

**Edit Attempt**:
One requested edit during a benchmark run, including requests that fail or
leave file contents unchanged.
_Avoid_: File Modification

**File Modification**:
One successful edit operation's actual change to a file's contents during a
benchmark run, including a change that is later overwritten or reverted.
_Avoid_: Edit Attempt, Files Changed at the End

**Files Changed at the End**:
The distinct files whose contents at the end of a benchmark run differ from
their contents at its start, including created and deleted files.
_Avoid_: File Modifications, Edit Attempts

**Model Tool Call**:
One tool request issued by the model during a benchmark run, including failed
requests, unknown tool names, and requests recovered from the model's text.
_Avoid_: Adapter Operation

**Adapter Operation**:
An action performed by the adapter to deliver inputs or process model output,
such as preparing a source snapshot or validating and applying a returned patch.
It is distinct from a Model Tool Call.
