#!/usr/bin/env python3
"""A local-model adapter for an OpenAI-compatible inference server.

The model edits the candidate through a small tool-enabled scaffold.  It never
receives a repository snapshot and the adapter never applies a model-generated
patch: read/write/command tools mutate the candidate directory directly.
"""

from __future__ import annotations

import argparse
import json
import os
import re
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
    metadata_headers,
    read_requests,
    request_metadata,
    usage_to_tokens,
)


DEFAULT_URL = "http://127.0.0.1:8000/v1/chat/completions"
DEFAULT_TIMEOUT_SECONDS = 600
DEFAULT_MAX_TURNS = 20
MAX_FILE_BYTES = 256_000
READ_FILE_HISTORY_PLACEHOLDER = "[read_file content elided from history]"
EDIT_HISTORY_PLACEHOLDER = "[edit content elided from history]"

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
TOOL_NAMES = frozenset(
    tool["function"]["name"] for tool in TOOLS if isinstance(tool.get("function"), dict)
)


class ToolError(ValueError):
    """An expected tool failure that can be returned to the model."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


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
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT_SECONDS, help="HTTP timeout in seconds")
    parser.add_argument("--max-turns", type=int, default=DEFAULT_MAX_TURNS, help="Maximum tool-use turns")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    try:
        settings = parse_args(argv)
        for request, instruction in read_requests():
            try:
                if handle_preflight(request):
                    continue
                metadata = request_metadata(request)
                response, usages = run_agent(
                    instruction,
                    Path.cwd(),
                    url=settings.url,
                    model=settings.model or metadata["model"],
                    metadata=metadata,
                    timeout=settings.timeout,
                    max_turns=settings.max_turns,
                )
                emit_result(
                    status="ok",
                    response=response,
                    tokens=aggregate_usages(usages),
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
    metadata: dict[str, str] | None = None,
) -> tuple[str, list[dict[str, int] | None]]:
    if timeout <= 0:
        raise ValueError("--timeout must be positive")
    if max_turns < 1:
        raise ValueError("--max-turns must be positive")
    if not model.strip():
        raise ValueError("--model must not be empty")

    messages: list[dict[str, Any]] = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": instruction},
    ]
    usages: list[dict[str, int] | None] = []
    final_response = ""
    current_turn_start = len(messages)

    for _ in range(max_turns):
        compact_history(messages, current_turn_start)
        payload = {
            "model": model,
            "messages": messages,
            "tools": TOOLS,
            "tool_choice": "auto",
        }
        result = post_json(url, payload, timeout, metadata=metadata)
        usages.append(usage_to_tokens(result.get("usage")))
        choices = result.get("choices")
        if not isinstance(choices, list) or not choices or not isinstance(choices[0], dict):
            raise RuntimeError("local model response has no choices")
        message = choices[0].get("message")
        if not isinstance(message, dict):
            raise RuntimeError("local model response has no assistant message")

        content = message.get("content")
        tool_calls = message.get("tool_calls")
        if not isinstance(tool_calls, list) or not tool_calls:
            recovered_calls = recover_tool_calls(content)
            if recovered_calls:
                message = dict(message)
                message["tool_calls"] = recovered_calls
                tool_calls = recovered_calls
            else:
                messages.append(message)
                if isinstance(content, str):
                    final_response = content
                return final_response, usages

        assistant_message_start = len(messages)
        messages.append(message)

        for call in tool_calls:
            tool_result = execute_model_tool_call(call, root)
            messages.append(
                {
                    "role": "tool",
                    "tool_call_id": call.get("id", "unknown") if isinstance(call, dict) else "unknown",
                    "content": json.dumps(tool_result, ensure_ascii=False),
                }
            )
        current_turn_start = assistant_message_start

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
    """Recover registered JSON tool calls that a model emitted as text."""

    if not isinstance(content, str) or not content.strip():
        return []

    candidates = [
        match.group("plain_body") or match.group("special_body") or match.group("alternate_body")
        for match in TOOL_CALL_TAG.finditer(content)
    ]
    candidates.append(content)
    decoder = json.JSONDecoder()
    recovered: list[dict[str, Any]] = []
    seen: set[str] = set()

    def add_candidate(candidate: Any) -> None:
        if isinstance(candidate, list):
            for item in candidate:
                add_candidate(item)
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
        if not isinstance(name, str) or name not in TOOL_NAMES or not isinstance(arguments, dict):
            return
        identity = json.dumps([name, arguments], sort_keys=True, ensure_ascii=False)
        if identity in seen:
            return
        seen.add(identity)
        recovered.append(
            {
                "id": f"recovered-{len(recovered) + 1}",
                "type": "function",
                "function": {
                    "name": name,
                    "arguments": json.dumps(arguments, ensure_ascii=False),
                },
            }
        )

    for candidate in candidates:
        try:
            add_candidate(json.loads(candidate.strip()))
        except json.JSONDecodeError:
            pass

    # A prose wrapper may surround the JSON object. Scan each possible object
    # without interpreting arbitrary JSON: only a registered name plus an
    # arguments object qualifies as a recovered tool call.
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
    # ``str_replace({"path": "api.py", ...})``. Recover that form too, but
    # only for names in the registered tool set.
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
    metadata: dict[str, str] | None = None,
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
        if name == "run_command":
            return run_command(root, arguments)
        return tool_error("unknown_tool", f"unknown tool {name!r}")
    except ToolError as error:
        return tool_error(error.code, str(error))
    except FileNotFoundError as error:
        return tool_error("file_not_found", str(error))
    except (KeyError, OSError, TypeError, ValueError, subprocess.SubprocessError) as error:
        return tool_error("tool_error", str(error))


def tool_error(code: str, message: str) -> dict[str, Any]:
    return {"ok": False, "error": message, "error_code": code}


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
    return {"ok": True, "path": relative, "bytes": len(content.encode("utf-8"))}


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
    with target.open("w", encoding="utf-8", newline="") as output:
        output.write(text.replace(old_string, new_string, 1))
    return {"ok": True, "path": relative, "replacements": 1}


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
        raise ToolError("path_error", "path escapes the candidate directory") from error
    if is_managed_state_path(relative_path.as_posix()):
        raise ToolError("path_error", "path belongs to managed tool state")
    return candidate


if __name__ == "__main__":
    raise SystemExit(main())
