package fwhttp

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/charlesonunze/fw"
)

type wrongRouterModule struct{ testModule }

func (*wrongRouterModule) RegisterRoutes(*http.ServeMux) {}

func TestIncompatibleModuleFailsBeforeRegisteringRoutes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	router := newTestRouter()
	valid := &testModule{}
	transport := New(router, Config{Addr: addr})
	err = transport.Prepare(t.Context(), testDeps(valid, &wrongRouterModule{}))
	if err == nil || !strings.Contains(err.Error(), "incompatible RegisterRoutes") {
		t.Fatalf("Prepare() = %v", err)
	}
	if valid.registrations != 0 || len(router.routes) != 0 {
		t.Fatal("routes registered before signature validation")
	}
	listener, err = net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("startup failure retained listener: %v", err)
	}
	listener.Close()
	if err := transport.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterModulesRejectsNil(t *testing.T) {
	if err := RegisterModules([]fw.Module{nil}, newTestRouter()); err == nil {
		t.Fatal("accepted nil module")
	}
}
