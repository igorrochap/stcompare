#!/usr/bin/env python3
"""A local-model adapter for an OpenAI-compatible inference server.

The model edits the candidate through a small tool-enabled scaffold.  It never
receives a repository snapshot and the adapter never applies a model-generated
patch: read/write tools mutate the candidate directory directly.
"""

from __future__ import annotations

import argparse
import copy
import datetime as dt
import difflib
import hashlib
import json
import math
import os
import re
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

from _protocol import (
    aggregate_usages,
    cap_text,
    emit_error,
    emit_result,
    handle_preflight,
    is_managed_state_path,
    is_preflight_request,
    metadata_headers,
    read_requests,
    request_metadata,
    usage_to_tokens,
)


DEFAULT_URL = "http://127.0.0.1:8000/v1/chat/completions"
DEFAULT_TIMEOUT_SECONDS = 600
DEFAULT_MAX_TURNS = 20
DEFAULT_TEMPERATURE = 0.0
MAX_TEMPERATURE = 2.0
MAX_FILE_BYTES = 256_000
READ_FILE_HISTORY_PLACEHOLDER = "[read_file content elided from history]"
EDIT_HISTORY_PLACEHOLDER = "[edit content elided from history]"
EDIT_TOOL_NAMES = frozenset({"str_replace", "write_file"})

SYSTEM_PROMPT = """You are the coding agent inside a stbench adapter.
Available tools: list_files, read_file, str_replace, and write_file.
Use only the provided tools. Do not build, test, or run verification commands.
Use str_replace to edit existing files and write_file to create new files. Make
edits directly in the candidate.
When finished, send a plain message with no tool call.
"""

NUDGE_PROMPT = """Use the provided tools if work remains.
When finished, send a plain message with no tool call.
"""

TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "list_files",
            "description": "List candidate files below a relative directory.",
            "parameters": {
                "type": "object",
                "properties": {"path": {"type": "string", "default": "."}},
                "additionalProperties": False,
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "read_file",
            "description": "Read a UTF-8 text file in the candidate.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string"},
                    "max_bytes": {"type": "integer", "default": MAX_FILE_BYTES},
                },
                "required": ["path"],
                "additionalProperties": False,
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "write_file",
            "description": "Create a new UTF-8 candidate file and its parent directories.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string"},
                    "content": {"type": "string"},
                },
                "required": ["path", "content"],
                "additionalProperties": False,
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "str_replace",
            "description": "Replace one unique, exact UTF-8 substring in an existing candidate file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string"},
                    "old_string": {"type": "string"},
                    "new_string": {"type": "string"},
                },
                "required": ["path", "old_string", "new_string"],
                "additionalProperties": False,
            },
        },
    },
]
TOOL_NAMES = frozenset(
    tool["function"]["name"] for tool in TOOLS if isinstance(tool.get("function"), dict)
)


