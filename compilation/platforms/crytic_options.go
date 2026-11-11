package platforms

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type cryticEffectiveOptions struct {
	compileForceFramework     string
	compileRemoveMetadata     bool
	compileAutolink           bool
	compileLibraries          string
	ignoreCompile             bool
	foundryIgnoreCompile      bool
	foundryOutDirectory       string
	foundryBuildInfoDirectory string
}

func (options cryticEffectiveOptions) foundryCompilationIgnored() bool {
	return options.ignoreCompile || options.foundryIgnoreCompile
}

func (options cryticEffectiveOptions) libraryLinkingEnabled() bool {
	return options.compileAutolink || options.compileLibraries != ""
}

// resolveCryticEffectiveOptions mirrors Crytic Compile's config precedence for the small allowlist that controls
// Foundry build-info selection and bytecode transformations. Unknown config fields are never retained or logged.
func resolveCryticEffectiveOptions(args []string, workingDirectory string) (cryticEffectiveOptions, error) {
	options := cryticEffectiveOptions{}
	configPath := "crytic_compile.config.json"
	if value, present, err := cryticArgLastValue(args, "--config-file"); err != nil {
		return options, err
	} else if present {
		configPath = value
	}
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(workingDirectory, configPath)
	}

	config, err := readCryticOptionsConfig(configPath)
	if err != nil {
		return options, err
	}
	if config != nil {
		if err := applyCryticStringOption(config, "compile_force_framework", &options.compileForceFramework); err != nil {
			return options, err
		}
		if err := applyCryticBoolOption(config, "compile_remove_metadata", &options.compileRemoveMetadata); err != nil {
			return options, err
		}
		if err := applyCryticBoolOption(config, "compile_autolink", &options.compileAutolink); err != nil {
			return options, err
		}
		if err := applyCryticStringOption(config, "compile_libraries", &options.compileLibraries); err != nil {
			return options, err
		}
		if err := applyCryticBoolOption(config, "ignore_compile", &options.ignoreCompile); err != nil {
			return options, err
		}
		if err := applyCryticBoolOption(config, "foundry_ignore_compile", &options.foundryIgnoreCompile); err != nil {
			return options, err
		}
		if err := applyCryticStringOption(config, "foundry_out_directory", &options.foundryOutDirectory); err != nil {
			return options, err
		}
		if err := applyCryticStringOption(config, "foundry_build_info_directory", &options.foundryBuildInfoDirectory); err != nil {
			return options, err
		}
	}

	if err := overrideCryticStringOption(args, "--compile-force-framework", &options.compileForceFramework); err != nil {
		return options, err
	}
	if err := overrideCryticStringOption(args, "--compile-libraries", &options.compileLibraries); err != nil {
		return options, err
	}
	if err := overrideCryticStringOption(args, "--foundry-out-directory", &options.foundryOutDirectory); err != nil {
		return options, err
	}
	if err := overrideCryticStringOption(args, "--foundry-build-info-directory", &options.foundryBuildInfoDirectory); err != nil {
		return options, err
	}
	if cryticBoolArgPresent(args, "--compile-remove-metadata") {
		options.compileRemoveMetadata = true
	}
	if cryticBoolArgPresent(args, "--compile-autolink") {
		options.compileAutolink = true
	}
	if cryticBoolArgPresent(args, "--ignore-compile") {
		options.ignoreCompile = true
	}
	if cryticBoolArgPresent(args, "--foundry-ignore-compile") {
		options.foundryIgnoreCompile = true
	}
	return options, nil
}

func readCryticOptionsConfig(path string) (map[string]json.RawMessage, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not inspect Crytic Compile config %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read Crytic Compile config %q: %w", path, err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(data, &config); err != nil {
		// Crytic Compile logs malformed JSON and continues with its built-in defaults.
		return nil, nil
	}
	return config, nil
}

func applyCryticStringOption(config map[string]json.RawMessage, name string, destination *string) error {
	raw, exists := config[name]
	if !exists || string(raw) == "null" {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("crytic-compile config option %q must be a string or null", name)
	}
	*destination = value
	return nil
}

func applyCryticBoolOption(config map[string]json.RawMessage, name string, destination *bool) error {
	raw, exists := config[name]
	if !exists || string(raw) == "null" {
		return nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("crytic-compile config option %q must be a boolean or null", name)
	}
	*destination = value
	return nil
}

func overrideCryticStringOption(args []string, name string, destination *string) error {
	value, present, err := cryticArgLastValue(args, name)
	if err != nil {
		return err
	}
	if present {
		*destination = value
	}
	return nil
}

func cryticArgLastValue(args []string, name string) (string, bool, error) {
	var value string
	present := false
	for index, arg := range args {
		if arg == name {
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return "", false, fmt.Errorf("crytic-compile option %q requires a value", name)
			}
			value = args[index+1]
			present = true
			continue
		}
		if strings.HasPrefix(arg, name+"=") {
			value = strings.TrimPrefix(arg, name+"=")
			present = true
		}
	}
	return value, present, nil
}

func cryticBoolArgPresent(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}
