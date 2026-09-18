package generator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/version"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

type decoupleData struct {
	Name        string // e.g. "order"
	Pascal      string // e.g. "Order"
	ModuleName  string // e.g. "order-service"
	Port        string // e.g. ":8081"
	ExposedPort string // e.g. "8081"
	GoVersion   string // e.g. "1.25.2"
	Router      string // chi or gin
	Transport   string // http or grpc
}

type clientData struct {
	DepName   string   // e.g. "user"
	DepPascal string   // e.g. "User"
	Methods   []string // e.g. ["GetUser", "CreateUser"]
}

// DecoupleModule generates a new standalone project for the target module.
func DecoupleModule(name, modPath, output, port, transport, router, localFWPath string) (err error) {
	if err := validateModuleName(name); err != nil {
		return err
	}
	if err := validateModulePath(modPath); err != nil {
		return err
	}
	if transport != "http" && transport != "grpc" {
		return fmt.Errorf("unsupported transport %q: use http or grpc", transport)
	}
	if transport == "http" {
		if err := validateRouter(router); err != nil {
			return err
		}
	}
	replacementRouter := router
	if transport == "grpc" {
		replacementRouter = ""
	}
	if localFWPath != "" {
		if _, err := validateLocalReplacements(replacementRouter, localFWPath); err != nil {
			return err
		}
	}
	exposedPort, err := validatePort(port)
	if err != nil {
		return err
	}

	// 1. Verify source module exists and supports the selected transport.
	if _, err := findModuleFile(name); err != nil {
		return fmt.Errorf(
			"module %q not found: expected %s_module.go\nRun 'fw generate module %s' first",
			name, name, name,
		)
	}
	supported, err := moduleSupportsTransport(name, transport)
	if err != nil {
		return fmt.Errorf("inspect module transport: %w", err)
	}
	if !supported {
		interfaceName := "fwhttp.Module"
		methodName := "RegisterRoutes"
		if transport == "grpc" {
			interfaceName = "fwgrpc.Module"
			methodName = "RegisterGRPC"
		}
		return fmt.Errorf(
			"module %q does not implement %s: add %s before decoupling with --transport %s",
			name,
			interfaceName,
			methodName,
			transport,
		)
	}

	// 2. Guard output path
	sourceDir := filepath.Join("internal", "modules", name)
	if err := validateOutputPath(output, sourceDir); err != nil {
		return err
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
		ExposedPort: exposedPort,
		GoVersion:   goVersion,
		Router:      router,
		Transport:   transport,
	}
	if err := createGeneratedDir(output); err != nil {
		return err
	}
	defer cleanupGeneratedDir(output, &err)

	// 4. Restructure module files into a flat internal/ layout with rewritten imports
	fmt.Printf("  copy   internal/modules/%s → %s/internal/\n", name, output)
	if err = restructureModule(name, modPath, data.ModuleName, output); err != nil {
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

		if err = writeTemplate(clientPath, tmpl, cd); err != nil {
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
		if err = writeTemplate(filepath.Join(output, f.relPath), f.tmpl, data); err != nil {
			return err
		}
	}
	if err = writeDevelopmentFiles(output); err != nil {
		return err
	}

	// Write go.mod (not a template — content is built dynamically)
	fmt.Printf("  create go.mod\n")
	if err = writeGoMod(output, data.ModuleName, data.GoVersion); err != nil {
		return err
	}
	if localFWPath != "" {
		if err = addLocalReplacements(output, replacementRouter, localFWPath); err != nil {
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
	if err := validateLocalDirectoryPath(base); err != nil {
		return "", err
	}
	if info, err := os.Lstat(base); err != nil {
		return "", err
	} else if !info.IsDir() {
		return "", fmt.Errorf("module path %s is not a directory", base)
	}
	candidates := []string{
		filepath.Join(base, name+"_module.go"),
		filepath.Join(base, "module.go"),
	}
	for _, path := range candidates {
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

func findServiceFile(name string) (string, error) {
	base := filepath.Join("internal", "modules", name)
	if err := validateLocalDirectoryPath(base); err != nil {
		return "", err
	}
	if info, err := os.Lstat(base); err != nil {
		return "", err
	} else if !info.IsDir() {
		return "", fmt.Errorf("module path %s is not a directory", base)
	}
	candidates := []string{
		filepath.Join(base, name+"_service.go"),
		filepath.Join(base, "service", name+"_service.go"),
	}
	for _, path := range candidates {
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

func moduleSupportsTransport(name, transport string) (bool, error) {
	methodName := "RegisterRoutes"
	if transport == "grpc" {
		methodName = "RegisterGRPC"
	}

	dir := filepath.Join("internal", "modules", name)
	var moduleType typeReference
	supportedReceivers := make(map[string]methodSet)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 && strings.HasSuffix(path, ".go") {
			return fmt.Errorf("refuse to inspect symlinked Go file %s", path)
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		imports := importAliases(file)
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if function.Recv == nil && function.Name.Name == "New" && fieldCount(function.Type.Params) == 0 && fieldCount(function.Type.Results) == 1 {
				moduleType, _ = referenceType(function.Type.Results.List[0].Type)
				continue
			}
			if function.Recv != nil && function.Name.Name == methodName && validTransportMethod(function.Type, imports, transport) {
				receiver, ok := referenceType(function.Recv.List[0].Type)
				if !ok {
					continue
				}
				methods := supportedReceivers[receiver.name]
				if receiver.pointerDepth == 1 {
					methods.pointer = true
				} else if receiver.pointerDepth == 0 {
					methods.value = true
				}
				supportedReceivers[receiver.name] = methods
			}
		}
		return nil
	})
	if err != nil || moduleType.name == "" {
		return false, err
	}
	methods := supportedReceivers[moduleType.name]
	if moduleType.pointerDepth == 1 {
		return methods.pointer || methods.value, nil
	}
	if moduleType.pointerDepth == 0 {
		return methods.value, nil
	}
	return false, nil
}

type typeReference struct {
	name         string
	pointerDepth int
}

type methodSet struct {
	value   bool
	pointer bool
}

func referenceType(expression ast.Expr) (typeReference, bool) {
	switch value := expression.(type) {
	case *ast.Ident:
		return typeReference{name: value.Name}, true
	case *ast.ParenExpr:
		return referenceType(value.X)
	case *ast.StarExpr:
		reference, ok := referenceType(value.X)
		if !ok {
			return typeReference{}, false
		}
		reference.pointerDepth++
		return reference, true
	case *ast.IndexExpr:
		return referenceType(value.X)
	case *ast.IndexListExpr:
		return referenceType(value.X)
	default:
		return typeReference{}, false
	}
}

func validTransportMethod(function *ast.FuncType, imports map[string]string, transport string) bool {
	if fieldCount(function.Params) != 1 || fieldCount(function.Results) != 0 {
		return false
	}
	parameter := function.Params.List[0].Type
	wantImport := "github.com/charlesonunze/fw/transport/http"
	wantType := "Router"
	if transport == "grpc" {
		pointer, ok := parameter.(*ast.StarExpr)
		if !ok {
			return false
		}
		parameter = pointer.X
		wantImport = "google.golang.org/grpc"
		wantType = "Server"
	}
	selector, ok := parameter.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != wantType {
		return false
	}
	alias, ok := selector.X.(*ast.Ident)
	return ok && imports[alias.Name] == wantImport
}

func fieldCount(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			count++
		} else {
			count += len(field.Names)
		}
	}
	return count
}

func importAliases(file *ast.File) map[string]string {
	aliases := make(map[string]string)
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		alias := filepath.Base(importPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		aliases[alias] = importPath
	}
	return aliases
}

// detectDeps scans all .go files under internal/modules/<name>/ for imports
// rooted at <modPath>/internal/modules. It supports flat and nested modules.
func detectDeps(name, modPath string) ([]string, error) {
	seen := map[string]bool{}
	var deps []string
	importPrefix := modPath + "/internal/modules/"

	dir := filepath.Join("internal", "modules", name)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 && strings.HasSuffix(path, ".go") {
			return fmt.Errorf("refuse to inspect symlinked Go file %s", path)
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
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
			if err := validateModuleName(dep); err != nil {
				return fmt.Errorf("invalid module dependency in import %q: %w", importPath, err)
			}
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
	if info, err := os.Lstat(path); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refuse to inspect non-regular Go file %s", path)
	}
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
	reference, ok := referenceType(expr)
	if !ok {
		return ""
	}
	return reference.name
}

// detectGoVersion reads the go version from the current project's go.mod.
func detectGoVersion() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "1.21"
	}
	file, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil || file.Go == nil || !version.IsValid("go"+file.Go.Version) {
		return "1.21"
	}
	return file.Go.Version
}

// writeGoMod writes a minimal go.mod for the new standalone project.
func writeGoMod(output, moduleName, goVersion string) error {
	content := fmt.Sprintf("module %s\n\ngo %s\n", moduleName, goVersion)
	path := filepath.Join(output, "go.mod")
	return writeFileExclusive(path, []byte(content), 0o644)
}

// restructureModule copies a module into the standalone service's internal
// package and rewrites imports rooted at the original module path.
func restructureModule(name, modPath, moduleName, output string) error {
	srcBase := filepath.Join("internal", "modules", name)
	oldImportRoot := modPath + "/internal/modules/" + name
	newImportRoot := moduleName + "/internal"

	return filepath.Walk(srcBase, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 && strings.HasSuffix(path, ".go") {
			return fmt.Errorf("refuse to copy symlinked Go file %s", path)
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
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

		rewritten, err := rewriteModuleImports(path, content, oldImportRoot, newImportRoot)
		if err != nil {
			return err
		}
		return writeFileExclusive(dst, rewritten, 0o644)
	})
}

func rewriteModuleImports(path string, content []byte, oldRoot, newRoot string) ([]byte, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	changed := false
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("parse import in %s: %w", path, err)
		}
		if importPath != oldRoot && !strings.HasPrefix(importPath, oldRoot+"/") {
			continue
		}
		spec.Path.Value = strconv.Quote(newRoot + strings.TrimPrefix(importPath, oldRoot))
		changed = true
	}
	if !changed {
		return content, nil
	}

	var rewritten bytes.Buffer
	if err := format.Node(&rewritten, fileSet, file); err != nil {
		return nil, fmt.Errorf("format rewritten file %s: %w", path, err)
	}
	return rewritten.Bytes(), nil
}

