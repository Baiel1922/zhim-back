package http

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
)

// The role policy is keyed by operation name; a typo would silently make an
// operation owner-only (or, for the public list, leave it protected).
func TestRolePolicyNamesExist(t *testing.T) {
	iface := reflect.TypeFor[oapi.StrictServerInterface]()
	for _, list := range []map[string]bool{managerOperations, publicAdminOperations} {
		for name := range list {
			if _, ok := iface.MethodByName(name); !ok {
				t.Errorf("%s is not an API operation", name)
			}
		}
	}
	var admin, managerOnly int
	for i := range iface.NumMethod() {
		name := iface.Method(i).Name
		if strings.HasPrefix(name, "Admin") {
			admin++
			if managerOperations[name] {
				managerOnly++
			}
		}
	}
	if admin < 40 || managerOnly != len(managerOperations) {
		t.Fatalf("admin operations = %d, manager = %d", admin, managerOnly)
	}
}

func TestBearerToken(t *testing.T) {
	for header, want := range map[string]string{
		"Bearer abc":   "abc",
		"bearer  abc ": "abc",
		"Basic abc":    "",
		"Bearer":       "",
		"Bearer ":      "",
		"":             "",
	} {
		got, ok := bearerToken(header)
		if got != want || ok != (want != "") {
			t.Errorf("bearerToken(%q) = %q, %v", header, got, ok)
		}
	}
}