class ToolError(ValueError):
    """An expected tool failure that can be returned to the model."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class AuditCaptureError(RuntimeError):
    """A required audit event could not be durably saved."""


class AuditWriter:
    """Persist model-turn and activity events without changing the model request."""

    def __init__(self, path: Path, document: dict[str, Any]) -> None:
        self.path = path
        self.document = document

    @classmethod
    def create(cls, request: dict[str, Any], metadata: dict[str, Any]) -> "AuditWriter | None":
        context = audit_context(request)
        if context is None:
            return None
        run_id = required_audit_value(context, "run_id")
        path = Path(required_audit_value(context, "path"))
        document = {
            "schema_version": "1",
            "run": {
                "id": run_id,
                "candidate": str(context.get("candidate", "")),
                "baseline": str(context.get("baseline", "")),
                "agent": metadata["agent"],
                "model": metadata["model"],
                "effort": str(metadata.get("effort", "")),
                "hardware": metadata["hardware"],
                "started_at": utc_now(),
            },
            "capture": {
                "enabled": True,
                "status": "in_progress",
                "complete": False,
            },
            "iterations": [],
            "shared_content": {},
            "file_modifications": [],
            "events": [],
        }
        writer = cls(path, document)
        writer._write()
        return writer

    @classmethod
    def open(cls, request: dict[str, Any]) -> "AuditWriter | None":
        context = audit_context(request)
        if context is None:
            return None
        path = Path(required_audit_value(context, "path"))
        try:
            document = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as error:
            raise AuditCaptureError(f"cannot read audit artifact {path}: {error}") from error
        if not isinstance(document, dict) or document.get("schema_version") != "1":
            raise AuditCaptureError(f"audit artifact {path} has an unsupported schema")
        return cls(path, document)

    def record_turn_started(self, payload: dict[str, Any], context: dict[str, Any]) -> dict[str, Any]:
        iteration = int(context.get("iteration", 0))
        iteration_id = required_audit_value(context, "iteration_id")
        turn_number = (
            sum(
                event.get("type") == "model_turn" and event.get("iteration_id") == iteration_id
                for event in self.document["events"]
            )
            + 1
        )
        turn_id = f"{iteration_id}-turn-{turn_number}"
        captured_input, input_references = capture_shared_content(
            payload,
            self.document.setdefault("shared_content", {}),
        )
        event = {
            "sequence": len(self.document["events"]) + 1,
            "type": "model_turn",
            "run_id": required_audit_value(context, "run_id"),
            "iteration_id": iteration_id,
            "iteration": iteration,
            "turn_id": turn_id,
            "status": "started",
            "started_at": utc_now(),
            "sampling": sampling_settings(payload),
            "input": captured_input,
        }
        if input_references:
            event["input_content_references"] = input_references
        self.document["events"].append(event)
        self._ensure_iteration(iteration_id, iteration, turn_id)
        self._write()
        event["_started_monotonic"] = time.monotonic()
        return event

    def record_turn_completed(self, event: dict[str, Any], response: dict[str, Any]) -> None:
        event["status"] = "completed"
        event["ended_at"] = utc_now()
        event["duration_ms"] = elapsed_milliseconds(event)
        event["returned"] = copy.deepcopy(response)
        event["returned_messages"] = returned_messages(response)
        event["tokens"] = usage_to_tokens(response.get("usage"))
        event.pop("_started_monotonic", None)
        self._write()

    def record_turn_failed(self, event: dict[str, Any], error: Exception) -> None:
        event["status"] = "failed"
        event["ended_at"] = utc_now()
        event["duration_ms"] = elapsed_milliseconds(event)
        event["error"] = str(error)
        event.pop("_started_monotonic", None)
        self._write()

    def record_tool_call_started(
        self,
        call: Any,
        turn_event: dict[str, Any],
        provenance: str,
    ) -> dict[str, Any]:
        tool_name, arguments = tool_call_details(call)
        tool_call_id = "unknown"
        if isinstance(call, dict):
            tool_call_id = call.get("id", "unknown")
        event = {
            "sequence": len(self.document["events"]) + 1,
            "type": "model_tool_call",
            "id": f"model-tool-call-{len(self.document['events']) + 1}",
            "run_id": turn_event["run_id"],
            "iteration_id": turn_event["iteration_id"],
            "iteration": turn_event["iteration"],
            "turn_id": turn_event["turn_id"],
            "tool_call_id": tool_call_id,
            "tool_name": tool_name,
            "edit_attempt": tool_name in EDIT_TOOL_NAMES,
            "provenance": provenance,
            "arguments": copy.deepcopy(arguments),
            "request": copy.deepcopy(call),
            "status": "started",
            "started_at": utc_now(),
        }
        self.document["events"].append(event)
        self._write()
        event["_started_monotonic"] = time.monotonic()
        return event

    def record_tool_call_completed(self, event: dict[str, Any], result: dict[str, Any]) -> None:
        finish_activity_event(event, result)
        self._write()

    def record_tool_call_failed(self, event: dict[str, Any], error: Exception) -> None:
        finish_activity_event(event, error=error)
        self._write()

    def record_adapter_operation_started(self, tool_event: dict[str, Any]) -> dict[str, Any]:
        event = {
            "sequence": len(self.document["events"]) + 1,
            "type": "adapter_operation",
            "id": f"adapter-operation-{len(self.document['events']) + 1}",
            "run_id": tool_event["run_id"],
            "iteration_id": tool_event["iteration_id"],
            "iteration": tool_event["iteration"],
            "turn_id": tool_event["turn_id"],
            "model_tool_call_id": tool_event["id"],
            "tool_call_id": tool_event["tool_call_id"],
            "tool_name": tool_event["tool_name"],
            "edit_attempt": tool_event.get("edit_attempt", False),
            "operation": "execute_model_tool_call",
            "arguments": copy.deepcopy(tool_event["arguments"]),
            "status": "started",
            "started_at": utc_now(),
        }
        self.document["events"].append(event)
        self._write()
        event["_started_monotonic"] = time.monotonic()
        return event

    def record_adapter_operation_completed(
        self,
        event: dict[str, Any],
        result: dict[str, Any],
    ) -> None:
        finish_activity_event(event, result)
        self._record_file_modifications(event, result)
        self._write()

    def record_adapter_operation_failed(self, event: dict[str, Any], error: Exception) -> None:
        finish_activity_event(event, error=error)
        self._write()

    def _record_file_modifications(
        self,
        operation_event: dict[str, Any],
        result: dict[str, Any],
    ) -> None:
        if result.get("ok") is not True:
            return
        changes = result.get("file_modifications", [])
        if not isinstance(changes, list) or not changes:
            return
        modifications = self.document.setdefault("file_modifications", [])
        modification_ids: list[str] = []
        for change in changes:
            if not isinstance(change, dict):
                continue
            modification = {
                "sequence": len(modifications) + 1,
                "id": f"file-modification-{len(modifications) + 1}",
                "path": change.get("path", ""),
                "operation": operation_event.get("operation", ""),
                "tool_name": operation_event.get("tool_name", ""),
                "run_id": operation_event.get("run_id", ""),
                "model_tool_call_id": operation_event.get("model_tool_call_id", ""),
                "adapter_operation_id": operation_event.get("id", ""),
                "turn_id": operation_event.get("turn_id", ""),
                "iteration_id": operation_event.get("iteration_id", ""),
                "iteration": operation_event.get("iteration", 0),
                "before": copy.deepcopy(change.get("before")),
                "after": copy.deepcopy(change.get("after")),
                "diff": change.get("diff", ""),
                "created": change.get("before") is None,
            }
            modifications.append(modification)
            modification_ids.append(modification["id"])
        if modification_ids:
            operation_event["file_modification_ids"] = modification_ids

    def _ensure_iteration(self, iteration_id: str, number: int, turn_id: str) -> None:
        for iteration in self.document["iterations"]:
            if iteration.get("id") == iteration_id:
                iteration["turn_ids"].append(turn_id)
                return
        self.document["iterations"].append({"id": iteration_id, "number": number, "turn_ids": [turn_id]})

    def _write(self) -> None:
        try:
            refresh_activity(self.document)
            self.path.parent.mkdir(parents=True, exist_ok=True)
            temporary = self.path.with_name(f".{self.path.name}.{os.getpid()}.tmp")
            with temporary.open("w", encoding="utf-8", newline="") as output:
                json.dump(self.document, output, ensure_ascii=False, indent=2)
                output.write("\n")
                output.flush()
                os.fsync(output.fileno())
            os.replace(temporary, self.path)
        except OSError as error:
            try:
                temporary.unlink()
            except (UnboundLocalError, OSError):
                pass
            raise AuditCaptureError(f"cannot write audit artifact {self.path}: {error}") from error


def audit_context(request: dict[str, Any]) -> dict[str, Any] | None:
    context = request.get("audit")
    if not isinstance(context, dict) or context.get("enabled") is not True:
        return None
    return context


def required_audit_value(context: dict[str, Any], name: str) -> str:
    value = context.get(name)
    if not isinstance(value, str) or not value:
        raise AuditCaptureError(f"audit context {name} is required")
    return value


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def sampling_settings(payload: dict[str, Any]) -> dict[str, Any]:
    settings = {"temperature": payload.get("temperature")}
    if "top_p" in payload:
        settings["top_p"] = payload["top_p"]
    return settings


def returned_messages(response: dict[str, Any]) -> list[dict[str, Any]]:
    messages: list[dict[str, Any]] = []
    choices = response.get("choices")
    if not isinstance(choices, list):
        return messages
    for choice in choices:
        if not isinstance(choice, dict) or not isinstance(choice.get("message"), dict):
            continue
        messages.append(copy.deepcopy(choice["message"]))
    return messages


def elapsed_milliseconds(event: dict[str, Any]) -> int:
    started = event.get("_started_monotonic")
    if not isinstance(started, float):
        return 0
    return round((time.monotonic() - started) * 1000)


def tool_call_details(call: Any) -> tuple[str, Any]:
    if not isinstance(call, dict):
        return "unknown", None
    function = call.get("function")
    if not isinstance(function, dict):
        return "unknown", None
    name = function.get("name")
    tool_name = "unknown"
    if isinstance(name, str) and name:
        tool_name = name
    arguments = function.get("arguments")
    if isinstance(arguments, str):
        try:
            return tool_name, json.loads(arguments)
        except json.JSONDecodeError:
            return tool_name, arguments
    return tool_name, arguments


def finish_activity_event(
    event: dict[str, Any],
    result: dict[str, Any] | None = None,
    *,
    error: Exception | None = None,
) -> None:
    successful = error is None and result is not None and result.get("ok") is True
    event["status"] = "completed"
    if not successful:
        event["status"] = "failed"
    event["ended_at"] = utc_now()
    event["duration_ms"] = elapsed_milliseconds(event)
    if result is not None:
        event["result"] = copy.deepcopy(result)
        if event["status"] == "failed":
            event["error"] = str(result.get("error", "tool execution failed"))
    if error is not None:
        event["error"] = str(error)
    event.pop("_started_monotonic", None)


def activity_summary(
    events: list[dict[str, Any]],
    status: str,
    file_modifications: list[dict[str, Any]] | None = None,
    iteration_id: str = "",
) -> dict[str, Any]:
    summary = {
        "status": status,
        "model_tool_calls": activity_counts(),
        "adapter_operations": activity_counts(),
        "edit_attempts": 0,
        "file_modifications": 0,
    }
    for event in events:
        event_type = event.get("type")
        if event_type == "model_tool_call":
            counts = summary["model_tool_calls"]
            if event.get("edit_attempt") is True or event.get("tool_name") in EDIT_TOOL_NAMES:
                summary["edit_attempts"] += 1
        elif event_type == "adapter_operation":
            counts = summary["adapter_operations"]
            if event.get("edit_attempt") is True and not event.get("model_tool_call_id"):
                summary["edit_attempts"] += 1
        else:
            continue
        counts["count"] += 1
        event_status = event.get("status")
        if event_status == "completed":
            counts["completed"] += 1
            counts["duration_ms"] += int(event.get("duration_ms", 0) or 0)
        elif event_status == "failed":
            counts["failed"] += 1
        else:
            counts["incomplete"] += 1
    recorded_modifications = file_modifications if isinstance(file_modifications, list) else []
    for modification in recorded_modifications:
        if not isinstance(modification, dict):
            continue
        if iteration_id and modification.get("iteration_id") != iteration_id:
            continue
        summary["file_modifications"] += 1
    return summary


def activity_counts() -> dict[str, int]:
    return {"count": 0, "completed": 0, "failed": 0, "incomplete": 0, "duration_ms": 0}


def is_activity_event(event: dict[str, Any]) -> bool:
    return event.get("type") in {"model_tool_call", "adapter_operation"}


def event_is_incomplete(event: dict[str, Any]) -> bool:
    event_type = event.get("type")
    if event_type == "model_turn":
        return event.get("status") != "completed"
    if not is_activity_event(event):
        return False
    return event.get("status") not in {"completed", "failed"}


def refresh_activity(document: dict[str, Any]) -> None:
    events = document.get("events", [])
    modifications = document.get("file_modifications", [])
    has_activity = any(is_activity_event(event) for event in events)
    has_modifications = isinstance(modifications, list) and bool(modifications)
    if not has_activity and not has_modifications:
        document.pop("activity", None)
        for iteration in document.get("iterations", []):
            iteration.pop("activity", None)
        return
    capture = document.get("capture", {})
    status = "partial"
    capture_is_complete = capture.get("status") == "complete" and capture.get("complete") is True
    if capture_is_complete and not any(event_is_incomplete(event) for event in events):
        status = "complete"
    document["activity"] = activity_summary(events, status, modifications)
    for iteration in document.get("iterations", []):
        iteration_id = iteration.get("id")
        iteration_events = [event for event in events if event.get("iteration_id") == iteration_id]
        iteration["activity"] = activity_summary(iteration_events, status, modifications, iteration_id)


def capture_shared_content(
    payload: dict[str, Any],
    shared_content: dict[str, Any],
) -> tuple[dict[str, Any], list[dict[str, str]]]:
    captured = copy.deepcopy(payload)
    references: list[dict[str, str]] = []

    def visit(value: Any, path: str) -> None:
        if isinstance(value, dict):
            for key, child in value.items():
                child_path = f"{path}/{escape_json_pointer(key)}"
                if key == "content":
                    reference_id = shared_content_id(child)
                    if reference_id not in shared_content:
                        shared_content[reference_id] = copy.deepcopy(child)
                    value[key] = None
                    references.append({"path": child_path, "id": reference_id})
                    continue
                visit(child, child_path)
            return
        if isinstance(value, list):
            for index, child in enumerate(value):
                visit(child, f"{path}/{index}")

    visit(captured, "")
    return captured, references


def reconstruct_input(document: dict[str, Any], event: dict[str, Any]) -> dict[str, Any]:
    reconstructed = copy.deepcopy(event["input"])
    shared_content = document.get("shared_content", {})
    if not isinstance(shared_content, dict):
        raise AuditCaptureError("audit shared_content must be an object")
    for reference in event.get("input_content_references", []):
        if not isinstance(reference, dict):
            raise AuditCaptureError("audit input content reference must be an object")
        reference_id = reference.get("id")
        path = reference.get("path")
        if not isinstance(reference_id, str) or reference_id not in shared_content:
            raise AuditCaptureError(f"audit content reference {reference_id!r} is missing")
        if not isinstance(path, str):
            raise AuditCaptureError("audit input content reference path is required")
        set_json_pointer(reconstructed, path, copy.deepcopy(shared_content[reference_id]))
    return reconstructed


def shared_content_id(value: Any) -> str:
    serialized = json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    digest = hashlib.sha256(serialized.encode("utf-8")).hexdigest()
    return f"content-{digest}"


def escape_json_pointer(value: str) -> str:
    return value.replace("~", "~0").replace("/", "~1")


def set_json_pointer(document: Any, pointer: str, value: Any) -> None:
    if not pointer.startswith("/"):
        raise AuditCaptureError(f"audit JSON pointer {pointer!r} is invalid")
    current = document
    parts = [part.replace("~1", "/").replace("~0", "~") for part in pointer[1:].split("/")]
    for part in parts[:-1]:
        if isinstance(current, dict) and part in current:
            current = current[part]
            continue
        if isinstance(current, list) and part.isdigit() and int(part) < len(current):
            current = current[int(part)]
            continue
        raise AuditCaptureError(f"audit JSON pointer {pointer!r} does not exist")
    if not parts:
        raise AuditCaptureError("audit JSON pointer cannot replace the root")
    last = parts[-1]
    if isinstance(current, dict) and last in current:
        current[last] = value
        return
    if isinstance(current, list) and last.isdigit() and int(last) < len(current):
        current[int(last)] = value
        return
    raise AuditCaptureError(f"audit JSON pointer {pointer!r} does not exist")


TOOL_CALL_TAG = re.compile(
    r"<tool_call>(?P<plain_body>.*?)</tool_call>"
    r"|<\|tool_call\|>(?P<special_body>.*?)<\|/tool_call\|>"
    r"|<\|tool_call\|>(?P<alternate_body>.*?)</\|tool_call\|>",
    flags=re.DOTALL,
)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default=DEFAULT_URL, help="OpenAI-compatible chat completions URL")
    parser.add_argument(
        "--model",
        help="Explicit model override; defaults to the stbench request metadata",
    )
    parser.add_argument(
        "--temperature",
        type=float,
        default=None,
        help="Sampling temperature override; defaults to campaign metadata or 0",
    )
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT_SECONDS, help="HTTP timeout in seconds")
    parser.add_argument("--max-turns", type=int, default=DEFAULT_MAX_TURNS, help="Maximum tool-use turns")
    return parser.parse_args(argv)


def resolve_temperature(
    flag_temperature: float | None,
    metadata: dict[str, Any] | None,
) -> float:
    """Resolve and validate the one sampling temperature used for a run."""

    if flag_temperature is not None:
        return validate_temperature(flag_temperature, "--temperature")
    if metadata is not None and metadata.get("temperature") is not None:
        return validate_temperature(metadata["temperature"], "campaign temperature")
    return DEFAULT_TEMPERATURE


def validate_temperature(value: Any, source: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError(f"{source} must be a number between 0 and {MAX_TEMPERATURE:g}")
    temperature = float(value)
    if not math.isfinite(temperature) or temperature < 0 or temperature > MAX_TEMPERATURE:
        raise ValueError(f"{source} must be between 0 and {MAX_TEMPERATURE:g}")
    return temperature


def main(argv: list[str] | None = None) -> int:
    try:
        settings = parse_args(argv)

        for request, instruction in read_requests():
            try:
                if is_preflight_request(request):
                    if all(name in request for name in ("agent", "model", "hardware")):
                        metadata = request_metadata(request)
                        temperature = resolve_temperature(settings.temperature, metadata)
                        AuditWriter.create(request, metadata)
                        emit_result(status="ok", temperature=temperature)
                    else:
                        handle_preflight(request)
                    continue

                metadata = request_metadata(request)
                temperature = resolve_temperature(settings.temperature, metadata)
                audit = AuditWriter.open(request)
                response, usages = run_agent(
                    instruction,
                    Path.cwd(),
                    url=settings.url,
                    model=settings.model or metadata["model"],
                    temperature=temperature,
                    metadata=metadata,
                    timeout=settings.timeout,
                    max_turns=settings.max_turns,
                    audit=audit,
                    audit_context_value=audit_context(request),
                )
                emit_result(
                    status="ok",
                    response=response,
                    tokens=aggregate_usages(usages),
                    temperature=temperature,
                )
            except AuditCaptureError as error:
                emit_error(str(error), audit_error=str(error))
            except (OSError, ValueError, RuntimeError) as error:
                emit_error(str(error))
        return 0
    except (OSError, ValueError, RuntimeError) as error:
        emit_error(str(error))
        return 0
    except Exception as error:  # pragma: no cover - last-resort protocol guard
        emit_error(f"local-model adapter failed: {error}")
        return 0


def _env_flag(name: str) -> bool:
    """Read a boolean opt-in environment flag."""

    return os.environ.get(name, "").strip().lower() not in ("", "0", "false", "no")


def _env_int(name: str, default: int) -> int:
    try:
        return int(os.environ.get(name, "").strip() or default)
    except ValueError:
        return default


def _debug(line: str) -> None:
    """Write one trace line to stderr when STBENCH_ADAPTER_DEBUG is set.

    stbench forwards adapter stderr, so the trace appears in the run output. It
    never touches stdout, which carries the adapter's JSON protocol messages.
    """

    if _env_flag("STBENCH_ADAPTER_DEBUG"):
        print(line, file=sys.stderr, flush=True)


def _debug_turn(turn_number: int, max_turns: int, content: Any, tool_calls: Any) -> None:
    if not _env_flag("STBENCH_ADAPTER_DEBUG"):
        return
    if isinstance(tool_calls, list) and tool_calls:
        calls = []
        for call in tool_calls:
            function = call.get("function") if isinstance(call, dict) else None
            name = function.get("name") if isinstance(function, dict) else None
            arguments = function.get("arguments") if isinstance(function, dict) else None
            calls.append(f"{name}({str(arguments)[:200]})")
        summary = "tool_calls=" + "; ".join(calls)
    else:
        text = content if isinstance(content, str) else repr(content)
        summary = f"content[{len(text)}]={text[:300]!r}"
    _debug(f"[turn {turn_number}/{max_turns}] {summary}")


def _debug_tool(call: Any, tool_result: Any) -> None:
    if not _env_flag("STBENCH_ADAPTER_DEBUG"):
        return
    function = call.get("function") if isinstance(call, dict) else None
    name = function.get("name") if isinstance(function, dict) else "?"
    ok = tool_result.get("ok") if isinstance(tool_result, dict) else None
    error = tool_result.get("error") if isinstance(tool_result, dict) else None
    detail = f" error={error!r}" if error else ""
    _debug(f"    -> {name}: ok={ok}{detail}")


def _tool_call_signature(tool_calls: list[Any]) -> str:
    """Stable identity for a turn's tool calls, used to detect a stalled loop."""

    parts: list[str] = []
    for call in tool_calls:
        function = call.get("function") if isinstance(call, dict) else None
        parts.append(
            json.dumps(
                [
                    function.get("name") if isinstance(function, dict) else None,
                    function.get("arguments") if isinstance(function, dict) else None,
                ],
                sort_keys=True,
                ensure_ascii=False,
            )
        )
    return "\n".join(parts)


