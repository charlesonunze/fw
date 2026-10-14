package generator

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFindSourceImports(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFixture(t, "output/internal/service.go", `package service

import (
	db "example.com/app/internal/database"
	_ "example.com/app/internal/database"
	. "example.com/app/shared"
	"example.com/app"
	"example.com/application/internal/database"
	"example.com/app-extra/internal/database"
	"example.com/other/internal/database"
	"user-service/internal/pb"
	"context"
)

const documentation = "example.com/app/internal/not_an_import"
`)
	writeFixture(t, "output/internal/nested/helper_test.go", `package nested

import "example.com/app/internal/testsupport"
`)
	writeFixture(t, "output/internal/platform_windows.go", `//go:build windows

package service

//line original.go:100
import _ "example.com/app/internal/platform"
`)
	writeFixture(t, "output/internal/notes.txt", "example.com/app/internal/not_go")

	got, err := findSourceImports("output/internal", "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	want := []sourceImport{
		{filepath.FromSlash("output/internal/nested/helper_test.go"), 3, "example.com/app/internal/testsupport"},
		{filepath.FromSlash("output/internal/platform_windows.go"), 6, "example.com/app/internal/platform"},
		{filepath.FromSlash("output/internal/service.go"), 4, "example.com/app/internal/database"},
		{filepath.FromSlash("output/internal/service.go"), 5, "example.com/app/internal/database"},
		{filepath.FromSlash("output/internal/service.go"), 6, "example.com/app/shared"},
		{filepath.FromSlash("output/internal/service.go"), 7, "example.com/app"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("imports = %+v, want %+v", got, want)
	}
}

func TestFindSourceImportsRejectsInvalidFiles(t *testing.T) {
	t.Run("malformed import", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeFixture(t, "output/broken.go", "package broken\nimport (")
		if _, err := findSourceImports("output", "example.com/app"); err == nil {
			t.Fatal("accepted malformed imports")
		}
	})
	t.Run("missing directory", func(t *testing.T) {
		if _, err := findSourceImports(filepath.Join(t.TempDir(), "missing"), "example.com/app"); !os.IsNotExist(err) {
			t.Fatalf("error = %v, want missing directory", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeFixture(t, "outside.go", "package outside\n")
		if err := os.Mkdir("output", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../outside.go", "output/linked.go"); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := findSourceImports("output", "example.com/app"); err == nil || !strings.Contains(err.Error(), "non-regular") {
			t.Fatalf("error = %v, want non-regular file", err)
		}
	})
}

func TestDecoupleReportsRemainingSourceImports(t *testing.T) {
	for _, tt := range []struct{ name, transport, router string }{
		{"chi", "http", routerChi},
		{"gin", "http", routerGin},
		{"grpc", "grpc", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.26.9\n")
			cfg := ModuleConfig{Router: tt.router}
			if tt.transport == "grpc" {
				cfg.Transport = ModuleTransportNone
			}
			for _, name := range []string{"order", "user"} {
				if err := NewModule(name, "example.com/app", cfg); err != nil {
					t.Fatal(err)
				}
			}
			if tt.transport == "grpc" {
				writeFixture(t, "internal/modules/order/order_grpc.go", `package order
import "google.golang.org/grpc"
func (*Module) RegisterGRPC(*grpc.Server) {}
`)
			}
			const source = `package order

import (
	_ "example.com/app/internal/database"
	_ "example.com/app/internal/modules/order/pb"
	_ "example.com/app/internal/modules/user"
	_ "example.com/app/internal/services/postgres"
)
`
			writeFixture(t, "internal/modules/order/order_dependencies.go", source)
			writeFixture(t, "internal/modules/order/pb/types.go", "package pb\n")
			writeFixture(t, "internal/database/schema.go", "package database\n")

			var generateErr error
			output := captureStdout(t, func() {
				generateErr = DecoupleModule("order", "example.com/app", "output", ":8081", tt.transport, tt.router, "")
			})
			if generateErr != nil {
				t.Fatal(generateErr)
			}
			for _, want := range []string{
				"copied Go files still import source-project packages:",
				filepath.FromSlash("output/internal/order_dependencies.go") + ":4: example.com/app/internal/database",
				filepath.FromSlash("output/internal/order_dependencies.go") + ":5: example.com/app/internal/modules/user",
				filepath.FromSlash("output/internal/order_dependencies.go") + ":6: example.com/app/internal/services/postgres",
				"These imports were not migrated.",
				"A replace directive does not bypass Go's internal-package visibility rules.",
				"Resolve the source-project imports listed above",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q:\n%s", want, output)
				}
			}
			if strings.Contains(output, "example.com/app/internal/modules/order/pb") {
				t.Errorf("reported a rewritten import:\n%s", output)
			}
			original, err := os.ReadFile("internal/modules/order/order_dependencies.go")
			if err != nil || string(original) != source {
				t.Fatalf("source changed: %q, %v", original, err)
			}
			copied, err := os.ReadFile("output/internal/order_dependencies.go")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"order-service/internal/pb",
				"example.com/app/internal/database",
				"example.com/app/internal/modules/user",
				"example.com/app/internal/services/postgres",
			} {
				if !strings.Contains(string(copied), want) {
					t.Errorf("unexpected import rewrite: missing %q in %s", want, copied)
				}
			}
			if _, err := os.Stat("output/internal/database"); !os.IsNotExist(err) {
				t.Fatalf("shared database package was copied: %v", err)
			}
		})
	}
}

func TestDecoupleDoesNotWarnForSelfContainedModule(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.26.9\n")
	if err := NewModule("user", "example.com/app", ModuleConfig{Router: routerChi}); err != nil {
		t.Fatal(err)
	}
	writeDecoupleAssetFixture(t)
	var generateErr error
	output := captureStdout(t, func() {
		generateErr = DecoupleModule("user", "example.com/app", "output", ":8081", "http", routerChi, "")
	})
	if generateErr != nil {
		t.Fatal(generateErr)
	}
	if strings.Contains(output, "warn") || strings.Contains(output, "source-project imports") {
		t.Fatalf("unexpected diagnostic for self-contained module:\n%s", output)
	}
}
