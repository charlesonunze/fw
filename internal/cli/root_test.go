package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandFlagsAreIndependent(t *testing.T) {
	tests := []struct {
		name     string
		path     []string
		flags    []string
		defaults map[string]string
	}{
		{
			name:     "new",
			path:     []string{"new"},
			flags:    []string{"--module", "example.com/first", "--router", "gin", "--local", "../fw"},
			defaults: map[string]string{"module": "", "router": "chi", "local": ""},
		},
		{
			name:     "module",
			path:     []string{"generate", "module"},
			flags:    []string{"--transport", "none", "--router", "gin"},
			defaults: map[string]string{"transport": "http", "router": ""},
		},
		{
			name:     "decouple",
			path:     []string{"decouple"},
			flags:    []string{"--output", "standalone", "--port", ":9090", "--transport", "grpc", "--router", "gin", "--local", "../fw"},
			defaults: map[string]string{"output": "", "port": ":8081", "transport": "http", "router": "", "local": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, _, err := newRootCommand().Find(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			second, _, err := newRootCommand().Find(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if err := first.ParseFlags(tt.flags); err != nil {
				t.Fatal(err)
			}
			for name, want := range tt.defaults {
				got, err := second.Flags().GetString(name)
				if err != nil || got != want {
					t.Errorf("second --%s = %q, %v; want %q", name, got, err, want)
				}
				if second.Flags().Changed(name) {
					t.Errorf("second --%s marked as changed", name)
				}
			}
		})
	}
}

func TestExecuteDoesNotRetainState(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError bool
		router    string
	}{
		{
			name:   "transport after success",
			args:   []string{"generate", "module", "first", "--transport", "none"},
			router: "chi",
		},
		{
			name:      "transport after failure",
			args:      []string{"generate", "module", "first", "--transport", "invalid"},
			wantError: true,
			router:    "chi",
		},
		{
			name:      "flags after parse failure",
			args:      []string{"generate", "module", "first", "--transport", "none", "--unknown"},
			wantError: true,
			router:    "chi",
		},
		{
			name:   "router from project metadata",
			args:   []string{"generate", "module", "first", "--router", "chi"},
			router: "gin",
		},
		{
			name:   "help",
			args:   []string{"generate", "module", "--help"},
			router: "chi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeCLIFile(t, "go.mod", "module example.com/app\n\ngo 1.26.9\n")
			writeCLIFile(t, ".fw.json", `{"router":"chi"}`)
			if err := executeCLI(t, tt.args...); (err != nil) != tt.wantError {
				t.Fatalf("first Execute() = %v, want error %v", err, tt.wantError)
			}

			writeCLIFile(t, ".fw.json", `{"router":"`+tt.router+`"}`)
			if err := executeCLI(t, "generate", "module", "user"); err != nil {
				t.Fatalf("second Execute() = %v", err)
			}
			handler, err := os.ReadFile(filepath.Join("internal", "modules", "user", "user_http.go"))
			if err != nil {
				t.Fatalf("read generated HTTP handler: %v", err)
			}
			want := "github.com/go-chi/chi/v5"
			if tt.router == "gin" {
				want = "github.com/gin-gonic/gin"
			}
			if !strings.Contains(string(handler), want) {
				t.Fatalf("HTTP handler does not import %q:\n%s", want, handler)
			}
		})
	}
}

func TestDecoupleDefaultsAfterCustomFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	writeCLIFile(t, "go.mod", "module example.com/app\n\ngo 1.26.9\n")
	writeCLIFile(t, ".fw.json", `{"router":"chi"}`)
	for _, args := range [][]string{
		{"generate", "module", "user"},
		{"decouple", "user", "--output", "custom", "--port", ":9090", "--router", "chi"},
		{"decouple", "user"},
	} {
		if err := executeCLI(t, args...); err != nil {
			t.Fatalf("Execute(%v) = %v", args, err)
		}
	}
	for path, port := range map[string]string{"custom": ":9090", "microservices/user": ":8081"} {
		content, err := os.ReadFile(filepath.Join(path, "cmd", "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), `"`+port+`"`) {
			t.Errorf("generated main in %s does not use %s:\n%s", path, port, content)
		}
	}
}

func TestProjectPromptUsesCommandStreams(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		module    string
		wantError string
	}{
		{name: "prompt", input: "  example.com/app  \n", wantError: `unsupported router "invalid"`},
		{name: "flag", module: "example.com/app", wantError: `unsupported router "invalid"`},
		{name: "empty path", input: " \n", wantError: "module path is required"},
		{name: "read failure", wantError: "failed to read input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(io.Discard)
			cmd.SetIn(strings.NewReader(tt.input))
			args := []string{"new", "app", "--router", "invalid"}
			if tt.module != "" {
				args = append(args, "--module", tt.module)
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Execute() = %v, want %q", err, tt.wantError)
			}
			if tt.name == "read failure" && !errors.Is(err, io.EOF) {
				t.Errorf("Execute() = %v, want wrapped io.EOF", err)
			}
			prompted := strings.Contains(output.String(), "Go module path (e.g. github.com/you/app): ")
			if prompted != (tt.module == "") {
				t.Errorf("prompted = %v, output = %q", prompted, output.String())
			}
		})
	}
}

func executeCLI(t *testing.T, args ...string) error {
	t.Helper()
	previous := os.Args
	defer func() { os.Args = previous }()
	os.Args = append([]string{"fw"}, args...)
	return Execute()
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
