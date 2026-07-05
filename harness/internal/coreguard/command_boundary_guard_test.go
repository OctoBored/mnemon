package coreguard

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type commandImportBoundary struct {
	dir string
	// extraDirs are harness-relative package dirs that carry the same
	// boundary — the R4 S2 absorption moved each binary's logic into an
	// internal CLI package, and the guard follows the code, not the shim.
	extraDirs []string
	forbids   []string
	rationale string
}

var commandImportBoundaries = []commandImportBoundary{
	{
		dir:       "mnemond",
		extraDirs: []string{"internal/nodecli"},
		forbids: []string{
			"harness/internal/mnemonhub",
			"harness/internal/productconfig",
			"harness/internal/surface/",
			"harness/cmd/",
		},
		rationale: "mnemond is the local event node, not a remote exchange backend or external display surface",
	},
	{
		dir:       "mnemon-hub",
		extraDirs: []string{"internal/mnemonhub/hubcli"},
		forbids: []string{
			"harness/internal/activationtrace",
			"harness/internal/app",
			"harness/internal/codexapp",
			"harness/internal/daemon",
			"harness/internal/drive",
			"harness/internal/driver",
			"harness/internal/hostagent",
			"harness/internal/mnemond/access",
			"harness/internal/mnemond/admission",
			"harness/internal/mnemond/presentation",
			"harness/internal/productconfig",
			"harness/internal/runtime",
			"harness/internal/surface/",
			"harness/cmd/",
		},
		rationale: "mnemon-hub is the remote event exchange backend and must not grow local runtime, hostagent, or projection behavior",
	},
	{
		dir: "mnemon-multica",
		forbids: []string{
			"harness/internal/app",
			"harness/internal/codexapp",
			"harness/internal/eventstore",
			"harness/internal/hostagent",
			"harness/internal/mnemond/admission",
			"harness/internal/mnemond/state",
			"harness/internal/mnemonhub",
			"harness/internal/productconfig",
			"harness/internal/runtime",
			"harness/cmd/",
		},
		rationale: "mnemon-multica is an external adapter; it may call mnemond access/render APIs but must not own local state, hub exchange, or product config",
	},
}

func TestCommandImportBoundaryGuardLogicIsNotVacuous(t *testing.T) {
	if !commandImportForbidden("github.com/mnemon-dev/mnemon/harness/internal/mnemonhub", commandImportBoundaries[0].forbids) {
		t.Fatal("mnemond command boundary must flag mnemonhub imports")
	}
	if !commandImportForbidden("github.com/mnemon-dev/mnemon/harness/internal/app", commandImportBoundaries[1].forbids) {
		t.Fatal("mnemon-hub command boundary must flag app imports")
	}
	if !commandImportForbidden("github.com/mnemon-dev/mnemon/harness/internal/mnemond/access", commandImportBoundaries[1].forbids) {
		t.Fatal("mnemon-hub command boundary must flag local mnemond access imports")
	}
	if commandImportForbidden("github.com/mnemon-dev/mnemon/harness/internal/mnemond/access", commandImportBoundaries[2].forbids) {
		t.Fatal("mnemon-multica must be allowed to call mnemond access APIs")
	}
}

func TestServiceCommandsKeepR2Boundaries(t *testing.T) {
	for _, boundary := range commandImportBoundaries {
		assertCommandAvoidsForbiddenImports(t, boundary)
	}
}

func assertCommandAvoidsForbiddenImports(t *testing.T, boundary commandImportBoundary) {
	t.Helper()
	roots := []string{filepath.Join("..", "..", "cmd", boundary.dir)}
	for _, extra := range boundary.extraDirs {
		roots = append(roots, filepath.Join("..", "..", filepath.FromSlash(extra)))
	}
	for _, root := range roots {
		assertDirAvoidsForbiddenImports(t, root, boundary)
	}
}

func assertDirAvoidsForbiddenImports(t *testing.T, root string, boundary commandImportBoundary) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if commandImportForbidden(importPath, boundary.forbids) {
				t.Errorf("%s imports forbidden package %q: %s", path, importPath, boundary.rationale)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func commandImportForbidden(path string, forbids []string) bool {
	for _, forbidden := range forbids {
		if strings.Contains(path, forbidden) {
			return true
		}
	}
	return false
}
