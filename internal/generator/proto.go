package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type protoData struct {
	Name       string // e.g. "inventory"
	Pascal     string // e.g. "Inventory"
	ModulePath string // e.g. "github.com/you/myapp"
}

// NewProto scaffolds a .proto file for a module and generates its Go code.
func NewProto(name, modPath string) error {
	data := protoData{
		Name:       name,
		Pascal:     pascal(name),
		ModulePath: modPath,
	}

	protoFile := filepath.Join("proto", name+".proto")
	pbOut := filepath.Join("internal", "modules", name, "pb")

	if _, err := os.Stat(protoFile); err == nil {
		return fmt.Errorf("proto file %q already exists", protoFile)
	}
	moduleDir := filepath.Join("internal", "modules", name)
	if info, err := os.Stat(moduleDir); err != nil || !info.IsDir() {
		return fmt.Errorf("module %q not found at %s: run 'fw generate module %s' first", name, moduleDir, name)
	}
	if err := checkProtoTools(); err != nil {
		return err
	}

	fmt.Printf("  create %s\n", protoFile)
	if err := writeTemplate(protoFile, protoFileTmpl, data); err != nil {
		return err
	}

	if err := runProtoc(protoFile, pbOut); err != nil {
		return fmt.Errorf("generate Go code from %s: %w", protoFile, err)
	}

	fmt.Printf("\nProto file created at %s\n", protoFile)
	fmt.Printf("Generated Go code will be at %s/\n", pbOut)
	return nil
}

// GenerateProto runs protoc on all .proto files found under proto/.
func GenerateProto() error {
	files, err := filepath.Glob(filepath.Join("proto", "*.proto"))
	if err != nil {
		return fmt.Errorf("find proto files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no .proto files found under proto/")
	}
	if err := checkProtoTools(); err != nil {
		return err
	}

	for _, f := range files {
		name := stripExt(filepath.Base(f))
		pbOut := filepath.Join("internal", "modules", name, "pb")
		fmt.Printf("  gen    %s → %s/\n", f, pbOut)
		if err := runProtoc(f, pbOut); err != nil {
			return fmt.Errorf("protoc failed for %s: %w", f, err)
		}
	}
	return nil
}

func runProtoc(protoFile, pbOut string) error {
	if err := os.MkdirAll(pbOut, 0o755); err != nil {
		return fmt.Errorf("create protobuf output directory: %w", err)
	}
	cmd := exec.Command("protoc",
		"--go_out="+pbOut,
		"--go_opt=paths=source_relative",
		"--go-grpc_out="+pbOut,
		"--go-grpc_opt=paths=source_relative",
		"-I", "proto",
		filepath.Base(protoFile),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run protoc: %w", err)
	}
	return nil
}

func checkProtoTools() error {
	tools := []string{"protoc", "protoc-gen-go", "protoc-gen-go-grpc"}
	var missing []string
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"missing protobuf tools: %s; install protoc and run 'go install google.golang.org/protobuf/cmd/protoc-gen-go@latest' and 'go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest'",
			strings.Join(missing, ", "),
		)
	}
	return nil
}

func stripExt(filename string) string {
	ext := filepath.Ext(filename)
	return filename[:len(filename)-len(ext)]
}

var protoFileTmpl = `syntax = "proto3";

package {{ .Name }}.v1;

option go_package = "{{ .ModulePath }}/internal/modules/{{ .Name }}/pb;{{ .Name }}pb";

// {{ .Pascal }}Service manages {{ .Name }} operations.
service {{ .Pascal }}Service {
  rpc Get{{ .Pascal }}(Get{{ .Pascal }}Request) returns (Get{{ .Pascal }}Response);
}

message Get{{ .Pascal }}Request {
  string id = 1;
}

message Get{{ .Pascal }}Response {
  string id = 1;
}
`
