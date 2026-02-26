package generator

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type decoupleData struct {
	Name        string // e.g. "order"
	Pascal      string // e.g. "Order"
	ModuleName  string // e.g. "order-service"
	Port        string // e.g. ":8081"
	ExposedPort string // e.g. "8081"
	GoVersion   string // e.g. "1.25.2"
}

type clientData struct {
	DepName   string   // e.g. "user"
	DepPascal string   // e.g. "User"
	Methods   []string // e.g. ["GetUser", "CreateUser"]
}

// DecoupleModule generates a new standalone project for the target module.
func DecoupleModule(name, modPath, output, port, transport string) error {
	// 1. Verify source module exists
	srcModulePath := filepath.Join("internal", "modules", name, "module.go")
	if _, err := os.Stat(srcModulePath); os.IsNotExist(err) {
		return fmt.Errorf(
			"module %q not found: expected %s to exist\nRun 'fw generate module %s' first",
			name, srcModulePath, name,
		)
	}

	// 2. Guard output path
	if _, err := os.Stat(output); err == nil {
		return fmt.Errorf("output directory %q already exists", output)
	}

	// 3. Detect cross-module dependencies
	deps, err := detectDeps(name, modPath)
	if err != nil {
		return fmt.Errorf("failed to detect dependencies: %w", err)
	}

	goVersion := detectGoVersion()
	data := decoupleData{
		Name:        name,
		Pascal:      pascal(name),
		ModuleName:  name + "-service",
		Port:        port,
		ExposedPort: strings.TrimPrefix(port, ":"),
		GoVersion:   goVersion,
	}

	// 4. Restructure module files into a flat internal/ layout with rewritten imports
	fmt.Printf("  copy   internal/modules/%s → %s/internal/\n", name, output)
	if err := restructureModule(name, modPath, data.ModuleName, output); err != nil {
		return fmt.Errorf("failed to restructure module: %w", err)
	}

	// 5. Generate client scaffolds for each dependency
	for _, dep := range deps {
		methods, err := extractMethods(filepath.Join("internal", "modules", dep, "service", dep+"_service.go"))
		if err != nil {
			fmt.Printf("  warn   could not extract methods for %s: %v\n", dep, err)
			methods = []string{}
		}

		cd := clientData{
			DepName:   dep,
			DepPascal: pascal(dep),
			Methods:   methods,
		}

		clientPath := filepath.Join(output, "internal", "clients", dep, "client.go")
		fmt.Printf("  create internal/clients/%s/client.go\n", dep)

		tmpl := clientHTTPTmpl
		if transport == "grpc" {
			tmpl = clientGRPCTmpl
		}

		if err := writeTemplate(clientPath, tmpl, cd); err != nil {
			return err
		}
	}

	// 6. Write scaffolded project files
	scaffoldedFiles := []struct {
		relPath string
		tmpl    string
	}{
		{filepath.Join("cmd", "main.go"), decoupleCmdTmpl},
		{"Dockerfile", decoupleDockerfileTmpl},
	}

	for _, f := range scaffoldedFiles {
		fmt.Printf("  create %s\n", f.relPath)
		if err := writeTemplate(filepath.Join(output, f.relPath), f.tmpl, data); err != nil {
			return err
		}
	}

	// Write go.mod (not a template — content is built dynamically)
	fmt.Printf("  create go.mod\n")
	if err := writeGoMod(output, data.ModuleName, data.GoVersion); err != nil {
		return err
	}

	// Copy config.yaml if present
	if _, err := os.Stat("config.yaml"); err == nil {
		fmt.Printf("  copy   config.yaml\n")
		if err := copyFile("config.yaml", filepath.Join(output, "config.yaml")); err != nil {
			return fmt.Errorf("failed to copy config.yaml: %w", err)
		}
	}

	// 7. Print summary
	fmt.Printf("\nModule %q decoupled → %s\n\n", name, output)
	fmt.Printf("Next steps:\n\n")
	for i, dep := range deps {
		fmt.Printf("  %d. Implement client stubs in %s/internal/clients/%s/client.go\n", i+1, output, dep)
	}
	base := len(deps) + 1
	if len(deps) > 0 {
		fmt.Printf("  %d. Update the copied service to use the generated interface\n", base)
		fmt.Printf("     instead of the original service registry type\n")
		base++
	}
	fmt.Printf("  %d. cd %s && go mod tidy && go build ./...\n\n", base, output)
	fmt.Printf("  Run standalone:  go run ./cmd/\n")
	fmt.Printf("  Build image:     docker build -t %s .\n\n", data.ModuleName)

	return nil
}

// detectDeps scans all .go files under internal/modules/<name>/ for imports
// matching <modPath>/internal/modules/*/. Returns unique dependency names.
func detectDeps(name, modPath string) ([]string, error) {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(modPath) + `/internal/modules/(\w+)/`)

	seen := map[string]bool{}
	var deps []string

	dir := filepath.Join("internal", "modules", name)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, match := range pattern.FindAllStringSubmatch(string(content), -1) {
			dep := match[1]
			if dep != name && !seen[dep] {
				seen[dep] = true
				deps = append(deps, dep)
			}
		}
		return nil
	})

	return deps, err
}

