#!/usr/bin/env python3
"""Talk to a running medusa campaign through its MCP server.

Usage:
    medusa_mcp.py [--port PORT] TOOL [ARGUMENTS] [--json]
    medusa_mcp.py [--port PORT] wait [--stall SECONDS] [--max SECONDS]

TOOL calls one of the server's tools and prints its result as JSON. ARGUMENTS is a JSON object, or @path to read one
from a file. It defaults to no arguments. The results of execute_sequence and add_sequence are summarized one line per
call; pass --json for the full result, with complete traces.

`wait` waits until there is something to look at, then prints the reason, the number of failed tests and the
campaign status. It returns as soon as one of these happens: a test fails, coverage has not grown for --stall seconds
(default 60, counted from when coverage last grew), the campaign ends, or --max seconds pass (default 300). It also
waits for the server to come up, so it can be run right after starting medusa.

Examples:
    medusa_mcp.py wait
    medusa_mcp.py status
    medusa_mcp.py tests '{"status": "failed"}'
    medusa_mcp.py coverage '{"file": "Vault.sol"}'
    medusa_mcp.py execute_sequence @sequence.json
"""

import argparse
import json
import re
import sys
import time
import urllib.error
import urllib.request
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

DEFAULT_PORT = 8765

# How often `wait` polls the campaign status, in seconds.
POLL_INTERVAL = 2.0


class ToolError(Exception):
    """A tool rejected its request, e.g. because a contract or method does not exist."""


def call_tool(port: int, tool: str, arguments: dict[str, Any]) -> Any:
    """Call a tool of the MCP server on the given local port and return its structured result.

    Raises ToolError if the tool rejects the request, and urllib.error.URLError if the server cannot be reached.
    """
    request = urllib.request.Request(
        f"http://127.0.0.1:{port}/mcp",
        data=json.dumps(
            {
                "jsonrpc": "2.0",
                "id": 1,
                "method": "tools/call",
                "params": {"name": tool, "arguments": arguments},
            }
        ).encode(),
        headers={
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        },
    )
    with urllib.request.urlopen(request, timeout=600) as response:
        reply = json.load(response)
    if "error" in reply:
        raise ToolError(reply["error"].get("message", json.dumps(reply["error"])))
    result = reply["result"]
    if result.get("isError"):
        raise ToolError(
            "\n".join(
                item.get("text", "")
                for item in result.get("content", [])
                if item.get("type") == "text"
            )
        )
    return result.get("structuredContent")


def parse_time(value: str) -> datetime:
    """Parse a timestamp from the server, which may have more fractional digits than Python accepts."""
    return datetime.fromisoformat(re.sub(r"(\.\d{6})\d+", r"\1", value))


def seconds_without_new_coverage(status: dict[str, Any]) -> float:
    """Return how long the campaign has gone without reaching new code."""
    last = status.get("metrics", {}).get("lastNewCoverageAt")
    if last is None:
        # Coverage has not grown since the campaign started.
        return float(status.get("durationSeconds", 0))
    return (datetime.now(UTC) - parse_time(last)).total_seconds()


def wait(port: int, stall_seconds: float, max_seconds: float) -> dict[str, Any]:
    """Wait until a test fails, coverage stalls, the campaign ends or max_seconds pass.

    Returns the reason, the number of failed tests and the last campaign status.
    """
    deadline = time.monotonic() + max_seconds
    failed_at_start: int | None = None
    status: dict[str, Any] = {}

    def result(reason: str) -> dict[str, Any]:
        failed = status.get("tests", {}).get("failed", 0)
        return {"reason": reason, "failedTests": failed, "status": status}

    while True:
        try:
            status = call_tool(port, "status", {})
        except urllib.error.URLError:
            # The server is not up yet, or the campaign has ended and taken it down.
            if failed_at_start is not None:
                return result("the campaign ended")
            if time.monotonic() >= deadline:
                return result("the server did not come up")
            time.sleep(POLL_INTERVAL)
            continue

        failed = status.get("tests", {}).get("failed", 0)
        if failed_at_start is None:
            failed_at_start = failed
        if status.get("outcome") != "running":
            return result("the campaign ended")
        if failed > failed_at_start:
            return result("a test failed")
        if seconds_without_new_coverage(status) >= stall_seconds:
            return result(f"coverage has not grown for {stall_seconds:g} seconds")
        if time.monotonic() >= deadline:
            return result(f"nothing happened within {max_seconds:g} seconds")
        time.sleep(POLL_INTERVAL)


