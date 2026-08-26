#!/usr/bin/env python3
"""A local-model adapter for an OpenAI-compatible inference server.

The model edits the candidate through a small tool-enabled scaffold.  It never
receives a repository snapshot and the adapter never applies a model-generated
patch: read/write/command tools mutate the candidate directory directly.
"""

from __future__ import annotations

import argparse
import json
import math
import os
import subprocess
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
DEFAULT_TIMEOUT_SECONDS = 300
DEFAULT_MAX_TURNS = 20
DEFAULT_TEMPERATURE = 0.0
MAX_TEMPERATURE = 2.0
MAX_FILE_BYTES = 256_000

SYSTEM_PROMPT = """You are the coding agent inside a stbench adapter.
The user message is the complete rendered stbench task instruction and is
authoritative. Preserve its scope and success criteria. Inspect and edit the
candidate in the current working directory with the provided tools, run useful
local checks, and stop when the requested fix is complete. Do not invent a new
benchmark loop or consult stcompare artifacts that are not in the user task.
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
            "description": "Write UTF-8 text to a candidate file, creating parents.",
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
            "name": "run_command",
            "description": "Run a local candidate command without a shell.",
            "parameters": {
                "type": "object",
                "properties": {
                    "command": {"type": "array", "items": {"type": "string"}},
                    "timeout_seconds": {"type": "number", "default": 30},
                },
                "required": ["command"],
                "additionalProperties": False,
            },
        },
    },
]


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
                        emit_result(status="ok", temperature=temperature)
                    else:
                        handle_preflight(request)
                    continue
                metadata = request_metadata(request)
                temperature = resolve_temperature(settings.temperature, metadata)
                response, usages = run_agent(
                    instruction,
                    Path.cwd(),
                    url=settings.url,
                    model=settings.model or metadata["model"],
                    temperature=temperature,
                    metadata=metadata,
                    timeout=settings.timeout,
                    max_turns=settings.max_turns,
                )
                emit_result(
                    status="ok",
                    response=response,
                    tokens=aggregate_usages(usages),
                    temperature=temperature,
                )
            except (OSError, ValueError, RuntimeError) as error:
                emit_error(str(error))
        return 0
    except (OSError, ValueError, RuntimeError) as error:
        emit_error(str(error))
        return 0
    except Exception as error:  # pragma: no cover - last-resort protocol guard
        emit_error(f"local-model adapter failed: {error}")
        return 0


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

    for _ in range(max_turns):
        payload = {
            "model": model,
            "temperature": temperature,
            "messages": messages,
            "tools": TOOLS,
            "tool_choice": "auto",
        }
        if temperature == DEFAULT_TEMPERATURE:
            payload["top_p"] = 1
        result = post_json(url, payload, timeout, metadata=metadata)
        usages.append(usage_to_tokens(result.get("usage")))
        choices = result.get("choices")
        if not isinstance(choices, list) or not choices or not isinstance(choices[0], dict):
            raise RuntimeError("local model response has no choices")
        message = choices[0].get("message")
        if not isinstance(message, dict):
            raise RuntimeError("local model response has no assistant message")
        messages.append(message)

        content = message.get("content")
        if isinstance(content, str):
            final_response = content
        tool_calls = message.get("tool_calls")
        if not isinstance(tool_calls, list) or not tool_calls:
            return final_response, usages

        for call in tool_calls:
            if not isinstance(call, dict):
                raise RuntimeError("local model returned an invalid tool call")
            function = call.get("function")
            if not isinstance(function, dict):
                raise RuntimeError("local model tool call has no function")
            name = function.get("name")
            arguments = function.get("arguments", {})
            if isinstance(arguments, str):
                arguments = json.loads(arguments)
            if not isinstance(name, str) or not isinstance(arguments, dict):
                raise RuntimeError("local model returned invalid tool arguments")
            tool_result = execute_tool(name, arguments, root)
            messages.append(
                {
                    "role": "tool",
                    "tool_call_id": call.get("id", "unknown"),
                    "content": json.dumps(tool_result, ensure_ascii=False),
                }
            )

    raise RuntimeError(f"local model reached the {max_turns}-turn limit")


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
    try:
        if name == "list_files":
            return list_files(root, str(arguments.get("path", ".")))
        if name == "read_file":
            return read_file(root, str(arguments["path"]), int(arguments.get("max_bytes", MAX_FILE_BYTES)))
        if name == "write_file":
            return write_file(root, str(arguments["path"]), str(arguments["content"]))
        if name == "run_command":
            return run_command(root, arguments)
        return {"ok": False, "error": f"unknown tool {name!r}"}
    except (KeyError, OSError, ValueError, subprocess.SubprocessError) as error:
        return {"ok": False, "error": str(error)}


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
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")
    return {"ok": True, "path": relative, "bytes": len(content.encode("utf-8"))}


def run_command(root: Path, arguments: dict[str, Any]) -> dict[str, Any]:
    command = arguments.get("command")
    if not isinstance(command, list) or not command or not all(isinstance(item, str) for item in command):
        raise ValueError("command must be a non-empty string array")
    timeout = float(arguments.get("timeout_seconds", 30))
    if timeout <= 0:
        raise ValueError("timeout_seconds must be positive")
    completed = subprocess.run(
        command,
        cwd=root,
        capture_output=True,
        text=True,
        timeout=timeout,
        check=False,
    )
    return {
        "ok": completed.returncode == 0,
        "exit_code": completed.returncode,
        "stdout": cap_text(completed.stdout),
        "stderr": cap_text(completed.stderr),
    }


def safe_path(root: Path, relative: str) -> Path:
    root = root.resolve()
    candidate = (root / relative).resolve()
    try:
        relative_path = candidate.relative_to(root)
    except ValueError as error:
        raise ValueError("path escapes the candidate directory") from error
    if is_managed_state_path(relative_path.as_posix()):
        raise ValueError("path belongs to managed tool state")
    return candidate


if __name__ == "__main__":
    raise SystemExit(main())
