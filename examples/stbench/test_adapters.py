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
LOCAL_ADAPTER = EXAMPLES / "local_model_adapter.py"
CLI_ADAPTER = EXAMPLES / "coding_agent_adapter.py"


class AdapterExamplesTest(unittest.TestCase):
    def test_coding_agent_adapter_delivers_instruction_and_reports_usage(self) -> None:
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryDirectory() as fake_bin:
            candidate = Path(directory)
            fake_cli = Path(fake_bin) / "codex"
            fake_cli.write_text(
                "#!/bin/sh\n"
                "cat > received-instruction.txt\n"
                "printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"edited candidate\"}}'\n"
                "printf '%s\\n' '{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":7,\"total_tokens\":12}}'\n",
                encoding="utf-8",
            )
            fake_cli.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = fake_bin + os.pathsep + environment["PATH"]

            completed = subprocess.run(
                [sys.executable, str(CLI_ADAPTER), "--agent", "codex", "--timeout", "5"],
                cwd=candidate,
                input=json.dumps({"instruction": "exact task", "view": {"actionable": []}}),
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

    def test_local_model_adapter_uses_tools_to_edit_candidate(self) -> None:
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
                    "--url",
                    url,
                    "--model",
                    "local-code-model",
                    "--timeout",
                    "5",
                    "--max-turns",
                    "20",
                ],
                cwd=directory,
                input=json.dumps({"instruction": "exact task", "view": {"actionable": []}}),
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
            self.assertEqual(result["tokens"], {"input": 8, "output": 6, "total": 14})
            self.assertEqual((Path(directory) / "fixed.txt").read_text(), "fixed\n")
            self.assertEqual(calls[0]["messages"][1]["content"], "exact task")
            self.assertEqual(calls[1]["messages"][-1]["role"], "tool")

    def test_coding_agent_adapter_supports_claude_code_json_output(self) -> None:
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryDirectory() as fake_bin:
            fake_cli = Path(fake_bin) / "claude"
            fake_cli.write_text(
                "#!/bin/sh\n"
                "cat >/dev/null\n"
                "printf '%s\\n' '{\"type\":\"result\",\"is_error\":false,\"result\":\"claude edited candidate\",\"usage\":{\"input_tokens\":11,\"output_tokens\":13,\"total_tokens\":24}}'\n",
                encoding="utf-8",
            )
            fake_cli.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = fake_bin + os.pathsep + environment["PATH"]

            completed = subprocess.run(
                [sys.executable, str(CLI_ADAPTER), "--agent", "claude", "--timeout", "5"],
                cwd=directory,
                input=json.dumps({"instruction": "exact task", "view": {}}),
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


if __name__ == "__main__":
    unittest.main()