def describe_call(number: int, call: dict[str, Any], new_coverage: bool) -> str:
    """Describe one call of an executed sequence on a single line."""
    trace = call.get("trace") or {}
    name = f"{trace.get('contract', '?')}.{trace.get('method', '?')}"
    arguments = trace.get("arguments")
    if arguments is not None:
        name = f"{name.split('(')[0]}({arguments})"
    details = [f"from {trace.get('from', '?')}"]
    if trace.get("value", "0") != "0":
        details.append(f"value {trace['value']}")
    if call.get("blockNumberDelay"):
        details.append(f"+{call['blockNumberDelay']} blocks")
    if call.get("blockTimestampDelay"):
        details.append(f"+{call['blockTimestampDelay']}s")
    outcome = trace.get("status", "?")
    if trace.get("error"):
        outcome = f"{outcome} ({trace['error']})"
    elif trace.get("returnValues"):
        outcome = f"{outcome}, returned {trace['returnValues']}"
    line = f"{number}. {name} [{', '.join(details)}]: {outcome}"
    if new_coverage:
        line += ", NEW COVERAGE"
    return line


def describe_sequence_result(result: dict[str, Any], added: bool) -> str:
    """Summarize the result of execute_sequence or add_sequence: one line per call, then the failed tests.

    If added is set, the result is from add_sequence and the summary says whether the corpus took the sequence.
    """
    lines = [
        describe_call(number, call, new_coverage)
        for number, (call, new_coverage) in enumerate(
            zip(result.get("calls", []), result.get("newCoverage", []), strict=False),
            start=1,
        )
    ]
    failures = result.get("failedTests", [])
    if failures:
        lines.append("Failed tests:")
        for failure in failures:
            line = f"  {failure['name']} fails after call {failure['afterCall']}"
            trace = failure.get("trace") or {}
            if trace.get("error"):
                line += f": {trace['status']} ({trace['error']})"
            elif trace.get("returnValues"):
                line += f": returned {trace['returnValues']}"
            lines.append(line)
    else:
        lines.append("No test fails.")
    if added:
        lines.append(
            "Added to corpus."
            if result.get("addedToCorpus")
            else "Not added: the corpus already has this sequence."
        )
    return "\n".join(lines)


def read_arguments(value: str | None) -> dict[str, Any]:
    """Parse the ARGUMENTS command line value into a JSON object."""
    if value is None:
        return {}
    text = Path(value[1:]).read_text() if value.startswith("@") else value
    arguments = json.loads(text)
    if not isinstance(arguments, dict):
        raise TypeError("the arguments must be a JSON object")
    return arguments


def print_result(result: Any) -> None:
    """Print a tool result as JSON, with annotated source (from coverage) printed as plain text after it."""
    if isinstance(result, dict) and isinstance(result.get("source"), str):
        source = result.pop("source")
        print(json.dumps(result, indent=2))
        print(source, end="")
    else:
        print(json.dumps(result, indent=2))


def main() -> int:
    """Run the command line interface. Returns the process exit code."""
    parser = argparse.ArgumentParser(
        description="Talk to a running medusa campaign through its MCP server.",
        epilog="Run `wait` to wait until a test fails, coverage stalls or the campaign ends.",
    )
    parser.add_argument(
        "--port",
        type=int,
        default=DEFAULT_PORT,
        help=f"port passed to medusa fuzz --mcp-port (default {DEFAULT_PORT})",
    )
    parser.add_argument(
        "tool",
        help="status, target, tests, coverage, execute_sequence, add_sequence, or wait",
    )
    parser.add_argument(
        "arguments",
        nargs="?",
        help="JSON object of tool arguments, or @path to a file holding one",
    )
    parser.add_argument(
        "--stall",
        type=float,
        default=60,
        help="wait: seconds without new coverage that count as a stall (default 60)",
    )
    parser.add_argument(
        "--max",
        type=float,
        default=300,
        help="wait: longest time to wait, in seconds (default 300)",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="execute_sequence, add_sequence: print the full result instead of a summary",
    )
    options = parser.parse_args()

    try:
        if options.tool == "wait":
            print_result(wait(options.port, options.stall, options.max))
            return 0
        result = call_tool(
            options.port, options.tool, read_arguments(options.arguments)
        )
        if options.tool in ("execute_sequence", "add_sequence") and not options.json:
            print(describe_sequence_result(result, options.tool == "add_sequence"))
        else:
            print_result(result)
    except urllib.error.URLError as error:
        print(
            f"Could not reach medusa on port {options.port}: {error.reason}. "
            f"Is `medusa fuzz --mcp-port {options.port}` still running?",
            file=sys.stderr,
        )
        return 1
    except (OSError, ValueError, TypeError, ToolError) as error:
        print(f"Error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
