package compilation

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/crytic/medusa/compilation/types"
	"github.com/crytic/medusa/logging"
	"github.com/crytic/medusa/logging/colors"
)

// ArtifactHashCacheFileName is the name of the file used to store the artifact hash.
const ArtifactHashCacheFileName = ".medusa-artifact-hash"

// ArtifactHashCache stores the hash of compilation artifacts along with metadata.
type ArtifactHashCache struct {
	// Hash is the SHA-256 hash of the compiled bytecode.
	Hash string `json:"hash"`
	// Timestamp is when the hash was computed.
	Timestamp time.Time `json:"timestamp"`
}

// ComputeArtifactHash computes a SHA-256 hash of all compiled contract bytecode from the provided compilations. It
// sorts framed contract content so map order, compilation order, and host-specific source paths do not change the hash.
func ComputeArtifactHash(compilations []types.Compilation) string {
	hasher := sha256.New()

	// Collect all contract bytecodes with their names for deterministic ordering
	type contractBytecode struct {
		name                string
		initBytecode        []byte
		runtimeBytecode     []byte
		immutableReferences []types.ImmutableReference
	}
	var contracts []contractBytecode

	for _, compilation := range compilations {
		for _, source := range compilation.SourcePathToArtifact {
			for name, contract := range source.Contracts {
				references, err := types.NormalizeImmutableReferences(
					contract.RuntimeBytecodeImmutableReferences,
					len(contract.RuntimeBytecode),
				)
				if err != nil {
					// Tracer setup rejects malformed ranges. Retain their supplied order here so a changed malformed
					// schema still changes the cache key.
					references = contract.RuntimeBytecodeImmutableReferences
				}
				contracts = append(contracts, contractBytecode{
					name:                name,
					initBytecode:        contract.InitBytecode,
					runtimeBytecode:     contract.RuntimeBytecode,
					immutableReferences: references,
				})
			}
		}
	}

	// Sort by framed contract content. Host-specific source paths and compilation order must not change the cache key.
	sort.Slice(contracts, func(i, j int) bool {
		left := contracts[i]
		right := contracts[j]
		if left.name != right.name {
			return left.name < right.name
		}
		if comparison := bytes.Compare(left.initBytecode, right.initBytecode); comparison != 0 {
			return comparison < 0
		}
		if comparison := bytes.Compare(left.runtimeBytecode, right.runtimeBytecode); comparison != 0 {
			return comparison < 0
		}
		if len(left.immutableReferences) != len(right.immutableReferences) {
			return len(left.immutableReferences) < len(right.immutableReferences)
		}
		for referenceIndex := range left.immutableReferences {
			if left.immutableReferences[referenceIndex].Start != right.immutableReferences[referenceIndex].Start {
				return left.immutableReferences[referenceIndex].Start < right.immutableReferences[referenceIndex].Start
			}
			if left.immutableReferences[referenceIndex].Length != right.immutableReferences[referenceIndex].Length {
				return left.immutableReferences[referenceIndex].Length < right.immutableReferences[referenceIndex].Length
			}
		}
		return false
	})

	// Hash each contract's bytecode
	writeBytes := func(value []byte) {
		var encodedLength [8]byte
		binary.BigEndian.PutUint64(encodedLength[:], uint64(len(value)))
		hasher.Write(encodedLength[:])
		hasher.Write(value)
	}
	for _, c := range contracts {
		writeBytes([]byte(c.name))
		writeBytes(c.initBytecode)
		writeBytes(c.runtimeBytecode)
		var encoded [16]byte
		binary.BigEndian.PutUint64(encoded[:8], uint64(len(c.immutableReferences)))
		hasher.Write(encoded[:8])
		for _, reference := range c.immutableReferences {
			binary.BigEndian.PutUint64(encoded[:8], reference.Start)
			binary.BigEndian.PutUint64(encoded[8:], reference.Length)
			hasher.Write(encoded[:])
		}
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// LoadArtifactHashCache loads the artifact hash cache from the specified directory.
// Returns nil if the cache file does not exist or cannot be parsed.
func LoadArtifactHashCache(directory string) *ArtifactHashCache {
	cachePath := filepath.Join(directory, ArtifactHashCacheFileName)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}

	var cache ArtifactHashCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}

	return &cache
}

// SaveArtifactHashCache saves the artifact hash cache to the specified directory.
// Returns an error if the cache cannot be written.
func SaveArtifactHashCache(directory string, cache *ArtifactHashCache) error {
	// Ensure the directory exists
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	cachePath := filepath.Join(directory, ArtifactHashCacheFileName)
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// NotifyArtifactHashStatus compares the current artifact hash with a cached hash
// and logs an appropriate message. It also updates the cache with the new hash.
// The cacheDirectory parameter specifies where the cache file is stored.
func NotifyArtifactHashStatus(
	compilations []types.Compilation,
	cacheDirectory string,
	logger *logging.Logger,
) {
	if len(compilations) == 0 {
		return
	}

	// Compute the current hash
	currentHash := ComputeArtifactHash(compilations)

	// Load the cached hash
	cachedHash := LoadArtifactHashCache(cacheDirectory)

	// Compare and log the appropriate message
	if cachedHash == nil || cachedHash.Hash != currentHash {
		// New or changed artifacts
		logger.Info(
			colors.Bold, "artifacts: ", colors.Reset,
			"Medusa is running against a ", colors.GreenBold, "new", colors.Reset, " set of build artifacts",
		)
	} else {
		// Same artifacts as before
		timeSince := time.Since(cachedHash.Timestamp)
		logger.Warn(
			colors.Bold, "artifacts: ", colors.Reset,
			"Medusa is running against the ", colors.YellowBold, "same", colors.Reset,
			" build artifacts as previously (last run: ", formatDuration(timeSince), " ago)",
		)
	}

	// Update the cache with the current hash
	newCache := &ArtifactHashCache{
		Hash:      currentHash,
		Timestamp: time.Now(),
	}
	if err := SaveArtifactHashCache(cacheDirectory, newCache); err != nil {
		logger.Warn("Failed to save artifact hash cache", err)
	}
}

// formatDuration formats a duration into a human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