def run_agent(
    instruction: str,
    root: Path,
    *,
    url: str,
    model: str,
    timeout: float,
    max_turns: int,
    metadata: dict[str, Any] | None = None,
    temperature: float | None = None,
    audit: AuditWriter | None = None,
    audit_context_value: dict[str, Any] | None = None,
) -> tuple[str, list[dict[str, int] | None]]:
    if timeout <= 0:
        raise ValueError("--timeout must be positive")
    if max_turns < 1:
        raise ValueError("--max-turns must be positive")
    if not model.strip():
        raise ValueError("--model must not be empty")
    if temperature is None:
        temperature = resolve_temperature(None, metadata)
    temperature = validate_temperature(temperature, "temperature")

    messages: list[dict[str, Any]] = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": instruction},
    ]
    usages: list[dict[str, int] | None] = []
    final_response = ""
    current_turn_start = len(messages)

    # Stop a run that is stuck re-issuing the same failing tool call rather than
    # burning every remaining turn. Set STBENCH_ADAPTER_MAX_REPEATS=0 to disable.
    max_repeats = _env_int("STBENCH_ADAPTER_MAX_REPEATS", 4)
    stall_signature: str | None = None
    stall_count = 0
    stalled = False

    for turn_index in range(max_turns):
        compact_history(messages, current_turn_start)
        payload = {
            "model": model,
            "temperature": temperature,
            "messages": messages,
            "tools": TOOLS,
            "tool_choice": "auto",
        }
        if temperature == DEFAULT_TEMPERATURE:
            payload["top_p"] = 1
        turn_event = None
        if audit is not None:
            if audit_context_value is None:
                raise AuditCaptureError("audit context is required for a model turn")
            turn_event = audit.record_turn_started(payload, audit_context_value)
        try:
            result = post_json(url, payload, timeout, metadata=metadata)
        except Exception as error:
            if audit is not None and turn_event is not None:
                audit.record_turn_failed(turn_event, error)
            raise
        if audit is not None and turn_event is not None:
            audit.record_turn_completed(turn_event, result)
        usages.append(usage_to_tokens(result.get("usage")))
        choices = result.get("choices")
        if not isinstance(choices, list) or not choices or not isinstance(choices[0], dict):
            raise RuntimeError("local model response has no choices")
        message = choices[0].get("message")
        if not isinstance(message, dict):
            raise RuntimeError("local model response has no assistant message")

        content = message.get("content")
        tool_calls = message.get("tool_calls")
        _debug_turn(turn_index + 1, max_turns, content, tool_calls)
        if not isinstance(tool_calls, list) or not tool_calls:
            recovered_calls = recover_tool_calls(content)
            if recovered_calls:
                message = dict(message)
                message["tool_calls"] = recovered_calls
                tool_calls = recovered_calls
                tool_call_provenance = "model_text_recovery"
            else:
                messages.append(message)
                if isinstance(content, str) and content.strip():
                    final_response = content
                    return final_response, usages
                messages.append({"role": "user", "content": NUDGE_PROMPT})
                current_turn_start = len(messages) - 2
                continue
        else:
            tool_call_provenance = "model_response"

        assistant_message_start = len(messages)
        messages.append(message)

        any_success = False
        for call in tool_calls:
            tool_event = None
            operation_event = None
            if audit is not None and turn_event is not None:
                tool_event = audit.record_tool_call_started(call, turn_event, tool_call_provenance)
                operation_event = audit.record_adapter_operation_started(tool_event)
            try:
                tool_result = execute_model_tool_call(call, root)
            except Exception as error:
                if audit is not None and operation_event is not None:
                    audit.record_adapter_operation_failed(operation_event, error)
                if audit is not None and tool_event is not None:
                    audit.record_tool_call_failed(tool_event, error)
                raise
            if audit is not None and operation_event is not None:
                audit.record_adapter_operation_completed(operation_event, tool_result)
            if audit is not None and tool_event is not None:
                audit.record_tool_call_completed(tool_event, tool_result)
            _debug_tool(call, tool_result)
            if isinstance(tool_result, dict) and tool_result.get("ok"):
                any_success = True
            model_tool_result = tool_result_for_model(tool_result)
            messages.append(
                {
                    "role": "tool",
                    "tool_call_id": call.get("id", "unknown") if isinstance(call, dict) else "unknown",
                    "content": json.dumps(model_tool_result, ensure_ascii=False),
                }
            )
        current_turn_start = assistant_message_start

        turn_signature = _tool_call_signature(tool_calls)
        if not any_success and turn_signature == stall_signature:
            stall_count += 1
        else:
            stall_count = 0
            stall_signature = turn_signature
        if max_repeats > 0 and stall_count >= max_repeats:
            _debug(
                f"[stall] same failing tool call repeated {stall_count + 1}x; "
                f"stopping at turn {turn_index + 1}/{max_turns}"
            )
            stalled = True
            break

    if stalled:
        return final_response, usages

    raise RuntimeError(f"local model reached the {max_turns}-turn limit")


