package platforms

import (
	"bytes"
	"encoding/binary"
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

type foundryCompilerReference struct {
	Start  *uint64 `json:"start"`
	Length *uint64 `json:"length"`
}

type foundryCompilerBytecode struct {
	Object              string                                           `json:"object"`
	ImmutableReferences map[string][]foundryCompilerReference            `json:"immutableReferences"`
	LinkReferences      map[string]map[string][]foundryCompilerReference `json:"linkReferences"`
}

type foundryCompilerContract struct {
	EVM struct {
		DeployedBytecode foundryCompilerBytecode `json:"deployedBytecode"`
	} `json:"evm"`
}

type foundryCompilerOutput struct {
	Contracts map[string]map[string]foundryCompilerContract `json:"contracts"`
}

type foundryBuildInfo struct {
	Output *foundryCompilerOutput `json:"output"`
}

type foundryCompilerOutputFile struct {
	path   string
	output foundryCompilerOutput
}

type foundryArtifactOwner struct {
	compilationIndex int
	sourcePath       string
	contractName     string
}

var (
	foundryLibraryPlaceholderPattern     = regexp.MustCompile(`__(\$[0-9a-zA-Z]{34}\$|[0-9a-zA-Z_]{36})__`)
	foundryFullLibraryPlaceholderPattern = regexp.MustCompile(`^__(\$[0-9a-zA-Z]{34}\$|[0-9a-zA-Z_]{36})__$`)
)

// enrichFoundryImmutableReferences reads the exact Foundry build-info files that Crytic Compile consumed. Its
// normalized solc export drops immutableReferences, while build-info retains compiler-owned immutable and link ranges.
func enrichFoundryImmutableReferences(
	compilations []types.Compilation,
	target string,
	cryticArgs []string,
) error {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not resolve the Crytic Compile working directory: %w", err)
	}
	options, err := resolveCryticEffectiveOptions(cryticArgs, workingDirectory)
	if err != nil {
		return err
	}
	projectRoot, isFoundryProject, err := resolveFoundryProjectRoot(target, options)
	if err != nil || !isFoundryProject {
		return err
	}

	buildInfoDirectory, err := resolveFoundryBuildInfoDirectory(projectRoot, options)
	if err != nil {
		return err
	}
	outputs, err := readFoundryCompilerOutputs(buildInfoDirectory)
	if err != nil {
		return err
	}
	ownersByTarget := indexFoundryArtifactOwners(compilations, projectRoot)
	for _, outputFile := range outputs {
		if err := enrichFoundryCompilerOutput(
			compilations,
			projectRoot,
			ownersByTarget,
			outputFile,
			options,
		); err != nil {
			return err
		}
	}
	return nil
}

