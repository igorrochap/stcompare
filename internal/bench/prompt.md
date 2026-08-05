Task prompt {{.Prompt.ID}}@{{.Prompt.Version}}:

Use the comparison result below to fix the candidate source. Apply the necessary fixes and preserve existing behavior outside the reported problems.

Do not build or test the app. Only apply the code changes and exit; the harness runs the build and tests itself.

Comparison view:
{{.ComparisonView}}
