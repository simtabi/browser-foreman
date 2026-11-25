package version

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Tests make their tokens, secrets and passwords when they run (crypto/rand), so no credential is written down for a
// secret scanner to report, and none is reused from one run to the next. This fails on a written-down one: a string
// given to a field or variable named like a credential, or a string that spells one out (a bearer token, NAME_TOKEN=,
// "secret":"...").
func TestTestsWriteDownNoCredentials(t *testing.T) {
	var files, tests []string
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		files = append(files, path)
		if strings.HasSuffix(path, "_test.go") && filepath.Base(path) != "credential_literals_test.go" {
			tests = append(tests, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	params := map[string][]int{}
	for _, path := range files {
		if err := credentialParams(path, nil, params); err != nil {
			t.Fatal(err)
		}
	}
	if len(params) < 3 {
		t.Fatalf("found only %d functions that take a credential; the search has lost its way", len(params))
	}
	seen, found := len(tests), []string{}
	for _, path := range tests {
		got, err := credentialLiterals(path, nil, params)
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, got...)
	}
	if seen < 100 {
		t.Fatalf("read only %d test files; the walk has lost its way", seen)
	}
	for _, f := range found {
		t.Errorf("a credential written down in a test (make it with rand.Text()): %s", f)
	}
}

// The check must be able to fail: each of these is found, and the same code with a made value is not.
func TestTheCredentialCheckFindsWhatItLooksFor(t *testing.T) {
	q := func(s string) string { return strconv.Quote(s) }
	planted := map[string]string{
		"field":      "x := Subscription{Secret: " + q("abc") + "}",
		"assignment": "c.APIToken = " + q("abc"),
		"comparison": "ok := c.APIToken != " + q("abc"),
		"bearer":     "h := " + q("Bear"+"er abc"),
		"env line":   "e := " + q("X_TO"+"KEN=abc\\n"),
		"json":       "b := `{" + q("pass"+"word") + ":" + q("abc") + "}`",
		"argument":   "s := serve(t, " + q("abc") + ")",
	}
	params := map[string][]int{}
	if err := credentialParams("decl.go", []byte("package p\nfunc serve(t int, token string) {}\n"), params); err != nil || len(params["serve"]) != 1 {
		t.Fatalf("%v %v", params, err)
	}
	for name, stmt := range planted {
		if got, _ := credentialLiterals("planted.go", []byte("package p\nfunc f() {\n"+stmt+"\n}\n"), params); len(got) != 1 {
			t.Errorf("%s: found %v in %s, want one", name, got, stmt)
		}
	}
	clean := "package p\nfunc f() {\nx := Subscription{Secret: rand.Text()}\nh := " + q("Bear"+"er ") + " + tok\ne := " + q("X_TO"+"KEN=\\n") + "\ns := serve(t, tok)\nu := serve(t+1, tok)\n}\n"
	if got, _ := credentialLiterals("clean.go", []byte(clean), params); len(got) != 0 {
		t.Errorf("found %v in made values", got)
	}
}

var (
	credentialName = regexp.MustCompile(`(?i)(token|secret|password|passwordhash|apikey)$`)
	credentialText = []*regexp.Regexp{
		regexp.MustCompile(`Bearer [A-Za-z0-9]`),
		regexp.MustCompile(`_(TOKEN|SECRET|KEY)=(\\")?[^\s\\"]`),
		regexp.MustCompile(`(?i)"(token|secret|password)"\s*:\s*"[^"]+"`),
	}
)

// parse reads a Go file: src, or the file at path when src is nil.
func parse(fset *token.FileSet, path string, src []byte) (*ast.File, error) {
	var from any // a nil []byte is an empty file to the parser, so the path is read only when src is nil
	if src != nil {
		from = src
	}
	return parser.ParseFile(fset, path, from, 0)
}

// credentialParams adds to params, by function name, the positions of the parameters named like a credential. Names
// are not told apart by package, so a clash can only report too much, never too little.
func credentialParams(path string, src []byte, params map[string][]int) error {
	f, err := parse(token.NewFileSet(), path, src)
	if err != nil {
		return err
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		i := 0
		for _, field := range fn.Type.Params.List {
			for _, n := range field.Names {
				if credentialName.MatchString(n.Name) {
					params[fn.Name.Name] = append(params[fn.Name.Name], i)
				}
				i++
			}
			if len(field.Names) == 0 {
				i++
			}
		}
	}
	return nil
}

// credentialLiterals lists the credentials written in a Go file (src, or the file at path when src is nil). params are
// the functions that take a credential, from credentialParams.
func credentialLiterals(path string, src []byte, params map[string][]int) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parse(fset, path, src)
	if err != nil {
		return nil, err
	}
	var found []string
	report := func(n ast.Node, why string) { found = append(found, fset.Position(n.Pos()).String()+": "+why) }
	literal := func(e ast.Expr) bool {
		b, ok := e.(*ast.BasicLit)
		return ok && b.Kind == token.STRING && len(b.Value) > 2
	}
	named := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.Ident:
			return credentialName.MatchString(x.Name)
		case *ast.SelectorExpr:
			return credentialName.MatchString(x.Sel.Name)
		}
		return false
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if named(x.Key) && literal(x.Value) {
				report(x, "a credential field given a written value")
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if i < len(x.Rhs) && named(l) && literal(x.Rhs[i]) {
					report(x, "a credential variable given a written value")
				}
			}
		case *ast.CallExpr:
			name := ""
			switch fn := x.Fun.(type) {
			case *ast.Ident:
				name = fn.Name
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			}
			for _, i := range params[name] {
				if i < len(x.Args) && literal(x.Args[i]) {
					report(x.Args[i], "a credential argument given a written value")
				}
			}
		case *ast.BinaryExpr:
			if (x.Op == token.EQL || x.Op == token.NEQ) && ((named(x.X) && literal(x.Y)) || (named(x.Y) && literal(x.X))) {
				report(x, "a credential compared with a written value")
			}
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				for _, re := range credentialText {
					if re.MatchString(x.Value) {
						report(x, "a string that spells out a credential")
						break
					}
				}
			}
		}
		return true
	})
	return found, nil
}
