# `expectRevert` and `expectPartialRevert`

Use `expectRevert` before an external call that should revert. A matching revert satisfies the expectation and the call
appears successful to the calling Solidity code.

```solidity
error Unauthorized();

function checkUnauthorized() public {
    vm.expectRevert(Unauthorized.selector);
    target.restrictedCall();
}
```

The expectation selects the next direct child EVM call or contract creation from the frame that set it. Calls to the
cheatcode and console addresses do not count. An internal Solidity call creates no EVM call frame, so it cannot satisfy
an expectation by itself:

```solidity
vm.expectRevert();
_internalFunctionThatReverts(); // Fails: there was no external EVM call.
```

## Revert-data matching

- `expectRevert()` accepts any revert.
- A `bytes4` argument requires the complete revert data to equal those four bytes. This works for a custom error with
  no arguments.
- A `bytes` argument requires the complete revert data to match. For a custom error with arguments, pass its full ABI
  encoding.
- `expectPartialRevert(bytes4)` compares only the first four bytes. Use it to match the selector of a custom error that
  has arguments.
- A raw UTF-8 reason passed as `bytes`, such as `bytes("denied")`, matches the same reason encoded as `Error(string)` or
  `CheatcodeError(string)`.

For example, `ValueFailure(uint256)` includes an encoded argument, so selector-only exact matching fails. Use either
the full data or partial matching:

```solidity
error ValueFailure(uint256 value);

vm.expectRevert(abi.encodeWithSelector(ValueFailure.selector, 7));
target.failWithValue(7);

vm.expectPartialRevert(ValueFailure.selector);
target.failWithValue(8);
```

## Reverter and count

Overloads with `address reverter` also require that address to be the deepest frame in the selected call tree that
reverted. This lets a test distinguish a target's own revert from a revert in one of its dependencies:

```solidity
vm.expectRevert(TokenError.selector, address(token));
vault.callToken();
```

Overloads with `uint64 count` apply the same expectation to the next `count` direct external calls. Each call must
match. A count of zero has the opposite requirement: the next selected call must succeed.

```solidity
vm.expectRevert(TokenError.selector, uint64(2));
token.fail();
token.fail();

vm.expectRevert(uint64(0));
token.succeed();
```

## Failure behavior

Only one expectation may be pending at a time across the active call tree. Medusa produces `Panic(0x01)` when:

- the selected call succeeds when a revert was required;
- its revert data or reverter does not match;
- a count finishes early;
- the frame exits without making a selected external call; or
- code sets a second expectation while one is pending.

The failure remains active through nested calls, so Solidity `try`/`catch` cannot hide it. A new top-level transaction
clears the internal expectation state. `snapshot` and `revertTo` cannot cancel a pending or failed expectation.

For a matched revert, `CALL`, `CALLCODE`, `DELEGATECALL`, and `STATICCALL` return success with 8,192 zero bytes.
`CREATE` and `CREATE2` return the nonzero placeholder address `address(1)`. Do not rely on return data from a call whose
revert was expected.

## Function signatures

Medusa implements these 14 overloads:

```solidity
function expectRevert() external;
function expectRevert(bytes4 revertData) external;
function expectRevert(bytes calldata revertData) external;
function expectRevert(address reverter) external;
function expectRevert(bytes4 revertData, address reverter) external;
function expectRevert(bytes calldata revertData, address reverter) external;

function expectRevert(uint64 count) external;
function expectRevert(bytes4 revertData, uint64 count) external;
function expectRevert(bytes calldata revertData, uint64 count) external;
function expectRevert(address reverter, uint64 count) external;
function expectRevert(bytes4 revertData, address reverter, uint64 count) external;
function expectRevert(bytes calldata revertData, address reverter, uint64 count) external;

function expectPartialRevert(bytes4 revertData) external;
function expectPartialRevert(bytes4 revertData, address reverter) external;
```
