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
from unittest.mock import patch


EXAMPLES = Path(__file__).parent
sys.path.insert(0, str(EXAMPLES))

from adapter import apply_patch, tracked_snapshot
from local_model_adapter import (
    AuditCaptureError,
    AuditWriter,
    NUDGE_PROMPT,
    SYSTEM_PROMPT,
    TOOLS,
    activity_summary,
    reconstruct_input,
    recover_tool_calls,
    execute_tool,
    list_files,
    parse_args,
    run_agent,
    safe_path,
    refresh_activity,
    resolve_temperature,
)

LOCAL_ADAPTER = EXAMPLES / "local_model_adapter.py"
CLI_ADAPTER = EXAMPLES / "coding_agent_adapter.py"
FALLBACK_ADAPTER = EXAMPLES / "adapter.py"


class AdapterExamplesTest(unittest.TestCase):
    def test_recovery_counts_identical_tagged_requests_individually(self) -> None:
        content = (
            '<tool_call>{"name":"list_files","arguments":{"path":"."}}</tool_call>'
            '<tool_call>{"name":"list_files","arguments":{"path":"."}}</tool_call>'
        )

        calls = recover_tool_calls(content)

        self.assertEqual(len(calls), 2)

    def test_local_model_prompts_are_task_neutral_and_advertise_final_tools(self) -> None:
        tool_names = [tool["function"]["name"] for tool in TOOLS]
        self.assertEqual(tool_names, ["list_files", "read_file", "write_file", "str_replace"])

        self.assertIn("You are the coding agent", SYSTEM_PROMPT)
        self.assertIn("Available tools: list_files, read_file, str_replace, and write_file.", SYSTEM_PROMPT)
        self.assertIn("Use only the provided tools.", SYSTEM_PROMPT)
        self.assertIn("Do not build, test, or run verification commands.", SYSTEM_PROMPT)
        self.assertIn("Use str_replace to edit existing files and write_file to create new files.", SYSTEM_PROMPT)
        self.assertIn("When finished, send a plain message with no tool call.", SYSTEM_PROMPT)
        self.assertNotIn("run useful", SYSTEM_PROMPT.lower())
        self.assertNotIn("benchmark loop", SYSTEM_PROMPT.lower())
        self.assertNotIn("stcompare artifact", SYSTEM_PROMPT.lower())
        self.assertNotIn("map each", SYSTEM_PROMPT.lower())
        self.assertNotIn("status code", SYSTEM_PROMPT.lower())
        self.assertNotIn("documentation", SYSTEM_PROMPT.lower())
        self.assertNotIn("annotation", SYSTEM_PROMPT.lower())

        self.assertIn("Use the provided tools if work remains.", NUDGE_PROMPT)
        self.assertIn("When finished, send a plain message with no tool call.", NUDGE_PROMPT)
        self.assertNotIn("verification", NUDGE_PROMPT.lower())
        self.assertNotIn("build", NUDGE_PROMPT.lower())
        self.assertNotIn("test", NUDGE_PROMPT.lower())
        self.assertNotIn("map each", NUDGE_PROMPT.lower())
        self.assertNotIn("status code", NUDGE_PROMPT.lower())

    def test_local_model_audit_does_not_invent_activity_without_tool_events(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            audit_path = root / "benchmark-audit.json"
            request = {
                "audit": {
                    "enabled": True,
                    "path": str(audit_path),
                    "run_id": "run-no-activity",
                }
            }
            metadata = {"agent": "local", "model": "model", "hardware": "machine"}
            writer = AuditWriter.create(request, metadata)
            self.assertIsNotNone(writer)
            created = json.loads(audit_path.read_text(encoding="utf-8"))
            self.assertNotIn("activity", created)

            assert writer is not None
            writer.record_turn_started(
                {"model": "model", "messages": [], "tools": []},
                {**request["audit"], "iteration": 1, "iteration_id": "iteration-1"},
            )
            updated = json.loads(audit_path.read_text(encoding="utf-8"))
            self.assertNotIn("activity", updated)

    def test_local_model_activity_keeps_started_calls_partial_after_capture(self) -> None:
        document = {
            "capture": {"enabled": True, "status": "complete", "complete": True},
            "iterations": [{"id": "iteration-1", "number": 1}],
            "events": [{"type": "model_tool_call", "status": "started", "iteration_id": "iteration-1"}],
        }

        refresh_activity(document)

        self.assertEqual(document["activity"]["status"], "partial")
        self.assertEqual(document["activity"]["model_tool_calls"]["incomplete"], 1)

    def test_local_model_activity_duration_excludes_failed_events(self) -> None:
        summary = activity_summary(
            [
                {"type": "model_tool_call", "status": "completed", "duration_ms": 11},
                {"type": "model_tool_call", "status": "failed", "duration_ms": 7},
                {"type": "adapter_operation", "status": "completed", "duration_ms": 5},
                {"type": "adapter_operation", "status": "failed", "duration_ms": 3},
            ],
            "complete",
        )

        self.assertEqual(summary["model_tool_calls"]["duration_ms"], 11)
        self.assertEqual(summary["adapter_operations"]["duration_ms"], 5)

    def test_local_model_adapter_nudges_only_empty_non_tool_turns(self) -> None:
        responses = [
            {"choices": [{"message": {"role": "assistant", "content": ""}}]},
            {"choices": [{"message": {"role": "assistant", "content": "done"}}]},
        ]

        with tempfile.TemporaryDirectory() as directory, patch(
            "local_model_adapter.post_json", side_effect=responses
        ) as post_json:
            response, _ = run_agent(
                "task",
                Path(directory),
                url="http://model.invalid",
                model="local-model",
                timeout=5,
                max_turns=2,
            )

        self.assertEqual(response, "done")
        self.assertEqual(post_json.call_count, 2)
        second_messages = post_json.call_args_list[1].args[1]["messages"]
        self.assertIn({"role": "user", "content": NUDGE_PROMPT}, second_messages)

    def test_local_model_adapter_ends_on_plain_message_without_nudge(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch(
            "local_model_adapter.post_json",
            return_value={"choices": [{"message": {"role": "assistant", "content": "done"}}]},
        ) as post_json:
            response, _ = run_agent(
                "task",
                Path(directory),
                url="http://model.invalid",
                model="local-model",
                timeout=5,
                max_turns=2,
            )

        self.assertEqual(response, "done")
        self.assertEqual(post_json.call_count, 1)

    def test_local_model_audit_captures_exact_inputs_and_returned_messages(self) -> None:
        requests: list[dict] = []
        responses = [
            {
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
            },
            {
                "choices": [
                    {
                        "message": {
                            "role": "assistant",
                            "content": "I changed the file because the failing behavior required it.",
                        }
                    }
                ],
                "usage": {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
            },
        ]

        def respond(_url: str, payload: dict, _timeout: float, *, metadata: dict | None = None) -> dict:
            del metadata
            requests.append(json.loads(json.dumps(payload)))
            return responses[len(requests) - 1]

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            audit_path = root / "benchmark-audit.json"
            request = {
                "audit": {
                    "enabled": True,
                    "path": str(audit_path),
                    "run_id": "run-1",
                    "candidate": "candidate",
                    "baseline": "baseline",
                }
            }
            metadata = {
                "agent": "local-model",
                "model": "local-model",
                "effort": "high",
                "hardware": "test-machine",
            }
            AuditWriter.create(request, metadata)
            context = {
                **request["audit"],
                "iteration": 2,
                "iteration_id": "iteration-2",
            }
            with patch("local_model_adapter.post_json", side_effect=respond):
                response, usages = run_agent(
                    "exact task",
                    root,
                    url="http://model.invalid",
                    model="local-model",
                    timeout=5,
                    max_turns=2,
                    metadata=metadata,
                    audit=AuditWriter.open(request),
                    audit_context_value=context,
                )

            document = json.loads(audit_path.read_text(encoding="utf-8"))
            self.assertEqual(response, responses[1]["choices"][0]["message"]["content"])
            self.assertEqual(usages, [{"input": 3, "output": 4, "total": 7}, {"input": 5, "output": 2, "total": 7}])
            turn_events = [event for event in document["events"] if event["type"] == "model_turn"]
            self.assertEqual(len(turn_events), 2)
            self.assertEqual(document["iterations"][0]["id"], "iteration-2")
            self.assertEqual(document["iterations"][0]["number"], 2)
            self.assertEqual(
                document["iterations"][0]["turn_ids"],
                ["iteration-2-turn-1", "iteration-2-turn-2"],
            )
            self.assertEqual(document["activity"]["model_tool_calls"]["count"], 1)
            self.assertEqual(document["activity"]["adapter_operations"]["count"], 1)
            self.assertEqual([event["sequence"] for event in turn_events], [1, 4])
            for index, event in enumerate(turn_events):
                self.assertEqual(event["run_id"], "run-1")
                self.assertEqual(reconstruct_input(document, event), requests[index])
                self.assertEqual(event["status"], "completed")
                self.assertEqual(event["returned_messages"], [responses[index]["choices"][0]["message"]])
                self.assertEqual(event["sampling"], {"temperature": 0.0, "top_p": 1})

            self.assertTrue(document["shared_content"])
            self.assertTrue(
                any("input_content_references" in event for event in document["events"])
            )

            serialized = audit_path.read_text(encoding="utf-8")
            self.assertNotIn("STBENCH_LOCAL_MODEL_API_KEY", serialized)
            self.assertNotIn("Authorization", serialized)

    def test_local_model_audit_writes_start_before_inference_and_retains_model_error(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            audit_path = root / "benchmark-audit.json"
            request = {
                "audit": {
                    "enabled": True,
                    "path": str(audit_path),
                    "run_id": "run-1",
                    "candidate": "candidate",
                    "baseline": "baseline",
                }
            }
            metadata = {"agent": "local", "model": "model", "hardware": "machine"}
            AuditWriter.create(request, metadata)
            writer = AuditWriter.open(request)
            assert writer is not None
            event = writer.record_turn_started(
                {"model": "model", "messages": [], "tools": []},
                {**request["audit"], "iteration": 1, "iteration_id": "iteration-1"},
            )
            pending = json.loads(audit_path.read_text(encoding="utf-8"))
            self.assertEqual(pending["events"][0]["status"], "started")
            self.assertEqual(pending["events"][0]["turn_id"], event["turn_id"])

            writer.record_turn_failed(event, TimeoutError("inference timed out"))
            failed = json.loads(audit_path.read_text(encoding="utf-8"))
            self.assertEqual(failed["events"][0]["status"], "failed")
            self.assertIn("inference timed out", failed["events"][0]["error"])

    def test_local_model_audit_counts_each_call_and_separates_adapter_operations(self) -> None:
        responses = [
            {
                "choices": [
                    {
                        "message": {
                            "role": "assistant",
                            "tool_calls": [
                                {
                                    "id": "read-1",
                                    "type": "function",
                                    "function": {
                                        "name": "list_files",
                                        "arguments": json.dumps({"path": "."}),
                                    },
                                },
                                {
                                    "id": "unknown-1",
                                    "type": "function",
                                    "function": {
                                        "name": "missing_tool",
                                        "arguments": "{}",
                                    },
                                },
                                {
                                    "id": "malformed-1",
                                    "type": "function",
                                    "function": {
                                        "name": "read_file",
                                        "arguments": "{not-json",
                                    },
                                },
                            ],
                        }
                    }
                ]
            },
            {"choices": [{"message": {"role": "assistant", "content": "done"}}]},
        ]

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            audit_path = root / "benchmark-audit.json"
            request = {
                "audit": {
                    "enabled": True,
                    "path": str(audit_path),
                    "run_id": "run-activity",
                    "candidate": "candidate",
                    "baseline": "baseline",
                }
            }
            metadata = {"agent": "local", "model": "model", "hardware": "machine"}
            AuditWriter.create(request, metadata)

            with patch("local_model_adapter.post_json", side_effect=responses):
                response, _ = run_agent(
                    "task",
                    root,
                    url="http://model.invalid",
                    model="model",
                    timeout=5,
                    max_turns=2,
                    metadata=metadata,
                    audit=AuditWriter.open(request),
                    audit_context_value={
                        **request["audit"],
                        "iteration": 1,
                        "iteration_id": "iteration-1",
                    },
                )

            document = json.loads(audit_path.read_text(encoding="utf-8"))
            calls = [event for event in document["events"] if event["type"] == "model_tool_call"]
            operations = [event for event in document["events"] if event["type"] == "adapter_operation"]
            self.assertEqual(response, "done")
            self.assertEqual(len(calls), 3)
            self.assertEqual(len(operations), 3)
            self.assertEqual(
                [event["status"] for event in calls],
                ["completed", "failed", "failed"],
            )
            self.assertEqual(document["activity"]["model_tool_calls"]["count"], 3)
            self.assertEqual(document["activity"]["model_tool_calls"]["completed"], 1)
            self.assertEqual(document["activity"]["model_tool_calls"]["failed"], 2)
            self.assertEqual(document["activity"]["adapter_operations"]["count"], 3)
            self.assertEqual(calls[0]["arguments"], {"path": "."})
            self.assertEqual(calls[1]["tool_name"], "missing_tool")
            self.assertEqual(calls[2]["arguments"], "{not-json")
            self.assertIn("unknown tool", calls[1]["error"])
            self.assertIn("invalid JSON arguments", calls[2]["error"])
            self.assertTrue(all(event["turn_id"] == "iteration-1-turn-1" for event in calls))
            self.assertTrue(all(event["model_tool_call_id"] in {call["id"] for call in calls} for event in operations))
            self.assertTrue(all("duration_ms" in event for event in calls + operations))

    def test_local_model_audit_keeps_one_recovered_call_with_provenance(self) -> None:
        responses = [
            {
                "choices": [
                    {
                        "message": {
                            "role": "assistant",
                            "content": '<tool_call>{"name":"list_files","arguments":{"path":"."}}</tool_call>',
                        }
                    }
                ]
            },
            {"choices": [{"message": {"role": "assistant", "content": "done"}}]},
        ]

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            audit_path = root / "benchmark-audit.json"
            request = {
                "audit": {
                    "enabled": True,
                    "path": str(audit_path),
                    "run_id": "run-recovery",
                    "candidate": "candidate",
                    "baseline": "baseline",
                }
            }
            metadata = {"agent": "local", "model": "model", "hardware": "machine"}
            AuditWriter.create(request, metadata)
            with patch("local_model_adapter.post_json", side_effect=responses):
                run_agent(
                    "task",
                    root,
                    url="http://model.invalid",
                    model="model",
                    timeout=5,
                    max_turns=2,
                    metadata=metadata,
                    audit=AuditWriter.open(request),
                    audit_context_value={
                        **request["audit"],
                        "iteration": 1,
                        "iteration_id": "iteration-1",
                    },
                )

            document = json.loads(audit_path.read_text(encoding="utf-8"))
            calls = [event for event in document["events"] if event["type"] == "model_tool_call"]
            self.assertEqual(len(calls), 1)
            self.assertEqual(calls[0]["provenance"], "model_text_recovery")
            self.assertEqual(document["activity"]["model_tool_calls"]["count"], 1)
            self.assertEqual(document["activity"]["adapter_operations"]["count"], 1)

    def test_local_model_audit_recovers_malformed_tagged_request_as_failure(self) -> None:
        responses = [
            {"choices": [{"message": {"role": "assistant", "content": "<tool_call>{not-json</tool_call>"}}]},
            {"choices": [{"message": {"role": "assistant", "content": "done"}}]},
        ]

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            audit_path = root / "benchmark-audit.json"
            request = {
                "audit": {
                    "enabled": True,
                    "path": str(audit_path),
                    "run_id": "run-malformed-recovery",
                    "candidate": "candidate",
                    "baseline": "baseline",
                }
            }
            metadata = {"agent": "local", "model": "model", "hardware": "machine"}
            AuditWriter.create(request, metadata)
            with patch("local_model_adapter.post_json", side_effect=responses):
                run_agent(
                    "task",
                    root,
                    url="http://model.invalid",
                    model="model",
                    timeout=5,
                    max_turns=2,
                    metadata=metadata,
                    audit=AuditWriter.open(request),
                    audit_context_value={
                        **request["audit"],
                        "iteration": 1,
                        "iteration_id": "iteration-1",
                    },
                )

            document = json.loads(audit_path.read_text(encoding="utf-8"))
            calls = [event for event in document["events"] if event["type"] == "model_tool_call"]
            self.assertEqual(len(calls), 1)
            self.assertEqual(calls[0]["status"], "failed")
            self.assertEqual(calls[0]["provenance"], "model_text_recovery")
            self.assertEqual(calls[0]["arguments"], "{not-json")
            self.assertIn("invalid JSON arguments", calls[0]["error"])

    def test_local_model_audit_keeps_started_activity_incomplete(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            audit_path = root / "benchmark-audit.json"
            request = {
                "audit": {
                    "enabled": True,
                    "path": str(audit_path),
                    "run_id": "run-incomplete",
                    "candidate": "candidate",
                    "baseline": "baseline",
                }
            }
            metadata = {"agent": "local", "model": "model", "hardware": "machine"}
            writer = AuditWriter.create(request, metadata)
            assert writer is not None
            turn = writer.record_turn_started(
                {"model": "model", "messages": [], "tools": []},
                {**request["audit"], "iteration": 1, "iteration_id": "iteration-1"},
            )
            writer.record_tool_call_started(
                {
                    "id": "call-1",
                    "type": "function",
                    "function": {"name": "read_file", "arguments": json.dumps({"path": "api.py"})},
                },
                turn,
                "model_response",
            )

            document = json.loads(audit_path.read_text(encoding="utf-8"))
            call = next(event for event in document["events"] if event["type"] == "model_tool_call")
            self.assertEqual(call["status"], "started")
            self.assertNotIn("result", call)
            self.assertEqual(document["activity"]["model_tool_calls"]["incomplete"], 1)
            self.assertEqual(document["activity"]["status"], "partial")

    def test_local_model_audit_storage_failure_is_explicit(self) -> None:
        request = {
            "audit": {
                "enabled": True,
                "path": str(Path(tempfile.gettempdir()) / "benchmark-audit.json"),
                "run_id": "run-1",
            }
        }
        metadata = {"agent": "local", "model": "model", "hardware": "machine"}
        with patch("local_model_adapter.os.replace", side_effect=OSError("disk full")):
            with self.assertRaisesRegex(AuditCaptureError, "cannot write audit artifact"):
                AuditWriter.create(request, metadata)

    def test_local_model_preflight_enables_audit_without_inference(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            audit_path = Path(directory) / "benchmark-audit.json"
            completed = subprocess.run(
                [sys.executable, str(LOCAL_ADAPTER)],
                cwd=directory,
                input=json.dumps(
                    {
                        "agent": "local-model",
                        "model": "local-code-model",
                        "effort": "high",
                        "hardware": "machine",
                        "preflight": True,
                        "audit": {
                            "enabled": True,
                            "path": str(audit_path),
                            "run_id": "run-1",
                            "candidate": "candidate",
                            "baseline": "baseline",
                        },
                    }
                ),
                text=True,
                capture_output=True,
                check=False,
            )

            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertEqual(json.loads(completed.stdout)["status"], "ok")
            document = json.loads(audit_path.read_text(encoding="utf-8"))
            self.assertEqual(document["run"]["id"], "run-1")
            self.assertEqual(document["run"]["effort"], "high")
            self.assertTrue(document["capture"]["enabled"])
            self.assertEqual(document["events"], [])

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