// extractMethods reads a service Go file and extracts exported method names.
func extractMethods(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	pattern := regexp.MustCompile(`^func \(\w+ \*\w+\) ([A-Z]\w+)\(`)

	var methods []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if m := pattern.FindStringSubmatch(scanner.Text()); m != nil {
			methods = append(methods, m[1])
		}
	}

	return methods, scanner.Err()
}

// detectGoVersion reads the go version from the current project's go.mod.
func detectGoVersion() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "1.21"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			return strings.TrimPrefix(line, "go ")
		}
	}
	return "1.21"
}

// writeGoMod writes a minimal go.mod for the new standalone project.
func writeGoMod(output, moduleName, goVersion string) error {
	content := fmt.Sprintf("module %s\n\ngo %s\n\nrequire github.com/charlesonunze/fw v0.0.0\n", moduleName, goVersion)
	path := filepath.Join(output, "go.mod")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// restructureModule copies a module's files from the nested monolith layout
// (internal/modules/<name>/<subpkg>/) into a flat layout (<output>/internal/<subpkg>/)
// and rewrites intra-module import paths to use the new standalone module name.
func restructureModule(name, modPath, moduleName, output string) error {
	srcBase := filepath.Join("internal", "modules", name)

	// Build a replacer that rewrites internal subpackage imports.
	// e.g. "github.com/you/app/internal/modules/order/domain" → "order-service/internal/domain"
	subpkgs := []string{"domain", "service", "repo", "api", "transform"}
	pairs := make([]string, 0, len(subpkgs)*2)
	for _, pkg := range subpkgs {
		old := fmt.Sprintf(`"%s/internal/modules/%s/%s"`, modPath, name, pkg)
		new := fmt.Sprintf(`"%s/internal/%s"`, moduleName, pkg)
		pairs = append(pairs, old, new)
	}
	replacer := strings.NewReplacer(pairs...)

	return filepath.Walk(srcBase, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}

		rel, err := filepath.Rel(srcBase, path)
		if err != nil {
			return err
		}

		// module.go → <output>/internal/module.go
		// domain/order_domain.go → <output>/internal/domain/order_domain.go
		dst := filepath.Join(output, "internal", rel)

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rewritten := replacer.Replace(string(content))

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte(rewritten), 0o644)
	})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// --- Templates ---

var decoupleCmdTmpl = `package main

import (
	"log"

	"github.com/charlesonunze/fw"
	{{ .Name }} "{{ .ModuleName }}/internal"
)

func main() {
	app := fw.New(
		fw.WithAddr("{{ .Port }}"),
		fw.WithConfigPath("config.yaml"),
	)

	app.Register(
		{{ .Name }}.New(),
	)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
`

var decoupleDockerfileTmpl = `# syntax=docker/dockerfile:1

FROM golang:{{ .GoVersion }}-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/{{ .Name }} ./cmd/

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /bin/{{ .Name }} /bin/{{ .Name }}
COPY config.yaml ./
EXPOSE {{ .ExposedPort }}
ENTRYPOINT ["/bin/{{ .Name }}"]
`

var clientHTTPTmpl = `package {{ .DepName }}

import (
	"context"
	"net/http"
)

// {{ .DepPascal }}Service is the interface for calling the {{ .DepName }} service remotely.
// The methods below were detected as dependencies of the decoupled module.
type {{ .DepPascal }}Service interface {
{{- range .Methods }}
	{{ . }}(ctx context.Context) error // TODO: update signature to match your needs
{{- end }}
}

// HTTPClient implements {{ .DepPascal }}Service over HTTP.
type HTTPClient struct {
	BaseURL string
	client  *http.Client
}

// New creates a new HTTPClient targeting the given base URL.
// Example: New("http://user-service:8080")
func New(baseURL string) *HTTPClient {
	return &HTTPClient{BaseURL: baseURL, client: &http.Client{}}
}
{{ range .Methods }}
// {{ . }} TODO: implement HTTP call to the {{ $.DepName }} service.
func (c *HTTPClient) {{ . }}(ctx context.Context) error {
	panic("not implemented: HTTPClient.{{ . }}")
}
{{ end }}`

var clientGRPCTmpl = `package {{ .DepName }}

import (
	"context"

	"google.golang.org/grpc"
)

// {{ .DepPascal }}Service is the interface for calling the {{ .DepName }} service remotely.
// The methods below were detected as dependencies of the decoupled module.
type {{ .DepPascal }}Service interface {
{{- range .Methods }}
	{{ . }}(ctx context.Context) error // TODO: update signature to match your proto definition
{{- end }}
}

// GRPCClient implements {{ .DepPascal }}Service over gRPC.
type GRPCClient struct {
	conn *grpc.ClientConn
}

// New creates a new GRPCClient connected to the given address.
// Example: New("user-service:50051")
func New(addr string) (*GRPCClient, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure()) //nolint:staticcheck
	if err != nil {
		return nil, err
	}
	return &GRPCClient{conn: conn}, nil
}
{{ range .Methods }}
// {{ . }} TODO: implement gRPC call to the {{ $.DepName }} service.
func (c *GRPCClient) {{ . }}(ctx context.Context) error {
	panic("not implemented: GRPCClient.{{ . }}")
}
{{ end }}`
