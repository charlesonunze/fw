package hotreload

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEnsureConfigCreatesCurrentAirConfig(t *testing.T) {
	dir := t.TempDir()

	created, err := EnsureConfig(dir)
	if err != nil {
		t.Fatalf("EnsureConfig() error = %v", err)
	}
	if !created {
		t.Fatal("EnsureConfig() created = false, want true")
	}

	content, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	config := string(content)
	for _, want := range []string{
		`cmd = "go build -o ./tmp/main ./cmd"`,
		`entrypoint = ["./tmp/main"]`,
		`[build.windows]`,
		`clean_on_exit = true`,
	} {
		if !strings.Contains(config, want) {
			t.Errorf("generated Air configuration missing %q:\n%s", want, config)
		}
	}
	if strings.Contains(config, "\n  bin =") {
		t.Errorf("generated Air configuration uses deprecated build.bin:\n%s", config)
	}
}

func TestEnsureConfigPreservesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFile)
	const existing = "custom = true\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	created, err := EnsureConfig(dir)
	if err != nil {
		t.Fatalf("EnsureConfig() error = %v", err)
	}
	if created {
		t.Fatal("EnsureConfig() created = true, want false")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != existing {
		t.Errorf("existing configuration changed: got %q, want %q", content, existing)
	}
}

func TestEnsureConfigRequiresExistingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")

	if _, err := EnsureConfig(dir); err == nil {
		t.Fatal("EnsureConfig() error = nil, want missing directory error")
	}
}

func TestEnsureConfigCreatesAtMostOnceConcurrently(t *testing.T) {
	dir := t.TempDir()
	results := make(chan bool, 16)
	errors := make(chan error, 16)

	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			created, err := EnsureConfig(dir)
			results <- created
			errors <- err
		}()
	}
	group.Wait()
	close(results)
	close(errors)

	createdCount := 0
	for created := range results {
		if created {
			createdCount++
		}
	}
	for err := range errors {
		if err != nil {
			t.Errorf("EnsureConfig() concurrent error = %v", err)
		}
	}
	if createdCount != 1 {
		t.Fatalf("EnsureConfig() created count = %d, want 1", createdCount)
	}
}
