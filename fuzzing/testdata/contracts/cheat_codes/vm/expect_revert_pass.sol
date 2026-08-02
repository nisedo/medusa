pragma solidity ^0.8.0;

interface Vm {
    function expectRevert() external;
    function expectRevert(bytes4 revertData) external;
    function expectRevert(bytes calldata revertData) external;
    function expectRevert(address reverter) external;
    function expectRevert(bytes4 revertData, address reverter) external;
    function expectRevert(bytes calldata revertData, address reverter) external;
    function expectRevert(uint64 count) external;
    function expectRevert(bytes4 revertData, uint64 count) external;
    function expectRevert(bytes calldata revertData, uint64 count) external;
    function expectRevert(address reverter, uint64 count) external;
    function expectRevert(bytes4 revertData, address reverter, uint64 count) external;
    function expectRevert(bytes calldata revertData, address reverter, uint64 count) external;
    function expectPartialRevert(bytes4 revertData) external;
    function expectPartialRevert(bytes4 revertData, address reverter) external;

    function warp(uint256 timestamp) external;
}

interface Console {
    function log(string calldata message) external;
}

error NoArgsFailure();
error ValueFailure(uint256 value);
error ConstructorFailure();

contract Reverter {
    function revertWithoutData() external pure {
        assembly {
            revert(0, 0)
        }
    }

    function revertNoArgs() external pure {
        revert NoArgsFailure();
    }

    function revertValue(uint256 value) external pure {
        revert ValueFailure(value);
    }

    function revertString() external pure {
        revert("expected reason");
    }

    function succeed() external pure returns (uint256) {
        return 7;
    }
}

contract Bubbler {
    Reverter private immutable reverter;

    constructor(Reverter target) {
        reverter = target;
    }

    function bubbleValueFailure() external view {
        reverter.revertValue(99);
    }
}

contract RevertingDeployment {
    constructor() {
        revert ConstructorFailure();
    }
}

contract TestContract {
    Vm private constant vm = Vm(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);
    Console private constant console =
        Console(0x000000000000000000636F6e736F6c652e6c6f67);

    Reverter private immutable reverter;
    Bubbler private immutable bubbler;

    constructor() {
        reverter = new Reverter();
        bubbler = new Bubbler(reverter);
    }

    // Calling this function invokes every supported expectRevert overload.
    // If even one selector is absent or decoded with the wrong ABI type, this
    // transaction fails.
    function testAllOverloads() external {
        vm.expectRevert();
        reverter.revertWithoutData();

        vm.expectRevert(NoArgsFailure.selector);
        reverter.revertNoArgs();

        vm.expectRevert(_valueFailure(1));
        reverter.revertValue(1);

        vm.expectRevert(address(reverter));
        reverter.revertWithoutData();

        vm.expectRevert(NoArgsFailure.selector, address(reverter));
        reverter.revertNoArgs();

        vm.expectRevert(_valueFailure(2), address(reverter));
        reverter.revertValue(2);

        vm.expectRevert(uint64(2));
        reverter.revertWithoutData();
        reverter.revertWithoutData();

        vm.expectRevert(NoArgsFailure.selector, uint64(2));
        reverter.revertNoArgs();
        reverter.revertNoArgs();

        vm.expectRevert(_valueFailure(3), uint64(2));
        reverter.revertValue(3);
        reverter.revertValue(3);

        vm.expectRevert(address(reverter), uint64(2));
        reverter.revertWithoutData();
        reverter.revertWithoutData();

        vm.expectRevert(
            NoArgsFailure.selector,
            address(reverter),
            uint64(2)
        );
        reverter.revertNoArgs();
        reverter.revertNoArgs();

        vm.expectRevert(
            _valueFailure(4),
            address(reverter),
            uint64(2)
        );
        reverter.revertValue(4);
        reverter.revertValue(4);

        vm.expectPartialRevert(ValueFailure.selector);
        reverter.revertValue(5);

        vm.expectPartialRevert(ValueFailure.selector, address(reverter));
        reverter.revertValue(6);
    }

    function testExactAndPartialData() external {
        vm.expectRevert(_valueFailure(10));
        reverter.revertValue(10);

        vm.expectPartialRevert(ValueFailure.selector);
        reverter.revertValue(11);
    }

    function testErrorString() external {
        vm.expectRevert(bytes("expected reason"));
        reverter.revertString();

        vm.expectRevert(
            abi.encodeWithSignature("Error(string)", "expected reason")
        );
        reverter.revertString();
    }

    function testCountZeroAndTwo() external {
        vm.expectRevert(uint64(0));
        require(reverter.succeed() == 7, "count zero changed success");

        vm.expectRevert(NoArgsFailure.selector, uint64(2));
        reverter.revertNoArgs();
        reverter.revertNoArgs();
    }

    function testSkipsConsoleAndOtherCheatCalls() external {
        vm.expectRevert(NoArgsFailure.selector);
        console.log("expectRevert must skip console");
        vm.warp(block.timestamp + 1);
        reverter.revertNoArgs();
    }

    function testNestedBubbledReverter() external {
        vm.expectRevert(
            _valueFailure(99),
            address(reverter)
        );
        bubbler.bubbleValueFailure();
    }

    function testCallAndCreatePresentation() external {
        vm.expectRevert(_valueFailure(20));
        (bool ok, bytes memory output) = address(reverter).call(
            abi.encodeCall(Reverter.revertValue, (20))
        );
        require(ok, "matched CALL still failed");
        require(output.length == 8192, "CALL did not return dummy output");

        vm.expectRevert(ConstructorFailure.selector);
        RevertingDeployment deployment = new RevertingDeployment();
        require(
            address(deployment) == address(1),
            "CREATE did not return dummy address"
        );
    }

    function testSequentialExpectations() external {
        vm.expectRevert(NoArgsFailure.selector);
        reverter.revertNoArgs();

        vm.expectRevert(_valueFailure(30));
        reverter.revertValue(30);

        vm.expectRevert(address(reverter));
        reverter.revertWithoutData();
    }

    function _valueFailure(uint256 value) private pure returns (bytes memory) {
        return abi.encodeWithSelector(ValueFailure.selector, value);
    }
}
