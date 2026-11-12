# Assertion cheatcodes

The cheatcode contract provides `assertTrue`, `assertFalse`, `assertEq`, `assertNotEq`, `assertLt`, `assertLe`,
`assertGt`, and `assertGe`. A failed check reverts with `Panic(0x01)`, like Solidity's `assert()`. Medusa reports it
as an assertion-test failure when `fuzzing.testing.assertionTesting.panicCodeConfig.failOnAssertion` is enabled,
which is the default.

For example, this check fails when `actual` and `expected` contain different bytes:

```solidity
function check(bytes memory actual, bytes memory expected) public {
    vm.assertEq(actual, expected);
}
```

## Function signatures

```solidity
function assertTrue(bool) external;
function assertFalse(bool) external;

function assertEq(bool, bool) external;
function assertEq(uint256, uint256) external;
function assertEq(int256, int256) external;
function assertEq(address, address) external;
function assertEq(bytes32, bytes32) external;
function assertEq(string calldata, string calldata) external;
function assertEq(bytes calldata, bytes calldata) external;

function assertNotEq(bool, bool) external;
function assertNotEq(uint256, uint256) external;
function assertNotEq(int256, int256) external;
function assertNotEq(address, address) external;
function assertNotEq(bytes32, bytes32) external;
function assertNotEq(string calldata, string calldata) external;
function assertNotEq(bytes calldata, bytes calldata) external;

function assertLt(uint256, uint256) external;
function assertLt(int256, int256) external;
function assertLe(uint256, uint256) external;
function assertLe(int256, int256) external;
function assertGt(uint256, uint256) external;
function assertGt(int256, int256) external;
function assertGe(uint256, uint256) external;
function assertGe(int256, int256) external;
```
