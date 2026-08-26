from __future__ import annotations

import json
import os
import socketserver
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler
from pathlib import Path


EXAMPLES = Path(__file__).parent
sys.path.insert(0, str(EXAMPLES))

from adapter import apply_patch, tracked_snapshot
from local_model_adapter import execute_tool, list_files, parse_args, safe_path, resolve_temperature

LOCAL_ADAPTER = EXAMPLES / "local_model_adapter.py"
CLI_ADAPTER = EXAMPLES / "coding_agent_adapter.py"
FALLBACK_ADAPTER = EXAMPLES / "adapter.py"


class AdapterExamplesTest(unittest.TestCase):
    def test_local_model_temperature_resolution_is_explicit_and_bounded(self) -> None:
        tests = [
            ("default independent of effort", None, {"effort": "luna-high"}, 0.0),
            ("campaign", None, {"temperature": 0.35}, 0.35),
            ("flag overrides campaign", 0.9, {"temperature": 0.35}, 0.9),
        ]
        for name, flag, metadata, expected in tests:
            with self.subTest(name=name):
                self.assertEqual(resolve_temperature(flag, metadata), expected)

        self.assertIsNone(parse_args([]).temperature)
        for value in (-0.01, 2.01):
            with self.subTest(value=value):
                with self.assertRaisesRegex(ValueError, "temperature.*between 0 and 2"):
                    resolve_temperature(value, {})

    def test_cloud_snapshot_excludes_managed_tool_state(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            (root / "api.py").write_text("print('api')\n", encoding="utf-8")
            state_file = root / ".local" / "stbench" / "stop.sh"
            state_file.parent.mkdir(parents=True)
            state_file.write_text("echo stop\n", encoding="utf-8")
            subprocess.run(["git", "add", "."], cwd=root, check=True)

            snapshot = tracked_snapshot(root, 10_000)

            self.assertIn("--- api.py ---", snapshot)
            self.assertNotIn(".local/stbench", snapshot)

    def test_cloud_patch_rejects_managed_tool_state(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            state_file = root / ".local" / "stbench" / "stop.sh"
            state_file.parent.mkdir(parents=True)
            state_file.write_text("echo stop\n", encoding="utf-8")
            subprocess.run(["git", "add", "."], cwd=root, check=True)
            patch = (
                "diff --git a/.local/stbench/stop.sh b/.local/stbench/stop.sh\n"
                "--- a/.local/stbench/stop.sh\n"
                "+++ b/.local/stbench/stop.sh\n"
                "@@ -1 +1 @@\n"
                "-echo stop\n"
                "+echo changed\n"
            )

            with self.assertRaises(ValueError):
                apply_patch(root, patch)

            self.assertEqual(state_file.read_text(encoding="utf-8"), "echo stop\n")

    def test_local_model_tools_hide_and_reject_managed_tool_state(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "api.py").write_text("print('api')\n", encoding="utf-8")
            state_file = root / ".local" / "stbench" / "stop.sh"
            state_file.parent.mkdir(parents=True)
            state_file.write_text("echo stop\n", encoding="utf-8")

            listing = list_files(root, ".")

            self.assertEqual(listing["files"], ["api.py"])
            with self.assertRaises(ValueError):
                safe_path(root, ".local/stbench/stop.sh")

    def test_local_model_str_replace_applies_unique_whitespace_exact_match(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "api.py"
            target.write_text("def answer():\n    return 41\n", encoding="utf-8")

            result = execute_tool(
                "str_replace",
                {
                    "path": "api.py",
                    "old_string": "def answer():\n    return 41",
                    "new_string": "def answer():\n    return 42",
                },
                root,
            )

            self.assertEqual(result["ok"], True)
            self.assertEqual(target.read_text(encoding="utf-8"), "def answer():\n    return 42\n")

    def test_local_model_str_replace_returns_distinct_recoverable_match_errors(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "api.py").write_text("return 1\nreturn 2\n", encoding="utf-8")

            no_match = execute_tool(
                "str_replace",
                {"path": "api.py", "old_string": "return 3", "new_string": "return 4"},
                root,
            )
            multiple_matches = execute_tool(
                "str_replace",
                {"path": "api.py", "old_string": "return", "new_string": "yield"},
                root,
            )

            self.assertFalse(no_match["ok"])
            self.assertEqual(no_match["error_code"], "str_replace_no_match")
            self.assertIn("no match", no_match["error"])
            self.assertFalse(multiple_matches["ok"])
            self.assertEqual(multiple_matches["error_code"], "str_replace_multiple_matches")
            self.assertIn("2 matches", multiple_matches["error"])
            self.assertEqual((root / "api.py").read_text(encoding="utf-8"), "return 1\nreturn 2\n")

    def test_local_model_write_file_is_new_file_only(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "new.txt"
            target.write_text("original\n", encoding="utf-8")

            result = execute_tool(
                "write_file",
                {"path": "new.txt", "content": "replacement\n"},
                root,
            )

            self.assertFalse(result["ok"])
            self.assertEqual(result["error_code"], "write_file_existing")
            self.assertIn("only create new files", result["error"])
            self.assertEqual(target.read_text(encoding="utf-8"), "original\n")

    def test_local_model_str_replace_missing_and_escaping_paths_are_recoverable(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)

            missing = execute_tool(
                "str_replace",
                {"path": "missing.py", "old_string": "a", "new_string": "b"},
                root,
            )
            escaping = execute_tool(
                "str_replace",
                {"path": "../outside.py", "old_string": "a", "new_string": "b"},
                root,
            )

            self.assertFalse(missing["ok"])
            self.assertEqual(missing["error_code"], "file_not_found")
            self.assertFalse(escaping["ok"])
            self.assertEqual(escaping["error_code"], "path_error")

    def test_local_model_timeout_defaults_to_600_seconds_and_is_overridable(self) -> None:
        self.assertEqual(parse_args([]).timeout, 600)
        self.assertEqual(parse_args(["--timeout", "7.5"]).timeout, 7.5)

    def test_adapter_examples_accept_no_op_preflight_without_running_agent(self) -> None:
        request = json.dumps({"preflight": True, "reuse_process": True})
        with tempfile.TemporaryDirectory() as directory:
            for adapter in (LOCAL_ADAPTER, CLI_ADAPTER, FALLBACK_ADAPTER):
                with self.subTest(adapter=adapter.name):
                    completed = subprocess.run(
                        [sys.executable, str(adapter)],
                        cwd=directory,
                        input=request,
                        text=True,
                        capture_output=True,
                        check=False,
                    )

                    self.assertEqual(completed.returncode, 0, completed.stderr)
                    result = json.loads(completed.stdout)
                    self.assertEqual(result["status"], "ok")
                    self.assertFalse(result["reuse_process"])

    def test_local_model_adapter_reports_flag_override_during_preflight(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            completed = subprocess.run(
                [sys.executable, str(LOCAL_ADAPTER), "--temperature", "0.9"],
                cwd=directory,
                input=json.dumps(
                    {
                        "agent": "local-model",
                        "model": "local-code-model",
                        "hardware": "m4-pro",
                        "temperature": 0.35,
                        "preflight": True,
                    }
                ),
                text=True,
                capture_output=True,
                check=False,
            )

            self.assertEqual(completed.returncode, 0, completed.stderr)
            result = json.loads(completed.stdout)
            self.assertEqual(result["status"], "ok")
            self.assertEqual(result["temperature"], 0.9)

    def test_coding_agent_adapter_delivers_instruction_and_reports_usage(self) -> None:
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryDirectory() as fake_bin:
            candidate = Path(directory)
            fake_cli = Path(fake_bin) / "codex"
            fake_cli.write_text(
                "#!/bin/sh\n"
                "cat > received-instruction.txt\n"
                "printf '%s\\n' \"$@\" > received-args.txt\n"
                "printf '%s\\n' \"$STBENCH_HARDWARE\" > received-hardware.txt\n"
                "printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"edited candidate\"}}'\n"
                "printf '%s\\n' '{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":7,\"total_tokens\":12}}'\n",
                encoding="utf-8",
            )
            fake_cli.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = fake_bin + os.pathsep + environment["PATH"]

            completed = subprocess.run(
                [sys.executable, str(CLI_ADAPTER), "--timeout", "5"],
                cwd=candidate,
                input=json.dumps(
                    {
                        "agent": "codex",
                        "model": "gpt-5",
                        "hardware": "m4-pro",
                        "instruction": "exact task",
                        "view": {"actionable": []},
                    }
                ),
                text=True,
                capture_output=True,
                env=environment,
                check=False,
            )

            self.assertEqual(completed.returncode, 0, completed.stderr)
            result = json.loads(completed.stdout)
            self.assertEqual(result["status"], "ok")
            self.assertEqual(result["response"], "edited candidate")
            self.assertEqual(result["tokens"], {"input": 5, "output": 7, "total": 12})
            self.assertEqual((candidate / "received-instruction.txt").read_text(), "exact task")
            self.assertIn("--model\ngpt-5\n", (candidate / "received-args.txt").read_text())
            self.assertEqual((candidate / "received-hardware.txt").read_text(), "m4-pro\n")

    def test_local_model_adapter_uses_tools_to_edit_candidate(self) -> None:
        calls: list[dict] = []
        received_metadata: list[dict[str, str | None]] = []

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:  # noqa: N802 - stdlib handler API
                length = int(self.headers["Content-Length"])
                calls.append(json.loads(self.rfile.read(length)))
                received_metadata.append(
                    {
                        "agent": self.headers.get("X-Stbench-Agent"),
                        "model": self.headers.get("X-Stbench-Model"),
                        "hardware": self.headers.get("X-Stbench-Hardware"),
                    }
                )
                if len(calls) == 1:
                    response = {
                        "choices": [
                            {
                                "message": {
                                    "role": "assistant",
                                    "tool_calls": [
                                        {
                                            "id": "call-1",
                                            "type": "function",
                                            "function": {
                                                "name": "write_file",
                                                "arguments": json.dumps(
                                                    {"path": "fixed.txt", "content": "fixed\n"}
                                                ),
                                            },
                                        }
                                    ],
                                }
                            }
                        ],
                        "usage": {"prompt_tokens": 3, "completion_tokens": 4, "total_tokens": 7},
                    }
                else:
                    response = {
                        "choices": [{"message": {"role": "assistant", "content": "done"}}],
                        "usage": {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
                    }
                encoded = json.dumps(response).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

            def log_message(self, *_: object) -> None:
                return

        class Server(socketserver.ThreadingMixIn, socketserver.TCPServer):
            allow_reuse_address = True

        with Server(("127.0.0.1", 0), Handler) as server, tempfile.TemporaryDirectory() as directory:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            environment = os.environ.copy()
            url = f"http://127.0.0.1:{server.server_address[1]}/v1/chat/completions"
            completed = subprocess.run(
                [
                    sys.executable,
                    str(LOCAL_ADAPTER),
                    "--temperature",
                    "0.35",
                    "--url",
                    url,
                    "--timeout",
                    "5",
                    "--max-turns",
                    "20",
                ],
                cwd=directory,
                input=json.dumps(
                    {
                        "agent": "local-model",
                        "model": "local-code-model",
                        "hardware": "m4-pro",
                        "instruction": "exact task",
                        "view": {"actionable": []},
                    }
                ),
                text=True,
                capture_output=True,
                env=environment,
                check=False,
            )
            server.shutdown()
            thread.join(timeout=5)

            self.assertEqual(completed.returncode, 0, completed.stderr)
            result = json.loads(completed.stdout)
            self.assertEqual(result["status"], "ok")
            self.assertEqual(result["response"], "done")
            self.assertEqual(result["temperature"], 0.35)
            self.assertEqual(result["tokens"], {"input": 8, "output": 6, "total": 14})
            self.assertEqual((Path(directory) / "fixed.txt").read_text(), "fixed\n")
            self.assertEqual(calls[0]["model"], "local-code-model")
            self.assertEqual([call["temperature"] for call in calls], [0.35, 0.35])
            for call in calls:
                self.assertNotIn("top_p", call)
            self.assertEqual(
                received_metadata[0],
                {"agent": "local-model", "model": "local-code-model", "hardware": "m4-pro"},
            )
            self.assertEqual(calls[0]["messages"][1]["content"], "exact task")
            self.assertEqual(calls[1]["messages"][-1]["role"], "tool")

    def test_local_model_adapter_recovers_str_replace_from_assistant_text(self) -> None:
        calls: list[dict] = []

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:  # noqa: N802 - stdlib handler API
                length = int(self.headers["Content-Length"])
                calls.append(json.loads(self.rfile.read(length)))
                if len(calls) == 1:
                    response = {
                        "choices": [
                            {
                                "message": {
                                    "role": "assistant",
                                    "content": (
                                        "<tool_call>"
                                        '{"name":"str_replace","arguments":'
                                        '{"path":"api.py","old_string":"return 1",'
                                        '"new_string":"return 2"}}'
                                        "</tool_call>"
                                    ),
                                }
                            }
                        ]
                    }
                else:
                    response = {"choices": [{"message": {"role": "assistant", "content": "done"}}]}
                encoded = json.dumps(response).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

            def log_message(self, *_: object) -> None:
                return

        class Server(socketserver.ThreadingMixIn, socketserver.TCPServer):
            allow_reuse_address = True

        with Server(("127.0.0.1", 0), Handler) as server, tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "api.py").write_text("return 1\n", encoding="utf-8")
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            url = f"http://127.0.0.1:{server.server_address[1]}/v1/chat/completions"
            completed = subprocess.run(
                [sys.executable, str(LOCAL_ADAPTER), "--url", url, "--timeout", "5"],
                cwd=directory,
                input=json.dumps(
                    {
                        "agent": "local-model",
                        "model": "local-code-model",
                        "hardware": "m4-pro",
                        "instruction": "exact task",
                        "view": {"actionable": []},
                    }
                ),
                text=True,
                capture_output=True,
                check=False,
            )
            server.shutdown()
            thread.join(timeout=5)

            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertEqual(json.loads(completed.stdout)["response"], "done")
            self.assertEqual((root / "api.py").read_text(encoding="utf-8"), "return 2\n")
            self.assertEqual(calls[1]["messages"][-1]["role"], "tool")

    def test_local_model_adapter_elides_older_file_and_edit_content_from_history(self) -> None:
        calls: list[dict] = []
        source = "".join(f"line {index:05d}\n" for index in range(5_000))
        replacement = source.replace("line 02500", "edited 02500")

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:  # noqa: N802 - stdlib handler API
                length = int(self.headers["Content-Length"])
                calls.append(json.loads(self.rfile.read(length)))
                if len(calls) == 1:
                    message = {
                        "role": "assistant",
                        "tool_calls": [
                            {
                                "id": "read-1",
                                "type": "function",
                                "function": {
                                    "name": "read_file",
                                    "arguments": json.dumps({"path": "api.py"}),
                                },
                            }
                        ],
                    }
                elif len(calls) == 2:
                    message = {
                        "role": "assistant",
                        "tool_calls": [
                            {
                                "id": "replace-1",
                                "type": "function",
                                "function": {
                                    "name": "str_replace",
                                    "arguments": json.dumps(
                                        {
                                            "path": "api.py",
                                            "old_string": source,
                                            "new_string": replacement,
                                        }
                                    ),
                                },
                            }
                        ],
                    }
                elif len(calls) == 3:
                    message = {
                        "role": "assistant",
                        "tool_calls": [
                            {
                                "id": "command-1",
                                "type": "function",
                                "function": {
                                    "name": "run_command",
                                    "arguments": json.dumps({"command": ["true"]}),
                                },
                            }
                        ],
                    }
                else:
                    message = {"role": "assistant", "content": "done"}
                encoded = json.dumps({"choices": [{"message": message}]}).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

            def log_message(self, *_: object) -> None:
                return

        class Server(socketserver.ThreadingMixIn, socketserver.TCPServer):
            allow_reuse_address = True

        with Server(("127.0.0.1", 0), Handler) as server, tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "api.py").write_text(source, encoding="utf-8")
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            url = f"http://127.0.0.1:{server.server_address[1]}/v1/chat/completions"
            completed = subprocess.run(
                [sys.executable, str(LOCAL_ADAPTER), "--url", url, "--timeout", "5", "--max-turns", "5"],
                cwd=directory,
                input=json.dumps(
                    {
                        "agent": "local-model",
                        "model": "local-code-model",
                        "hardware": "m4-pro",
                        "instruction": "exact task",
                        "view": {"actionable": []},
                    }
                ),
                text=True,
                capture_output=True,
                check=False,
            )
            server.shutdown()
            thread.join(timeout=5)

            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertEqual(json.loads(completed.stdout)["response"], "done")
            self.assertEqual((root / "api.py").read_text(encoding="utf-8"), replacement)
            self.assertEqual(len(calls), 4)

            fourth_messages = calls[3]["messages"]
            older_history = json.dumps(fourth_messages[2:6])
            self.assertIn("[read_file content elided from history]", older_history)
            self.assertNotIn(source, older_history)
            self.assertNotIn(replacement, older_history)
            older_edit = json.dumps(fourth_messages[4:6])
            self.assertIn("[edit content elided from history]", older_edit)
            self.assertNotIn(source, older_edit)
            self.assertNotIn(replacement, older_edit)
            self.assertEqual(fourth_messages[-2]["tool_calls"][0]["function"]["name"], "run_command")

    def test_coding_agent_adapter_supports_claude_code_json_output(self) -> None:
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryDirectory() as fake_bin:
            fake_cli = Path(fake_bin) / "claude"
            fake_cli.write_text(
                "#!/bin/sh\n"
                "cat >/dev/null\n"
                "printf '%s\\n' \"$@\" > received-args.txt\n"
                "printf '%s\\n' '{\"type\":\"result\",\"is_error\":false,\"result\":\"claude edited candidate\",\"usage\":{\"input_tokens\":11,\"output_tokens\":13,\"total_tokens\":24}}'\n",
                encoding="utf-8",
            )
            fake_cli.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = fake_bin + os.pathsep + environment["PATH"]

            completed = subprocess.run(
                [sys.executable, str(CLI_ADAPTER), "--timeout", "5"],
                cwd=directory,
                input=json.dumps(
                    {
                        "agent": "claude",
                        "model": "claude-model",
                        "hardware": "m4-pro",
                        "instruction": "exact task",
                        "view": {},
                    }
                ),
                text=True,
                capture_output=True,
                env=environment,
                check=False,
            )

            self.assertEqual(completed.returncode, 0, completed.stderr)
            result = json.loads(completed.stdout)
            self.assertEqual(result["status"], "ok")
            self.assertEqual(result["response"], "claude edited candidate")
            self.assertEqual(result["tokens"], {"input": 11, "output": 13, "total": 24})
            self.assertIn("--model\nclaude-model\n", (Path(directory) / "received-args.txt").read_text())


if __name__ == "__main__":
    unittest.main()
