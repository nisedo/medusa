# Medusa Reports

Medusa provides three types of reports to help you analyze the results of your fuzzing campaigns:

1. **Coverage Reports**: Show which parts of your contracts were executed during fuzzing
2. **Revert Reports**: Help you understand which functions are reverting and why (experimental)
3. **Results File**: Describes the whole campaign as JSON, for CI pipelines and other tools

## Coverage Reports

Coverage reports help you understand which parts of your contract code have been executed during fuzzing. This is valuable for:

- Identifying code paths that haven't been tested
- Focusing your fuzzing efforts on uncovered areas
- Measuring the effectiveness of your fuzzing campaign

Medusa supports two types of coverage report formats:

### HTML Coverage Reports

HTML reports provide a visual representation of code coverage with highlighted source code. This is the most user-friendly format for quickly understanding coverage.

### LCOV Coverage Reports

LCOV reports are machine-readable and useful for integrating with other tools or continuous integration systems.

### Enabling Coverage Reports

Coverage is enabled by default, and the default `coverageFormats` value generates both HTML and LCOV reports. These
settings belong under `fuzzing`. Set `corpusDirectory` when you want reports and corpus entries under a stable project
directory:

```json
{
  "fuzzing": {
    "coverageEnabled": true,
    "coverageFormats": ["html", "lcov"],
    "corpusDirectory": "corpus"
  }
}
```

With this example, reports are written to `corpus/coverage`, and the coverage corpus persists between campaigns. When
`corpusDirectory` is empty, reports go to `crytic-export/coverage`, and corpus entries remain in memory only.

### Viewing HTML Coverage Reports

The example above generates `corpus/coverage/coverage_report.html`. Open it in a browser.

### Using LCOV Reports

LCOV files can be used with various tools:

#### Generate HTML from LCOV

First, install the LCOV tools:

Linux:

```bash
apt-get install lcov
```

MacOS:

```bash
brew install lcov
```

Then generate HTML from the LCOV data:

```bash
genhtml corpus/coverage/lcov.info --output-dir corpus --rc derive_function_end_line=0
```

> 🚩 WARNING
> The `derive_function_end_line` flag is required to prevent the `genhtml` tool from crashing when processing Solidity source code.

Open the `corpus/index.html` file in your browser to view the report.

#### View Coverage in VSCode

