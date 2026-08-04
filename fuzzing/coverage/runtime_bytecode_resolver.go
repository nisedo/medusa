package coverage

import (
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/crytic/medusa-geth/common"
	"github.com/crytic/medusa-geth/crypto"
	compilationTypes "github.com/crytic/medusa/compilation/types"
	"github.com/crytic/medusa/fuzzing/contracts"
)

var (
	immutableRuntimeCoverageIdentityDomain = []byte("medusa:immutable-runtime-coverage:v1\x00")
	ambiguousRuntimeCoverageIdentityDomain = []byte("medusa:ambiguous-runtime-coverage:v1\x00")
	metadataRuntimeCoverageIdentityDomain  = []byte("medusa:metadata-runtime-coverage:v1\x00")
	ordinaryRuntimeCoverageIdentityDomain  = []byte("medusa:ordinary-runtime-coverage:v1\x00")
	initCoverageIdentityDomain             = []byte("medusa:init-coverage:v1\x00")
)

type immutableRuntimeDescriptor struct {
	template   []byte
	references []compilationTypes.ImmutableReference
	lookupHash common.Hash
	descriptor string
}

// RuntimeBytecodeResolver associates deployed runtime code with a compiled immutable template. Candidates are grouped
// by byte length, and each deployed geth code hash is resolved only once by CoverageTracer's existing cache.
type RuntimeBytecodeResolver struct {
	immutableCandidatesByLength map[int][]immutableRuntimeDescriptor
	exactCompiledIdentities     map[string]map[string]common.Hash
}

// NewRuntimeBytecodeResolver builds a read-only resolver from linked compilation definitions. One resolver can be
// shared by all coverage tracers in a fuzzing campaign.
func NewRuntimeBytecodeResolver(contractDefinitions contracts.Contracts) (*RuntimeBytecodeResolver, error) {
	resolver := &RuntimeBytecodeResolver{
		immutableCandidatesByLength: make(map[int][]immutableRuntimeDescriptor),
		exactCompiledIdentities:     make(map[string]map[string]common.Hash),
	}

	// Build immutable candidates first. Ordinary exact identities matter only at byte lengths that can match one.
	for _, definition := range contractDefinitions {
		if definition == nil || definition.CompiledContract() == nil {
			continue
		}
		contract := definition.CompiledContract()
		if len(contract.RuntimeBytecode) == 0 || len(contract.LibraryPlaceholders) > 0 ||
			len(contract.RuntimeBytecodeImmutableReferences) == 0 {
			continue
		}
		if _, ok := runtimeMetadataCoverageMapHash(contract.RuntimeBytecode); ok && !useFullBytecode() {
			continue
		}

		references, err := compilationTypes.ValidateImmutableReferences(
			contract.RuntimeBytecode,
			contract.RuntimeBytecodeImmutableReferences,
		)
		if err != nil {
			return nil, fmt.Errorf("contract %q has invalid runtime immutable references: %w", definition.Name(), err)
		}
		identity := immutableRuntimeCoverageMapHash(contract.RuntimeBytecode, references)
		resolver.immutableCandidatesByLength[len(contract.RuntimeBytecode)] = append(
			resolver.immutableCandidatesByLength[len(contract.RuntimeBytecode)],
			immutableRuntimeDescriptor{
				template:   slices.Clone(contract.RuntimeBytecode),
				references: slices.Clone(references),
				lookupHash: identity,
				descriptor: immutableRuntimeCoverageDescriptor(contract.RuntimeBytecode, references),
			},
		)
	}
	if len(resolver.immutableCandidatesByLength) == 0 {
		return nil, nil
	}

	// Index exact compiled templates at candidate lengths to detect ordinary contracts and distinct immutable masks
	// that make runtime-only attribution ambiguous.
	for _, definition := range contractDefinitions {
		if definition == nil || definition.CompiledContract() == nil {
			continue
		}
		contract := definition.CompiledContract()
		if len(contract.RuntimeBytecode) == 0 || len(contract.LibraryPlaceholders) > 0 ||
			len(resolver.immutableCandidatesByLength[len(contract.RuntimeBytecode)]) == 0 {
			continue
		}
		if _, ok := runtimeMetadataCoverageMapHash(contract.RuntimeBytecode); ok && !useFullBytecode() {
			continue
		}

		var identity common.Hash
		var descriptor string
		if len(contract.RuntimeBytecodeImmutableReferences) == 0 {
			identity = getContractCoverageMapHash(contract.RuntimeBytecode, false)
			descriptor = ordinaryRuntimeCoverageDescriptor(contract.RuntimeBytecode)
		} else {
			references, err := compilationTypes.ValidateImmutableReferences(
				contract.RuntimeBytecode,
				contract.RuntimeBytecodeImmutableReferences,
			)
			if err != nil {
				return nil, fmt.Errorf("contract %q has invalid runtime immutable references: %w", definition.Name(), err)
			}
			identity = immutableRuntimeCoverageMapHash(contract.RuntimeBytecode, references)
			descriptor = immutableRuntimeCoverageDescriptor(contract.RuntimeBytecode, references)
		}

		templateKey := string(contract.RuntimeBytecode)
		if resolver.exactCompiledIdentities[templateKey] == nil {
			resolver.exactCompiledIdentities[templateKey] = make(map[string]common.Hash)
		}
		resolver.exactCompiledIdentities[templateKey][descriptor] = identity
	}

	return resolver, nil
}

