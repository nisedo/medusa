# `setNonce`

## Description

The `setNonce` cheatcode sets the nonce of `account` to `nonce`. Unlike Foundry, Medusa does not require the new nonce
to exceed the current one.

## Example

```solidity
// Obtain our cheat code contract reference.
IStdCheats cheats = IStdCheats(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);

// Set nonce and verify
address acc = address(msg.sender);
cheats.setNonce(acc, 7);
assert(cheats.getNonce(acc) == 7);
```

## Function Signature

```solidity
function setNonce(address account, uint64 nonce) external;
```