def execute_model_tool_call(call: Any, root: Path) -> dict[str, Any]:
    """Validate one structured or recovered model call and execute it."""

    if not isinstance(call, dict):
        return tool_error("invalid_tool_call", "local model returned an invalid tool call")
    function = call.get("function")
    if not isinstance(function, dict):
        return tool_error("invalid_tool_call", "local model tool call has no function")
    name = function.get("name")
    arguments = function.get("arguments", {})
    if isinstance(arguments, str):
        try:
            arguments = json.loads(arguments)
        except json.JSONDecodeError as error:
            return tool_error("invalid_tool_arguments", f"local model returned invalid JSON arguments: {error}")
    if not isinstance(name, str) or not isinstance(arguments, dict):
        return tool_error("invalid_tool_arguments", "local model returned invalid tool arguments")
    return execute_tool(name, arguments, root)


def recover_tool_calls(content: Any) -> list[dict[str, Any]]:
    """Recover JSON tool requests, including unknown names, emitted as text."""

    if not isinstance(content, str) or not content.strip():
        return []

    tagged_candidates = [
        match.group("plain_body") or match.group("special_body") or match.group("alternate_body")
        for match in TOOL_CALL_TAG.finditer(content)
    ]
    decoder = json.JSONDecoder()
    recovered: list[dict[str, Any]] = []
    seen: set[str] = set()

    def add_recovered_call(
        name: str,
        arguments: Any,
        *,
        serialized_arguments: str | None = None,
        deduplicate: bool = True,
    ) -> None:
        identity = json.dumps([name, arguments], sort_keys=True, ensure_ascii=False)
        if deduplicate and identity in seen:
            return
        if deduplicate:
            seen.add(identity)
        encoded_arguments = serialized_arguments
        if encoded_arguments is None:
            encoded_arguments = json.dumps(arguments, ensure_ascii=False)
        recovered.append(
            {
                "id": f"recovered-{len(recovered) + 1}",
                "type": "function",
                "function": {
                    "name": name,
                    "arguments": encoded_arguments,
                },
            }
        )

    def add_candidate(candidate: Any, *, deduplicate: bool = True) -> None:
        if isinstance(candidate, list):
            for item in candidate:
                add_candidate(item, deduplicate=deduplicate)
            return
        if not isinstance(candidate, dict):
            return
        name = candidate.get("name", candidate.get("tool"))
        arguments = candidate.get(
            "arguments",
            candidate.get("parameters", candidate.get("args")),
        )
        if isinstance(arguments, str):
            try:
                arguments = json.loads(arguments)
            except json.JSONDecodeError:
                return
        if not isinstance(name, str) or not name.strip() or not isinstance(arguments, dict):
            return
        add_recovered_call(name, arguments, deduplicate=deduplicate)

    def add_malformed_candidate(candidate: Any, *, deduplicate: bool = True) -> None:
        if isinstance(candidate, dict):
            name = candidate.get("name", candidate.get("tool", "unknown"))
            arguments = candidate.get(
                "arguments",
                candidate.get("parameters", candidate.get("args")),
            )
        else:
            name = "unknown"
            arguments = candidate
        if not isinstance(name, str) or not name.strip():
            name = "unknown"
        serialized_arguments = arguments if isinstance(arguments, str) else None
        add_recovered_call(
            name,
            arguments,
            serialized_arguments=serialized_arguments,
            deduplicate=deduplicate,
        )

    for candidate in tagged_candidates:
        try:
            decoded = json.loads(candidate.strip())
        except json.JSONDecodeError:
            add_malformed_candidate(candidate)
            continue
        before = len(recovered)
        add_candidate(decoded, deduplicate=False)
        if len(recovered) == before:
            add_malformed_candidate(decoded, deduplicate=False)

    if not tagged_candidates:
        try:
            add_candidate(json.loads(content.strip()))
        except json.JSONDecodeError:
            pass

    if not tagged_candidates:
        # A prose wrapper may surround the JSON object. Scan each possible
        # object without interpreting arbitrary JSON: only a tool-shaped name
        # plus an arguments object qualifies as a recovered tool call.
        for start, character in enumerate(content):
            if character != "{":
                continue
            try:
                candidate, _ = decoder.raw_decode(content[start:])
            except json.JSONDecodeError:
                continue
            add_candidate(candidate)

        # Some instruction-tuned servers put a function name in the text and
        # follow it with a JSON argument object, for example
        # ``str_replace({"path": "api.py", ...})``. Recover that form too,
        # but only for names in the registered tool set.
        for name in sorted(TOOL_NAMES):
            for match in re.finditer(rf"(?<![\w-]){re.escape(name)}\s*(?:\(|:)?\s*", content):
                start = match.end()
                if start >= len(content) or content[start] != "{":
                    continue
                try:
                    arguments, _ = decoder.raw_decode(content[start:])
                except json.JSONDecodeError:
                    continue
                add_candidate({"name": name, "arguments": arguments})

    return recovered


