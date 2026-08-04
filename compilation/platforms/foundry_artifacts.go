package platforms

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/crytic/medusa/compilation/types"
)

type foundryArtifactBytecode struct {
	Object              string                                         `json:"object"`
	ImmutableReferences map[string][]foundryArtifactImmutableReference `json:"immutableReferences"`
}

type foundryArtifactImmutableReference struct {
	Start  *uint64 `json:"start"`
	Length *uint64 `json:"length"`
}

type foundryArtifact struct {
	DeployedBytecode foundryArtifactBytecode `json:"deployedBytecode"`
	Metadata         struct {
		Settings struct {
			CompilationTarget map[string]string `json:"compilationTarget"`
		} `json:"settings"`
	} `json:"metadata"`
}

type foundryArtifactOwner struct {
	compilationIndex int
	sourcePath       string
	contractName     string
}

var foundryLibraryPlaceholderPattern = regexp.MustCompile(`__(\$[0-9a-zA-Z]*\$|\w*)__`)

// enrichFoundryImmutableReferences reads the raw Foundry artifacts that crytic-compile consumes. Crytic-compile's
// normalized solc export drops immutableReferences, so the raw artifact is the nearest compiler-owned source for them.
func enrichFoundryImmutableReferences(
	compilations []types.Compilation,
	target string,
	cryticArgs []string,
) error {
	projectRoot, isFoundryProject, err := resolveFoundryProjectRoot(target, cryticArgs)
	if err != nil || !isFoundryProject {
		return err
	}

	outDirectory, err := resolveFoundryOutDirectory(projectRoot, cryticArgs)
	if err != nil {
		return err
	}
	ownersByTarget := indexFoundryArtifactOwners(compilations, projectRoot)

	return filepath.WalkDir(outDirectory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != outDirectory && entry.Name() == "build-info" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".json" {
			return nil
		}

		artifactData, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("could not read Foundry artifact %q: %w", path, err)
		}

		var artifact foundryArtifact
		if err := json.Unmarshal(artifactData, &artifact); err != nil {
			return fmt.Errorf("could not parse Foundry artifact %q: %w", path, err)
		}
		if len(artifact.DeployedBytecode.ImmutableReferences) == 0 {
			return nil
		}
		artifactSourcePath, artifactContractName, err := foundryArtifactCompilationTarget(artifact)
		if err != nil {
			return fmt.Errorf("invalid compilation target in Foundry artifact %q: %w", path, err)
		}
		owners := ownersByTarget[foundryArtifactTargetKey(projectRoot, artifactSourcePath, artifactContractName)]
		// Foundry can retain artifacts for sources excluded from crytic-compile's export. Ignore them before parsing
		// their compiler data, because they cannot contribute a Medusa source map.
		if len(owners) == 0 {
			return nil
		}

		template, err := decodeFoundryBytecodeTemplate(artifact.DeployedBytecode.Object)
		if err != nil {
			return fmt.Errorf("could not decode deployed bytecode in Foundry artifact %q: %w", path, err)
		}

		references, err := flattenFoundryImmutableReferences(artifact.DeployedBytecode.ImmutableReferences)
		if err != nil {
			return fmt.Errorf("invalid immutable references in Foundry artifact %q: %w", path, err)
		}
		references, err = types.ValidateImmutableReferences(template, references)
		if err != nil {
			return fmt.Errorf("invalid immutable references in Foundry artifact %q: %w", path, err)
		}

		artifactObject := strings.TrimPrefix(artifact.DeployedBytecode.Object, "0x")
		templateMatches := 0
		for _, owner := range owners {
			source := compilations[owner.compilationIndex].SourcePathToArtifact[owner.sourcePath]
			contract := source.Contracts[owner.contractName]
			if !compiledRuntimeMatchesFoundryArtifact(contract.RuntimeBytecode, template, artifactObject) {
				continue
			}
			if len(contract.RuntimeBytecodeImmutableReferences) > 0 &&
				!slices.Equal(contract.RuntimeBytecodeImmutableReferences, references) {
				return fmt.Errorf(
					"Foundry artifacts declare conflicting immutable references for contract %q in source %q",
					artifactContractName,
					owner.sourcePath,
				)
			}
			contract.RuntimeBytecodeImmutableReferences = slices.Clone(references)
			source.Contracts[owner.contractName] = contract
			compilations[owner.compilationIndex].SourcePathToArtifact[owner.sourcePath] = source
			templateMatches++
		}
		if templateMatches == 0 {
			return fmt.Errorf(
				"deployed bytecode in Foundry artifact %q does not match compiled contract %q in source %q",
				path,
				artifactContractName,
				artifactSourcePath,
			)
		}

		return nil
	})
}

