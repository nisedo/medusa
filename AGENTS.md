# AGENTS.md

This file provides guidance to LLM Agents (e.g. Claude Code or Codex) when working with code in this repository.

## Project Overview

Medusa is a cross-platform smart contract fuzzer written in Go, built on go-ethereum and inspired by Echidna. It provides parallelized, coverage-guided fuzz testing of Solidity smart contracts through CLI or Go API. The fuzzer supports assertion testing, property testing, optimization testing, and cheat codes.

## Development Commands

### Build

- `go build` - Compile the CLI binary for local testing
- `./medusa --help` - Verify the built binary

### Test

- `go test -v ./...` - Run all unit and integration tests
- `go test -v ./fuzzing/...` - Run tests for a specific package with verbose output
- `go test -v -run TestName ./package/...` - Run a specific unit test by name
- `go test -cover ./...` - Run tests with coverage metrics

### Lint & Format

- `goimports -w .` - Format imports and code (groups standard-library and external imports)
- `go fmt ./...` - Format Go code (required before commits)
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --timeout 8m` - Run lint checks (the config needs golangci-lint v2)
- `python3 scripts/check_docs.py` - Check that every CLI flag, config option and cheatcode is documented

### Run

- `go run . fuzz --config path/to/medusa.json` - Compile and run the fuzzer

## Architecture & Code Structure

### Module Responsibilities

- **`cmd/`** - CLI entry point using Cobra. Defines `fuzz`, `init`, `corpus clean`, and `completion`.
- **`fuzzing/`** - Core fuzzing orchestration including workers, corpus management, coverage tracking, value generation, and test case providers.
- **`chain/`** - EVM test harness (TestChain) built on medusa-geth with state management and cheat code support (vm.\* functions).
- **`compilation/`** - Smart contract compilation abstraction with platform adapters for solc and crytic-compile.
- **`logging/`** - Structured logging primitives using Zerolog.
- **`events/`** - Event emitter system for fuzzer lifecycle hooks and extensibility.
- **`utils/`** - Shared utilities for random values, reflection, file operations.

### High-Level System Flow

```
CLI (main.go)
  ↓
cmd/fuzz.go (Cobra Command)
  ↓
fuzzing.NewFuzzer(ProjectConfig)
  ├─→ Compilation (compile contracts via crytic-compile/solc)
  ├─→ Corpus (loads/saves coverage-increasing call sequences)
  └─→ Test Case Providers (Assertion, Property, Optimization)

Fuzzer.Start()
  ├─→ Deploy target contracts once on a base TestChain (ChainSetupFunc)
  └─→ Start N parallel workers; each clones the base chain into an isolated
      TestChain and attaches a coverage tracer when coverage is enabled

Worker Loop (per worker, in parallel):
  1. Generate a CallSequence (new or mutated from the corpus)
  2. Execute each call on the worker's TestChain
  3. After each call, update coverage/corpus and run CallSequenceTestFuncs
  4. If a test fails, shrink the sequence and save the result to the corpus
  5. Reset chain state to testingBaseBlockIndex and repeat
