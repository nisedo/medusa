// This test ensures that we can take a snapshot of the current state of the testchain and revert to the state at that snapshot using the snapshot and revertTo cheatcodes
pragma solidity ^0.8.0;

interface CheatCodes {
    function warp(uint256) external;

    function roll(uint256) external;

    function fee(uint256) external;

    function prevrandao(bytes32) external;

    function coinbase(address) external;

    function chainId(uint256) external;

    function deal(address, uint256) external;

    function snapshot() external returns (uint256);

    function revertTo(uint256) external returns (bool);
}

error SnapshotCreated(uint256 snapshotId);
error ExpectedRollback();

struct Storage {
    uint slot0;
    uint slot1;
}

contract SnapshotChild {
    CheatCodes private constant cheats =
        CheatCodes(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);

    function revertAfterSameScopeSnapshot() external {
        uint256 snapshotId = cheats.snapshot();
        require(cheats.revertTo(snapshotId), "same-scope revertTo failed");
        revert ExpectedRollback();
    }

    function createSnapshotAndRevert() external {
        revert SnapshotCreated(cheats.snapshot());
    }

    function rollAndRevert(uint256 newBlockNumber) external {
        cheats.roll(newBlockNumber);
        revert ExpectedRollback();
    }

    function createThenSnapshot() external returns (uint256, address) {
        Ephemeral ephemeral = new Ephemeral();
        return (cheats.snapshot(), address(ephemeral));
    }
}

contract Ephemeral {}

contract SelfDestructor {
    function destroy() external {
        selfdestruct(payable(msg.sender));
    }
}

contract TestContract {
    CheatCodes private constant cheats =
        CheatCodes(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);

    Storage store;
    uint256 timestamp;
    SnapshotChild private immutable child = new SnapshotChild();
    SelfDestructor private immutable selfDestructor = new SelfDestructor();

    function test() public {
        store.slot0 = 10;
        store.slot1 = 20;
        timestamp = block.timestamp;
        cheats.deal(address(this), 5 ether);

        // Save state
        uint256 snapshot = cheats.snapshot();

        // Change state
        store.slot0 = 300;
        store.slot1 = 400;
        cheats.deal(address(this), 500 ether);
        cheats.warp(12345);

        // Assert that state has been changed
        assert(store.slot0 == 300);
        assert(store.slot1 == 400);
        assert(address(this).balance == 500 ether);
        assert(block.timestamp == 12345);

        // Revert to snapshot
        require(cheats.revertTo(snapshot), "revertTo failed");

        // Ensure state has been reset
        assert(store.slot0 == 10);
        assert(store.slot1 == 20);
        assert(address(this).balance == 5 ether);
        assert(block.timestamp == timestamp);
    }

    function testManagedBlockState() public {
        uint256 originalTime = block.timestamp;
        uint256 originalNumber = block.number;
        uint256 originalBaseFee = block.basefee;
        uint256 originalRandom = block.prevrandao;
        address originalCoinbase = block.coinbase;
        uint256 originalChainId = block.chainid;
        uint256 snapshotId = cheats.snapshot();

        cheats.warp(originalTime + 100);
        cheats.roll(originalNumber + 100);
        cheats.fee(originalBaseFee + 100);
        cheats.prevrandao(bytes32(uint256(0x1234)));
        cheats.coinbase(address(0xCAFE));
        cheats.chainId(originalChainId + 100);

        assert(block.timestamp == originalTime + 100);
        assert(block.number == originalNumber + 100);
        assert(block.basefee == originalBaseFee + 100);
        assert(block.prevrandao == uint256(0x1234));
        assert(block.coinbase == address(0xCAFE));
        assert(block.chainid == originalChainId + 100);
        require(cheats.revertTo(snapshotId), "managed-state revertTo failed");

        assert(block.timestamp == originalTime);
        assert(block.number == originalNumber);
        assert(block.basefee == originalBaseFee);
        assert(block.prevrandao == originalRandom);
        assert(block.coinbase == originalCoinbase);
        assert(block.chainid == originalChainId);
    }

    function testSameScopeSnapshotThenOwnerRevert() public {
        try child.revertAfterSameScopeSnapshot() {
            assert(false);
        } catch (bytes memory reason) {
            assert(bytes4(reason) == ExpectedRollback.selector);
        }
    }

    function testChildSnapshotExpiresOnRevert() public {
        try child.createSnapshotAndRevert() {
            assert(false);
        } catch (bytes memory reason) {
            assert(bytes4(reason) == SnapshotCreated.selector);
            uint256 snapshotId = abi.decode(
                _withoutSelector(reason),
                (uint256)
            );
            assert(!cheats.revertTo(snapshotId));
        }
    }

    function testRollRestoresWhenChildReverts() public {
        uint256 originalNumber = block.number;
        try child.rollAndRevert(originalNumber + 100) {
            assert(false);
        } catch (bytes memory reason) {
            assert(bytes4(reason) == ExpectedRollback.selector);
        }
        assert(block.number == originalNumber);
    }

    function testDeploymentMetadataReverted() public {
        uint256 snapshotId = cheats.snapshot();
        Ephemeral ephemeral = new Ephemeral();
        assert(address(ephemeral).code.length > 0);
        selfDestructor.destroy();

        require(cheats.revertTo(snapshotId), "deployment revertTo failed");
        assert(address(ephemeral).code.length == 0);
        assert(address(selfDestructor).code.length > 0);
    }

    function testChildSnapshotUsedByParent() public {
        (uint256 snapshotId, address retainedAddress) = child
            .createThenSnapshot();
        Ephemeral revertedDeployment = new Ephemeral();

        require(cheats.revertTo(snapshotId), "ancestor revertTo failed");
        assert(retainedAddress.code.length > 0);
        assert(address(revertedDeployment).code.length == 0);
    }

    function _withoutSelector(
        bytes memory data
    ) private pure returns (bytes memory result) {
        require(data.length >= 4, "missing selector");
        result = new bytes(data.length - 4);
        for (uint256 i = 4; i < data.length; i++) {
            result[i - 4] = data[i];
        }
    }
}
