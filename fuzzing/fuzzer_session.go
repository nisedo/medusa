package fuzzing

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/crytic/medusa-geth/accounts/abi"
	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa/chain"
	"github.com/crytic/medusa/fuzzing/calls"
	"github.com/crytic/medusa/fuzzing/config"
	fuzzerTypes "github.com/crytic/medusa/fuzzing/contracts"
	"github.com/crytic/medusa/fuzzing/coverage"
	"github.com/crytic/medusa/fuzzing/executiontracer"
	"github.com/crytic/medusa/fuzzing/report"
	"github.com/crytic/medusa/fuzzing/valuegeneration"
	"github.com/crytic/medusa/utils"
)

// ErrNoCampaignRunning is returned when an operation needs a running fuzzing campaign and there is none.
var ErrNoCampaignRunning = errors.New("no fuzzing campaign is running")

// CallInput describes one call of a call sequence supplied from outside the fuzzer, e.g. by an agent. It names what
// to call rather than how: the fuzzer resolves addresses, encodes arguments and fills in gas and nonces.
type CallInput struct {
	// Contract is the name of a deployed contract, or its address if several contracts with that name are deployed.
	Contract string `json:"contract" jsonschema:"Name of a deployed contract, or its address if several contracts with that name are deployed"`

	// Method is the name of the method to call, or its full signature if the name is overloaded.
	Method string `json:"method" jsonschema:"Name of the method to call, or its full signature (e.g. transfer(address,uint256)) if the name is overloaded"`

	// Arguments are the arguments of the call, in order. Integers of any size are decimal or 0x-prefixed strings,
	// addresses and bytes are hex strings, arrays are JSON arrays and structs are JSON objects keyed by field name.
	// An address can also be "DeployedContract:<name>" to refer to a deployed contract.
	Arguments []any `json:"arguments,omitempty" jsonschema:"Arguments in order. Integers are decimal or 0x strings, addresses and bytes are hex strings, arrays are JSON arrays, structs are objects keyed by field name. An address may be DeployedContract:<name>"`

	// Sender is the address which makes the call. If empty, the first configured sender is used.
	Sender string `json:"sender,omitempty" jsonschema:"Address which makes the call. Defaults to the first configured sender"`

	// Value is the amount of wei to send with the call, as a decimal or 0x-prefixed string. If empty, none is sent.
	Value string `json:"value,omitempty" jsonschema:"Wei to send with the call, as a decimal or 0x string. Defaults to 0"`

	// BlockNumberDelay is the number of blocks to advance before the call.
	BlockNumberDelay uint64 `json:"blockNumberDelay,omitempty" jsonschema:"Number of blocks to advance before the call"`

	// BlockTimestampDelay is the number of seconds to advance the block timestamp by before the call.
	BlockTimestampDelay uint64 `json:"blockTimestampDelay,omitempty" jsonschema:"Seconds to advance the block timestamp by before the call"`
}

// SequenceResult describes what happened when a call sequence supplied from outside the fuzzer was executed.
type SequenceResult struct {
	// Calls describes each call which was executed, with its execution trace. Each call has the fields of a corpus
	// call sequence element.
	Calls []report.Call `json:"calls"`

	// NewCoverage lists, for each call, whether it reached code no call sequence in the corpus has reached.
	NewCoverage []bool `json:"newCoverage"`

	// AddedToCorpus indicates whether the call sequence was added to the corpus. It is always false for a call
	// sequence which was only executed, and for one the corpus already had.
	AddedToCorpus bool `json:"addedToCorpus"`
}

// sequenceSession executes call sequences supplied from outside the fuzzer, on its own copy of the chain. It exists
// for as long as a fuzzing campaign is running.
type sequenceSession struct {
	// fuzzer is the Fuzzer the session belongs to.
	fuzzer *Fuzzer

	// lock ensures one call sequence is executed at a time, as they share the chain.
	lock sync.Mutex

	// chain is the session's own copy of the chain, at the state every call sequence starts from.
	chain *chain.TestChain

	// baseBlockIndex is the block index the chain is reverted to after each call sequence.
	baseBlockIndex uint64

	// deployedContracts maps the address of each contract deployed on the chain to its definition.
	deployedContracts map[common.Address]*fuzzerTypes.Contract
}

