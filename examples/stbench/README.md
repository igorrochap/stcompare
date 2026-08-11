# stbench adapter examples

`stbench` owns the benchmark loop. An adapter is only the per-iteration
delivery boundary:

1. `stbench` sends one JSON request on stdin: the `agent`, `model`, and
   `hardware` metadata from `stcompare.yml`, plus the rendered, versioned task
   instruction and the compact `agentreport.View`.

   ```json
   {"agent":"codex","model":"gpt-5","hardware":"local-machine","instruction":"...","view":{"actionable":[]}}
   ```

2. The adapter delivers that instruction in the selected agent's envelope and
   edits the candidate in its current working directory.
3. The adapter writes one JSON result on stdout:

   ```json
   {
     "status": "ok",
     "message": "",
     "response": "short audit text",
     "tokens": {"input": 12, "output": 34, "total": 46}
   }
   ```

   Report `"tokens": null` when the agent does not expose token usage.

Before the first comparison, `stbench` sends `{"preflight": true}`. The
adapter must return an `ok` result for this no-op request without invoking the
model or editing the candidate. This lets `stbench` verify that the configured
adapter command is runnable before spending model time.

## Optional process reuse

Set `reuse_process: true` (or pass `--reuse-process`) only for an adapter that
implements the negotiated line-delimited session protocol. During preflight,
stbench includes `"reuse_process": true`; the adapter opts in by returning
`"reuse_process": true` in its successful result and then keeping stdin/stdout
open. It receives one complete JSON request and returns one complete JSON
result per line thereafter.

Each line still contains the same metadata, rendered instruction, and compact
view that a cold invocation would receive. No previous prompt, reasoning,
tool transcript, or session history is added. This is process reuse, not
context carry; context carry is out of scope because it would confound agent
comparisons and can exceed local-model context windows. A stateless adapter
must leave the capability false, in which case stbench falls back to the cold
path and records `process_reuse: false`.

The adapter must not run the compare/fix loop, replace the task with its own
prompt, or read `junit.xml`/HAR/VCR/NDJSON files. The instruction already
contains the fixed stbench task and compact view from `compare --format agent`.
The request metadata is the source of truth for the executed agent and model;
use adapter flags only for deliberate overrides.

## Which example to use

| File | Audience | Edits candidate in place | External dependency |
| --- | --- | --- | --- |
| [`local_model_adapter.py`](local_model_adapter.py) | Study subjects using an on-prem model | Yes, through read/write/command tools | An OpenAI-compatible local inference server |
| [`coding_agent_adapter.py`](coding_agent_adapter.py) | Research engineers testing with Codex or Claude Code | Yes, through the installed CLI | `codex` or `claude` |
| [`adapter.py`](adapter.py) | Explicit fallback when neither path is available | Yes, after validating a generated diff | OpenAI cloud API and Git |

All scripts use only the Python standard library. Keep `_protocol.py` beside
the adapter outside the API repository; do not copy adapter support files into
the source tree being modified by the agent. `stbench init` keeps lifecycle
scripts in `.local/stbench/` and adds that directory to `.gitignore`.

These four Python files are also the canonical sources embedded into the
`stbench` binary. Running `stbench init` installs byte-identical copies in
`.local/stbench/adapters/`.

## Local-model adapter

Start an on-prem inference server that exposes a compatible
`/v1/chat/completions` endpoint with tool-call support, then configure the
adapter command. Keep the server URL, model, hardware, timeout, and turn limit
in the `stbench` configuration:

```yaml
stbench:
  agent: local-model
  model: my-local-code-model
  hardware: local-machine
  adapter: python /absolute/path/to/stcompare/examples/stbench/local_model_adapter.py --url http://127.0.0.1:8000/v1/chat/completions --timeout 300 --max-turns 20
```

Use an absolute script path when `source_dir` is not the repository root.
If the local server requires authentication, keep the credential in
`STBENCH_LOCAL_MODEL_API_KEY`.

The scaffold exposes `list_files`, `read_file`, `write_file`, and shell-free
`run_command` tools. Paths are confined to the API source tree, and managed
`.local/stbench` and `.local/stcompare` state is hidden from file listing and
write tools. `stbench init` uses `.local/stbench` by default, while an external
state directory can be selected explicitly. The adapter sums usage reported by
each inference response and returns `null` if a response omits usage.

## Coding-agent CLI adapter

This is a first-class, supported path for research engineers doing local
sanity checks, demos, or out-of-target comparisons. It invokes one installed
CLI per stbench iteration; the CLI can use its own native file and command
tools, but the adapter itself does not own the loop. Keep the adapter
implementation outside the API repository; the CLI runs from the API
repository so the agent can modify its real source.

Codex:

```yaml
stbench:
  agent: codex
  model: gpt-5
  hardware: local-machine
  adapter: python /absolute/path/to/stcompare/examples/stbench/coding_agent_adapter.py --timeout 1800
```

Claude Code:

```yaml
stbench:
  agent: claude
  model: claude-model
  hardware: local-machine
  adapter: python /absolute/path/to/stcompare/examples/stbench/coding_agent_adapter.py --timeout 1800
```

The adapter uses Codex's non-interactive `codex exec --json` mode with
`--sandbox workspace-write`, and Claude Code's `claude -p --output-format
json` mode. The selected CLI must be available on `PATH`. The CLI adapter
reports usage when the CLI emits it and otherwise returns `tokens: null`. For
an alternate launcher, pass `--command` with the command and arguments to
invoke; the request's `agent` metadata selects the matching output format.
Use `--agent` only as an explicit override.

The Codex CLI details are documented in [Codex non-interactive mode](https://developers.openai.com/codex/noninteractive/), and the Claude Code
flags are documented in the [Claude Code CLI reference](https://docs.anthropic.com/en/docs/claude-code/cli-usage).

## Cloud API fallback

[`adapter.py`](adapter.py) is intentionally a fallback, not the default study
path. It requires `OPENAI_API_KEY`, sends a snapshot of tracked text files to
the cloud Responses API, asks for a unified diff, validates the diff, and
applies it with `git apply`.

```sh
export OPENAI_API_KEY=...
```

Configure it without changing the runner:

```yaml
stbench:
  agent: cloud-fallback
  model: gpt-5
  hardware: cloud-runner
  adapter: python /absolute/path/to/stcompare/examples/stbench/adapter.py --responses-url https://api.openai.com/v1/responses --timeout 600 --max-snapshot-bytes 1000000
```

Before using this fallback, account for its limitations:

- candidate source leaves the on-prem environment and is subject to cloud API
  retention, access, and size policies;
- only tracked files are included, and the snapshot is capped by
  `--max-snapshot-bytes` (1 MiB by default);
- the model must express edits as a valid unified diff, so large or binary
  repositories are a poor fit;
- `git apply --check` validates the patch before mutation, but this remains a
  less capable editing path than a native coding-agent CLI or local tool
  scaffold.
- the raw model response is returned in the adapter result and archived in the
  benchmark record, but it does not make the cloud-generated patch auditable
  without also retaining the surrounding run artifacts.

The cloud fallback uses the [OpenAI Responses API](https://platform.openai.com/docs/quickstart/make-your-first-api-request). It is provided so the
adapter protocol remains usable when the first-class local and CLI options
are unavailable; it is not the recommended path for the on-prem study.
