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
}
