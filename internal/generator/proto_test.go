package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckProtoToolsReportsMissingTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := checkProtoTools()
	if err == nil {
		t.Fatal("checkProtoTools() error = nil, want missing tools error")
	}
	for _, tool := range []string{"protoc", "protoc-gen-go", "protoc-gen-go-grpc"} {
		if !strings.Contains(err.Error(), tool) {
			t.Errorf("checkProtoTools() error %q does not mention %s", err, tool)
		}
	}
}

func TestNewProtoRequiresModule(t *testing.T) {
	t.Chdir(t.TempDir())

	err := NewProto("user", "example.com/app")
	if err == nil {
		t.Fatal("NewProto() error = nil, want missing module error")
	}
	if _, statErr := os.Stat(filepath.Join("proto", "user.proto")); !os.IsNotExist(statErr) {
		t.Fatalf("proto file created without module; stat error = %v", statErr)
	}
}

func TestProtoTemplateUsesModulePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.proto")
	data := protoData{Name: "user", Pascal: "User", ModulePath: "example.com/app"}
	if err := writeTemplate(path, protoFileTmpl, data); err != nil {
		t.Fatalf("writeTemplate() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	want := `option go_package = "example.com/app/internal/modules/user/pb;userpb";`
	if !strings.Contains(string(content), want) {
		t.Errorf("generated proto missing %q:\n%s", want, content)
	}
}

func TestNewProtoGeneratesPrefixedFiles(t *testing.T) {
	if err := checkProtoTools(); err != nil {
		t.Skip(err)
	}
	t.Chdir(t.TempDir())

	if err := NewModule("user", "example.com/app"); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}
	if err := NewProto("user", "example.com/app"); err != nil {
		t.Fatalf("NewProto() error = %v", err)
	}

	for _, path := range []string{
		filepath.Join("proto", "user.proto"),
		filepath.Join("internal", "modules", "user", "pb", "user.pb.go"),
		filepath.Join("internal", "modules", "user", "pb", "user_grpc.pb.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("generated file %q: %v", path, err)
		}
	}
}
