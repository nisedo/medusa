// The assertion in this contract can only fail after unlock is called with a key the fuzzer cannot guess: the key
// is a hash, so it is not among the constants the fuzzer collects from the source.
contract TestContract {
    bool public unlocked;

    function unlock(uint256 key) public {
        if (key == uint256(keccak256("medusa"))) {
            unlocked = true;
        }
    }

    function checkLocked() public view {
        assert(!unlocked);
    }

    // The same check as a property test.
    function property_locked() public view returns (bool) {
        return !unlocked;
    }

    // Only succeeds ten days after deployment, longer than the fuzzer's default maximum delay between two calls.
    uint256 immutable deployedAt = block.timestamp;

    function afterTenDays() public view {
        require(block.timestamp >= deployedAt + 10 days, "too early");
    }
}