1. Install the [Coverage Gutters](https://marketplace.visualstudio.com/items?itemName=ryanluker.vscode-coverage-gutters) extension
2. Right-click in a project file and select `Coverage Gutters: Display Coverage`

## Revert Reports

Revert reports are an **experimental feature** that helps you understand which functions in your contract frequently revert during fuzzing and why. This is particularly useful for:

- Debugging your fuzzing harness
- Identifying input validation issues
- Understanding constraints that are limiting your fuzzer's exploration
- Improving your test cases and assertions

### Enabling Revert Reports

To enable revert reports, you need to set the `revertReporterEnabled` parameter to `true` in your configuration file:

```json
{
  "fuzzing": {
    "revertReporterEnabled": true,
    "corpusDirectory": "corpus"
  }
}
```

If a `corpusDirectory` is not provided, the reports will be saved at `crytic-export/coverage`.

### Viewing Revert Reports

Two report files are generated:

1. `corpus/coverage/revert_report.html` - HTML visualization of revert metrics
2. `corpus/coverage/revert_report.json` - Machine-readable JSON format

The HTML report provides detailed statistics on:

- Which functions revert most frequently
- Common revert reasons for each function
- Comparison with previous fuzzing runs (when available)

### Benefits of Revert Reports

Revert reports are especially helpful during the early stages of fuzzing development:

- **Harness Development**: Identify issues in your fuzzing setup that prevent effective testing
- **Input Improvement**: Determine which inputs cause frequent reverts and refine your value generation
- **Contract Constraints**: Understand logical constraints in your contract that may be limiting fuzzing effectiveness

## Results File

The results file is a JSON report of a fuzzing campaign, meant to be read by CI pipelines and other tools rather than
by people. It is written when the campaign ends, however it ends: on success, on a failed test, on an interrupt, and
on an error such as a failed compilation.

### Enabling the Results File

Set `resultsFile` under `fuzzing`, or pass `--results-file` on the command line:

```json
{
  "fuzzing": {
    "resultsFile": "results.json"
  }
}
```

### Contents

| Field             | Description                                                                           |
| ----------------- | ------------------------------------------------------------------------------------- |
| `schemaVersion`   | The version of the format. It changes when a field is removed, renamed or redefined   |
| `outcome`         | `passed`, `failed` (a test failed) or `error` (the campaign could not run to the end) |
| `error`           | The error the campaign ended with, when `outcome` is `error`                          |
| `startedAt`       | When the campaign started                                                             |
| `endedAt`         | When the campaign ended                                                               |
| `durationSeconds` | How long the campaign ran                                                             |
| `metrics`         | Calls and sequences tested, failed tests, gas used, corpus size and branches hit      |
| `tests`           | Every test, see below                                                                 |
| `coverage`        | Lines covered per source file, see below. Left out when coverage is disabled          |

Each entry of `tests` has:

| Field          | Description                                                                             |
| -------------- | --------------------------------------------------------------------------------------- |
| `id`           | A stable identifier of the test                                                         |
| `type`         | `assertion`, `property`, `optimization` or `custom`                                     |
| `name`         | The display name of the test                                                            |
| `contract`     | The contract the tested method belongs to                                               |
| `method`       | The signature of the tested method                                                      |
| `status`       | `notStarted`, `running`, `passed` or `failed`                                           |
| `value`        | The highest value an optimization test returned, as a decimal string                    |
| `callSequence` | The calls which failed the test, or produced `value`. Left out for tests which passed   |
| `trace`        | The execution trace of the property or optimization method, run after the call sequence |
| `message`      | The description of the result, as printed at the end of the campaign                    |

Each entry of `callSequence` has the same fields as a call in a corpus file, so a call sequence can be copied into
the corpus to replay it. It also has a `trace` when the call was traced. By default only the last call of a sequence
is traced; raise the [`verbosity`](../project_configuration/testing_config.md#verbosity) to trace every call.

A `trace` describes one call and everything which happened inside it:

| Field            | Description                                                                         |
| ---------------- | ----------------------------------------------------------------------------------- |
| `kind`           | `call`, `proxyCall` (e.g. a delegate call) or `creation`                            |
| `from`, `to`     | The caller and the called address                                                   |
| `codeAddress`    | The address of the executed code, for a `proxyCall`                                 |
| `contract`       | The name of the contract whose code ran, when known                                 |
| `method`         | The signature of the called method, when known                                      |
| `arguments`      | The decoded arguments, when known                                                   |
| `input`          | The raw call data                                                                   |
| `value`          | The wei sent with the call, as a decimal string                                     |
| `status`         | `success`, `revert`, `panic` (e.g. a failed assertion) or `error` (e.g. out of gas) |
| `error`          | The panic reason, revert reason, custom error or EVM error                          |
| `returnValues`   | The decoded return values, when the call succeeded and they are known               |
| `output`         | The raw returned data                                                               |
| `selfDestructed` | Set when the call executed `SELFDESTRUCT`                                           |
| `steps`          | The nested calls (`call`) and emitted events (`event`), in the order they happened  |

`coverage` has the total `linesCovered` and `linesTotal`, and one entry per source file in `files`:

| Field               | Description                                                                |
| ------------------- | -------------------------------------------------------------------------- |
| `path`              | The path of the source file                                                |
| `linesCovered`      | The number of executable lines which were executed                         |
| `linesTotal`        | The number of executable lines                                             |
| `uncoveredLines`    | The ranges of lines which were never executed                              |
| `revertedOnlyLines` | The ranges of lines which were only executed by calls that reverted        |
| `functions`         | Each named function with the `lines` it spans and whether it was `reached` |

Line ranges have a `start` and an `end`, both 1-based and inclusive. A line counts as covered if it was executed at
all, including by a call which later reverted.