func enrichFoundryCompilerOutput(
	compilations []types.Compilation,
	projectRoot string,
	ownersByTarget map[string][]foundryArtifactOwner,
	outputFile foundryCompilerOutputFile,
	options cryticEffectiveOptions,
) error {
	sourcePaths := make([]string, 0, len(outputFile.output.Contracts))
	for sourcePath := range outputFile.output.Contracts {
		sourcePaths = append(sourcePaths, sourcePath)
	}
	sort.Strings(sourcePaths)

	for _, artifactSourcePath := range sourcePaths {
		contractsByName := outputFile.output.Contracts[artifactSourcePath]
		contractNames := make([]string, 0, len(contractsByName))
		for contractName := range contractsByName {
			contractNames = append(contractNames, contractName)
		}
		sort.Strings(contractNames)

		for _, artifactContractName := range contractNames {
			bytecode := contractsByName[artifactContractName].EVM.DeployedBytecode
			if len(bytecode.ImmutableReferences) == 0 {
				continue
			}
			owners := ownersByTarget[foundryArtifactTargetKey(projectRoot, artifactSourcePath, artifactContractName)]
			// Foundry can retain targets excluded from Crytic Compile's export. They cannot own a Medusa source map.
			if len(owners) == 0 {
				continue
			}

			artifactObject := strings.TrimPrefix(bytecode.Object, "0x")
			var err error
			if options.compileRemoveMetadata {
				artifactObject, err = stripCryticMetadataSuffix(artifactObject)
				if err != nil {
					return fmt.Errorf("could not remove metadata from Foundry build-info %q contract %q: %w", outputFile.path, artifactContractName, err)
				}
			}

			linkReferences, err := flattenFoundryLinkReferences(bytecode.LinkReferences)
			if err != nil {
				return fmt.Errorf("invalid link references in Foundry build-info %q contract %q: %w", outputFile.path, artifactContractName, err)
			}
			template, linkReferences, err := decodeFoundryCompilerTemplate(artifactObject, linkReferences)
			if err != nil {
				return fmt.Errorf("could not decode deployed bytecode in Foundry build-info %q contract %q: %w", outputFile.path, artifactContractName, err)
			}

			immutableReferences, err := flattenFoundryImmutableReferences(bytecode.ImmutableReferences)
			if err != nil {
				return fmt.Errorf("invalid immutable references in Foundry build-info %q contract %q: %w", outputFile.path, artifactContractName, err)
			}
			immutableReferences, err = types.ValidateImmutableReferences(template, immutableReferences)
			if err != nil {
				return fmt.Errorf("invalid immutable references in Foundry build-info %q contract %q: %w", outputFile.path, artifactContractName, err)
			}
			if err := rejectOverlappingCompilerReferences(immutableReferences, linkReferences); err != nil {
				return fmt.Errorf("invalid compiler references in Foundry build-info %q contract %q: %w", outputFile.path, artifactContractName, err)
			}

			templateMatches := 0
			for _, owner := range owners {
				source := compilations[owner.compilationIndex].SourcePathToArtifact[owner.sourcePath]
				contract := source.Contracts[owner.contractName]
				if !compiledRuntimeMatchesFoundryCompilerTemplate(
					contract.RuntimeBytecode,
					artifactObject,
					template,
					linkReferences,
					options.libraryLinkingEnabled(),
				) {
					continue
				}
				if len(contract.RuntimeBytecodeImmutableReferences) > 0 &&
					!slices.Equal(contract.RuntimeBytecodeImmutableReferences, immutableReferences) {
					return fmt.Errorf(
						"foundry build-info declares conflicting immutable references for contract %q in source %q",
						artifactContractName,
						owner.sourcePath,
					)
				}
				contract.RuntimeBytecodeImmutableReferences = slices.Clone(immutableReferences)
				source.Contracts[owner.contractName] = contract
				compilations[owner.compilationIndex].SourcePathToArtifact[owner.sourcePath] = source
				templateMatches++
			}
			if templateMatches == 0 {
				return fmt.Errorf(
					"deployed bytecode in Foundry build-info %q does not match compiled contract %q in source %q",
					outputFile.path,
					artifactContractName,
					artifactSourcePath,
				)
			}
		}
	}
	return nil
}

