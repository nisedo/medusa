// This test ensures prank and startPrank use the spoofed msg.sender as the
// balance source for value-bearing CALLs.
interface CheatCodes {
    function deal(address account, uint256 newBalance) external;
    function prank(address msgSender) external;
    function startPrank(address msgSender) external;
    function stopPrank() external;
}

error ObservedAndReverted(address sender, uint256 value);

contract SenderSink {
    address public lastSender;
    uint256 public lastValue;

    function deposit() external payable {
        lastSender = msg.sender;
        lastValue = msg.value;
    }

    function depositAndRevert() external payable {
        lastSender = msg.sender;
        lastValue = msg.value;
        revert ObservedAndReverted(msg.sender, msg.value);
    }
}

contract SenderObserver {
    address public immutable constructorSender;

    constructor() {
        constructorSender = msg.sender;
    }

    function sender() external view returns (address) {
        return msg.sender;
    }
}

contract TestContract {
    CheatCodes private constant vm = CheatCodes(0x7109709ECfa91a80626fF3989D68f67F5b1DD12D);
    address private constant actor = address(0xBEEF);
    uint256 private constant callValue = 1 ether;

    function test() public {
        testOrdinaryCall();
        testOneShotPrank();
        testStartPrank();
        testNonCallSenderBehavior();
    }

    function testOrdinaryCall() internal {
        SenderSink sink = new SenderSink();
        vm.deal(address(this), 2 ether);
        vm.deal(actor, 0);

        (bool ok,) = address(sink).call{value: callValue}(abi.encodeCall(SenderSink.deposit, ()));

        assert(ok);
        assert(sink.lastSender() == address(this));
        assert(sink.lastValue() == callValue);
        assert(address(this).balance == 1 ether);
        assert(actor.balance == 0);
        assert(address(sink).balance == callValue);
    }

    function testOneShotPrank() internal {
        // A funded harness must not pay for an empty pranked actor. The failed
        // CALL consumes the one-shot prank, so the following ordinary CALL is
        // paid by this contract.
        SenderSink insufficientSink = new SenderSink();
        vm.deal(address(this), 2 ether);
        vm.deal(actor, 0);
        vm.prank(actor);
        (bool ok,) = address(insufficientSink).call{value: callValue}(
            abi.encodeCall(SenderSink.deposit, ())
        );
        assert(!ok);
        assert(address(this).balance == 2 ether);
        assert(actor.balance == 0);
        assert(address(insufficientSink).balance == 0);
        assert(insufficientSink.lastSender() == address(0));
        assert(insufficientSink.lastValue() == 0);

        (ok,) = address(insufficientSink).call{value: callValue}(abi.encodeCall(SenderSink.deposit, ()));
        assert(ok);
        assert(insufficientSink.lastSender() == address(this));
        assert(insufficientSink.lastValue() == callValue);
        assert(address(this).balance == 1 ether);
        assert(address(insufficientSink).balance == callValue);

        // A funded actor must pay even when the harness is empty.
        SenderSink fundedSink = new SenderSink();
        vm.deal(address(this), 0);
        vm.deal(actor, 2 ether);
        vm.prank(actor);
        (ok,) = address(fundedSink).call{value: callValue}(abi.encodeCall(SenderSink.deposit, ()));
        assert(ok);
        assert(fundedSink.lastSender() == actor);
        assert(fundedSink.lastValue() == callValue);
        assert(address(this).balance == 0);
        assert(actor.balance == 1 ether);
        assert(address(fundedSink).balance == callValue);

        // A reverting callee observes the actor and value, then the EVM rolls
        // the actor-to-sink transfer and storage writes back.
        SenderSink revertingSink = new SenderSink();
        vm.deal(address(this), 0);
        vm.deal(actor, 2 ether);
        vm.prank(actor);
        bytes memory revertData;
        (ok, revertData) = address(revertingSink).call{value: callValue}(
            abi.encodeCall(SenderSink.depositAndRevert, ())
        );
        assert(!ok);
        assertRevertObservedActor(revertData);
        assert(address(this).balance == 0);
        assert(actor.balance == 2 ether);
        assert(address(revertingSink).balance == 0);
        assert(revertingSink.lastSender() == address(0));
        assert(revertingSink.lastValue() == 0);
    }

    function testStartPrank() internal {
        SenderSink sink = new SenderSink();

        // An insufficient-balance failure does not consume startPrank.
        vm.deal(address(this), 2 ether);
        vm.deal(actor, 0);
        vm.startPrank(actor);
        (bool ok,) = address(sink).call{value: callValue}(abi.encodeCall(SenderSink.deposit, ()));
        assert(!ok);
        assert(address(this).balance == 2 ether);
        assert(actor.balance == 0);
        assert(address(sink).balance == 0);

        // Cheatcode calls do not consume the persistent prank. After funding
        // the actor and emptying the harness, the next CALL succeeds from it.
        vm.deal(actor, 2 ether);
        vm.deal(address(this), 0);
        (ok,) = address(sink).call{value: callValue}(abi.encodeCall(SenderSink.deposit, ()));
        assert(ok);
        assert(sink.lastSender() == actor);
        assert(sink.lastValue() == callValue);
        assert(address(this).balance == 0);
        assert(actor.balance == 1 ether);
        assert(address(sink).balance == callValue);

        // Revert rolls back the transfer and storage while startPrank remains
        // active for the next direct CALL.
        SenderSink revertingSink = new SenderSink();
        bytes memory revertData;
        (ok, revertData) = address(revertingSink).call{value: callValue}(
            abi.encodeCall(SenderSink.depositAndRevert, ())
        );
        assert(!ok);
        assertRevertObservedActor(revertData);
        assert(actor.balance == 1 ether);
        assert(address(revertingSink).balance == 0);
        assert(revertingSink.lastSender() == address(0));
        assert(revertingSink.lastValue() == 0);

        (ok,) = address(sink).call{value: callValue}(abi.encodeCall(SenderSink.deposit, ()));
        assert(ok);
        assert(sink.lastSender() == actor);
        assert(sink.lastValue() == callValue);
        assert(actor.balance == 0);
        assert(address(sink).balance == 2 ether);

        // stopPrank restores ordinary value semantics.
        vm.stopPrank();
        SenderSink ordinarySink = new SenderSink();
        vm.deal(address(this), callValue);
        (ok,) = address(ordinarySink).call{value: callValue}(abi.encodeCall(SenderSink.deposit, ()));
        assert(ok);
        assert(ordinarySink.lastSender() == address(this));
        assert(ordinarySink.lastValue() == callValue);
        assert(address(this).balance == 0);
        assert(actor.balance == 0);
        assert(address(ordinarySink).balance == callValue);
    }

    function testNonCallSenderBehavior() internal {
        SenderObserver observer = new SenderObserver();

        // Solidity compiles this view call as STATICCALL. A one-shot prank is
        // applied to it and consumed instead of leaking into a later CALL.
        vm.prank(actor);
        assert(observer.sender() == actor);
        assert(observer.sender() == address(this));

        // startPrank retains the existing sender behavior across STATICCALLs.
        vm.startPrank(actor);
        assert(observer.sender() == actor);
        assert(observer.sender() == actor);
        vm.stopPrank();
        assert(observer.sender() == address(this));

        // Constructor msg.sender behavior is also preserved for CREATE.
        vm.prank(actor);
        SenderObserver created = new SenderObserver();
        assert(created.constructorSender() == actor);
        assert(observer.sender() == address(this));
    }

    function assertRevertObservedActor(bytes memory revertData) internal pure {
        bytes memory expected = abi.encodeWithSelector(
            bytes4(keccak256("ObservedAndReverted(address,uint256)")),
            actor,
            callValue
        );
        assert(keccak256(revertData) == keccak256(expected));
    }
}