// newSequenceSession creates a sequenceSession for the provided Fuzzer, cloning the provided chain.
func newSequenceSession(fuzzer *Fuzzer, baseTestChain *chain.TestChain) (*sequenceSession, error) {
	session := &sequenceSession{
		fuzzer:            fuzzer,
		deployedContracts: make(map[common.Address]*fuzzerTypes.Contract),
	}

	// Clone the chain, tracking which contracts are deployed and collecting coverage, as a worker does.
	var err error
	session.chain, err = baseTestChain.Clone(func(initializedChain *chain.TestChain) error {
		initializedChain.Events.ContractDeploymentAddedEventEmitter.Subscribe(func(event chain.ContractDeploymentsAddedEvent) error {
			matchedContract := fuzzer.contractDefinitions.MatchBytecode(event.Contract.InitBytecode, event.Contract.RuntimeBytecode)
			if matchedContract != nil {
				session.deployedContracts[event.Contract.Address] = matchedContract
			}
			return nil
		})
		initializedChain.Events.ContractDeploymentRemovedEventEmitter.Subscribe(func(event chain.ContractDeploymentsRemovedEvent) error {
			delete(session.deployedContracts, event.Contract.Address)
			return nil
		})
		if fuzzer.config.Fuzzing.CoverageEnabled {
			coverageTracer := coverage.NewCoverageTracerWithRuntimeBytecodeResolver(fuzzer.coverageRuntimeBytecodeResolver)
			initializedChain.AddTracer(coverageTracer.NativeTracer(), true, false)
		}
		initializedChain.Labels = maps.Clone(baseTestChain.Labels)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Register contracts which were loaded from genesis state rather than deployed.
	if len(fuzzer.config.Fuzzing.TestChainConfig.GenesisContractMappings) > 0 {
		if err = fuzzer.applyGenesisContractMappings(session.deployedContracts); err != nil {
			session.chain.Close()
			return nil, err
		}
	}
	session.baseBlockIndex = uint64(len(session.chain.CommittedBlocks()))
	return session, nil
}

// close releases the session's chain. It waits for a call sequence which is being executed to finish.
func (s *sequenceSession) close() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.chain.Close()
}

// execute runs the provided call sequence on the session's chain and reverts the chain afterwards.
// Returns the result, and the executed call sequence, or an error if a call could not be built or executed.
func (s *sequenceSession) execute(inputs []CallInput) (*SequenceResult, calls.CallSequence, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Build every call up front, so a mistake in a later call is reported before anything is executed.
	callSequence := make(calls.CallSequence, len(inputs))
	for i, input := range inputs {
		element, err := s.buildElement(input)
		if err != nil {
			return nil, nil, fmt.Errorf("call %d: %w", i+1, err)
		}
		callSequence[i] = element
	}

	// Execute the calls with an execution tracer attached, filling in chain-dependent fields (e.g. the nonce) as
	// each call is reached. The chain is reverted however this ends.
	executionTracer := executiontracer.NewExecutionTracer(s.fuzzer.contractDefinitions, s.chain, config.VeryVeryVerbose)
	defer executionTracer.Close()
	fetchElementFunc := func(currentIndex int) (*calls.CallSequenceElement, error) {
		if currentIndex >= len(callSequence) {
			return nil, nil
		}
		callSequence[currentIndex].Call.FillFromTestChainProperties(s.chain)
		return callSequence[currentIndex], nil
	}
	executedSequence, err := calls.ExecuteCallSequenceIteratively(s.chain, fetchElementFunc, nil, executionTracer.NativeTracer())
	if revertErr := s.chain.RevertToBlockIndex(s.baseBlockIndex); err == nil {
		err = revertErr
	}
	if err != nil {
		return nil, nil, err
	}

	// Describe each call: its execution trace, and whether it reached code the corpus has not.
	result := &SequenceResult{NewCoverage: make([]bool, len(executedSequence))}
	corpusCoverageMaps := s.fuzzer.corpus.CoverageMaps()
	for i, element := range executedSequence {
		element.ExecutionTrace = executionTracer.GetTrace(utils.MessageToTransaction(element.Call.ToCoreMessage()).Hash())
		callCoverageMaps := coverage.GetCoverageTracerResults(element.ChainReference.MessageResults())
		result.NewCoverage[i] = corpusCoverageMaps.HasNewCoverage(callCoverageMaps)
	}
	result.Calls = report.NewCalls(executedSequence)
	return result, executedSequence, nil
}

// buildElement builds the call sequence element described by the provided input.
func (s *sequenceSession) buildElement(input CallInput) (*calls.CallSequenceElement, error) {
	// Resolve the contract, then the method within it.
	address, contract, err := s.resolveContract(input.Contract)
	if err != nil {
		return nil, err
	}
	method, err := resolveMethod(contract, input.Method)
	if err != nil {
		return nil, err
	}

	// Decode the arguments. They may refer to deployed contracts by name.
	deployedContractAddresses := make(map[string]common.Address, len(s.deployedContracts))
	for deployedAddress, deployedContract := range s.deployedContracts {
		deployedContractAddresses[deployedContract.Name()] = deployedAddress
	}
	jsonArguments, err := integersToStrings(input.Arguments)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments for %s.%s: %w", contract.Name(), method.Sig, err)
	}
	arguments, err := valuegeneration.DecodeJSONArgumentsFromSlice(method.Inputs, jsonArguments.([]any), deployedContractAddresses)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments for %s.%s: %w", contract.Name(), method.Sig, err)
	}

	// Resolve the sender and value.
	sender := s.fuzzer.senders[0]
	if input.Sender != "" {
		if sender, err = utils.HexStringToAddress(input.Sender); err != nil {
			return nil, fmt.Errorf("invalid sender %q: %w", input.Sender, err)
		}
	}
	value := new(big.Int)
	if input.Value != "" {
		if _, ok := value.SetString(input.Value, 0); !ok || value.Sign() < 0 {
			return nil, fmt.Errorf("invalid value %q: expected a non-negative decimal or 0x-prefixed integer", input.Value)
		}
	}
	if value.Sign() > 0 && method.StateMutability != "payable" {
		return nil, fmt.Errorf("%s.%s is not payable, so no value can be sent with it", contract.Name(), method.Sig)
	}

	// Create the call the same way the fuzzer does for the calls it generates.
	message := calls.NewCallMessageWithAbiValueData(sender, &address, 0, value, s.fuzzer.config.Fuzzing.TransactionGasLimit, nil, nil, nil, &calls.CallMessageDataAbiValues{
		Method:      method,
		InputValues: arguments,
	})
	if s.fuzzer.config.Fuzzing.TestChainConfig.SkipAccountChecks {
		message.SkipFromEOACheck = true
		message.SkipNonceChecks = true
	}
	return calls.NewCallSequenceElement(contract, message, input.BlockNumberDelay, input.BlockTimestampDelay), nil
}

