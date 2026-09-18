package fw

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type graphModule struct {
	name    ModuleName
	imports []ModuleName
}

func (m *graphModule) Name() ModuleName                { return m.name }
func (m *graphModule) Imports() []ModuleName           { return m.imports }
func (*graphModule) Register(*Deps) error              { return nil }
func (*graphModule) Init(context.Context, *Deps) error { return nil }
func (*graphModule) Health(context.Context) error      { return nil }
func (*graphModule) Close() error                      { return nil }

func TestOrderModulesIsDeterministic(t *testing.T) {
	user := &graphModule{name: "user"}
	auth := &graphModule{name: "auth", imports: []ModuleName{"user"}}
	todo := &graphModule{name: "todo", imports: []ModuleName{"auth"}}
	notification := &graphModule{name: "notification"}

	ordered, imports, err := orderModules([]Module{todo, user, notification, auth})
	if err != nil {
		t.Fatalf("orderModules() error = %v", err)
	}

	names := make([]ModuleName, len(ordered))
	for index, module := range ordered {
		names[index] = module.Name()
	}
	want := []ModuleName{"user", "auth", "notification", "todo"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("orderModules() names = %v, want %v", names, want)
	}
	if !reflect.DeepEqual(imports["auth"], []ModuleName{"user"}) {
		t.Fatalf("orderModules() auth imports = %v, want [user]", imports["auth"])
	}
}

func TestOrderModulesRejectsInvalidGraphs(t *testing.T) {
	var nilModule *graphModule
	tests := []struct {
		name    string
		modules []Module
		want    string
	}{
		{name: "nil", modules: []Module{nilModule}, want: "module 0 is nil"},
		{name: "empty name", modules: []Module{&graphModule{}}, want: "module 0 has an empty name"},
		{
			name:    "duplicate name",
			modules: []Module{&graphModule{name: "user"}, &graphModule{name: "user"}},
			want:    `module name "user" is registered more than once`,
		},
		{
			name:    "missing import",
			modules: []Module{&graphModule{name: "auth", imports: []ModuleName{"user"}}},
			want:    `module "auth" imports unregistered module "user"`,
		},
		{
			name: "invalid imports are reported deterministically",
			modules: []Module{
				&graphModule{name: "zeta", imports: []ModuleName{"missing-zeta"}},
				&graphModule{name: "alpha", imports: []ModuleName{"missing-alpha"}},
			},
			want: `module "alpha" imports unregistered module "missing-alpha"`,
		},
		{
			name: "duplicate import",
			modules: []Module{
				&graphModule{name: "auth", imports: []ModuleName{"user", "user"}},
				&graphModule{name: "user"},
			},
			want: `module "auth" imports module "user" more than once`,
		},
		{
			name: "cycle",
			modules: []Module{
				&graphModule{name: "auth", imports: []ModuleName{"user"}},
				&graphModule{name: "user", imports: []ModuleName{"auth"}},
			},
			want: "module dependency cycle: auth -> user -> auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := orderModules(tt.modules)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("orderModules() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
