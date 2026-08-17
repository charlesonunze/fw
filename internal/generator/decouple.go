package generator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type decoupleData struct {
	Name        string // e.g. "order"
	Pascal      string // e.g. "Order"
	ModuleName  string // e.g. "order-service"
	Port        string // e.g. ":8081"
	ExposedPort string // e.g. "8081"
	GoVersion   string // e.g. "1.25.2"
	Router      string // chi or gin
}

type clientData struct {
	DepName   string   // e.g. "user"
	DepPascal string   // e.g. "User"
	Methods   []string // e.g. ["GetUser", "CreateUser"]
}

// DecoupleModule generates a new standalone project for the target module.
func DecoupleModule(name, modPath, output, port, transport, router, localFWPath string) error {
	if err := validateRouter(router); err != nil {
		return err
	}

	// 1. Verify source module exists
	if _, err := findModuleFile(name); err != nil {
		return fmt.Errorf(
			"module %q not found: expected %s_module.go\nRun 'fw generate module %s' first",
			name, name, name,
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
		Router:      router,
	}

	// 4. Restructure module files into a flat internal/ layout with rewritten imports
	fmt.Printf("  copy   internal/modules/%s → %s/internal/\n", name, output)
	if err := restructureModule(name, modPath, data.ModuleName, output); err != nil {
		return fmt.Errorf("failed to restructure module: %w", err)
	}

	// 5. Generate client scaffolds for each dependency
	for _, dep := range deps {
		var methods []string
		servicePath, err := findServiceFile(dep)
		if err != nil {
			fmt.Printf("  warn   could not find service for %s: %v\n", dep, err)
		} else if methods, err = extractMethods(servicePath); err != nil {
			fmt.Printf("  warn   could not extract methods for %s: %v\n", dep, err)
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
	if err := writeDevelopmentFiles(output); err != nil {
		return err
	}

	// Write go.mod (not a template — content is built dynamically)
	fmt.Printf("  create go.mod\n")
	if err := writeGoMod(output, data.ModuleName, data.GoVersion); err != nil {
		return err
	}
	if localFWPath != "" {
		if err := addLocalReplacements(output, router, localFWPath); err != nil {
			return err
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
	fmt.Printf("  Run standalone:  fw dev\n")
	fmt.Printf("  Build image:     docker build -t %s .\n\n", data.ModuleName)

	return nil
}

func findModuleFile(name string) (string, error) {
	base := filepath.Join("internal", "modules", name)
	candidates := []string{
		filepath.Join(base, name+"_module.go"),
		filepath.Join(base, "module.go"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

func findServiceFile(name string) (string, error) {
	base := filepath.Join("internal", "modules", name)
	candidates := []string{
		filepath.Join(base, name+"_service.go"),
		filepath.Join(base, "service", name+"_service.go"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

// detectDeps scans all .go files under internal/modules/<name>/ for imports
// rooted at <modPath>/internal/modules. It supports flat and nested modules.
func detectDeps(name, modPath string) ([]string, error) {
	seen := map[string]bool{}
	var deps []string
	importPrefix := modPath + "/internal/modules/"

	dir := filepath.Join("internal", "modules", name)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse imports from %s: %w", path, err)
		}

		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || !strings.HasPrefix(importPath, importPrefix) {
				continue
			}
			remainder := strings.TrimPrefix(importPath, importPrefix)
			dep, _, _ := strings.Cut(remainder, "/")
			if dep != name && !seen[dep] {
				seen[dep] = true
				deps = append(deps, dep)
			}
		}
		return nil
	})

	sort.Strings(deps)
	return deps, err
}

// extractMethods returns exported business methods declared on a service type.
func extractMethods(path string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil, err
	}

	var methods []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !fn.Name.IsExported() {
			continue
		}
		if fn.Name.Name == "Name" || fn.Name.Name == "Close" {
			continue
		}
		receiver := receiverName(fn.Recv.List[0].Type)
		if receiver == "Service" || strings.HasSuffix(receiver, "Service") {
			methods = append(methods, fn.Name.Name)
		}
	}

	return methods, nil
}

func receiverName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverName(value.X)
	default:
		return ""
	}
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

// restructureModule copies a module into the standalone service's internal
// package and rewrites imports rooted at the original module path.
func restructureModule(name, modPath, moduleName, output string) error {
	srcBase := filepath.Join("internal", "modules", name)
	oldImportRoot := modPath + "/internal/modules/" + name
	newImportRoot := moduleName + "/internal"

	return filepath.Walk(srcBase, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}

		rel, err := filepath.Rel(srcBase, path)
		if err != nil {
			return err
		}

		dst := filepath.Join(output, "internal", rel)

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rewritten := strings.ReplaceAll(string(content), oldImportRoot, newImportRoot)

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte(rewritten), 0o644)
	})
}

// --- Templates ---

var decoupleCmdTmpl = `package main

import (
	"log"

	"github.com/charlesonunze/fw"
	{{- if eq .Router "chi" }}
	fwrouter "github.com/charlesonunze/fw/adapters/chi"
	"github.com/go-chi/chi/v5"
	{{- else }}
	fwrouter "github.com/charlesonunze/fw/adapters/gin"
	"github.com/gin-gonic/gin"
	{{- end }}
	{{ .Name }} "{{ .ModuleName }}/internal"
)

func main() {
	{{- if eq .Router "chi" }}
	router := chi.NewRouter()
	{{- else }}
	router := gin.New()
	{{- end }}
	app := fw.New(
		fw.WithAddr("{{ .Port }}"),
		fw.WithRouter(fwrouter.NewRouter(router)),
	)

	app.RegisterModules(
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
	"google.golang.org/grpc/credentials/insecure"
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
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
