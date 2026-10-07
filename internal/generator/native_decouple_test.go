package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeHTTPDecoupling(t *testing.T) {
	if testing.Short() {
		t.Skip("generated application integration test")
	}
	t.Setenv("GOWORK", "off")
	for _, router := range []string{routerChi, routerGin} {
		t.Run(router, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.25.13\n")
			if err := writeRouterMetadata(".", router); err != nil {
				t.Fatal(err)
			}
			if err := NewModule("user", "example.com/app", ModuleConfig{}); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join("microservices", "user")
			if err := DecoupleModule("user", "example.com/app", output, ":8081", "http", "", frameworkRoot(t)); err != nil {
				t.Fatal(err)
			}
			if got, err := ResolveRouter(output, ""); err != nil || got != router {
				t.Fatalf("router = %q, %v", got, err)
			}
			writeNativeRouteTest(t, output, "user-service/internal", router)
			if err := runGo(output, "mod", "tidy"); err != nil {
				t.Fatal(err)
			}
			if err := runGo(output, "test", "./..."); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDecoupleRejectsWrongNativeRouterBeforeWriting(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := NewModule("user", "example.com/app", ModuleConfig{Router: routerGin}); err != nil {
		t.Fatal(err)
	}
	err := DecoupleModule("user", "example.com/app", "output", ":8081", "http", routerChi, "")
	if err == nil || !strings.Contains(err.Error(), "fwchi.Module") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat("output"); !os.IsNotExist(err) {
		t.Fatalf("created output: %v", err)
	}
}
