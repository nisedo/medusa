# MCP Server

Medusa can serve a [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server while a campaign runs. An
AI agent, or any other MCP client, can use it to see what the fuzzer has not reached, try a call sequence, and hand
the fuzzer a call sequence to build on.

This helps when coverage stalls: code behind a condition the fuzzer cannot satisfy by chance, such as a specific
value or a specific order of calls, is often easy for an agent to reach by reading the source.

## Starting the Server

Pass `--mcp-port`, or set [`mcpPort`](../project_configuration/fuzzing_config.md#mcpport) under `fuzzing`:

```shell
medusa fuzz --mcp-port 8765
```

The server is available at `http://127.0.0.1:8765/mcp` for as long as the campaign runs. To let an agent work for a
while, run the campaign without a test limit or timeout.

To connect Claude Code:

```shell
claude mcp add --transport http medusa http://127.0.0.1:8765/mcp
```

## Tools

| Tool               | What it does                                                                            |
| ------------------ | --------------------------------------------------------------------------------------- |
| `status`           | How the campaign is going: metrics, when coverage last increased, and tests by status   |
| `target`           | The deployed contracts and methods a call sequence can call, and the sender addresses   |
| `tests`            | The tests, with the call sequence and execution trace of each failed one                |
| `coverage`         | The lines and functions never reached, per file. With a `file`, its source line by line |
| `execute_sequence` | Runs a call sequence and reports what each call did. Changes nothing                    |
| `add_sequence`     | Runs a call sequence, then adds it to the corpus for the fuzzer to test and mutate      |

The outputs of `tests`, `execute_sequence` and `add_sequence` use the same test, call and trace format as the
[results file](./reporting.md#results-file).

A typical loop is: `status` to see whether coverage has stalled, `coverage` to find what was not reached,
`execute_sequence` to try a call sequence aimed at it, and `add_sequence` once it reaches new code.

## Call Sequences

`execute_sequence` and `add_sequence` take a list of `calls`. Each call has:

| Field                 | Description                                                               |
| --------------------- | ------------------------------------------------------------------------- |
| `contract`            | The name of a deployed contract, or its address if several share the name |
| `method`              | The name of the method, or its full signature if the name is overloaded   |
| `arguments`           | The arguments, in order                                                   |
| `sender`              | The address making the call. Defaults to the first configured sender      |
| `value`               | The wei to send, as a decimal or `0x` string. Defaults to `0`             |
| `blockNumberDelay`    | The number of blocks to advance before the call                           |
| `blockTimestampDelay` | The number of seconds to advance the block timestamp by before the call   |

Arguments are written as JSON:

- Integers are decimal or `0x` strings, such as `"1000000000000000000"`. Small integers can be plain numbers.
- Addresses and bytes are hex strings. An address can also be `"DeployedContract:<name>"`.
- Arrays are JSON arrays, and structs are JSON objects keyed by field name.

```json
{
  "calls": [
    {
      "contract": "Vault",
      "method": "deposit",
      "arguments": ["1000"],
      "value": "1000"
    },
    {
      "contract": "Vault",
      "method": "withdraw",
      "arguments": ["1000"],
      "blockTimestampDelay": 86400
    }
  ]
}
```

Every sequence starts from the state the fuzzer's own sequences start from: right after deployment.

`execute_sequence` reports, for each call, its execution trace and whether it reached code the corpus has not. It
does not record coverage, change the corpus or run tests.

`add_sequence` does the same, then adds the sequence to the corpus. A fuzzer worker replays it shortly after, which
runs every test against it, records its coverage and makes it a starting point for mutations. If it fails a test, the
failure shows up in `tests`.

## Security

The server only listens on `127.0.0.1`, and rejects requests from web pages. Anyone who can reach the port can run
call sequences against your contracts, including cheatcodes. If [`enableFFI`](../project_configuration/chain_config.md#enableffi)
is on, that includes running commands on your machine, so do not combine it with the MCP server on a shared machine.
