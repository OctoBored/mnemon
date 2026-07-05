package coreguard

// Field-consumer registry guards (r4-registries §1). guard-1 is the compile
// layer and lives in the registry package itself (typed Fn references).
// Here: guard-2 (liveness — every registered consumer symbol has a call
// site in non-test code, so a "registered but dead" consumer goes red) and
// guard-3 (reverse — the policy decoders accept no rule-zone field outside
// the registry vocabulary, so removing a seat tightens the decoders).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/policy"
	"github.com/mnemon-dev/mnemon/harness/internal/registry"
)

func TestFieldRegistrySeatsAreClosedAndConsumed(t *testing.T) {
	if len(registry.Fields) != 11 {
		t.Fatalf("the rule-zone registry is a CLOSED 11-seat set, got %d", len(registry.Fields))
	}
	seen := map[string]bool{}
	for _, f := range registry.Fields {
		if seen[f.Name] {
			t.Errorf("duplicate seat %q", f.Name)
		}
		seen[f.Name] = true
		if len(f.Consumers) == 0 {
			t.Errorf("seat %q has no consumer — a seat without a real consumer cannot exist", f.Name)
		}
		for _, c := range f.Consumers {
			if c.Fn == nil || c.Package == "" || c.Func == "" {
				t.Errorf("seat %q: consumer must carry Fn + Package + Func (got %+v)", f.Name, c)
			}
		}
	}
}

// guard-2: every consumer Func is CALLED somewhere in non-test harness code.
func TestFieldRegistryConsumersAreAlive(t *testing.T) {
	calls := map[string]bool{}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				calls[fun.Name] = true
			case *ast.SelectorExpr:
				calls[fun.Sel.Name] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk harness: %v", err)
	}
	for _, f := range registry.Fields {
		for _, c := range f.Consumers {
			if !calls[c.Func] {
				t.Errorf("seat %q consumer %s.%s has no non-test call site (registered but dead)", f.Name, c.Package, c.Func)
			}
		}
	}
}

// guard-3: decoder rule-zone vocabulary ⊆ registry seats (+ subject aliases).
func TestPolicyDecodersStayInsideFieldRegistry(t *testing.T) {
	for kind, names := range policy.StandardRuleZoneFields() {
		for _, name := range names {
			if !registry.RuleZoneAllowed(name) {
				t.Errorf("%s decoder accepts rule-zone field %q outside the registry vocabulary; add a seat with a real consumer or remove the field", kind, name)
			}
		}
	}
}
