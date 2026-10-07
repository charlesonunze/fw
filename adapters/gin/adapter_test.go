package gin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/charlesonunze/fw"
	gingonic "github.com/gin-gonic/gin"
)

type nativeModule struct {
	routes func(gingonic.IRouter)
}

func (*nativeModule) Name() fw.ModuleName                  { return "users" }
func (*nativeModule) Imports() []fw.ModuleName             { return nil }
func (*nativeModule) Register(*fw.Deps) error              { return nil }
func (*nativeModule) Init(context.Context, *fw.Deps) error { return nil }
func (*nativeModule) Health(context.Context) error         { return nil }
func (*nativeModule) Close() error                         { return nil }
func (m *nativeModule) RegisterRoutes(r gingonic.IRouter)  { m.routes(r) }

func TestNativeRoutesMiddlewareAndBinding(t *testing.T) {
	gingonic.SetMode(gingonic.TestMode)
	for _, tc := range []struct {
		name    string
		blocked bool
	}{{name: "allowed"}, {name: "blocked", blocked: true}} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gingonic.New()
			var events []string
			engine.Use(func(c *gingonic.Context) {
				events = append(events, "root before")
				c.Next()
				events = append(events, "root after")
			})
			module := &nativeModule{routes: func(r gingonic.IRouter) {
				group := r.Group("/teams/:team", func(c *gingonic.Context) {
					events = append(events, "group before")
					if c.Param("team") != "core" || c.Param("id") != "42" {
						t.Errorf("native middleware parameters = %v", c.Params)
					}
					if tc.blocked {
						c.AbortWithStatus(http.StatusUnauthorized)
						return
					}
					c.Next()
					events = append(events, "group after")
				})
				group.POST("/users/:id", func(c *gingonic.Context) {
					events = append(events, "handler")
					var body struct {
						Name string `json:"name" binding:"required"`
					}
					if err := c.ShouldBindJSON(&body); err != nil {
						t.Fatal(err)
					}
					c.JSON(http.StatusCreated, gingonic.H{"id": c.Param("id"), "name": body.Name})
				})
			}}
			adapter := NewAdapter(engine)
			if err := adapter.RegisterModules([]fw.Module{module}); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/teams/core/users/42", strings.NewReader(`{"name":"Ada"}`))
			request.Header.Set("Content-Type", "application/json")
			adapter.ServeHTTP(response, request)
			wantStatus := http.StatusCreated
			wantEvents := []string{"root before", "group before", "handler", "group after", "root after"}
			if tc.blocked {
				wantStatus = http.StatusUnauthorized
				wantEvents = []string{"root before", "group before", "root after"}
			} else if response.Body.String() != `{"id":"42","name":"Ada"}` {
				t.Errorf("body = %s", response.Body)
			}
			if response.Code != wantStatus || !reflect.DeepEqual(events, wantEvents) {
				t.Fatalf("response = %d, events = %v; want %d, %v", response.Code, events, wantStatus, wantEvents)
			}
		})
	}
}

type foreignModule struct{ nativeModule }

func (*foreignModule) RegisterRoutes(*http.ServeMux) {}

type backgroundModule struct{ fw.Module }

func TestRegisterModulesRejectsMismatchBeforeMutatingEngine(t *testing.T) {
	gingonic.SetMode(gingonic.TestMode)
	engine := gingonic.New()
	valid := &nativeModule{routes: func(gingonic.IRouter) { t.Fatal("registered before validation finished") }}
	err := NewAdapter(engine).RegisterModules([]fw.Module{valid, &foreignModule{}})
	if err == nil || !strings.Contains(err.Error(), "users") || !strings.Contains(err.Error(), "gin.IRouter") {
		t.Fatalf("RegisterModules() = %v, want named router mismatch", err)
	}
	if len(engine.Routes()) != 0 {
		t.Fatal("mismatch mutated routes")
	}
	if err := NewAdapter(engine).RegisterModules([]fw.Module{&backgroundModule{}}); err != nil {
		t.Fatalf("background module: %v", err)
	}
}

func TestRegisterHealth(t *testing.T) {
	gingonic.SetMode(gingonic.TestMode)
	adapter := NewAdapter(gingonic.New())
	adapter.RegisterHealth(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
	)
	for path, want := range map[string]int{"/health/live": 200, "/health/ready": 503} {
		response := httptest.NewRecorder()
		adapter.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want {
			t.Errorf("%s = %d, want %d", path, response.Code, want)
		}
	}
}

func TestRegisterModulesRequiresEngine(t *testing.T) {
	for _, adapter := range []*Adapter{nil, {}, NewAdapter(nil)} {
		if err := adapter.RegisterModules(nil); err == nil {
			t.Fatal("missing engine accepted")
		}
	}
}

var _ Module = (*nativeModule)(nil)