// --- Templates ---

var decoupleCmdTmpl = `package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/charlesonunze/fw"
	{{- if eq .Transport "http" }}
	fwhttp "github.com/charlesonunze/fw/transport/http"
	{{- if eq .Router "chi" }}
	fwrouter "github.com/charlesonunze/fw/adapters/chi"
	"github.com/go-chi/chi/v5"
	{{- else }}
	fwrouter "github.com/charlesonunze/fw/adapters/gin"
	"github.com/gin-gonic/gin"
	{{- end }}
	{{- else }}
	fwgrpc "github.com/charlesonunze/fw/transport/grpc"
	{{- end }}
	{{ .Name }} "{{ .ModuleName }}/internal"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	{{- if eq .Transport "http" }}
	{{- if eq .Router "chi" }}
	router := chi.NewRouter()
	{{- else }}
	router := gin.New()
	{{- end }}
	httpTransport := fwhttp.New(fwrouter.NewRouter(router), fwhttp.Config{
		Addr: "{{ .Port }}",
	})
	app := fw.New(fw.Config{
		Transports: []fw.Transport{httpTransport},
	})
	{{- else }}
	grpcTransport := fwgrpc.New(fwgrpc.Config{Addr: "{{ .Port }}"})
	app := fw.New(fw.Config{
		Transports: []fw.Transport{grpcTransport},
	})
	{{- end }}
	module := {{ .Name }}.New()
	{{- if eq .Transport "http" }}
	var _ fwhttp.Module = module
	{{- else }}
	var _ fwgrpc.Module = module
	{{- end }}

	app.RegisterModules(
		module,
	)

	if err := app.Start(ctx); err != nil {
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
	{{- if .Methods }}
	"context"
	{{- end }}
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
	{{- if .Methods }}
	"context"
	{{- end }}
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

// New creates a GRPCClient connected with caller-provided transport credentials.
// GRPCClient owns the connection; callers must call Close when finished.
func New(addr string, transportCredentials credentials.TransportCredentials, options ...grpc.DialOption) (*GRPCClient, error) {
	if transportCredentials == nil {
		return nil, errors.New("gRPC transport credentials are required")
	}
	dialOptions := make([]grpc.DialOption, 0, len(options)+1)
	dialOptions = append(dialOptions, grpc.WithTransportCredentials(transportCredentials))
	dialOptions = append(dialOptions, options...)
	conn, err := grpc.NewClient(addr, dialOptions...)
	if err != nil {
		return nil, err
	}
	return &GRPCClient{conn: conn}, nil
}

// Close releases the client connection.
func (c *GRPCClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
{{ range .Methods }}
// {{ . }} TODO: implement gRPC call to the {{ $.DepName }} service.
func (c *GRPCClient) {{ . }}(ctx context.Context) error {
	panic("not implemented: GRPCClient.{{ . }}")
}
{{ end }}`
