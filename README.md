# medusa (nisedo fork)

Medusa is a coverage-guided, parallel fuzzer for Solidity smart contracts, built on go-ethereum. This is a personal
fork of [crytic/medusa](https://github.com/crytic/medusa), maintained on the `nisedo-dev` branch and run on macOS.

This README is written for AI agents given this repository's URL. It covers installing medusa, fuzzing a project with
it, and steering a running campaign. To change medusa itself, read [AGENTS.md](./AGENTS.md) instead.

## What this fork adds

- **MCP server** (`--mcp-port`): inspect and steer a running campaign. See [MCP server](#steer-a-running-campaign).
- **Results file** (`--results-file`): a versioned JSON report of a campaign, written however it ends.
- **Cheatcodes** `vm.assume` and `vm.expectRevert`, plus a fix making `prank` pay call value from the pranked
  sender.
- **Fixes** to shrinking, data races, block delays, cheatcodes leaking out of property tests, coverage of Foundry
  contracts with immutables, and silently skipped target contracts.

Homebrew, `go install github.com/crytic/medusa`, and `ghcr.io/crytic/medusa` all install upstream, without any of this.

## Install

Requirements: Go 1.26.10 or later, and [`crytic-compile`](https://github.com/crytic/crytic-compile), which compiles the
project. [Slither](https://github.com/crytic/slither) is optional: medusa runs it to collect constants for the fuzzer,
and without it logs a warning and collects them from the compiled code instead. Foundry's `cast` helps compute values
when steering a campaign.

```bash
git clone --branch nisedo-dev https://github.com/nisedo/medusa.git
cd medusa
go install .
```

The binary goes to `$(go env GOPATH)/bin/medusa`. Check it is this fork's build: `medusa fuzz --help` must list
`--mcp-port`. If another `medusa` comes first on `PATH`, it is upstream's: remove it.

`go install github.com/nisedo/medusa@nisedo-dev` does not work: the module path is still `github.com/crytic/medusa`.

## Fuzz a project

Run medusa from the project root. Foundry and Hardhat projects compile without configuration:

```bash
medusa fuzz --target-contracts MyHarness
```

`--target-contracts` lists the contracts to deploy and test, by name. They are deployed in that order by the deployer
address, before any fuzzing. A contract whose constructor takes arguments needs them in `medusa.json` (see
[`constructorArgs`](./docs/src/project_configuration/fuzzing_config.md#constructorargs)). A name that is not in the
compilation is an error. For a single file, add `--compilation-target path/to/File.sol`.

`medusa init` writes a `medusa.json` with every option at its default. With a config file present, flags override it.
The [configuration reference](./docs/src/project_configuration/overview.md) documents every option.

### What gets tested

Tests are methods of the target contracts, recognised by name and shape:

| Test         | Method                                       | Fails when                                    |
| ------------ | -------------------------------------------- | --------------------------------------------- |
| Property     | `property_*`, no arguments, returns `bool`   | it returns `false` or reverts, after any call |
| Assertion    | any other method the fuzzer calls            | the call hits `assert` (a Solidity panic)     |
| Optimization | `optimize_*`, no arguments, returns `int256` | never; medusa reports the highest value found |

The fuzzer calls the methods of the deployed contracts, including view methods, in sequences of up to 100 calls, from the senders `0x10000`, `0x20000` and `0x30000`, with random block and time delays between calls. A failing
sequence is shrunk to a minimal one.

The cheatcode contract is at `0x7109709ECfa91a80626fF3989D68f67F5b1DD12D`, with a Foundry-style interface
(`warp`, `roll`, `prank`, `deal`, `store`, `assume`, `expectRevert` and more). The full list is in
[cheatcodes](./docs/src/cheatcodes/cheatcodes_overview.md).

### Defaults that matter

- **Stops at the first failed test** (`stopOnFailedTest`). Pass `--fail-fast=false` to keep fuzzing.
- **Runs until stopped**: no time or call limit (`--timeout`, `--test-limit`, both `0`).
- **No corpus on disk**: pass `--corpus-dir <dir>` to keep coverage-increasing sequences across runs.
- **10 workers**, sequences of up to 100 calls, at most 7 days and 60480 blocks between two calls.
- **Ctrl-C or SIGTERM** stops gracefully: the corpus is saved, and coverage reports and the results file are
  written.

### Output

- Console: progress every few seconds, then each test's result, with the shrunk sequence and execution trace of every
  failure.
- `--results-file <path>`: JSON with the outcome, metrics, every test with its call sequence and structured traces,
  and line coverage. Its format is in [reporting](./docs/src/testing/reporting.md#results-file).
- Coverage reports (HTML and LCOV) in `<corpus-dir>/coverage`, or `crytic-export/coverage` without a corpus directory.
- Build artifacts in the project: `crytic-export/`, and `slither_results.json` when Slither runs.

Exit codes: `0` all tests passed, `7` a test failed, `6` the campaign failed to start or run (compilation, deployment
or configuration errors, already logged), `1` any other error, such as an unreadable config file.

## Steer a running campaign

Fuzzers stall on conditions they cannot hit by chance: hashes, signatures, values tied to the sender, long time locks,
strict call orders. An agent can read the source, build a call sequence that gets past the check, and hand it to
medusa, which then explores from there.

Start medusa with its MCP server, which listens on `127.0.0.1` only:

```bash
medusa fuzz --mcp-port 8765 --fail-fast=false --corpus-dir corpus --results-file corpus/results.json
```

The server's tools:

| Tool               | Use                                                                              |
| ------------------ | -------------------------------------------------------------------------------- |
| `status`           | Progress, and when coverage last grew                                            |
| `target`           | Deployed contracts, their methods, and the sender addresses                      |
| `coverage`         | Lines and functions never reached; with a `file`, its source marked line by line |
| `execute_sequence` | Run a call sequence: each call's trace, new coverage, and the tests it fails     |
| `add_sequence`     | Run a call sequence, then add it to the corpus for the fuzzer to explore from    |
| `tests`            | Tests, with the shrunk call sequence and trace of each failed one                |

Two ways to use it:

- **The `medusa-campaign` skill** in [`.claude/skills/medusa-campaign/`](./.claude/skills/medusa-campaign/SKILL.md).
  Read its `SKILL.md` and follow it. It walks through the whole loop, and its script `scripts/medusa_mcp.py` calls
  the server and summarizes results. To make it a Claude Code skill in every project, link it:
  `ln -s "$PWD/.claude/skills/medusa-campaign" ~/.claude/skills/medusa-campaign` from the repository root.
- **Any MCP client**, at `http://127.0.0.1:8765/mcp`. For Claude Code:
  `claude mcp add --transport http medusa http://127.0.0.1:8765/mcp`.

The call sequence format and every tool's output are in [MCP server](./docs/src/testing/mcp_server.md).

## Reference

- [Configuration](./docs/src/project_configuration/overview.md): every `medusa.json` option, with defaults.
- [CLI](./docs/src/cli/overview.md): every command and flag.
- [Cheatcodes](./docs/src/cheatcodes/cheatcodes_overview.md): interface and per-cheatcode behaviour.
- [Writing function-level](./docs/src/testing/writing-function-level-invariants.md) and
  [system-level](./docs/src/testing/writing-system-level-invariants.md) invariants: how to write harnesses.
- [Fuzzing pre-deployed contracts](./docs/src/testing/genesis_state.md): starting from a state snapshot.
- [Reporting](./docs/src/testing/reporting.md): coverage reports, revert reports and the results file.

## License

AGPLv3, see [LICENSE](./LICENSE).
