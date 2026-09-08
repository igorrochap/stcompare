# Benchmark audit capture is required research evidence

Benchmark research measures efficiency and supports a researcher's assessment
of fix quality, which replay success alone cannot establish. Audit capture is
enabled by default for the bundled local-model adapter and written
incrementally; a capture failure stops the run with an explicit audit failure
because continuing without the measurement would undermine the research.

Ordinary model errors, timeouts, and crashes retain the evidence already
recorded, with incomplete capture labeled partial and its counts identified as
partial. Missing evidence is never represented as zero activity. Failure to
render HTML is nonfatal when the underlying evidence remains intact.

The evidence includes tool arguments and results, emitted model messages, and
each actual file modification's before/after diff linked to its tool call,
model turn, and benchmark iteration. Model Tool Calls and Adapter Operations
remain distinct: the API patch adapter's validation and application commands
must not inflate its model tool-call count. Local-model capture comes first;
the API patch adapter may follow using the shared audit format, while the
Codex/Claude adapters are outside the initial scope.

Each model turn also preserves the exact model input, including compacted
history and adapter-added instructions, alongside the returned messages.
An activity history alone cannot establish what information was available to
the model at a particular turn. Repeated content may be stored once and
referenced without losing the ability to reconstruct the exact input.

The scorecard summarizes activity alongside existing efficiency and comparison
results, linking to a detailed chronological audit report backed by a
machine-readable artifact. The audit report is also available for runs that
fail before a comparison scorecard can be produced. Token usage and inference
time are recorded per model turn, execution time per tool call, with totals
per benchmark iteration and run; missing token usage remains unknown and
audit-recording overhead is measured separately.

## Considered Options

- Opt-in capture risks losing the evidence needed to interpret research runs.
- Best-effort capture that silently continues after recording failure leaves
  apparently usable results without their required supporting evidence.
- Final diffs alone conceal repeated edits and reversions, preventing accurate
  measurement of the work performed.

## Consequences

Capture adds storage and timing overhead that implementation must measure.
The audit presents evidence for human Fix Quality Assessment and identifies
model explanations as stated rationale; it does not automatically classify a
change as a real fix or workaround.

The first version provides evidence for assessments made outside the tool;
it does not store researcher annotations. Each iteration presents the problems
given to the model, its activity and edits, and subsequent comparison outcomes
without inferring which individual edit caused a particular outcome.

Alongside the individual modification diffs, the audit includes the net diff
from the candidate's actual source after the initial benchmark reset and
before the model starts to its final source. A Git commit is not assumed to
represent that starting state. Both views are needed because final diffs
support solution review while edit history preserves effort spent on changes
that were later overwritten or reverted.

The audit preserves the runner's actual sequence: lifecycle preparation and
comparison precede the model's edits, which are evaluated by the next
comparison. Edits without a subsequent comparison are labeled not evaluated.
Changes made by lifecycle commands remain visible in the audit and final diff
but are excluded from model edit counts; changes whose origin cannot be
established are labeled unattributed rather than assigned to the model.
