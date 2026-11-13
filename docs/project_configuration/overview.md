# Configuration Overview

`medusa`'s project configuration provides extensive and granular control over the execution of the fuzzer. The project
configuration is a `.json` file with four top-level sections: `fuzzing`, `compilation`, `slither`, and `logging`.
Testing and chain settings are nested at `fuzzing.testing` and `fuzzing.chainConfig`.

- [Fuzzing Configuration](./fuzzing_config.md): Settings under `fuzzing`.
- [Testing Configuration](./testing_config.md): Test selection and failure settings under `fuzzing.testing`.
- [Chain Configuration](./chain_config.md): EVM settings under `fuzzing.chainConfig`.
- [Compilation Configuration](./compilation_config.md): Compilation settings under `compilation`.
- [Slither Configuration](./slither_config.md): Settings under `slither` that control whether Slither should be used in
  `medusa` and whether the results from Slither should be cached.
- [Logging Configuration](./logging_config.md): Logging settings under `logging`.

To generate a project configuration file, run [`medusa init`](../cli/init.md).

You can also view this [example project configuration file](../static/medusa.json) for visualization.

## Recommended Configuration

A common issue that first-time users face is identifying which configuration options to change. `medusa` provides an
incredible level of flexibility on how the fuzzer should run but this comes with a tradeoff of understanding the nuances
of what configuration options control what feature. Outlined below is a list of configuration options that we recommend
you become familiar with and change before starting to fuzz test.

> **Note:** Having an [example project configuration file](../static/medusa.json) open will aid in visualizing which
> configuration options to change.

### `fuzzing.targetContracts`

Updating this configuration option is **required**! The `targetContracts` configuration option tells `medusa` which contracts
to fuzz test. You can specify one or more contracts for this option which is why it accepts an array
of strings. Let's say you have a fuzz testing contract called `TestStakingContract` that you want to test.
Then, you would set the value of `targetContracts` to `["TestStakingContract"]`.
You can learn more about this option [here](./fuzzing_config.md#targetcontracts).

### `fuzzing.testLimit`

Updating test limit is optional but recommended. Test limit determines how many transactions `medusa` will execute before
stopping the fuzzing campaign. By default, the `testLimit` is set to 0. This means that `medusa` will run indefinitely.
While you iterate over your fuzz tests, it is beneficial to have a non-zero value. Thus, it is recommended to update this
value to `10_000` or `100_000` depending on the use case. You can learn more about this option [here](./fuzzing_config.md#testlimit).

### `fuzzing.corpusDirectory`

Updating the corpus directory is optional but recommended. The corpus directory determines where corpus items should be
stored on disk. A corpus item is a sequence of transactions that increased `medusa`'s coverage of the system. Thus, these
corpus items are valuable to store so that they can be re-used for the next fuzzing campaign. Additionally, the directory
will also hold [coverage reports](../testing/reporting.md) which are valuable tools for debugging and validation. For most cases, you may set
`corpusDirectory`'s value to "corpus". This will create a `corpus/` directory in the same directory as the `medusa.json`
file.
You can learn more about this option [here](./fuzzing_config.md#corpusdirectory).