func resolveFoundryProjectRoot(target string, cryticArgs []string) (string, bool, error) {
	forcedFramework := strings.ToLower(cryticArgValue(cryticArgs, "--compile-force-framework"))
	if forcedFramework != "" && forcedFramework != "foundry" {
		return "", false, nil
	}

	targetPath, err := filepath.Abs(target)
	if err != nil {
		if forcedFramework != "foundry" {
			return "", false, nil
		}
		return "", false, fmt.Errorf("could not resolve compilation target %q: %w", target, err)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if forcedFramework != "foundry" {
			return "", false, nil
		}
		return "", false, fmt.Errorf("could not inspect compilation target %q: %w", targetPath, err)
	}
	searchPath := targetPath
	if !info.IsDir() {
		searchPath = filepath.Dir(searchPath)
	}
	startingPath := searchPath

	for {
		foundryConfig := filepath.Join(searchPath, "foundry.toml")
		if configInfo, statErr := os.Stat(foundryConfig); statErr == nil && !configInfo.IsDir() {
			return searchPath, true, nil
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return "", false, fmt.Errorf("could not inspect Foundry config %q: %w", foundryConfig, statErr)
		}

		parent := filepath.Dir(searchPath)
		if parent == searchPath {
			break
		}
		searchPath = parent
	}

	if forcedFramework == "foundry" {
		return startingPath, true, nil
	}
	return "", false, nil
}

func resolveFoundryOutDirectory(projectRoot string, cryticArgs []string) (string, error) {
	outDirectory := cryticArgValue(cryticArgs, "--foundry-out-directory")
	if outDirectory == "" {
		if cryticArgPresent(cryticArgs, "--foundry-ignore-compile") || cryticArgPresent(cryticArgs, "--ignore-compile") {
			outDirectory = "out"
		} else {
			cmd := exec.Command("forge", "config", "--json", "--root", projectRoot)
			output, err := cmd.Output()
			if err != nil {
				return "", fmt.Errorf("could not resolve Foundry artifact directory: %w", err)
			}

			var foundryConfig struct {
				Out string `json:"out"`
			}
			if err := json.Unmarshal(output, &foundryConfig); err != nil {
				return "", fmt.Errorf("could not parse `forge config --json` output: %w", err)
			}
			outDirectory = foundryConfig.Out
		}
	}
	if outDirectory == "" {
		outDirectory = "out"
	}
	if !filepath.IsAbs(outDirectory) {
		outDirectory = filepath.Join(projectRoot, outDirectory)
	}

	info, err := os.Stat(outDirectory)
	if err != nil {
		return "", fmt.Errorf("could not inspect Foundry artifact directory %q: %w", outDirectory, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Foundry artifact path %q is not a directory", outDirectory)
	}
	resolvedOutDirectory, err := filepath.EvalSymlinks(outDirectory)
	if err != nil {
		return "", fmt.Errorf("could not resolve Foundry artifact directory %q: %w", outDirectory, err)
	}
	return filepath.Clean(resolvedOutDirectory), nil
}

func cryticArgValue(args []string, name string) string {
	for index, arg := range args {
		if arg == name && index+1 < len(args) {
			return args[index+1]
		}
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimPrefix(arg, name+"=")
		}
	}
	return ""
}

