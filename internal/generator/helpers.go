package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
)

// writeTemplate renders a Go text/template to the given file path.
func writeTemplate(filePath string, tmpl string, data any) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	t, err := template.New(filepath.Base(filePath)).Parse(tmpl)
	if err != nil {
		return fmt.Errorf("failed to parse template for %s: %w", filePath, err)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filePath, err)
	}
	defer f.Close()

	if err := t.Execute(f, data); err != nil {
		return fmt.Errorf("failed to write template to %s: %w", filePath, err)
	}

	return nil
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
		return "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimPrefix(line, "module "), nil
		}
	}

	return "", fmt.Errorf("module directive not found in go.mod")
}
