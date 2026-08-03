#!/usr/bin/env python3
"""Cloud OpenAI API fallback adapter for stbench.

This deliberately simple adapter is retained for environments without a local
model or installed coding-agent CLI.  It snapshots tracked text files, asks the
Responses API for a unified diff, validates it with ``git apply --check``, and
applies it in the candidate directory.  See README.md in this directory for
the limitations; use the local or CLI adapter for normal benchmark runs.
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
    cap_text,
    emit_error,
    emit_result,
    handle_preflight,
    metadata_headers,
    read_request,
    request_metadata,
    usage_to_tokens,
)


DEFAULT_RESPONSES_URL = "https://api.openai.com/v1/responses"
DEFAULT_TIMEOUT_SECONDS = 600
DEFAULT_MAX_SNAPSHOT_BYTES = 1_000_000

SYSTEM_PROMPT = """You are a fallback patch generator for a stbench adapter.
The user task is the rendered stbench instruction. Return only a unified diff
inside <patch> and </patch>. The diff must be rooted at the candidate working
directory and must be applicable with git apply. Do not change the task scope.
"""


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--model",
        help="Explicit model override; defaults to the stbench request metadata",
    )
    parser.add_argument(
        "--responses-url",
        default=DEFAULT_RESPONSES_URL,
        help="OpenAI Responses API URL",
    )
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT_SECONDS, help="HTTP timeout in seconds")
    parser.add_argument(
        "--max-snapshot-bytes",
        type=int,
        default=DEFAULT_MAX_SNAPSHOT_BYTES,
        help="Maximum tracked-source snapshot size",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    raw_response = ""
    try:
        settings = parse_args(argv)
        request, instruction = read_request()
        if handle_preflight(request):
            return 0
        metadata = request_metadata(request)
        root = Path.cwd()
        snapshot = tracked_snapshot(root, settings.max_snapshot_bytes)
        raw_response, tokens = request_patch(
            instruction,
            snapshot,
            model=settings.model or metadata["model"],
            metadata=metadata,
            responses_url=settings.responses_url,
            timeout=settings.timeout,
        )
        patch = extract_patch(raw_response)
        apply_patch(root, patch)
        emit_result(status="ok", response=raw_response, tokens=tokens)
        return 0
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        emit_error(str(error), response=raw_response)
        return 0
    except Exception as error:  # pragma: no cover - last-resort protocol guard
        emit_error(f"cloud fallback adapter failed: {error}", response=raw_response)
        return 0


def tracked_snapshot(root: Path, max_snapshot_bytes: int) -> str:
    completed = subprocess.run(
        ["git", "ls-files", "-z"],
        cwd=root,
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        raise RuntimeError("cloud fallback requires a Git candidate directory")

    if max_snapshot_bytes < 1:
        raise ValueError("--max-snapshot-bytes must be positive")
    sections: list[str] = []
    used = 0
    skipped = 0
    for encoded_path in completed.stdout.split(b"\0"):
        if not encoded_path:
            continue
        relative = encoded_path.decode("utf-8")
        path = (root / relative).resolve()
        try:
            path.relative_to(root.resolve())
            contents = path.read_bytes()
        except (OSError, ValueError):
            skipped += 1
            continue
        if b"\0" in contents or used + len(contents) > max_snapshot_bytes:
            skipped += 1
            continue
        try:
            text = contents.decode("utf-8")
        except UnicodeDecodeError:
            skipped += 1
            continue
        section = f"\n--- {relative} ---\n{text}"
        sections.append(section)
        used += len(contents)

    note = f"\n[tracked files omitted from snapshot: {skipped}]" if skipped else ""
    return "".join(sections) + note


def request_patch(
    instruction: str,
    snapshot: str,
    *,
    model: str,
    responses_url: str,
    timeout: float,
    metadata: dict[str, str] | None = None,
) -> tuple[str, dict[str, int] | None]:
    api_key = os.environ.get("OPENAI_API_KEY", "").strip()
    if not api_key:
        raise ValueError("OPENAI_API_KEY is required by the cloud fallback adapter")
    if not model:
        raise ValueError("--model must not be empty")
    if timeout <= 0:
        raise ValueError("--timeout must be positive")
    payload = {
        "model": model,
        "input": [
            {"role": "developer", "content": [{"type": "input_text", "text": SYSTEM_PROMPT}]},
            {
                "role": "user",
                "content": [
                    {"type": "input_text", "text": instruction},
                    {"type": "input_text", "text": "Tracked candidate source snapshot:\n" + snapshot},
                ],
            },
        ],
    }
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }
    if metadata is not None:
        headers.update(metadata_headers(metadata))
    request = urllib.request.Request(
        responses_url,
        data=json.dumps(payload).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            decoded = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"OpenAI API HTTP {error.code}: {cap_text(detail)}") from error
    except urllib.error.URLError as error:
        raise RuntimeError(f"OpenAI API request failed: {error.reason}") from error
    if not isinstance(decoded, dict):
        raise RuntimeError("OpenAI API response must be a JSON object")
    if decoded.get("error"):
        raise RuntimeError(f"OpenAI API returned an error: {decoded['error']}")
    return response_text(decoded), usage_to_tokens(decoded.get("usage"))


def response_text(response: dict[str, Any]) -> str:
    output_text = response.get("output_text")
    if isinstance(output_text, str):
        return output_text
    messages: list[str] = []
    for item in response.get("output", []) or []:
        if not isinstance(item, dict):
            continue
        for content in item.get("content", []):
            if isinstance(content, dict) and content.get("type") == "output_text":
                text = content.get("text")
                if isinstance(text, str):
                    messages.append(text)
    return "\n".join(messages)


def extract_patch(response: str) -> str:
    tagged = re.search(r"<patch>\s*(.*?)\s*</patch>", response, flags=re.DOTALL)
    if tagged:
        response = tagged.group(1)
    else:
        fenced = re.search(r"```(?:diff|patch)?\s*\n(.*?)```", response, flags=re.DOTALL | re.IGNORECASE)
        if fenced:
            response = fenced.group(1)

    start = response.find("diff --git ")
    if start < 0:
        raise ValueError("cloud model response did not contain a unified diff")
    patch = response[start:].strip() + "\n"
    if "\n--- " not in patch or "\n+++ " not in patch:
        raise ValueError("cloud model response did not contain a complete unified diff")
    return patch


def apply_patch(root: Path, patch: str) -> None:
    check = subprocess.run(
        ["git", "apply", "--check", "--whitespace=nowarn", "-"],
        cwd=root,
        input=patch,
        text=True,
        capture_output=True,
        check=False,
    )
    if check.returncode != 0:
        raise RuntimeError(f"cloud patch failed validation: {cap_text(check.stderr.strip())}")
    applied = subprocess.run(
        ["git", "apply", "--whitespace=nowarn", "-"],
        cwd=root,
        input=patch,
        text=True,
        capture_output=True,
        check=False,
    )
    if applied.returncode != 0:
        raise RuntimeError(f"cloud patch failed to apply: {cap_text(applied.stderr.strip())}")


if __name__ == "__main__":
    raise SystemExit(main())