def compact_history(messages: list[dict[str, Any]], current_turn_start: int) -> None:
    """Elide bulky file payloads from messages older than the current turn."""

    if _env_flag("STBENCH_ADAPTER_NO_COMPACT"):
        _debug("[compact] disabled via STBENCH_ADAPTER_NO_COMPACT")
        return

    for message in messages[:current_turn_start]:
        role = message.get("role")
        if role == "assistant":
            compact_edit_arguments(message)
        elif role == "tool":
            compact_read_file_result(message)


def compact_edit_arguments(message: dict[str, Any]) -> None:
    tool_calls = message.get("tool_calls")
    if not isinstance(tool_calls, list):
        return
    echoed_content = message.get("content")
    redactions: list[str] = []
    for call in tool_calls:
        if not isinstance(call, dict):
            continue
        function = call.get("function")
        if not isinstance(function, dict):
            continue
        name = function.get("name")
        if name not in {"write_file", "str_replace"}:
            continue
        arguments = function.get("arguments")
        was_string = isinstance(arguments, str)
        if was_string:
            try:
                arguments = json.loads(arguments)
            except json.JSONDecodeError:
                continue
        if not isinstance(arguments, dict):
            continue
        fields = ("content",) if name == "write_file" else ("old_string", "new_string")
        changed = False
        for field in fields:
            value = arguments.get(field)
            if isinstance(value, str) and value and value != EDIT_HISTORY_PLACEHOLDER:
                redactions.append(value)
            if field in arguments and arguments[field] != EDIT_HISTORY_PLACEHOLDER:
                arguments[field] = EDIT_HISTORY_PLACEHOLDER
                changed = True
        if changed and was_string:
            function["arguments"] = json.dumps(arguments, ensure_ascii=False)
        elif changed:
            function["arguments"] = arguments
    if isinstance(echoed_content, str):
        # Text-recovered calls repeat their JSON arguments in assistant
        # content. Keep the surrounding model decision while eliding only the
        # repeated edit payload.
        for value in sorted(set(redactions), key=len, reverse=True):
            echoed_content = echoed_content.replace(value, EDIT_HISTORY_PLACEHOLDER)
        message["content"] = echoed_content