// resolve returns a canonical immutable identity when exactly one compiled descriptor matches. It returns an isolated
// unknown identity for ambiguous matches so coverage is never assigned to the wrong source map.
func (r *RuntimeBytecodeResolver) resolve(runtimeBytecode []byte) (common.Hash, bool) {
	if r == nil {
		return common.Hash{}, false
	}
	candidates := r.immutableCandidatesByLength[len(runtimeBytecode)]
	if len(candidates) == 0 {
		return common.Hash{}, false
	}

	identities := make(map[string]common.Hash)
	for _, candidate := range candidates {
		if compilationTypes.RuntimeBytecodeMatchesImmutableTemplate(candidate.template, runtimeBytecode, candidate.references) {
			identities[candidate.descriptor] = candidate.lookupHash
		}
	}
	if len(identities) == 0 {
		return common.Hash{}, false
	}

	for descriptor, identity := range r.exactCompiledIdentities[string(runtimeBytecode)] {
		identities[descriptor] = identity
	}
	if len(identities) != 1 {
		return domainSeparatedBytecodeHash(ambiguousRuntimeCoverageIdentityDomain, runtimeBytecode), true
	}
	for _, identity := range identities {
		return identity, true
	}
	panic("unreachable: one runtime coverage identity was expected")
}

func runtimeCoverageMapHash(runtimeBytecode []byte, resolver *RuntimeBytecodeResolver) common.Hash {
	if !useFullBytecode() {
		if metadataHash, ok := runtimeMetadataCoverageMapHash(runtimeBytecode); ok {
			return metadataHash
		}
	}
	if resolvedHash, ok := resolver.resolve(runtimeBytecode); ok {
		return resolvedHash
	}
	return getContractCoverageMapHash(runtimeBytecode, false)
}

// immutableRuntimeCoverageMapHash builds a platform-independent identity from the full compiler runtime template and
// its range schema. The template's immutable ranges have already been checked for zero placeholders.
func immutableRuntimeCoverageMapHash(
	template []byte,
	references []compilationTypes.ImmutableReference,
) common.Hash {
	return crypto.Keccak256Hash(immutableRuntimeCoverageMapPayload(template, references))
}

func immutableRuntimeCoverageDescriptor(
	template []byte,
	references []compilationTypes.ImmutableReference,
) string {
	return string(immutableRuntimeCoverageMapPayload(template, references))
}

func immutableRuntimeCoverageMapPayload(
	template []byte,
	references []compilationTypes.ImmutableReference,
) []byte {
	payload := make([]byte, 0, len(immutableRuntimeCoverageIdentityDomain)+16+(16*len(references))+len(template))
	payload = append(payload, immutableRuntimeCoverageIdentityDomain...)
	payload = appendUint64(payload, uint64(len(template)))
	payload = appendUint64(payload, uint64(len(references)))
	for _, reference := range references {
		payload = appendUint64(payload, reference.Start)
		payload = appendUint64(payload, reference.Length)
	}
	payload = append(payload, template...)
	return payload
}

func ordinaryRuntimeCoverageDescriptor(template []byte) string {
	payload := make([]byte, 0, len(ordinaryRuntimeCoverageIdentityDomain)+8+len(template))
	payload = append(payload, ordinaryRuntimeCoverageIdentityDomain...)
	payload = appendUint64(payload, uint64(len(template)))
	payload = append(payload, template...)
	return string(payload)
}

func domainSeparatedBytecodeHash(domain []byte, bytecode []byte) common.Hash {
	payload := make([]byte, 0, len(domain)+8+len(bytecode))
	payload = append(payload, domain...)
	payload = appendUint64(payload, uint64(len(bytecode)))
	payload = append(payload, bytecode...)
	return crypto.Keccak256Hash(payload)
}

func appendUint64(destination []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(destination, encoded[:]...)
}