```

### Key Architectural Patterns

**Event-Driven Extensibility**: `FuzzerEvents` emits lifecycle events (FuzzerStarting, FuzzerStopping, WorkerCreated, WorkerDestroyed). Test case providers subscribe to these events and register `CallSequenceTestFunc` hooks, enabling new test types without modifying core logic.

**Hook-Based Customization**: `FuzzerHooks` provides customization points:

- `NewCallSequenceGeneratorConfigFunc` - Customize sequence generation strategy
- `NewShrinkingValueMutatorFunc` - Customize shrinking heuristics
- `ChainSetupFunc` - Customize deployment and initialization logic
- `CallSequenceTestFuncs[]` - Register test functions to run after each call in a sequence

**Coverage-Guided Fuzzing**: The corpus stores call sequences that increase coverage. The generator uses weighted
head, tail, splice, and interleave strategies in mutated and unmodified forms. It generates wholly new sequences
through the separate `NewSequenceProbability`. A corpus pruner periodically removes redundant sequences.

**Worker-Based Parallelization**: Each `FuzzerWorker` has its own isolated `TestChain` instance with no shared state. Workers are periodically destroyed and recreated (`WorkerResetLimit`) to prevent memory bloat from geth's state accumulation.

**Call Sequence Shrinking**: When a test fails, a `ShrinkCallSequenceRequest` is generated. The worker iteratively removes/mutates calls while a `VerifierFunction` confirms the test still fails, producing a minimal reproduction.

**State Management**: `TestChain` rebases to `testingBaseBlockIndex` after each sequence execution, providing clean state for the next sequence. `MedusaStateDB` interface abstracts underlying state implementation (supports native geth StateDB and fork mode).

## Key Abstractions

### Core Data Structures

- **`CallSequence`** - Array of `CallSequenceElement` representing a transaction sequence to execute on the test chain.
- **`CallSequenceElement`** - Single call with target address, method, arguments, block delay, and gas limit.
- **`TestCase` interface** - Represents a test with Status (`NOT STARTED`, `RUNNING`, `PASSED`, `FAILED`), Name, CallSequence, and result Message.
- **`Corpus`** - Storage of coverage-increasing call sequences with pruning. It persists to disk only when `corpusDirectory` is set.
- **`CoverageMaps`** - Tracks branch coverage per contract (including jumps, returns, reverts, and contract entrance), used to identify coverage-increasing sequences.
- **`FuzzerWorker`** - Single execution thread with its own TestChain, deployed contracts, and sequence generator.
- **`ProjectConfig`** - Top-level configuration containing FuzzingConfig, CompilationConfig, SlitherConfig, and LoggingConfig.
- **`FuzzingConfig`** - Workers count, timeout, test limit, corpus directory, coverage settings, test case configurations.

### Test Case Providers

Three built-in test case providers run concurrently:

1. **AssertionTestCaseProvider**: Monitors EVM-level panic conditions for each contract method (e.g., `assert()`, arithmetic underflow/overflow, divide by zero, array access violations). Which panic codes trigger test failures is configured via `PanicCodeConfig` in `FuzzingConfig.Testing.AssertionTesting`.
2. **PropertyTestCaseProvider**: Calls prefix-named, no-argument functions that return `bool`. Calls run read-only; a `false` return or revert fails the test.
3. **OptimizationTestCaseProvider**: Calls prefix-named, no-argument functions that return `int256`, maximizes the returned value, and shrinks the best sequence.

## Testing Guidelines

### Test Organization

- Co-locate tests beside implementations using `*_test.go` suffix
- Use table-driven tests for deterministic logic
- Add `t.Parallel()` for parallelizable tests
- Document invariants with assertions

### Running Tests

- `go test -v ./...` - Run all tests
- `go test -v ./fuzzing/...` - Target specific packages
- `go test -v -run TestName ./package/...` - Run a specific unit test

### Corpus Analysis

Explicitly ask the user if these scripts should be run. In more cases than not, these scripts don't need to be touched.

- Use `python3 scripts/corpus_diff.py old new` to compare corpora and identify method coverage differences
- Use `python3 scripts/corpus_stats.py corpus` to generate statistics (sequence count, average length, method frequency)

## Workflow

This is a personal fork (`nisedo/medusa`, branch `nisedo-dev`), used only by its owner on macOS. There is no CI, no
branch protection and no pull request process.

### Branch & Commit Conventions

- Commit directly to `nisedo-dev` and push to `origin`. Never push to upstream (`crytic/medusa`).
- Commit format: `area: intent` (e.g., `fuzzing: tighten revert handling`)

### Code Standards

- **Naming**: Packages lowercase (`fuzzing`, `chain`), exported types UpperCamelCase (`TestChain`), private helpers lowerCamel (`newWorker`)
- **Documentation**: All exported symbols need doc comments. Add inline comments for complex logic.
- **JSON keys**: Use camelCase rather than snake_case
- **Paths**: Use the `filepath` package (not `path`) for file operations

### Dependencies

- Do not run `go mod tidy`: it removes the security minimums block at the end of `go.mod`. Add a dependency with
  `go get <module>@<version>` instead.

### The medusa-campaign Skill

`.claude/skills/medusa-campaign/` is a Claude Code skill that drives a campaign through the MCP server
(`fuzzing/mcpserver/`). It is linked from `~/.claude/skills/medusa-campaign`, so every project uses this copy.
Whenever a change affects how an agent uses medusa (MCP tools or their inputs and outputs, `fuzz` flags, the results
file, the call sequence format), update `SKILL.md` and `scripts/medusa_mcp.py` in the same commit.

### Before Pushing

Nothing checks a change after it is pushed, so run these first:

1. `gofmt -l .` - Must print nothing
2. `go vet ./...`
3. The lint command above
4. `python3 scripts/check_docs.py`
5. `go test ./...` - Takes about five minutes
6. `go test -race ./...` - For changes to anything shared between workers (about twelve minutes)

After pushing, run `go install .` so the `medusa` command (`~/go/bin/medusa`) is the build just pushed. It is the only
medusa installed.
