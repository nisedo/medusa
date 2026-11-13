---
name: medusa-campaign
description: Run and steer a medusa fuzzing campaign on Solidity contracts through medusa's MCP server, reaching code the fuzzer cannot reach on its own and triaging failed tests. Use this whenever the user wants to fuzz smart contracts with medusa, says coverage has stalled or plateaued, asks why some function or branch is never reached, wants help getting a fuzzer past a check (a magic value, a signature, a time lock, a specific call order), or wants a failing medusa test explained, even if they do not mention MCP.
---

# Steering a medusa campaign

Medusa fuzzes fast but blind: it cannot satisfy a condition it cannot stumble into, such as a hash comparison, a
value derived from the sender, or a call order that must hold across many calls. You can read the source and work
those conditions out. This skill pairs the two: medusa does the volume, you unblock it where it stalls.

You talk to the running campaign with `scripts/medusa_mcp.py`, which sits next to this file. It is executable: run it
by its full path (this skill's base directory, then `scripts/medusa_mcp.py`), since you will be working from the
project directory. The commands below write `medusa_mcp.py` for that full path. It prints results as JSON, sequence
results as a short summary, and a short error on stderr when a request is wrong.

## Read the source first

Before fuzzing, read the contracts under test and the harness. Note the checks a random fuzzer is unlikely to pass:
hashes or signatures to match, values tied to `msg.sender`, roles, long time locks, calls that must come in a set
order. Note too what the tests assert, and anything that looks wrong. This tells you where the campaign will stall,
and sometimes shows the bug outright; the campaign then confirms it with a concrete sequence.

## Start the campaign

Check that `medusa fuzz --help` lists `--mcp-port`. From the project directory, start medusa in the background, as
it runs until stopped:

```bash
medusa fuzz --mcp-port 8765 --fail-fast=false --timeout 0 --test-limit 0 --corpus-dir corpus --results-file corpus/results.json
```

- `--fail-fast=false` keeps the campaign going after a test fails. Medusa stops at the first failure by default,
  which would end your session the moment you find a bug.
- `--timeout 0 --test-limit 0` lets it run until you stop it.
- `--corpus-dir` keeps the corpus, including the sequences you add, across restarts. Leave it out if the project's
  `medusa.json` already sets `corpusDirectory`.
- Without a `medusa.json`, add `--compilation-target <path> --target-contracts <Name>`. Keep anything the user asked
  for (config file, target contracts).
- Compiling leaves build artifacts in the project (`crytic-export/`, and `slither_results.json` if Slither runs).

If port 8765 is taken, pick another and pass `--port <port>` to the script before the tool name.

## The loop

### 1. Let the fuzzer work, then look

```bash
medusa_mcp.py wait
```

`wait` returns once there is something to look at: a test failed, coverage has not grown for 60 seconds
(`--stall`, counted from when coverage last grew), or the campaign ended. Its output gives the `reason` and
`failedTests`, the number of tests failed so far; check it every time, since a test can fail before a `wait` starts.
`wait` also waits for the server to start, so run it right after starting medusa. It gives up after 300 seconds
(`--max`), so give the command a timeout above that. While coverage keeps growing, the fuzzer is doing fine on its
own; your effort pays off once it stalls.

### 2. Find what was not reached

```bash
medusa_mcp.py coverage
medusa_mcp.py coverage '{"file": "Vault.sol"}'
```

The summary lists, per file, `uncoveredLines` and each function with `reached`. With a `file` (its path or the end
of its path) you also get the source with a marker per line: `+` executed, `-` never executed, `r` only executed by
calls that reverted, blank for lines that are not executable.

The telling pattern is a `+` check followed by `-` lines: the fuzzer reaches the `require` or `if` but never gets
past it. Go after the code that matters: state changes, transfers, what sits behind access checks, and what the
tests' assertions depend on. Property and optimization test methods run outside transactions, so coverage never
marks them as reached; ignore them.

`medusa_mcp.py target` lists the deployed contracts, their methods and the `senders` the fuzzer calls from. Check a line is
reachable before chasing it: code behind `onlyOwner` is out of reach if the owner is not a sender. Say so rather than
forcing it.

### 3. Try a sequence

```bash
medusa_mcp.py execute_sequence @sequence.json
```

```json
{
  "calls": [
    {
      "contract": "Vault",
      "method": "deposit",
      "value": "1000000000000000000",
      "sender": "0x10000"
    },
    {
      "contract": "Vault",
      "method": "withdraw",
      "arguments": ["400", "0xab12..."],
      "sender": "0x10000",
      "blockTimestampDelay": 864000
    }
  ]
}
```

Each call takes `contract` (name, or address if several share the name), `method` (name, or full signature if
overloaded), `arguments` in order, and optionally `sender` (defaults to the first sender), `value`,
`blockNumberDelay` and `blockTimestampDelay` (seconds to move time forward before the call). Integers go as decimal
or `0x` strings (small ones may be plain numbers), addresses and bytes as hex strings, an address can be
`"DeployedContract:<Name>"`, arrays as arrays, structs as objects keyed by field name. Every sequence starts from the
state right after deployment.

Executing changes nothing, so iterate freely. The script prints one line per call: what was called, its outcome
(`success`, `revert`, `panic` for a failed assertion or other Solidity panic, or `error`) with the revert reason or
custom error, and `NEW COVERAGE` when the call reached code the corpus has not. After the calls it lists the tests
the sequence fails: assertion and property tests are checked after every call, as the fuzzer does. A revert names
the precondition you still miss. Add `--json` for the full result, including each call's trace with nested calls and
events under `steps`.

Compute the values a check expects (hashes, encodings, signatures) rather than guessing: use `cast` if installed
(`cast keccak`, `cast abi-encode`), or a short Python script.

### 4. Hand it to the fuzzer

When a call in the sequence reaches new code, add the sequence:

```bash
medusa_mcp.py add_sequence @sequence.json
```

A worker replays it shortly after: its coverage is recorded, any test it fails is recorded and shrunk, and the
fuzzer mutates it from then on, which is how one unlocked branch turns into many explored ones. Only add sequences
that reach new code; the rest dilute the corpus. Add the shortest sequence that unlocks the code rather than a full
exploit, and let the fuzzer explore from there. Then go back to step 1 with a longer stall, such as
`wait --stall 120`: mutations of a new seed take a minute or two to pay off.

The fuzzer moves time by at most 7 days between two of its own calls (`blockTimestampDelayMax`), so a longer lock
only opens across several calls. A sequence you add keeps its delays when the fuzzer mutates it.

### 5. Read failed tests

```bash
medusa_mcp.py tests '{"status": "failed"}'
```

A failed test has the shrunk `callSequence` that fails it, and `message`, the readable call sequence with execution
traces, as medusa prints it. Only the last call carries a structured `trace`; a property test also has the property
method's `trace`. Work out why it fails, then decide whether it is a real bug in the contracts or an artifact of the
harness (a wrong invariant, a setup the real system would never be in). Do not report a harness artifact as a
vulnerability.

## When the harness is the obstacle

If reaching some code needs something the harness does not offer (a contract that is not deployed, a role no sender
holds, a function it does not expose, a delay bound too small), tell the user what is missing and propose the change.
Do not edit the project's contracts, harness or config on your own: they are the user's work. Medusa compiles at
start, so a change needs a restart; the corpus directory keeps everything found so far.

## Stopping

Stop the background process. Medusa ends gracefully on an interrupt or termination signal: it saves the corpus and
writes the results file, which has the final tests, traces and coverage.

## Reporting back

Tell the user, briefly: which tests failed and whether each is a real bug or a harness artifact, with the root cause
and the shortest failing sequence; what you unlocked and how (the condition and what satisfied it); and what remains
unreached and why. Line coverage from `coverage` before and after shows the effect.
