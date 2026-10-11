package fw

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultLoggerWritesInfoJSONToStdout(t *testing.T) {
	output, err := os.Create(filepath.Join(t.TempDir(), "logs.json"))
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = output
	t.Cleanup(func() {
		os.Stdout = previous
		if err := output.Close(); err != nil {
			t.Error(err)
		}
	})

	app := New(Config{})
	app.ensureLogger()
	app.logger.Debug("filtered")
	app.logger.With("module", "user").Info("started")

	content, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	var entry struct {
		Level  string `json:"level"`
		Msg    string `json:"msg"`
		Module string `json:"module"`
	}
	if err := json.Unmarshal(content, &entry); err != nil {
		t.Fatalf("expected one JSON entry: %v\n%s", err, content)
	}
	if entry.Level != "INFO" || entry.Msg != "started" || entry.Module != "user" {
		t.Fatalf("unexpected log entry: %s", content)
	}
}
