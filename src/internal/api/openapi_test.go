package api

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// routes reads the routes Handler registers from its source. ServeMux cannot list what it holds, so the source is the
// registry; every registration is one m.HandleFunc line, which this matches.
func routes(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`m\.HandleFunc\("([A-Z]+) (/api/[^"]+)", (s\.require\(store\.Role([A-Za-z]+)|s\.(requireExtension))?`)
	out := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		role := "none"
		if m[4] != "" {
			role = strings.ToLower(m[4])
		}
		if m[5] != "" {
			role = "extension"
		}
		out[m[1]+" "+m[2]] = role
	}
	if len(out) < 35 {
		t.Fatalf("read only %d routes from api.go; the pattern no longer matches the registrations", len(out))
	}
	return out
}

func TestOpenAPIMatchesTheRoutes(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			Role string `json:"x-role"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(openapiDoc, &doc); err != nil {
		t.Fatal(err)
	}
	documented := map[string]string{}
	for p, ops := range doc.Paths {
		for m, op := range ops {
			documented[strings.ToUpper(m)+" "+p] = op.Role
		}
	}
	var problems []string
	for r, role := range routes(t) {
		got, ok := documented[r]
		switch {
		case !ok:
			problems = append(problems, "not documented: "+r)
		case got != role:
			problems = append(problems, r+": documented role "+got+", registered "+role)
		}
		delete(documented, r)
	}
	for r := range documented {
		problems = append(problems, "documented but not registered: "+r)
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}

func TestOpenAPIIsPublic(t *testing.T) {
	w := do(newServer(t, "tok").Handler(), "GET", "/api/v1/openapi.json", "", host, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"openapi": "3.1.0"`) {
		t.Fatalf("%d", w.Code)
	}
}