def compact_read_file_result(message: dict[str, Any]) -> None:
    content = message.get("content")
    if not isinstance(content, str):
        return
    try:
        result = json.loads(content)
    except json.JSONDecodeError:
        return
    if not isinstance(result, dict) or "content" not in result or result.get("ok") is not True:
        return
    result["content"] = READ_FILE_HISTORY_PLACEHOLDER
    message["content"] = json.dumps(result, ensure_ascii=False)


def post_json(
    url: str,
    payload: dict[str, Any],
    timeout: float,
    *,
    metadata: dict[str, Any] | None = None,
) -> dict[str, Any]:
    headers = {"Content-Type": "application/json"}
    if metadata is not None:
        headers.update(metadata_headers(metadata))
    api_key = os.environ.get("STBENCH_LOCAL_MODEL_API_KEY", "").strip()
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as result:
            decoded = json.loads(result.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"local model HTTP {error.code}: {cap_text(detail)}") from error
    except urllib.error.URLError as error:
        raise RuntimeError(f"local model request failed: {error.reason}") from error
    if not isinstance(decoded, dict):
        raise RuntimeError("local model response must be a JSON object")
    return decoded


def execute_tool(name: str, arguments: dict[str, Any], root: Path) -> dict[str, Any]:
    if not isinstance(arguments, dict):
        return tool_error("invalid_tool_arguments", "tool arguments must be an object")
    try:
        if name == "list_files":
            return list_files(root, str(arguments.get("path", ".")))
        if name == "read_file":
            return read_file(root, str(arguments["path"]), int(arguments.get("max_bytes", MAX_FILE_BYTES)))
        if name == "write_file":
            return write_file(root, str(arguments["path"]), str(arguments["content"]))
        if name == "str_replace":
            return str_replace(
                root,
                str(arguments["path"]),
                str(arguments["old_string"]),
                str(arguments["new_string"]),
            )
        return tool_error("unknown_tool", f"unknown tool {name!r}")
    except ToolError as error:
        return tool_error(error.code, str(error))
    except FileNotFoundError as error:
        return tool_error("file_not_found", str(error))
    except (KeyError, OSError, TypeError, ValueError) as error:
        return tool_error("tool_error", str(error))


