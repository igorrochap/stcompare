Task prompt {{.Prompt.ID}}@{{.Prompt.Version}}:

Use the comparison result below to fix the candidate source. Apply the necessary fixes and preserve existing behavior outside the reported problems.

Do not build or test the app. Only apply the code changes and exit; the harness runs the build and tests itself.

Each entry in the actionable list is one Problem Group. `count` is the number of failing cases in the group. `sample` is one concrete case with truncated request and response bodies. `refs` are interaction numbers in `{{.ComparisonPath}}`.

Comparison view:
{{.ComparisonView}}
