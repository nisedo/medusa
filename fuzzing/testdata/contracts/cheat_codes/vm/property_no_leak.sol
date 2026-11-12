// This test ensures that cheat codes used inside a property test do not change the chain for later transactions.
interface CheatCodes {
    function warp(uint256) external;
    function roll(uint256) external;
    function fee(uint256) external;
    function coinbase(address) external;
    function chainId(uint256) external;
}

contract TestContract {
    CheatCodes constant cheats = CheatCodes(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);

    // Values no ordinary block reaches, so seeing one in a transaction means a property test leaked it.
    uint256 constant LEAKED = 1e18;
    address constant LEAKED_COINBASE = address(0xBEEF);

    uint256 immutable originalChainId = block.chainid;

    // The property test changes the block environment. Its changes must be discarded after it returns.
    function property_changes_block_environment() public returns (bool) {
        cheats.warp(LEAKED);
        cheats.roll(LEAKED);
        cheats.fee(LEAKED);
        cheats.coinbase(LEAKED_COINBASE);
        cheats.chainId(LEAKED);
        return true;
    }

    // Every transaction checks that it does not observe the property test's changes.
    function checkNoLeak() public view {
        assert(block.timestamp < LEAKED);
        assert(block.number < LEAKED);
        assert(block.basefee < LEAKED);
        assert(block.coinbase != LEAKED_COINBASE);
        assert(block.chainid == originalChainId);
    }
}