// integersToStrings replaces every JSON number in the provided JSON value with its decimal string, which is how
// integer arguments are decoded. JSON numbers are accepted because they are the natural way to write a small
// integer, but they are floating point, so one which cannot be an exact integer is rejected.
func integersToStrings(value any) (any, error) {
	switch value := value.(type) {
	case float64:
		// Integers are exact in a float64 up to 2^53.
		const maxExactInteger = 1 << 53
		if value != math.Trunc(value) || math.Abs(value) > maxExactInteger {
			return nil, fmt.Errorf("the number %v is not an exact integer: write large integers as strings, e.g. \"1000000000000000000\"", value)
		}
		return strconv.FormatInt(int64(value), 10), nil
	case []any:
		converted := make([]any, len(value))
		for i, element := range value {
			var err error
			if converted[i], err = integersToStrings(element); err != nil {
				return nil, err
			}
		}
		return converted, nil
	case map[string]any:
		converted := make(map[string]any, len(value))
		for key, element := range value {
			var err error
			if converted[key], err = integersToStrings(element); err != nil {
				return nil, err
			}
		}
		return converted, nil
	default:
		return value, nil
	}
}

// resolveContract resolves a contract name or address to a deployed contract.
// Returns the address and definition of the contract, or an error if it is not deployed or the name is ambiguous.
func (s *sequenceSession) resolveContract(nameOrAddress string) (common.Address, *fuzzerTypes.Contract, error) {
	// An address refers to the contract deployed there.
	if strings.HasPrefix(nameOrAddress, "0x") {
		address, err := utils.HexStringToAddress(nameOrAddress)
		if err != nil {
			return common.Address{}, nil, fmt.Errorf("invalid contract address %q: %w", nameOrAddress, err)
		}
		contract, deployed := s.deployedContracts[address]
		if !deployed {
			return common.Address{}, nil, fmt.Errorf("no known contract is deployed at %s", address.Hex())
		}
		return address, contract, nil
	}

	// A name refers to the one contract deployed with that name.
	addresses := make([]common.Address, 0, 1)
	for address, contract := range s.deployedContracts {
		if contract.Name() == nameOrAddress {
			addresses = append(addresses, address)
		}
	}
	switch len(addresses) {
	case 0:
		names := make([]string, 0, len(s.deployedContracts))
		for _, contract := range s.deployedContracts {
			names = append(names, contract.Name())
		}
		slices.Sort(names)
		return common.Address{}, nil, fmt.Errorf("no contract named %q is deployed; deployed contracts: %s", nameOrAddress, strings.Join(slices.Compact(names), ", "))
	case 1:
		return addresses[0], s.deployedContracts[addresses[0]], nil
	default:
		return common.Address{}, nil, fmt.Errorf("%d contracts named %q are deployed; use the address of the one to call", len(addresses), nameOrAddress)
	}
}

