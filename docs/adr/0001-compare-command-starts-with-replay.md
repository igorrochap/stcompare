# Compare command starts with replay

`stcompare campaign compare <candidate>` is the user-facing command for candidate comparison, even though the first implementation only replays the baseline HAR transcript and writes a candidate response log. Keeping replay under `compare` avoids introducing a separate command that would later be folded into comparison reporting, while ticket 6 can extend the same command with stable JSON and Markdown comparison reports.