func resolveFoundryProjectRoot(
	target string,
	options cryticEffectiveOptions,
) (string, bool, error) {
	forcedFramework := strings.ToLower(options.compileForceFramework)
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
	targetPath, err = filepath.EvalSymlinks(targetPath)
	if err != nil {
		if forcedFramework != "foundry" {
			return "", false, nil
		}
		return "", false, fmt.Errorf("could not resolve compilation target %q: %w", targetPath, err)
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

func resolveFoundryBuildInfoDirectory(projectRoot string, options cryticEffectiveOptions) (string, error) {
	buildInfoDirectory := options.foundryBuildInfoDirectory
	var foundryConfig struct {
		Out           string `json:"out"`
		BuildInfoPath string `json:"build_info_path"`
	}
	if !options.foundryCompilationIgnored() {
		cmd := exec.Command("forge", "config", "--json")
		cmd.Dir = projectRoot
		output, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("could not resolve Foundry build-info directory: %w", err)
		}
		if err := json.Unmarshal(output, &foundryConfig); err != nil {
			return "", fmt.Errorf("could not parse `forge config --json` output: %w", err)
		}
	}

	if buildInfoDirectory == "" {
		if foundryConfig.BuildInfoPath != "" {
			buildInfoDirectory = foundryConfig.BuildInfoPath
		} else {
			outDirectory := options.foundryOutDirectory
			if outDirectory == "" {
				outDirectory = foundryConfig.Out
			}
			if outDirectory == "" {
				outDirectory = "out"
			}
			buildInfoDirectory = filepath.Join(outDirectory, "build-info")
		}
	}
	if !filepath.IsAbs(buildInfoDirectory) {
		buildInfoDirectory = filepath.Join(projectRoot, buildInfoDirectory)
	}

	info, err := os.Stat(buildInfoDirectory)
	if err != nil {
		return "", fmt.Errorf("could not inspect Foundry build-info directory %q: %w", buildInfoDirectory, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("foundry build-info path %q is not a directory", buildInfoDirectory)
	}
	resolvedDirectory, err := filepath.EvalSymlinks(buildInfoDirectory)
	if err != nil {
		return "", fmt.Errorf("could not resolve Foundry build-info directory %q: %w", buildInfoDirectory, err)
	}
	return filepath.Clean(resolvedDirectory), nil
}

func readFoundryCompilerOutputs(buildInfoDirectory string) ([]foundryCompilerOutputFile, error) {
	entries, err := os.ReadDir(buildInfoDirectory)
	if err != nil {
		return nil, fmt.Errorf("could not read Foundry build-info directory %q: %w", buildInfoDirectory, err)
	}
	outputs := make([]foundryCompilerOutputFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasSuffix(entry.Name(), ".output.json") {
			continue
		}
		path := filepath.Join(buildInfoDirectory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("could not read Foundry build-info %q: %w", path, err)
		}
		var buildInfo foundryBuildInfo
		if err := json.Unmarshal(data, &buildInfo); err != nil {
			return nil, fmt.Errorf("could not parse Foundry build-info %q: %w", path, err)
		}
		if buildInfo.Output == nil {
			companionPath := strings.TrimSuffix(path, ".json") + ".output.json"
			companionData, err := os.ReadFile(companionPath)
			if err != nil {
				return nil, fmt.Errorf("could not read Foundry build-info output %q: %w", companionPath, err)
			}
			if err := json.Unmarshal(companionData, &buildInfo); err != nil {
				return nil, fmt.Errorf("could not parse Foundry build-info output %q: %w", companionPath, err)
			}
			if buildInfo.Output == nil {
				return nil, fmt.Errorf("foundry build-info output %q has no output object", companionPath)
			}
		}
		outputs = append(outputs, foundryCompilerOutputFile{path: path, output: *buildInfo.Output})
	}
	if len(outputs) == 0 {
		return nil, fmt.Errorf("foundry build-info directory %q contains no build-info JSON files", buildInfoDirectory)
	}
	return outputs, nil
}

func flattenFoundryImmutableReferences(
	referenceMap map[string][]foundryCompilerReference,
) ([]types.ImmutableReference, error) {
	return flattenFoundryCompilerReferenceMap(referenceMap, "immutable AST ID")
}

func flattenFoundryCompilerReferenceMap(
	referenceMap map[string][]foundryCompilerReference,
	label string,
) ([]types.ImmutableReference, error) {
	keys := make([]string, 0, len(referenceMap))
	for key := range referenceMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var references []types.ImmutableReference
	for _, key := range keys {
		if len(referenceMap[key]) == 0 {
			return nil, fmt.Errorf("%s %q has no byte ranges", label, key)
		}
		for index, reference := range referenceMap[key] {
			if reference.Start == nil || reference.Length == nil {
				return nil, fmt.Errorf("%s %q range %d must contain start and length", label, key, index)
			}
			references = append(references, types.ImmutableReference{Start: *reference.Start, Length: *reference.Length})
		}
	}
	if len(references) == 0 {
		return nil, fmt.Errorf("%s map has no byte ranges", label)
	}
	return references, nil
}

func flattenFoundryLinkReferences(
	referenceMap map[string]map[string][]foundryCompilerReference,
) ([]types.ImmutableReference, error) {
	if len(referenceMap) == 0 {
		return nil, nil
	}
	sourcePaths := make([]string, 0, len(referenceMap))
	for sourcePath := range referenceMap {
		sourcePaths = append(sourcePaths, sourcePath)
	}
	sort.Strings(sourcePaths)

	var references []types.ImmutableReference
	for _, sourcePath := range sourcePaths {
		libraries := referenceMap[sourcePath]
		libraryNames := make([]string, 0, len(libraries))
		for libraryName := range libraries {
			libraryNames = append(libraryNames, libraryName)
		}
		sort.Strings(libraryNames)
		for _, libraryName := range libraryNames {
			ranges := libraries[libraryName]
			if len(ranges) == 0 {
				return nil, fmt.Errorf("library %q in source %q has no byte ranges", libraryName, sourcePath)
			}
			for index, reference := range ranges {
				if reference.Start == nil || reference.Length == nil {
					return nil, fmt.Errorf(
						"library %q in source %q range %d must contain start and length",
						libraryName,
						sourcePath,
						index,
					)
				}
				references = append(references, types.ImmutableReference{Start: *reference.Start, Length: *reference.Length})
			}
		}
	}
	return references, nil
}

func stripCryticMetadataSuffix(object string) (string, error) {
	object = strings.TrimPrefix(object, "0x")
	if len(object) < 4 || len(object)%2 != 0 {
		return "", fmt.Errorf("deployed bytecode has no valid two-byte metadata footer")
	}
	footer, err := hex.DecodeString(object[len(object)-4:])
	if err != nil {
		return "", fmt.Errorf("deployed bytecode metadata footer is not hexadecimal: %w", err)
	}
	metadataLength := int(binary.BigEndian.Uint16(footer))
	metadataCharacters := metadataLength * 2
	metadataStart := len(object) - 4 - metadataCharacters
	if metadataLength == 0 || metadataStart < 0 {
		return "", fmt.Errorf("deployed bytecode metadata footer is out of range")
	}
	metadataSuffix, err := hex.DecodeString(object[metadataStart:])
	if err != nil {
		return "", fmt.Errorf("deployed bytecode metadata suffix is not hexadecimal: %w", err)
	}
	if types.ExtractContractMetadata(metadataSuffix) == nil {
		return "", fmt.Errorf("deployed bytecode metadata suffix is not one exact CBOR map")
	}
	return object[:metadataStart], nil
}

func decodeFoundryCompilerTemplate(
	object string,
	linkReferences []types.ImmutableReference,
) ([]byte, []types.ImmutableReference, error) {
	object = strings.TrimPrefix(object, "0x")
	if len(object)%2 != 0 {
		return nil, nil, fmt.Errorf("deployed bytecode has odd hexadecimal length %d", len(object))
	}
	normalizedReferences, err := types.NormalizeImmutableReferences(linkReferences, len(object)/2)
	if err != nil {
		return nil, nil, err
	}
	normalizedObject := []byte(object)
	for _, reference := range normalizedReferences {
		if reference.Length != 20 {
			return nil, nil, fmt.Errorf(
				"library reference at byte %d has length %d instead of 20",
				reference.Start,
				reference.Length,
			)
		}
		start := int(reference.Start * 2)
		end := start + int(reference.Length*2)
		placeholder := string(normalizedObject[start:end])
		if !foundryFullLibraryPlaceholderPattern.MatchString(placeholder) {
			return nil, nil, fmt.Errorf(
				"library reference [%d,%d) does not contain one compiler linker placeholder",
				reference.Start,
				reference.Start+reference.Length,
			)
		}
		for index := start; index < end; index++ {
			normalizedObject[index] = '0'
		}
	}
	if placeholder := foundryLibraryPlaceholderPattern.FindString(string(normalizedObject)); placeholder != "" {
		return nil, nil, fmt.Errorf("deployed bytecode contains undeclared linker placeholder %q", placeholder)
	}
	template, err := hex.DecodeString(string(normalizedObject))
	if err != nil {
		return nil, nil, err
	}
	return template, normalizedReferences, nil
}

func compiledRuntimeMatchesFoundryCompilerTemplate(
	compiledRuntime []byte,
	artifactObject string,
	decodedTemplate []byte,
	linkReferences []types.ImmutableReference,
	linkingEnabled bool,
) bool {
	if bytes.Equal(compiledRuntime, decodedTemplate) {
		return true
	}
	artifactObject = strings.TrimPrefix(artifactObject, "0x")
	compiledObject := strings.TrimPrefix(string(compiledRuntime), "0x")
	if compiledObject == artifactObject {
		return true
	}
	if !linkingEnabled || len(linkReferences) == 0 {
		return false
	}
	if len(compiledRuntime) == len(decodedTemplate) {
		return byteSlicesEqualOutsideReferences(compiledRuntime, decodedTemplate, linkReferences)
	}
	if len(compiledObject) != len(artifactObject) {
		return false
	}
	previousEnd := 0
	for _, reference := range linkReferences {
		start := int(reference.Start * 2)
		end := start + int(reference.Length*2)
		if compiledObject[previousEnd:start] != artifactObject[previousEnd:start] {
			return false
		}
		compiledLink := compiledObject[start:end]
		artifactLink := artifactObject[start:end]
		if compiledLink != artifactLink {
			if _, err := hex.DecodeString(compiledLink); err != nil {
				return false
			}
		}
		previousEnd = end
	}
	return compiledObject[previousEnd:] == artifactObject[previousEnd:]
}

func byteSlicesEqualOutsideReferences(
	first []byte,
	second []byte,
	references []types.ImmutableReference,
) bool {
	if len(first) != len(second) {
		return false
	}
	previousEnd := 0
	for _, reference := range references {
		start := int(reference.Start)
		end := start + int(reference.Length)
		if !bytes.Equal(first[previousEnd:start], second[previousEnd:start]) {
			return false
		}
		previousEnd = end
	}
	return bytes.Equal(first[previousEnd:], second[previousEnd:])
}

func rejectOverlappingCompilerReferences(
	immutableReferences []types.ImmutableReference,
	linkReferences []types.ImmutableReference,
) error {
	for _, immutableReference := range immutableReferences {
		immutableEnd := immutableReference.Start + immutableReference.Length
		for _, linkReference := range linkReferences {
			linkEnd := linkReference.Start + linkReference.Length
			if immutableReference.Start < linkEnd && linkReference.Start < immutableEnd {
				return fmt.Errorf(
					"immutable reference [%d,%d) overlaps library reference [%d,%d)",
					immutableReference.Start,
					immutableEnd,
					linkReference.Start,
					linkEnd,
				)
			}
		}
	}
	return nil
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