// resolveMethod resolves a method name or signature to a method of the provided contract.
// Returns the method, or an error if the contract has no such method or the name is overloaded.
func resolveMethod(contract *fuzzerTypes.Contract, nameOrSignature string) (*abi.Method, error) {
	matches := make([]*abi.Method, 0, 1)
	for _, method := range contract.CompiledContract().Abi.Methods {
		if method.Sig == nameOrSignature {
			return &method, nil
		}
		if method.RawName == nameOrSignature {
			matches = append(matches, &method)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("contract %s has no method %q", contract.Name(), nameOrSignature)
	case 1:
		return matches[0], nil
	default:
		signatures := make([]string, len(matches))
		for i, method := range matches {
			signatures[i] = method.Sig
		}
		slices.Sort(signatures)
		return nil, fmt.Errorf("method %q of contract %s is overloaded; use one of: %s", nameOrSignature, contract.Name(), strings.Join(signatures, ", "))
	}
}

// DeployedContract describes a contract deployed on the chain every call sequence starts from.
type DeployedContract struct {
	// Name is the name of the contract.
	Name string `json:"name"`

	// Address is the address the contract is deployed at, as a hex string.
	Address string `json:"address"`

	// Methods are the signatures of the methods of the contract, sorted, with their state mutability.
	Methods []DeployedContractMethod `json:"methods"`
}

// DeployedContractMethod describes a method of a DeployedContract.
type DeployedContractMethod struct {
	// Signature is the signature of the method, e.g. "transfer(address,uint256)".
	Signature string `json:"signature"`

	// StateMutability is the state mutability of the method: "pure", "view", "nonpayable" or "payable".
	StateMutability string `json:"stateMutability"`
}

// deployed describes the contracts deployed on the session's chain, sorted by name then address.
func (s *sequenceSession) deployed() []DeployedContract {
	s.lock.Lock()
	defer s.lock.Unlock()

	deployedContracts := make([]DeployedContract, 0, len(s.deployedContracts))
	for address, contract := range s.deployedContracts {
		deployedContract := DeployedContract{Name: contract.Name(), Address: address.Hex(), Methods: make([]DeployedContractMethod, 0)}
		for _, method := range contract.CompiledContract().Abi.Methods {
			deployedContract.Methods = append(deployedContract.Methods, DeployedContractMethod{Signature: method.Sig, StateMutability: method.StateMutability})
		}
		slices.SortFunc(deployedContract.Methods, func(a, b DeployedContractMethod) int { return strings.Compare(a.Signature, b.Signature) })
		deployedContracts = append(deployedContracts, deployedContract)
	}
	slices.SortFunc(deployedContracts, func(a, b DeployedContract) int {
		if byName := strings.Compare(a.Name, b.Name); byName != 0 {
			return byName
		}
		return strings.Compare(a.Address, b.Address)
	})
	return deployedContracts
}

// currentSession returns the session of the running fuzzing campaign, or ErrNoCampaignRunning if there is none.
func (f *Fuzzer) currentSession() (*sequenceSession, error) {
	f.campaignLock.Lock()
	defer f.campaignLock.Unlock()
	if f.session == nil {
		return nil, ErrNoCampaignRunning
	}
	return f.session, nil
}

// DeployedContracts describes the contracts deployed on the chain every call sequence starts from, which are the
// contracts a call sequence passed to ExecuteSequence or AddSequence can call.
// Returns ErrNoCampaignRunning if no fuzzing campaign is running.
func (f *Fuzzer) DeployedContracts() ([]DeployedContract, error) {
	session, err := f.currentSession()
	if err != nil {
		return nil, err
	}
	return session.deployed(), nil
}

// ExecuteSequence executes the provided call sequence against the running fuzzing campaign's contracts, and reports
// what each call did. It changes nothing: no coverage is recorded, nothing is added to the corpus and no test is run.
// It can be called from any goroutine while the campaign is running.
// Returns ErrNoCampaignRunning if no fuzzing campaign is running, or an error if a call cannot be built or executed.
func (f *Fuzzer) ExecuteSequence(inputs []CallInput) (*SequenceResult, error) {
	session, err := f.currentSession()
	if err != nil {
		return nil, err
	}
	result, _, err := session.execute(inputs)
	return result, err
}

// AddSequence executes the provided call sequence like ExecuteSequence, then adds it to the corpus of the running
// fuzzing campaign. A worker replays it shortly after, which runs every test against it, records the coverage it
// reaches and makes it a target for mutations.
// Returns ErrNoCampaignRunning if no fuzzing campaign is running, or an error if a call cannot be built or executed.
func (f *Fuzzer) AddSequence(inputs []CallInput) (*SequenceResult, error) {
	session, err := f.currentSession()
	if err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return nil, errors.New("a call sequence needs at least one call")
	}
	result, executedSequence, err := session.execute(inputs)
	if err != nil {
		return nil, err
	}
	result.AddedToCorpus, err = f.corpus.AddExternalCallSequence(executedSequence)
	return result, err
}