def tool_error(code: str, message: str) -> dict[str, Any]:
    return {"ok": False, "error": message, "error_code": code}


def tool_result_for_model(result: Any) -> Any:
    """Keep audit-only file contents out of the model's subsequent context."""

    if not isinstance(result, dict) or "file_modifications" not in result:
        return result
    model_result = copy.deepcopy(result)
    model_result.pop("file_modifications", None)
    return model_result


def list_files(root: Path, relative: str) -> dict[str, Any]:
    root = root.resolve()
    directory = safe_path(root, relative)
    if not directory.is_dir():
        raise ValueError(f"not a directory: {relative}")
    paths: list[str] = []
    for path in sorted(directory.rglob("*")):
        if ".git" in path.parts or "__pycache__" in path.parts:
            continue
        if path.is_file():
            relative_path = path.relative_to(root)
            if is_managed_state_path(relative_path.as_posix()):
                continue
            paths.append(str(relative_path))
        if len(paths) >= 200:
            break
    return {"ok": True, "files": paths, "truncated": len(paths) >= 200}


def read_file(root: Path, relative: str, max_bytes: int) -> dict[str, Any]:
    if max_bytes < 1:
        raise ValueError("max_bytes must be positive")
    contents = safe_path(root, relative).read_bytes()
    if b"\x00" in contents[:max_bytes]:
        raise ValueError("read_file only supports text files")
    truncated = len(contents) > max_bytes
    return {
        "ok": True,
        "path": relative,
        "content": contents[:max_bytes].decode("utf-8", errors="replace"),
        "truncated": truncated,
    }


