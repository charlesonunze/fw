package generator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRouter(t *testing.T) {
	for _, tt := range []struct {
		name, metadata, requested, want string
		wantError                       bool
	}{
		{name: "missing choice", wantError: true},
		{name: "legacy explicit", requested: "gin", want: "gin"},
		{name: "recorded", metadata: `{"router":"gin"}`, want: "gin"},
		{name: "matching explicit", metadata: `{"router":"chi"}`, requested: "chi", want: "chi"},
		{name: "conflict", metadata: `{"router":"gin"}`, requested: "chi", wantError: true},
		{name: "malformed", metadata: `{`, requested: "gin", wantError: true},
		{name: "unknown router", metadata: `{"router":"echo"}`, wantError: true},
		{name: "missing router field", metadata: `{}`, wantError: true},
		{name: "unsupported flag", requested: "echo", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.metadata != "" {
				writeFixture(t, filepath.Join(dir, routerMetadataFile), tt.metadata)
			}
			got, err := ResolveRouter(dir, tt.requested)
			if (err != nil) != tt.wantError || got != tt.want {
				t.Fatalf("ResolveRouter() = %q, %v; want %q, error=%v", got, err, tt.want, tt.wantError)
			}
		})
	}
}

func TestMissingRouterDoesNotCreateModule(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := NewModule("user", "example.com/app", ModuleConfig{}); err == nil {
		t.Fatal("HTTP generation silently selected a router")
	}
	if _, err := os.Stat("internal"); !os.IsNotExist(err) {
		t.Fatalf("generation wrote files: %v", err)
	}
	if err := NewModule("worker", "example.com/app", ModuleConfig{Transport: ModuleTransportNone}); err != nil {
		t.Fatal(err)
	}
}
