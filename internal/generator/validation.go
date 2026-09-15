package generator

import (
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/module"
)

const (
	maxModuleNameLength  = 64
	maxProjectNameLength = 100
)

func validateModuleName(name string) error {
	if name == "" || len(name) > maxModuleNameLength {
		return fmt.Errorf("invalid module name %q: use 1-%d lowercase ASCII characters", name, maxModuleNameLength)
	}
	if token.Lookup(name).IsKeyword() {
		return fmt.Errorf("invalid module name %q: Go keywords are not allowed", name)
	}
	for i := range len(name) {
		character := name[i]
		if i == 0 {
			if character < 'a' || character > 'z' {
				return fmt.Errorf("invalid module name %q: the first character must be a lowercase ASCII letter", name)
			}
			continue
		}
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '_' {
			return fmt.Errorf("invalid module name %q: use lowercase ASCII letters, digits, and underscores", name)
		}
	}
	if name[len(name)-1] == '_' {
		return fmt.Errorf("invalid module name %q: the final character must be a letter or digit", name)
	}
	return nil
}

func validateProjectName(name string) error {
	if name == "" || len(name) > maxProjectNameLength {
		return fmt.Errorf("invalid project name %q: use 1-%d lowercase ASCII characters", name, maxProjectNameLength)
	}
	if filepath.IsAbs(name) || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("invalid project name %q: provide a directory name, not a path", name)
	}
	if err := module.CheckFilePath(name); err != nil {
		return fmt.Errorf("invalid project name %q: %w", name, err)
	}
	for i := range len(name) {
		character := name[i]
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '-' && character != '_' && character != '.' {
			return fmt.Errorf("invalid project name %q: use lowercase ASCII letters, digits, dots, dashes, and underscores", name)
		}
	}
	if !isLowerLetterOrDigit(name[0]) || !isLowerLetterOrDigit(name[len(name)-1]) {
		return fmt.Errorf("invalid project name %q: start and end with a lowercase letter or digit", name)
	}
	return nil
}

func isLowerLetterOrDigit(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}

func validateModulePath(path string) error {
	if err := module.CheckPath(path); err != nil {
		return fmt.Errorf("invalid Go module path %q: %w", path, err)
	}
	return nil
}

func validateOutputPath(output, source string) error {
	if strings.TrimSpace(output) == "" {
		return errors.New("output path is required")
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve output path %q: %w", output, err)
	}
	resolvedOutput, err := resolveProspectivePath(absolute)
	if err != nil {
		return fmt.Errorf("resolve output path %q: %w", output, err)
	}
	current, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve current directory: %w", err)
	}
	resolvedCurrent, err := filepath.EvalSymlinks(current)
	if err != nil {
		return fmt.Errorf("resolve current directory symlinks: %w", err)
	}
	volumeRoot := filepath.Clean(filepath.VolumeName(resolvedOutput) + string(filepath.Separator))
	if resolvedOutput == filepath.Clean(resolvedCurrent) || resolvedOutput == volumeRoot {
		return fmt.Errorf("unsafe output path %q: choose a new subdirectory", output)
	}
	if _, err := os.Lstat(absolute); err == nil {
		return fmt.Errorf("output path %q already exists", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output path %q: %w", output, err)
	}
	if source == "" {
		return nil
	}
	sourceAbsolute, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve source path %q: %w", source, err)
	}
	resolvedSource, err := filepath.EvalSymlinks(sourceAbsolute)
	if err != nil {
		return fmt.Errorf("resolve source path %q: %w", source, err)
	}
	if pathWithin(resolvedSource, resolvedOutput) {
		return fmt.Errorf("unsafe output path %q: output cannot be inside source %s", output, source)
	}
	return nil
}

func validateLocalDirectoryPath(path string) error {
	if filepath.IsAbs(path) {
		return fmt.Errorf("directory path %q must be relative to the project root", path)
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		return nil
	}

	current := ""
	for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			return fmt.Errorf("directory path %q escapes the project root", path)
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect directory path %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory path %s contains a symlink", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("directory path component %s is not a directory", current)
		}
	}
	return nil
}

func resolveProspectivePath(path string) (string, error) {
	path = filepath.Clean(path)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validatePort(address string) (string, error) {
	if len(address) < 2 || address[0] != ':' || strings.Count(address, ":") != 1 {
		return "", fmt.Errorf("invalid listen address %q: use :<port>", address)
	}
	port, err := strconv.Atoi(address[1:])
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid listen address %q: port must be between 1 and 65535", address)
	}
	return strconv.Itoa(port), nil
}
