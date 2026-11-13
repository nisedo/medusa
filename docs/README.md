# medusa documentation

Reference documentation for this fork. Start with the repository [README](../README.md) for installation and usage.

## Getting started

- [Installation](./getting_started/installation.md)
- [First steps](./getting_started/first_steps.md)

## Configuration

Every option of `medusa.json`, with its default.

- [Overview](./project_configuration/overview.md)
- [Fuzzing](./project_configuration/fuzzing_config.md)
- [Testing](./project_configuration/testing_config.md)
- [Chain](./project_configuration/chain_config.md)
- [Compilation](./project_configuration/compilation_config.md)
- [Slither](./project_configuration/slither_config.md)
- [Logging](./project_configuration/logging_config.md)

## Command line

- [Overview](./cli/overview.md)
- [`init`](./cli/init.md)
- [`fuzz`](./cli/fuzz.md)
- [`corpus clean`](./cli/corpus.md)
- [`completion`](./cli/completion.md)

## Writing tests

- [The fuzzing lifecycle](./testing/fuzzing_lifecycle.md)
- [Types of invariants](./testing/invariants.md)
- [Writing function-level invariants](./testing/writing-function-level-invariants.md)
- [Writing system-level invariants](./testing/writing-system-level-invariants.md)
- [Fuzzing pre-deployed contracts](./testing/genesis_state.md)
- [Console logging](./console_logging.md)

## Results and agents

- [Reporting](./testing/reporting.md): coverage reports, revert reports and the results file
- [MCP server](./testing/mcp_server.md): inspect and steer a running campaign

## Cheatcodes

- [Overview and interface](./cheatcodes/cheatcodes_overview.md), with one page per cheatcode in
  [`cheatcodes/`](./cheatcodes/)
