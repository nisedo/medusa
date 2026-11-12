# `startPrank`

## Description

The `startPrank` cheatcode will set the `msg.sender` for all subsequent calls until `stopPrank` is called. This is in
contrast to [`prank`](./prank.md) where `prank` only applies to the _next_ message call.

For a pranked `CALL` that sends ETH, the value is deducted from the pranked sender's balance.
The pranked sender must have enough ETH to fund the transfer.

## Example

```solidity
contract TestContract {
    address owner = address(123);
    ImportantContract ic;
    AnotherImportantContract aic;

    function deployImportantContract() public {
        require(msg.sender == owner);

        // Deploy important contract
        ic = new ImportantContract(msg.sender);
    }

    function deployAnotherImportantContract() public {
        require(msg.sender == owner);

        // Deploy another important contract
        aic = new AnotherImportantContract(msg.sender);
    }

    function test() public {
        // Obtain our cheat code contract reference.
        IStdCheats cheats = IStdCheats(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);

        // Prank and deploy important contracts
        cheats.startPrank(owner);
        deployImportantContract();
        deployAnotherImportantContract();
        cheats.stopPrank();

        assert(ic.owner() == owner);
        assert(aic.owner() == owner);
    }
```

## Function Signature

```solidity
function startPrank(address) external;
```