def write_file(root: Path, relative: str, content: str) -> dict[str, Any]:
    target = safe_path(root, relative)
    if target.exists():
        raise ToolError("write_file_existing", f"write_file can only create new files: {relative}")
    target.parent.mkdir(parents=True, exist_ok=True)
    try:
        with target.open("x", encoding="utf-8", newline="") as output:
            output.write(content)
    except FileExistsError as error:
        raise ToolError("write_file_existing", f"write_file can only create new files: {relative}") from error
    return {
        "ok": True,
        "path": relative,
        "bytes": len(content.encode("utf-8")),
        "outcome": "modified",
        "file_modifications": [build_file_modification(relative, None, content)],
    }


def str_replace(root: Path, relative: str, old_string: str, new_string: str) -> dict[str, Any]:
    if not old_string:
        raise ToolError("invalid_arguments", "old_string must not be empty")
    target = safe_path(root, relative)
    try:
        contents = target.read_bytes()
    except FileNotFoundError as error:
        raise ToolError("file_not_found", f"str_replace target does not exist: {relative}") from error
    if b"\x00" in contents:
        raise ToolError("invalid_file", "str_replace only supports text files")
    try:
        text = contents.decode("utf-8")
    except UnicodeDecodeError as error:
        raise ToolError("invalid_file", "str_replace only supports UTF-8 text files") from error

    matches = 0
    search_from = 0
    while True:
        match_at = text.find(old_string, search_from)
        if match_at < 0:
            break
        matches += 1
        search_from = match_at + 1
    if matches == 0:
        raise ToolError("str_replace_no_match", f"str_replace found no match in {relative}")
    if matches > 1:
        raise ToolError(
            "str_replace_multiple_matches",
            f"str_replace found {matches} matches in {relative}; the match must be unique",
        )
    replacement = text.replace(old_string, new_string, 1)
    if replacement == text:
        return {
            "ok": True,
            "path": relative,
            "replacements": 1,
            "outcome": "no_change",
            "file_modifications": [],
        }
    with target.open("w", encoding="utf-8", newline="") as output:
        output.write(replacement)
    return {
        "ok": True,
        "path": relative,
        "replacements": 1,
        "outcome": "modified",
        "file_modifications": [build_file_modification(relative, text, replacement)],
    }


def build_file_modification(relative: str, before: str | None, after: str | None) -> dict[str, Any]:
    before_lines = [] if before is None else before.splitlines(keepends=True)
    after_lines = [] if after is None else after.splitlines(keepends=True)
    before_name = "/dev/null" if before is None else f"a/{relative}"
    after_name = "/dev/null" if after is None else f"b/{relative}"
    diff = "".join(
        difflib.unified_diff(
            before_lines,
            after_lines,
            fromfile=before_name,
            tofile=after_name,
        )
    )
    return {
        "path": relative,
        "before": before,
        "after": after,
        "diff": diff,
    }


def safe_path(root: Path, relative: str) -> Path:
    root = root.resolve()
    candidate = (root / relative).resolve()
    try:
        relative_path = candidate.relative_to(root)
    except ValueError as error:
        raise ToolError("path_error", "path escapes the candidate directory") from error
    if is_managed_state_path(relative_path.as_posix()):
        raise ToolError("path_error", "path belongs to managed tool state")
    return candidate


if __name__ == "__main__":
    raise SystemExit(main())
