pragma solidity ^0.8.0;

interface Vm {
    function expectRevert() external;
    function expectRevert(bytes4 revertData) external;
    function expectRevert(bytes calldata revertData) external;
    function expectRevert(address reverter) external;
    function expectRevert(uint64 count) external;

    function snapshot() external returns (uint256);
    function revertTo(uint256 snapshotId) external returns (bool);
}

error ExpectedFailure();
error ActualFailure();
error ValueFailure(uint256 value);

contract Reverter {
    function succeed() external pure returns (uint256) {
        return 7;
    }

    function revertExpected() external pure {
        revert ExpectedFailure();
    }

    function revertActual() external pure {
        revert ActualFailure();
    }

    function revertValue(uint256 value) external pure {
        revert ValueFailure(value);
    }
}

contract SnapshotTarget {
    Vm private constant vm = Vm(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);

    function revertToWhileExpected(uint256 snapshotId) external {
        vm.revertTo(snapshotId);
    }
}

contract DeploymentBeforeMismatch {}

contract TestContract {
    Vm private constant vm = Vm(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);

    Reverter private immutable reverter;
    Reverter private immutable otherReverter;
    SnapshotTarget private immutable snapshotTarget;

    constructor() {
        reverter = new Reverter();
        otherReverter = new Reverter();
        snapshotTarget = new SnapshotTarget();
    }

    function testFailUnexpectedSuccess() external {
        vm.expectRevert();
        reverter.succeed();
    }

    function testFailWrongData() external {
        vm.expectRevert(
            abi.encodeWithSelector(ValueFailure.selector, uint256(1))
        );
        reverter.revertValue(2);
    }

    function testFailExactSelectorIsNotPartial() external {
        vm.expectRevert(ValueFailure.selector);
        reverter.revertValue(1);
    }

    function testFailWrongReverter() external {
        vm.expectRevert(address(otherReverter));
        reverter.revertExpected();
    }

    function testFailDuplicateExpectation() external {
        vm.expectRevert();
        vm.expectRevert();
        reverter.revertExpected();
    }

    function testFailDanglingExpectation() external {
        vm.expectRevert();
    }

    function testFailShortCount() external {
        vm.expectRevert(uint64(2));
        reverter.revertExpected();
    }

    function testFailCaughtMismatch() external {
        vm.expectRevert(ExpectedFailure.selector);
        try reverter.revertActual() {
            revert("call unexpectedly succeeded");
        } catch {
            // A catch must not hide a failed expectation.
        }
    }

    function testFailActiveTargetRevertTo() external {
        uint256 snapshotId = vm.snapshot();
        vm.expectRevert();
        try snapshotTarget.revertToWhileExpected(snapshotId) {
            revert("revertTo unexpectedly succeeded");
        } catch {
            // A catch must not hide invalidating the target frame snapshot.
        }
    }

    function testFailDeploymentBeforeMismatch() external {
        new DeploymentBeforeMismatch();
        vm.expectRevert();
        reverter.succeed();
    }
}