func cryticArgPresent(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func decodeFoundryBytecodeTemplate(object string) ([]byte, error) {
	object = strings.TrimPrefix(object, "0x")
	object = foundryLibraryPlaceholderPattern.ReplaceAllStringFunc(object, func(placeholder string) string {
		return strings.Repeat("0", len(placeholder))
	})
	if len(object)%2 != 0 {
		return nil, fmt.Errorf("deployed bytecode has odd hexadecimal length %d", len(object))
	}
	return hex.DecodeString(object)
}

func flattenFoundryImmutableReferences(
	referenceMap map[string][]foundryArtifactImmutableReference,
) ([]types.ImmutableReference, error) {
	immutableIDs := make([]string, 0, len(referenceMap))
	for immutableID := range referenceMap {
		immutableIDs = append(immutableIDs, immutableID)
	}
	sort.Strings(immutableIDs)

	var references []types.ImmutableReference
	for _, immutableID := range immutableIDs {
		if len(referenceMap[immutableID]) == 0 {
			return nil, fmt.Errorf("immutable AST ID %q has no byte ranges", immutableID)
		}
		for index, reference := range referenceMap[immutableID] {
			if reference.Start == nil || reference.Length == nil {
				return nil, fmt.Errorf("immutable AST ID %q range %d must contain start and length", immutableID, index)
			}
			references = append(references, types.ImmutableReference{Start: *reference.Start, Length: *reference.Length})
		}
	}
	if len(references) == 0 {
		return nil, fmt.Errorf("immutable reference map has no byte ranges")
	}
	return references, nil
}

func compiledRuntimeMatchesFoundryArtifact(compiledRuntime []byte, decodedTemplate []byte, artifactObject string) bool {
	if bytes.Equal(compiledRuntime, decodedTemplate) {
		return true
	}
	return strings.TrimPrefix(string(compiledRuntime), "0x") == artifactObject
}

func foundryArtifactCompilationTarget(artifact foundryArtifact) (string, string, error) {
	if len(artifact.Metadata.Settings.CompilationTarget) != 1 {
		return "", "", fmt.Errorf(
			"expected exactly one metadata.settings.compilationTarget entry, found %d",
			len(artifact.Metadata.Settings.CompilationTarget),
		)
	}
	for sourcePath, contractName := range artifact.Metadata.Settings.CompilationTarget {
		if sourcePath == "" || contractName == "" {
			return "", "", fmt.Errorf("compilation target source and contract names must not be empty")
		}
		return sourcePath, contractName, nil
	}
	panic("unreachable: one Foundry compilation target was expected")
}

func normalizeFoundrySourcePath(projectRoot string, sourcePath string) string {
	sourcePath = filepath.FromSlash(sourcePath)
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(projectRoot, sourcePath)
	}
	resolved, err := filepath.EvalSymlinks(sourcePath)
	if err == nil {
		sourcePath = resolved
	}
	sourcePath = filepath.Clean(sourcePath)
	if runtime.GOOS == "windows" {
		sourcePath = strings.ToLower(sourcePath)
	}
	return sourcePath
}

func foundryArtifactTargetKey(projectRoot string, sourcePath string, contractName string) string {
	return normalizeFoundrySourcePath(projectRoot, sourcePath) + "\x00" + contractName
}

func indexFoundryArtifactOwners(
	compilations []types.Compilation,
	projectRoot string,
) map[string][]foundryArtifactOwner {
	ownersByTarget := make(map[string][]foundryArtifactOwner)
	for compilationIndex := range compilations {
		for sourcePath, source := range compilations[compilationIndex].SourcePathToArtifact {
			for contractName := range source.Contracts {
				key := foundryArtifactTargetKey(projectRoot, sourcePath, contractName)
				ownersByTarget[key] = append(ownersByTarget[key], foundryArtifactOwner{
					compilationIndex: compilationIndex,
					sourcePath:       sourcePath,
					contractName:     contractName,
				})
			}
		}
	}
	return ownersByTarget
}
