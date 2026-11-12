# `prank`

## Description

The `prank` cheatcode will set the `msg.sender` for _only the next call_ to the specified input address.
Calls to the cheatcode and console addresses do not consume the prank.

For a pranked `CALL` that sends ETH, the value is deducted from the pranked sender's balance.
The pranked sender must have enough ETH to fund the transfer.

## Example

```solidity
contract TestContract {
    address owner = address(123);
    function transferOwnership(address _newOwner) public {
        require(msg.sender == owner);

        // Change ownership
        owner = _newOwner;
    }

    function test() public {
        // Obtain our cheat code contract reference.
        IStdCheats cheats = IStdCheats(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);

        // Prank, change ownership, and verify
        address newOwner = address(456);
        cheats.prank(owner);
        transferOwnership(newOwner);
        assert(owner == newOwner);
    }
```

## Function Signature

```solidity
function prank(address) external;
```
