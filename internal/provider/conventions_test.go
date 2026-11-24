package provider

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// These tests check the rules of AGENTS.md that a test can check. Each error
// tells what to change.

type registered struct {
	kind     string // "Resource" or "Data source", as in PRODUCTS.md
	typeName string
	pkg      string // the last element of the Go package path
}

func registeredTypes(t *testing.T) []registered {
	t.Helper()
	ctx := context.Background()
	p := New("test")()

	var all []registered
	for _, factory := range p.Resources(ctx) {
		r := factory()
		metadata := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
		all = append(all, registered{"Resource", metadata.TypeName, packageOf(r)})
	}
	for _, factory := range p.DataSources(ctx) {
		d := factory()
		metadata := &datasource.MetadataResponse{}
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
		all = append(all, registered{"Data source", metadata.TypeName, packageOf(d)})
	}
	return all
}

func packageOf(v any) string {
	typ := reflect.TypeOf(v)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return path.Base(typ.PkgPath())
}

// productOfName gives the package and the PRODUCTS.md value that the name of
// a type requires.
func productOfName(typeName string) (pkg, products string) {
	switch {
	case strings.HasPrefix(typeName, "sonarqube_cloud_"):
		return "cloud", "SonarQube Cloud"
	case strings.HasPrefix(typeName, "sonarqube_server_"):
		return "server", "SonarQube Server"
	default:
		return "shared", "Shared"
	}
}

func TestTypeNamesMatchTheirPackage(t *testing.T) {
	for _, r := range registeredTypes(t) {
		want, _ := productOfName(r.typeName)
		if r.pkg != want {
			t.Errorf("%s %s is in package %s. Its name requires package %s. See \"Names\" in AGENTS.md.",
				r.kind, r.typeName, r.pkg, want)
		}
	}
}

var productsRow = regexp.MustCompile("^\\| `([a-z0-9_]+)` \\| ([^|]+) \\|")

// readProductsTables gives, for each table of PRODUCTS.md ("Resource" or
// "Data source"), the Products column of each type name.
func readProductsTables(t *testing.T) map[string]map[string]string {
	t.Helper()
	content, err := os.ReadFile("PRODUCTS.md")
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string]map[string]string{"Resource": {}, "Data source": {}}
	var current map[string]string
	for _, line := range strings.Split(string(content), "\n") {
		switch {
		case strings.HasPrefix(line, "| Resource |"):
			current = tables["Resource"]
		case strings.HasPrefix(line, "| Data source |"):
			current = tables["Data source"]
		case strings.HasPrefix(line, "#"):
			current = nil
		}
		if match := productsRow.FindStringSubmatch(line); match != nil && current != nil {
			current[match[1]] = strings.TrimSpace(match[2])
		}
	}
	return tables
}

func TestProductsInventory(t *testing.T) {
	tables := readProductsTables(t)
	seen := map[string]map[string]bool{"Resource": {}, "Data source": {}}

	for _, r := range registeredTypes(t) {
		seen[r.kind][r.typeName] = true
		products, ok := tables[r.kind][r.typeName]
		if !ok {
			t.Errorf("%s %s has no row in internal/provider/PRODUCTS.md. Add the decision and its reason. See \"Before you add a resource or data source\" in AGENTS.md.",
				r.kind, r.typeName)
			continue
		}
		if _, want := productOfName(r.typeName); products != want {
			t.Errorf("PRODUCTS.md gives %q for %s %s. Its name requires %q.", products, r.kind, r.typeName, want)
		}
	}
	for kind, rows := range tables {
		for typeName := range rows {
			if !seen[kind][typeName] {
				t.Errorf("PRODUCTS.md has a %s row for %s, but the provider does not register it. Remove the row or register the %s.",
					strings.ToLower(kind), typeName, strings.ToLower(kind))
			}
		}
	}
}

// Each resource needs an acceptance test with an import step. A Config step
// of terraform-plugin-testing already fails when the plan after the apply is
// not empty, so the test needs no separate check of a second apply.
func TestEachTypeHasAnAcceptanceTest(t *testing.T) {
	packages := map[string]acceptanceTests{}
	for _, r := range registeredTypes(t) {
		tests, ok := packages[r.pkg]
		if !ok {
			tests = readAcceptanceTests(t, r.pkg)
			packages[r.pkg] = tests
		}
		used := false
		for _, source := range tests.sources {
			if strings.Contains(source, r.typeName+`"`) || strings.Contains(source, r.typeName+".") {
				used = true
				break
			}
		}
		switch {
		case !used:
			t.Errorf("%s %s is in no *_acc_test.go file of internal/provider/%s. Add an acceptance test.",
				r.kind, r.typeName, r.pkg)
		case r.kind == "Resource" && !tests.imported[r.typeName]:
			t.Errorf("Resource %s has no ImportState step whose ResourceName is a %s address in internal/provider/%s. Add an import step.",
				r.typeName, r.typeName, r.pkg)
		}
	}
}

type acceptanceTests struct {
	sources  []string
	imported map[string]bool // the resource types of the ImportState steps
}

// readAcceptanceTests reads the *_acc_test.go files of a package. It finds the
// type of each ImportState step from its ResourceName: a string literal, or a
// name that the package assigns a string literal to.
func readAcceptanceTests(t *testing.T, pkg string) acceptanceTests {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(pkg, "*_acc_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	tests := acceptanceTests{imported: map[string]bool{}}
	fset := token.NewFileSet()
	var parsed []*ast.File
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		tests.sources = append(tests.sources, string(content))
		f, err := parser.ParseFile(fset, file, content, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, f)
	}

	values := stringValues(parsed)
	for _, f := range parsed {
		ast.Inspect(f, func(n ast.Node) bool {
			for _, address := range importedAddresses(n, values) {
				tests.imported[strings.SplitN(address, ".", 2)[0]] = true
			}
			return true
		})
	}
	return tests
}

// stringValues gives the string literals that the files assign to each name.
func stringValues(files []*ast.File) map[string][]string {
	values := map[string][]string{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			addAssignedStrings(values, n)
			return true
		})
	}
	return values
}

func addAssignedStrings(values map[string][]string, n ast.Node) {
	switch n := n.(type) {
	case *ast.ValueSpec:
		for i := range min(len(n.Names), len(n.Values)) {
			addStringValue(values, n.Names[i].Name, n.Values[i])
		}
	case *ast.AssignStmt:
		for i := range min(len(n.Lhs), len(n.Rhs)) {
			if ident, ok := n.Lhs[i].(*ast.Ident); ok {
				addStringValue(values, ident.Name, n.Rhs[i])
			}
		}
	}
}

// importedAddresses gives the possible values of ResourceName when n is a
// test step with ImportState: true.
func importedAddresses(n ast.Node, values map[string][]string) []string {
	step, ok := n.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	fields := map[string]ast.Expr{}
	for _, elt := range step.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok {
				fields[key.Name] = kv.Value
			}
		}
	}
	if importState, ok := fields["ImportState"].(*ast.Ident); !ok || importState.Name != "true" {
		return nil
	}
	switch name := fields["ResourceName"].(type) {
	case *ast.BasicLit:
		return []string{stringLiteral(name)}
	case *ast.Ident:
		return values[name.Name]
	}
	return nil
}

func addStringValue(values map[string][]string, name string, expr ast.Expr) {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		values[name] = append(values[name], stringLiteral(lit))
	}
}

func stringLiteral(lit *ast.BasicLit) string {
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}
