"""Small helpers shared by the stbench adapter examples.

The examples intentionally use only the Python standard library.  Keep this
file next to an adapter when copying one into a candidate project.
"""

from __future__ import annotations

import json
import sys
from typing import Any


def is_preflight_request(request: dict[str, Any]) -> bool:
    """Return whether stbench sent the no-op adapter preflight request."""

    return request.get("preflight") is True


def read_request() -> tuple[dict[str, Any], str]:
    """Decode the one JSON request written by stbench."""

    raw = sys.stdin.read()
    if not raw.strip():
        raise ValueError("adapter input is empty")

    request = json.loads(raw)
    if not isinstance(request, dict):
        raise ValueError("adapter input must be a JSON object")

    if is_preflight_request(request):
        return request, ""

    instruction = request.get("instruction")
    if not isinstance(instruction, str) or not instruction.strip():
        raise ValueError("adapter input instruction is required")
    if not isinstance(request.get("view"), dict):
        raise ValueError("adapter input view must be a JSON object")

    return request, instruction


def request_metadata(request: dict[str, Any]) -> dict[str, str]:
    """Return the stbench execution metadata from an adapter request."""

    metadata: dict[str, str] = {}
    for name in ("agent", "model", "hardware"):
        value = request.get(name)
        if not isinstance(value, str):
            raise ValueError(f"adapter input {name} is required")
        metadata[name] = value
    return metadata


def metadata_environment(metadata: dict[str, str]) -> dict[str, str]:
    """Expose request metadata to an adapter's child process."""

    return {
        "STBENCH_AGENT": metadata["agent"],
        "STBENCH_MODEL": metadata["model"],
        "STBENCH_HARDWARE": metadata["hardware"],
    }


def metadata_headers(metadata: dict[str, str]) -> dict[str, str]:
    """Expose request metadata to an adapter's HTTP-backed agent."""

    return {
        "X-Stbench-Agent": metadata["agent"],
        "X-Stbench-Model": metadata["model"],
        "X-Stbench-Hardware": metadata["hardware"],
    }


def emit_result(
    *,
    status: str,
    response: str = "",
    tokens: dict[str, int] | None = None,
    message: str = "",
) -> None:
    """Write exactly one stbench adapter result to stdout."""

    payload = {
        "status": status,
        "message": message,
        "response": response,
        "tokens": tokens,
    }
    sys.stdout.write(json.dumps(payload, ensure_ascii=False) + "\n")


def emit_error(message: str, *, response: str = "") -> None:
    emit_result(status="error", response=response, message=message)


def handle_preflight(request: dict[str, Any]) -> bool:
    """Emit the successful no-op response when handling a preflight request."""

    if not is_preflight_request(request):
        return False
    emit_result(status="ok")
    return True


def usage_to_tokens(usage: Any) -> dict[str, int] | None:
    """Normalize common OpenAI-compatible usage shapes."""

    if not isinstance(usage, dict):
        return None

    input_tokens = _integer(usage.get("input_tokens"))
    if input_tokens is None:
        input_tokens = _integer(usage.get("prompt_tokens"))

    output_tokens = _integer(usage.get("output_tokens"))
    if output_tokens is None:
        output_tokens = _integer(usage.get("completion_tokens"))

    total_tokens = _integer(usage.get("total_tokens"))
    if input_tokens is None or output_tokens is None:
        return None
    if total_tokens is None:
        total_tokens = input_tokens + output_tokens

    return {
        "input": input_tokens,
        "output": output_tokens,
        "total": total_tokens,
    }


def aggregate_usages(usages: list[dict[str, int] | None]) -> dict[str, int] | None:
    """Sum usage records, preserving unknown usage as null."""

    if not usages or any(usage is None for usage in usages):
        return None
    return {
        "input": sum(usage["input"] for usage in usages if usage is not None),
        "output": sum(usage["output"] for usage in usages if usage is not None),
        "total": sum(usage["total"] for usage in usages if usage is not None),
    }


def cap_text(value: str, limit: int = 8_000) -> str:
    if len(value) <= limit:
        return value
    return value[:limit] + "\n[output truncated]"


def _integer(value: Any) -> int | None:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        return None
    return value
