# `assume`

The `assume` cheatcode rejects a fuzz input when its condition is false.

```solidity
function assume(bool condition) external;
```

Use it to restrict generated arguments to values that make sense for the handler:

```solidity
function deposit(uint256 amount) public {
    vm.assume(amount > 0 && amount <= 1_000 ether);
    vault.deposit(amount);
}
```

When `amount` is zero or greater than 1,000 ether, Medusa rejects that call before it runs invariant checks or records
the call in the coverage corpus, call count, gas total, or revert report. Medusa preserves the accepted call prefix and
generates a replacement at the same sequence depth.

Medusa preserves the prefix by resetting the worker chain and replaying its accepted calls. Those calls must be
deterministic. Do not combine `vm.assume` with accepted-prefix calls that have off-chain side effects, such as `vm.ffi`:
replay can repeat those side effects.

`assume(false)` is not an ordinary target revert. For example, `require(amount > 0)` records a reverted target call and
still runs configured tests, while `vm.assume(amount > 0)` rejects that generated input.

Medusa allows `fuzzing.maxAssumeRejects` rejected inputs while generating one call sequence and stops on the next
rejection. The default is 65,536; set it to `0` to disable the limit.
