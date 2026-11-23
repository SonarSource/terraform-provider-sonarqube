package client

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAPIInventory checks that APIS.md lists each exported Client method that
// sends a request, in the section of the file that declares the method, and
// that each function in APIS.md exists. See "Keep the API inventory current"
// in AGENTS.md.
func TestAPIInventory(t *testing.T) {
	methods := clientMethods(t)
	sending := sendingMethods(methods)
	listed := listedFunctions(t)

	for name, m := range methods {
		if !ast.IsExported(name) || !sending[name] {
			continue
		}
		file, ok := listed[name]
		switch {
		case !ok:
			t.Errorf("Client.%s in %s sends a request, but internal/client/APIS.md does not list it. Add a row to the section of %s.",
				name, m.file, m.file)
		case file != m.file:
			t.Errorf("APIS.md lists Client.%s in the section of %s, but %s declares it. Move the row.", name, file, m.file)
		}
	}
	for name, file := range listed {
		switch {
		case methods[name] == nil:
			t.Errorf("APIS.md lists %s in the section of %s, but Client has no such method. Remove or rename the row.", name, file)
		case !sending[name]:
			t.Errorf("APIS.md lists Client.%s, but it sends no request. Remove the row.", name)
		}
	}
}

type clientMethod struct {
	file  string
	calls []string // the Client methods that the body calls
}

// clientMethods parses the non-test files of the package and gives the
// methods of *Client.
func clientMethods(t *testing.T) map[string]*clientMethod {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]*clientMethod{}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				if receiver := clientReceiver(fn); receiver != "" {
					methods[fn.Name.Name] = &clientMethod{file: file, calls: callsOn(fn.Body, receiver)}
				}
			}
		}
	}
	return methods
}

// clientReceiver gives the name of the receiver when fn is a method of
// *Client.
func clientReceiver(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 {
		return ""
	}
	star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return ""
	}
	if ident, ok := star.X.(*ast.Ident); !ok || ident.Name != "Client" {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

// callsOn gives the methods that body calls on the receiver, also from a
// function literal in body.
func callsOn(body *ast.BlockStmt, receiver string) []string {
	var calls []string
	ast.Inspect(body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == receiver {
				calls = append(calls, sel.Sel.Name)
			}
		}
		return true
	})
	return calls
}

// sendingMethods gives the methods that reach send, the one method that makes
// an HTTP request.
func sendingMethods(methods map[string]*clientMethod) map[string]bool {
	sending := map[string]bool{"send": true}
	for changed := true; changed; {
		changed = false
		for name, m := range methods {
			if sending[name] {
				continue
			}
			for _, call := range m.calls {
				if sending[call] {
					sending[name] = true
					changed = true
					break
				}
			}
		}
	}
	return sending
}

var (
	sectionFile  = regexp.MustCompile("^## .*\\(`([a-z_]+\\.go)`\\)$")
	functionCell = regexp.MustCompile("`([A-Za-z0-9]+)`")
)

// listedFunctions gives, for each function in the first column of a table of
// APIS.md, the file of its section.
func listedFunctions(t *testing.T) map[string]string {
	t.Helper()
	content, err := os.ReadFile("APIS.md")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	file := ""
	for _, line := range strings.Split(string(content), "\n") {
		if match := sectionFile.FindStringSubmatch(line); match != nil {
			file = match[1]
			continue
		}
		if strings.HasPrefix(line, "## ") {
			file = ""
		}
		cells := strings.Split(line, "|")
		if file == "" || len(cells) < 3 || !strings.HasPrefix(line, "| `") {
			continue
		}
		for _, match := range functionCell.FindAllStringSubmatch(cells[1], -1) {
			listed[match[1]] = file
		}
	}
	return listed
}
