// This test ensures that assume rejects false fuzz inputs and permits true inputs.
interface CheatCodes {
    function assume(bool condition) external;
}

contract TestContract {
    CheatCodes private constant cheats = CheatCodes(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);

    uint256 public acceptedCalls;

    function assumeTrue() public {
        cheats.assume(true);
        assert(true);
    }

    function assumeFalse() public {
        cheats.assume(false);

        // Reaching this assertion means that assume(false) failed to reject the input.
        assert(false);
    }

    function step(bool accepted) public returns (uint256) {
        cheats.assume(accepted);
        acceptedCalls++;
        return acceptedCalls;
    }

    function property_assume() public pure returns (bool) {
        return true;
    }
}
