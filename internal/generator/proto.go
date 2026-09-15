package generator

import (
	"errors"
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
func NewProto(name, modPath string) (err error) {
	if err := validateModuleName(name); err != nil {
		return err
	}
	if err := validateModulePath(modPath); err != nil {
		return err
	}

	data := protoData{
		Name:       name,
		Pascal:     pascal(name),
		ModulePath: modPath,
	}

	protoFile := filepath.Join("proto", name+".proto")
	pbOut := filepath.Join("internal", "modules", name, "pb")

	if err := validateLocalDirectoryPath(filepath.Dir(protoFile)); err != nil {
		return err
	}
	if _, err := os.Lstat(protoFile); err == nil {
		return fmt.Errorf("proto file %q already exists", protoFile)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect proto file %q: %w", protoFile, err)
	}
	moduleDir := filepath.Join("internal", "modules", name)
	if err := validateLocalDirectoryPath(moduleDir); err != nil {
		return err
	}
	if info, err := os.Lstat(moduleDir); err != nil || !info.IsDir() {
		return fmt.Errorf("module %q not found at %s: run 'fw generate module %s' first", name, moduleDir, name)
	}
	if err := checkProtoTools(); err != nil {
		return err
	}

	fmt.Printf("  create %s\n", protoFile)
	if err = writeTemplate(protoFile, protoFileTmpl, data); err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		if removeErr := os.Remove(protoFile); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove incomplete proto file %s: %w", protoFile, removeErr))
		}
	}()

	if err = runProtoc(protoFile, pbOut); err != nil {
		return fmt.Errorf("generate Go code from %s: %w", protoFile, err)
	}

	fmt.Printf("\nProto file created at %s\n", protoFile)
	fmt.Printf("Generated Go code will be at %s/\n", pbOut)
	return nil
}

// GenerateProto runs protoc on all .proto files found under proto/.
func GenerateProto() error {
	if err := validateLocalDirectoryPath("proto"); err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join("proto", "*.proto"))
	if err != nil {
		return fmt.Errorf("find proto files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no .proto files found under proto/")
	}
	type target struct {
		file string
		name string
	}
	targets := make([]target, 0, len(files))
	for _, file := range files {
		info, err := os.Lstat(file)
		if err != nil {
			return fmt.Errorf("inspect proto file %q: %w", file, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse to generate from non-regular proto file %q", file)
		}
		name := stripExt(filepath.Base(file))
		if err := validateModuleName(name); err != nil {
			return fmt.Errorf("invalid proto filename %q: %w", file, err)
		}
		if err := validateLocalDirectoryPath(filepath.Join("internal", "modules", name, "pb")); err != nil {
			return err
		}
		targets = append(targets, target{file: file, name: name})
	}
	if err := checkProtoTools(); err != nil {
		return err
	}

	for _, target := range targets {
		pbOut := filepath.Join("internal", "modules", target.name, "pb")
		fmt.Printf("  gen    %s → %s/\n", target.file, pbOut)
		if err := runProtoc(target.file, pbOut); err != nil {
			return fmt.Errorf("protoc failed for %s: %w", target.file, err)
		}
	}
	return nil
}

func runProtoc(protoFile, pbOut string) (err error) {
	if err := validateLocalDirectoryPath(pbOut); err != nil {
		return err
	}
	if info, statErr := os.Lstat(pbOut); statErr == nil && !info.IsDir() {
		return fmt.Errorf("protobuf output path %s is not a directory", pbOut)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect protobuf output path %s: %w", pbOut, statErr)
	}
	stage, err := os.MkdirTemp("", "fw-protoc-*")
	if err != nil {
		return fmt.Errorf("create protobuf staging directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(stage); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove protobuf staging directory: %w", removeErr))
		}
	}()

	cmd := exec.Command("protoc",
		"--go_out="+stage,
		"--go_opt=paths=source_relative",
		"--go-grpc_out="+stage,
		"--go-grpc_opt=paths=source_relative",
		"-I", "proto",
		filepath.Base(protoFile),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run protoc: %w", err)
	}

	entries, err := os.ReadDir(stage)
	if err != nil {
		return fmt.Errorf("read protobuf staging directory: %w", err)
	}
	if len(entries) == 0 {
		return errors.New("protoc produced no output")
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected protoc output %q", entry.Name())
		}
		content, err := os.ReadFile(filepath.Join(stage, entry.Name()))
		if err != nil {
			return fmt.Errorf("read generated protobuf file %s: %w", entry.Name(), err)
		}
		if err := writeFileAtomic(filepath.Join(pbOut, entry.Name()), content, 0o644); err != nil {
			return err
		}
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
