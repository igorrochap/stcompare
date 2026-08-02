#!/usr/bin/env python3
"""Deliver the stbench task to Codex or Claude Code in the candidate cwd.

This adapter invokes one installed coding-agent CLI per stbench iteration.  It
does not implement the benchmark loop and does not rewrite the instruction;
the CLI receives the rendered task exactly as stbench supplied it on stdin.
"""

from __future__ import annotations

import argparse
import json
import os
import shlex
import subprocess
import sys
from typing import Any

from _protocol import (
    aggregate_usages,
    cap_text,
    emit_error,
    emit_result,
    read_request,
    usage_to_tokens,
)


DEFAULT_TIMEOUT_SECONDS = 1_800


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--agent",
        choices=("codex", "claude", "claude-code"),
        default="codex",
        help="Coding-agent CLI to invoke",
    )
    parser.add_argument("--timeout", type=int, default=DEFAULT_TIMEOUT_SECONDS, help="CLI timeout in seconds")
    parser.add_argument(
        "--command",
        help="Override the selected agent command; use --agent to select its output format",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    try:
        settings = parse_args(argv)
        _, instruction = read_request()
        agent = settings.agent.strip().lower()
        command = agent_command(agent, settings.command)
        timeout = settings.timeout
        if timeout <= 0:
            raise ValueError("--timeout must be positive")
        completed = subprocess.run(
            command,
            input=instruction,
            text=True,
            cwd=os.getcwd(),
            capture_output=True,
            timeout=timeout,
            check=False,
        )
        if completed.stderr:
            sys.stderr.write(completed.stderr)

        response, usages, agent_error = parse_output(agent, completed.stdout)
        if completed.returncode != 0:
            detail = agent_error or f"{agent} exited with status {completed.returncode}"
            if completed.stderr.strip():
                detail += f": {cap_text(completed.stderr.strip())}"
            emit_error(detail, response=response)
            return 0
        if agent_error:
            emit_error(agent_error, response=response)
            return 0

        emit_result(
            status="ok",
            response=response,
            tokens=aggregate_usages(usages),
        )
        return 0
    except subprocess.TimeoutExpired:
        emit_error("coding-agent CLI timed out")
        return 0
    except (OSError, ValueError, json.JSONDecodeError) as error:
        emit_error(str(error))
        return 0
    except Exception as error:  # pragma: no cover - last-resort protocol guard
        emit_error(f"coding-agent adapter failed: {error}")
        return 0


def agent_command(agent: str, override: str | None = None) -> list[str]:
    override = (override or "").strip()
    if override:
        command = shlex.split(override)
        if not command:
            raise ValueError("--command is empty")
        return command

    if agent == "codex":
        # `-` makes stdin the complete prompt. workspace-write is the least
        # permissive Codex mode that still lets the agent edit the candidate.
        return [
            "codex",
            "exec",
            "--json",
            "--sandbox",
            "workspace-write",
            "--ephemeral",
            "--skip-git-repo-check",
            "-",
        ]
    if agent in {"claude", "claude-code"}:
        return [
            "claude",
            "-p",
            "--output-format",
            "json",
            "--dangerously-skip-permissions",
        ]
    raise ValueError(
        f"unsupported agent {agent!r}; use codex, claude, or --command"
    )


def parse_output(agent: str, output: str) -> tuple[str, list[dict[str, int] | None], str]:
    if agent == "codex":
        return parse_codex_output(output)
    return parse_claude_output(output)


def parse_codex_output(output: str) -> tuple[str, list[dict[str, int] | None], str]:
    messages: list[str] = []
    usages: list[dict[str, int] | None] = []
    errors: list[str] = []
    parsed_any = False

    for line in output.splitlines():
        try:
            event: dict[str, Any] = json.loads(line)
        except json.JSONDecodeError:
            continue
        parsed_any = True
        event_type = event.get("type")
        if event_type == "turn.completed":
            usages.append(usage_to_tokens(event.get("usage")))
        elif event_type == "turn.failed":
            errors.append("Codex turn failed")
        elif event_type == "error":
            errors.append(str(event.get("message") or "Codex reported an error"))
        elif event_type == "item.completed":
            item = event.get("item")
            if isinstance(item, dict) and item.get("type") == "agent_message":
                text = item.get("text")
                if isinstance(text, str):
                    messages.append(text)

    if not parsed_any:
        return output.strip(), usages, ""
    return "\n".join(messages), usages, "; ".join(errors)


def parse_claude_output(output: str) -> tuple[str, list[dict[str, int] | None], str]:
    try:
        result: dict[str, Any] = json.loads(output)
    except json.JSONDecodeError:
        return output.strip(), [], ""

    response = result.get("result")
    if not isinstance(response, str):
        response = ""
    if result.get("is_error"):
        return response, [usage_to_tokens(result.get("usage"))], response or "Claude Code reported an error"
    return response, [usage_to_tokens(result.get("usage"))], ""


if __name__ == "__main__":
    raise SystemExit(main())
