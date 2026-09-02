package generator

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

	"golang.org/x/mod/modfile"
)

// writeTemplate renders a Go text/template to the given file path.
func writeTemplate(filePath string, tmpl string, data any) error {
	t, err := template.New(filepath.Base(filePath)).Parse(tmpl)
	if err != nil {
		return fmt.Errorf("parse template for %s: %w", filePath, err)
	}

	var rendered bytes.Buffer
	if err := t.Execute(&rendered, data); err != nil {
		return fmt.Errorf("render template for %s: %w", filePath, err)
	}
	return writeFileExclusive(filePath, rendered.Bytes(), 0o644)
}

func writeFileExclusive(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create file %s without overwriting: %w", path, err)
	}

	written, writeErr := file.Write(content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	closeErr := file.Close()
	if writeErr == nil && closeErr == nil {
		return nil
	}

	removeErr := os.Remove(path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(
		fmt.Errorf("write file %s: %w", path, errors.Join(writeErr, closeErr)),
		removeErr,
	)
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	temporaryPath := file.Name()
	defer func() {
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary file %s: %w", temporaryPath, removeErr))
		}
	}()

	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("set permissions on temporary file for %s: %w", path, err)
	}
	written, writeErr := file.Write(content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace file %s: %w", path, err)
	}
	return nil
}

func createGeneratedDir(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", path, err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		return fmt.Errorf("create generated directory %s: %w", path, err)
	}
	return nil
}

func cleanupGeneratedDir(path string, result *error) {
	if *result == nil {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		*result = errors.Join(*result, fmt.Errorf("remove incomplete output %s: %w", path, err))
	}
}

// pascal converts a snake_case or lowercase string to PascalCase.
// e.g. "user" -> "User", "order_item" -> "OrderItem"
func pascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-'
	})

	var result strings.Builder
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		result.WriteString(string(runes))
	}

	return result.String()
}

// DetectModulePath reads the go.mod file in the given directory
// and extracts the module path.
func DetectModulePath(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}

	path := modfile.ModulePath(data)
	if path == "" {
		return "", fmt.Errorf("module directive not found in go.mod")
	}
	if err := validateModulePath(path); err != nil {
		return "", err
	}
	return path, nil
}
