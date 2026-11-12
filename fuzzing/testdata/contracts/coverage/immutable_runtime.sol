// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

contract ImmutableCoverage {
    uint256 public immutable step;
    uint256 public total;

    constructor(uint256 step_) {
        step = step_;
    }

    function advance() external {
        total += step;
    }
}

contract OrdinaryCoverage {
    uint256 public total;

    function advance() external {
        total += 1;
    }
}
