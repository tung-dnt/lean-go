package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// module is the import path of this repository.
const module = "github.com/tung-dnt/lean-go"

// domains are the business domains: each is THREE packages, its business root internal/<domain> and the flat
// internal/<domain>/domain and internal/<domain>/adapters, and the other domains it may import. A domain that builds
// on another (a search over a catalog, say) lists it here. This is the table of LEANGO.md's "Architecture": change
// both together.
var domains = map[string][]string{
	"auth": nil,
	"note": nil,
}

// cmdAllowed are the edges from a command to a package of a domain, the only ones: admin hashes the password it
// prints with the rules of auth, and reaches neither its business root nor its adapters.
var cmdAllowed = map[string][]string{
	"cmd/admin": {"internal/auth/domain"},
}

// cmdRoots are the packages a command may import: the server, the shared code and the data layer.
var cmdRoots = []string{"internal/server", "pkg", "internal/external"}

// bannedLibraries are the libraries the repository replaced with the standard library, by import path prefix.
var bannedLibraries = []string{
	"github.com/labstack/echo",
	"github.com/gorilla/",
	"github.com/go-playground/validator",
	"github.com/go-chi/",
}

// sdkPrefixes are the third-party SDKs that only internal/external may import: the rest of the code sees the adapter.
var sdkPrefixes = []string{"github.com/aws/aws-sdk-go-v2"}

// The three packages of a domain: its business root, internal/<domain>, and the flat packages below it.
const (
	rootLayer     = "root"
	domainLayer   = "domain"
	adaptersLayer = "adapters"
)

// presentationImports are what only the adapters of a domain use, by import path (of this module relative to its
// root, of any other as it is written): the web (net/http, the typed paths), the pages (gomponents, internal/ui)
// and the helpers that speak to the visitor (the session, a flash message, a redirect, an HTTP error, htmx, the
// guards). The business root and the rules take plain parameters and answer with values and errors, so they import
// none of them. pkg/form is not one: its Decode and Submission serve the adapters, and its sentences and IsEmail
// are what the rules of a domain check with.
var presentationImports = []string{
	"net/http",
	"maragu.dev/gomponents",
	"internal/server/paths",
	"internal/ui",
	"pkg/htmx",
	"pkg/httperr",
	"pkg/middleware",
	"pkg/msg",
	"pkg/redirect",
	"pkg/session",
}

// retiredFileRoles are the file names that no package of a domain has any more. The business is <concern>.go and
// calls the generated sqlc queries directly, the rules are in domain/ and the handlers in adapters/.
var retiredFileRoles = []string{"*_api.go", "*_query.go", "*_store.go", "*_policy.go"}

// TestArchitecture__EveryImportFollowsTheEdges reads the imports of every package of the repository, its tests
// excepted, and holds them to the rules of the layout:
//
//   - every internal/<domain> is its business root plus the flat packages internal/<domain>/domain and
//     internal/<domain>/adapters: it has no other sub-package (testdata excepted).
//   - the three packages keep to their layers (layerForbidden): only the adapters import the web, the pages and the
//     session; the rules import neither JSON nor the database; no business root or rules import adapters, and the
//     rules import only rules.
//   - a domain imports no other domain, except along the edges listed in domains, whichever its layer.
//   - internal/server/paths is the one package under internal/server that a domain or internal/ui may import, and it
//     imports nothing of this module.
//   - pkg and internal/external import no domain, no internal/server and no internal/ui.
//   - internal/ui imports no domain and nothing of internal/server but its paths.
//   - only internal/server imports the domains, and only cmd/* imports internal/server; cmd/* imports only
//     internal/server, pkg and internal/external (plus the edges of cmdAllowed).
//   - no package imports echo, gorilla, go-playground/validator or chi.
//   - the third-party SDKs (aws-sdk-go-v2) are imported only under internal/external.
func TestArchitecture__EveryImportFollowsTheEdges(t *testing.T) {
	imports := goImports(t, "..")
	require.NotEmpty(t, imports["internal/server"], "the walk found the packages")

	for pkg, deps := range imports {
		if why := misplaced(pkg); why != "" {
			t.Errorf("%s: %s", pkg, why)
		}
		for _, dep := range deps {
			if why := forbidden(pkg, dep); why != "" {
				t.Errorf("%s imports %s: %s", pkg, dep, why)
			}
		}
	}

	for name := range domains {
		assert.Contains(t, imports, "internal/"+name, "domain %s is a package", name)
	}
}

// TestArchitecture__ADomainSplitsBusinessFromPresentation holds the files of every domain to the split:
//
//   - no package of a domain has a file named for a role the split retired (retiredFileRoles).
//   - no type of a domain is named Store, Query or Queries, or for one: it is named for the business concept.
func TestArchitecture__ADomainSplitsBusinessFromPresentation(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(domains)) {
		for _, layer := range []string{".", domainLayer, adaptersLayer} {
			dir := filepath.Join(name, layer)
			for _, role := range retiredFileRoles {
				found, err := filepath.Glob(filepath.Join(dir, role))
				require.NoError(t, err)
				assert.Empty(t, found, "domain %s: the business is <concern>.go calling sqlc directly, the rules are in domain/, the handlers in adapters/", name)
			}

			files, err := filepath.Glob(filepath.Join(dir, "*.go"))
			require.NoError(t, err)
			for _, file := range files {
				if strings.HasSuffix(file, "_test.go") {
					continue
				}
				for _, typeName := range typeNames(t, file) {
					for _, suffix := range []string{"Store", "Query", "Queries"} {
						assert.False(t, strings.HasSuffix(typeName, suffix), "%s: type %s names a store or query layer; name the business concept", file, typeName)
					}
				}
			}
		}
	}
}

// TestArchitecture__DomainEdgesAreJudged holds the rules of the layers to what they say, on packages that need not
// exist, so that the walk of the imports cannot pass because a rule judges nothing.
func TestArchitecture__DomainEdgesAreJudged(t *testing.T) {
	t.Run("what the adapters may import", func(t *testing.T) {
		for _, dep := range []string{
			"net/http", "maragu.dev/gomponents/html", "encoding/json/v2",
			module + "/internal/note", module + "/internal/note/domain", module + "/internal/external/db",
			module + "/internal/server/paths", module + "/internal/ui/layouts",
			module + "/pkg/htmx", module + "/pkg/httperr", module + "/pkg/middleware", module + "/pkg/msg",
			module + "/pkg/redirect", module + "/pkg/session",
		} {
			assert.Empty(t, layerForbidden("internal/note/adapters", dep), "adapters may import %s", dep)
		}
	})

	t.Run("what the business root may import", func(t *testing.T) {
		for _, dep := range []string{
			"encoding/json/v2", module + "/internal/note/domain", module + "/internal/external/db",
			module + "/pkg/pager", module + "/pkg/workflow",
		} {
			assert.Empty(t, layerForbidden("internal/note", dep), "the root may import %s", dep)
		}
		for _, dep := range []string{
			"net/http", "maragu.dev/gomponents/html", module + "/internal/note/adapters",
			module + "/internal/server/paths", module + "/internal/ui/layouts",
			module + "/pkg/htmx", module + "/pkg/httperr", module + "/pkg/middleware", module + "/pkg/msg",
			module + "/pkg/redirect", module + "/pkg/session",
		} {
			assert.NotEmpty(t, layerForbidden("internal/note", dep), "the root may not import %s", dep)
		}
	})

	t.Run("what the domain rules may import", func(t *testing.T) {
		for _, dep := range []string{"strings", module + "/pkg/form", module + "/internal/note/domain"} {
			assert.Empty(t, layerForbidden("internal/note/domain", dep), "the rules may import %s", dep)
		}
		for _, dep := range []string{
			"net/http", "maragu.dev/gomponents", "encoding/json/v2", module + "/internal/external/db",
			module + "/internal/note", module + "/internal/note/adapters", module + "/pkg/session",
		} {
			assert.NotEmpty(t, layerForbidden("internal/note/domain", dep), "the rules may not import %s", dep)
		}
	})

	t.Run("what a layer may import of another domain", func(t *testing.T) {
		assert.Empty(t, layerForbidden("internal/note/domain", module+"/internal/auth/domain"), "rules import rules of another domain")
		assert.NotEmpty(t, layerForbidden("internal/note/domain", module+"/internal/auth"), "and nothing else of it")
		assert.Empty(t, layerForbidden("internal/note", module+"/internal/auth"), "the root imports the root of another domain")
		assert.Empty(t, layerForbidden("internal/note", module+"/internal/auth/domain"), "and its rules")
		assert.NotEmpty(t, layerForbidden("internal/note", module+"/internal/auth/adapters"), "but not its adapters")
		assert.Empty(t, layerForbidden("internal/note/adapters", module+"/internal/auth/adapters"), "adapters import anything")
		assert.NotEmpty(t, forbidden("internal/note/adapters", module+"/internal/auth/adapters"), "that the edges of domains allow: note lists none")
	})

	t.Run("where a package may be", func(t *testing.T) {
		for _, pkg := range []string{"internal/note", "internal/note/domain", "internal/note/adapters", "internal/server/paths"} {
			assert.Empty(t, misplaced(pkg), pkg)
		}
		for _, pkg := range []string{"internal/note/services", "internal/note/adapters/nested", "internal/note/domain/nested", "internal/other"} {
			assert.NotEmpty(t, misplaced(pkg), pkg)
		}
	})

	t.Run("what a command may import of a domain", func(t *testing.T) {
		assert.Empty(t, cmdForbidden("cmd/admin", "internal/auth/domain"), "admin hashes with the rules of auth")
		assert.NotEmpty(t, cmdForbidden("cmd/admin", "internal/auth"), "and does not reach its business root")
		assert.NotEmpty(t, cmdForbidden("cmd/admin", "internal/auth/adapters"), "or its adapters")
		assert.NotEmpty(t, cmdForbidden("cmd/web", "internal/auth/domain"), "another command has no such edge")
	})
}

// typeNames are the names of the types that a Go file declares at its top level.
func typeNames(t *testing.T, file string) []string {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	var names []string
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			names = append(names, spec.(*ast.TypeSpec).Name.Name)
		}
	}

	return names
}

// misplaced says why a package may not be where it is, or "" if it may: everything under internal/ is one of
// internal/{server,ui,pkg,external} or a domain, which is its business root plus the flat domain/ and adapters/.
func misplaced(pkg string) string {
	rest, ok := strings.CutPrefix(pkg, "internal/")
	if !ok {
		return ""
	}
	top, sub, _ := strings.Cut(rest, "/")
	if _, ok := domains[top]; ok {
		switch sub {
		case "", domainLayer, adaptersLayer:
			return ""
		}
		return "a domain is its business root plus the flat domain/ and adapters/ packages: no other sub-package"
	}
	switch top {
	case "server", "ui", "pkg", "external":
		return ""
	}

	return "a package under internal/ is internal/server, internal/ui, pkg, internal/external or a domain listed in architecture_test.go"
}

// forbidden says why pkg may not import dep, both as import paths (relative to the module root for this module's
// own code), or "" if it may.
func forbidden(pkg, dep string) string {
	if why := layerForbidden(pkg, dep); why != "" {
		return why
	}
	for _, lib := range bannedLibraries {
		if strings.HasPrefix(dep, lib) {
			return "the standard library replaced " + lib
		}
	}
	for _, sdk := range sdkPrefixes {
		if strings.HasPrefix(dep, sdk) && !isUnder(pkg, "internal/external") {
			return "a third-party SDK is imported only under internal/external"
		}
	}
	if !strings.HasPrefix(dep, module+"/") {
		return ""
	}
	dep = strings.TrimPrefix(dep, module+"/")

	switch {
	case strings.HasPrefix(pkg, "cmd/"):
		return cmdForbidden(pkg, dep)
	case isUnder(pkg, "pkg") || isUnder(pkg, "internal/external"):
		if domainOf(dep) != "" || isUnder(dep, "internal/server") || isUnder(dep, "internal/ui") {
			return "pkg and internal/external import no domain, no internal/server and no internal/ui"
		}
	case isUnder(pkg, "internal/ui"):
		if domainOf(dep) != "" || isServer(dep) {
			return "internal/ui imports no domain and nothing of internal/server but its paths"
		}
	case pkg == pathsPkg:
		return "internal/server/paths is a leaf: it imports nothing of this module, so that everything may import it"
	case isUnder(pkg, "internal/server"):
		return ""
	case domainOf(pkg) != "":
		if isServer(dep) {
			return "a domain imports nothing of internal/server but its paths"
		}
		if other := domainOf(dep); other != "" && other != domainOf(pkg) && !slices.Contains(domains[domainOf(pkg)], other) {
			return "a domain imports no other domain, except along the edges listed in domains (architecture_test.go)"
		}
	default:
		if domainOf(dep) != "" {
			return "only internal/server imports the domains"
		}
	}

	return ""
}

// cmdForbidden says why a command may not import dep, or "" if it may.
func cmdForbidden(pkg, dep string) string {
	if slices.Contains(cmdAllowed[pkg], dep) {
		return ""
	}
	for _, root := range cmdRoots {
		if isUnder(dep, root) {
			return ""
		}
	}

	return "cmd/* imports only internal/server, pkg and internal/external"
}

// layerOf is the layer of a package of a domain: rootLayer for internal/<domain>, and the name of the sub-package
// for the ones below it (domainLayer, adaptersLayer, or what misplaced rejects). It is "" for a package outside
// the domains.
func layerOf(pkg string) string {
	if domainOf(pkg) == "" {
		return ""
	}
	_, sub, _ := strings.Cut(strings.TrimPrefix(pkg, "internal/"), "/")
	if sub == "" {
		return rootLayer
	}

	return sub
}

// layerForbidden says why pkg may not import dep because of the layer it is in, or "" if it may: the adapters are
// held to nothing here, the business root and the rules import no presentation, nor any adapters, and the rules
// are pure besides (no JSON, no database, only the rules of a domain). It does not know the edges of domains, which
// forbidden adds.
func layerForbidden(pkg, dep string) string {
	layer := layerOf(pkg)
	if layer == "" || layer == adaptersLayer {
		return ""
	}
	local := strings.TrimPrefix(dep, module+"/")
	for _, presentation := range presentationImports {
		if isUnder(local, presentation) {
			return "only the adapters of a domain speak to the web and draw its pages: the business root and the domain rules import no " + presentation
		}
	}
	if layerOf(local) == adaptersLayer {
		return "the business root and the domain rules never import adapters"
	}
	if layer != domainLayer {
		return ""
	}
	switch {
	case isUnder(local, "encoding/json"):
		return "domain rules are pure: JSON belongs to the adapters or the business root"
	case isUnder(local, "internal/external"):
		return "domain rules are pure: the business root calls the database"
	case layerOf(local) == rootLayer:
		return "domain rules import only domain rules"
	}

	return ""
}

// pathsPkg is the one package under internal/server that the domains and internal/ui may import: the typed paths of
// the routes, kept beside routes.go. It imports nothing of this module, so importing it makes no cycle.
const pathsPkg = "internal/server/paths"

// isServer reports whether pkg is internal/server or below it, its paths excepted.
func isServer(pkg string) bool {
	return isUnder(pkg, "internal/server") && pkg != pathsPkg
}

// isUnder reports whether pkg is root or inside it.
func isUnder(pkg, root string) bool {
	return pkg == root || strings.HasPrefix(pkg, root+"/")
}

// domainOf is the domain a package of this module is in, or "" if it is not in one.
func domainOf(pkg string) string {
	rest, ok := strings.CutPrefix(pkg, "internal/")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	if _, ok := domains[name]; ok {
		return name
	}

	return ""
}

// goImports maps every package under root, relative to it, to the import paths of its non-test Go files: this
// module's own packages as written (full path), the rest as they are.
func goImports(t *testing.T, root string) map[string][]string {
	t.Helper()

	out := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "testdata", "tmp", "dbs", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}

		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		if _, ok := out[pkg]; !ok {
			out[pkg] = nil
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(imp, module+"/") {
				imp = module + "/" + path.Clean(strings.TrimPrefix(imp, module+"/"))
			}
			if !slices.Contains(out[pkg], imp) {
				out[pkg] = append(out[pkg], imp)
			}
		}
		return nil
	})
	require.NoError(t, err)

	return out
}

// webAreas are the folders web/js is cut into, and the other areas each may import: an area always imports its own
// files. The root files, webRootFiles, import every area. This is the table of LEANGO.md's "Architecture"
// (Frontend): change both together.
var webAreas = map[string][]string{
	"shell": nil,
	"note":  {"shell"},
}

// webAreaNames are the areas, in the order a message lists them.
var webAreaNames = slices.Sorted(maps.Keys(webAreas))

// webRootFiles are the only files that sit in web/js itself, outside the areas: app.ts, the entry that esbuild
// bundles; register.ts, the Alpine registry, which imports every area as internal/server imports every
// domain; and globals.d.ts, the declarations of the globals.
var webRootFiles = []string{"app.ts", "register.ts", "globals.d.ts"}

// importSpecifier matches the string of an import, its specifier, in the three forms that carry one: after the
// `from` of import … from and export … from (type-only ones too), after an `import` that runs a file for its
// effects, and inside import("…"), which splits a chunk off and which a type uses to name another file's.
var importSpecifier = regexp.MustCompile(`(?:\bfrom\s*|\bimport\s*\(?\s*)["']([^"'\n]+)["']`)

// TestArchitecture__WebAreasFollowTheirEdges reads the imports of every TypeScript file of web/js and holds each
// one to the rules of the layout of the frontend:
//
//   - web/js is cut into areas, the folders web/js/{shell,note}, and a file is in one of
//     them or is one of the root files app.ts (the entry), register.ts (the Alpine registry) and globals.d.ts:
//     no other file sits in web/js itself, and no other folder holds one.
//   - the root files import every area and each other: app.ts imports register.ts and the shell, and register.ts
//     the components of every area.
//   - note imports shell, and shell imports no other area. An area imports its own files freely.
//   - an area imports no root file, and no import leaves web/js.
//
// The imports it reads are the relative ones, "./" and "../": import … from, import type … from, export … from,
// the import "…" that runs a file for its effects, and import("…"). A comment that spells one is not read, and a
// bare specifier names an npm package, which the layout does not hold.
func TestArchitecture__WebAreasFollowTheirEdges(t *testing.T) {
	root := filepath.Join("..", "web", "js")
	imports := webImports(t, root)
	require.NotEmpty(t, imports["app.ts"], "the walk found the entry and its imports")

	for _, file := range slices.Sorted(maps.Keys(imports)) {
		if why := webMisplaced(file); why != "" {
			t.Errorf("web/js/%s: %s", file, why)
			continue
		}
		for _, spec := range imports[file] {
			if why := webForbidden(file, spec); why != "" {
				t.Errorf("web/js/%s imports %q: %s", file, spec, why)
			}
		}
	}

	for area := range webAreas {
		assert.DirExists(t, filepath.Join(root, area), "area %s exists", area)
	}
	for _, name := range webRootFiles {
		assert.FileExists(t, filepath.Join(root, name), "root file %s exists", name)
	}
}

// TestArchitecture__WebEdgesAreJudged holds the rules of the layout to what they say, on paths that need not exist,
// so that the test above cannot pass because a rule judges nothing.
func TestArchitecture__WebEdgesAreJudged(t *testing.T) {
	t.Run("where a file may be", func(t *testing.T) {
		for _, file := range []string{
			"app.ts", "register.ts", "globals.d.ts",
			"shell/htmx.ts", "note/charCount.ts", "note/nested/file.ts",
		} {
			assert.Empty(t, webMisplaced(file), file)
		}
		for _, file := range []string{
			"components/index.ts", "lib/db.ts", "other/file.ts", "helpers.ts", "shell.ts", "nested/shell/htmx.ts",
		} {
			assert.Contains(t, webMisplaced(file), "and nowhere else", file)
		}
	})

	t.Run("what a file may import", func(t *testing.T) {
		for _, tc := range []struct{ file, spec string }{
			{"app.ts", "./register"},
			{"app.ts", "./shell/htmx"},
			{"register.ts", "./note/charCount"},
			{"note/charCount.ts", "./count"},
			{"note/charCount.ts", "../shell/htmx"},
			{"note/nested/file.ts", "../charCount"},
			{"note/charCount.ts", "../shell"},
			{"note/charCount.ts", "."},
			{"shell/htmx.ts", "./other"},
		} {
			assert.Empty(t, webForbidden(tc.file, tc.spec), "%s imports %s", tc.file, tc.spec)
		}
	})

	t.Run("what a file may not import", func(t *testing.T) {
		for _, tc := range []struct{ file, spec, rule string }{
			{"shell/htmx.ts", "../note/charCount", "shell imports only its own files"},
			{"shell/htmx.ts", "../note", "shell imports only its own files"},
			{"note/charCount.ts", "../app", "an area imports no root file"},
			{"shell/htmx.ts", "../register", "an area imports no root file"},
			{"shell/htmx.ts", "../../outside", "no import leaves web/js"},
			{"app.ts", "../outside", "no import leaves web/js"},
			{"app.ts", "./components/widget", "components is not an area"},
			{"register.ts", "./lib/db", "lib is not an area"},
			{"note/charCount.ts", "../components/index", "components is not an area"},
		} {
			assert.Contains(t, webForbidden(tc.file, tc.spec), tc.rule, "%s imports %s", tc.file, tc.spec)
		}
	})
}

// TestArchitecture__WebImportsAreRead holds the reading of imports to the forms that TypeScript writes them in, so
// that the test of the edges cannot pass because an import was not seen.
func TestArchitecture__WebImportsAreRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"an import of names", `import { a } from "./a";`, []string{"./a"}},
		{"a type import", `import type { A } from "../b";`, []string{"../b"}},
		{"a type import of a namespace", `import type * as NS from "../shell/c";`, []string{"../shell/c"}},
		{"an import over several lines", "import {\n  a,\n  type B,\n} from \"./multi\";", []string{"./multi"}},
		{"a re-export", `export * from "./d"; export { x } from "../e"; export type { T } from "./f";`, []string{"./d", "../e", "./f"}},
		{"an import for its effects", `import "./side";`, []string{"./side"}},
		{"a dynamic import", `loading ??= import("./g").then((m) => m);`, []string{"./g"}},
		{"a dynamic import in single quotes", `const m = await import ( './h' );`, []string{"./h"}},
		{"a type that names a file", `type M = typeof import("../i");`, []string{"../i"}},
		{"the folder itself", `import { j } from "."; import { k } from "..";`, []string{".", ".."}},
		{"an npm package", `import Alpine from "alpinejs"; import "htmx.org/dist/ext/hx-preload.js"; const c = await import("cropperjs");`, nil},
		{"a file imported twice", `import type { A } from "./l"; import { b } from "./l";`, []string{"./l"}},
		{"comments that spell imports", "// import(\"./m\")\n/* import { x } from \"./n\" */\n/**\n * export * from \"./o\";\n */", nil},
		{"a comment after an import", `import { p } from "./p"; // from "./not"`, []string{"./p"}},
		{"a comment start inside a string", `const accept = "image/*"; // import("./comment")` + "\n" + `import("./q");`, []string{"./q"}},
		{"a url inside a string", `const u = "http://x"; import("./r");`, []string{"./r"}},
		{"an apostrophe in a comment", "// don't\nimport(\"./s\");", []string{"./s"}},
		{"an apostrophe in a string", `const s = "it's"; import("./t");`, []string{"./t"}},
		{"a comment start inside a template", "const t = `// not a comment`;\nimport(\"./u\");", []string{"./u"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, relativeImports(tc.src))
		})
	}
}

// webAreaOf is the area a path relative to web/js is in, or "" if it is in the root. The path of a folder that an
// import names, rather than a file, is in the area of that name.
func webAreaOf(rel string) string {
	area, _, nested := strings.Cut(rel, "/")
	if _, isArea := webAreas[area]; nested || isArea {
		return area
	}
	return ""
}

// webMisplaced says why a file may not be where it is, or "" if it may: a file of web/js is in an area folder or is
// a root file, and nowhere else. rel is its path relative to web/js.
func webMisplaced(rel string) string {
	area := webAreaOf(rel)
	_, inArea := webAreas[area]
	if inArea || (area == "" && slices.Contains(webRootFiles, rel)) {
		return ""
	}
	return "a file of web/js is in an area folder (" + strings.Join(webAreaNames, ", ") + ") or is one of " +
		strings.Join(webRootFiles, ", ") + ", and nowhere else"
}

// webForbidden says why the file at rel, a path relative to web/js, may not import spec, or "" if it may. It judges
// the imports of a file that webMisplaced has passed.
func webForbidden(rel, spec string) string {
	target := path.Join(path.Dir(rel), spec)
	if target == ".." || strings.HasPrefix(target, "../") {
		return "no import leaves web/js"
	}

	from, to := webAreaOf(rel), webAreaOf(target)
	if _, ok := webAreas[to]; to != "" && !ok {
		return to + " is not an area: the areas are " + strings.Join(webAreaNames, ", ")
	}
	switch {
	case from == "" || from == to:
		return "" // the root imports every area and itself, and an area imports its own files
	case to == "":
		return "an area imports no root file: the root imports the areas, and no area imports it"
	case slices.Contains(webAreas[from], to):
		return ""
	}

	rule := "not an edge of the area table (LEANGO.md, Architecture): " + from + " imports only its own files"
	if others := webAreas[from]; len(others) > 0 {
		rule += " and those of " + strings.Join(others, ", ")
	}
	return rule
}

// webImports maps every TypeScript file under root, relative to it, to the relative specifiers it imports.
func webImports(t *testing.T, root string) map[string][]string {
	t.Helper()

	out := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".ts") {
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		src, err := fs.ReadFile(os.DirFS(root), filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = relativeImports(string(src))
		return nil
	})
	require.NoError(t, err)

	return out
}

// relativeImports lists the specifiers of the imports of a TypeScript source that name a file of the same tree, "./"
// and "../" and the two folders themselves, each once and in the order they are written.
func relativeImports(src string) []string {
	var specs []string
	for _, m := range importSpecifier.FindAllStringSubmatch(withoutComments(src), -1) {
		spec := m[1]
		relative := spec == "." || spec == ".." || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../")
		if relative && !slices.Contains(specs, spec) {
			specs = append(specs, spec)
		}
	}

	return specs
}

// withoutComments is src with its comments blanked, so that an import a comment spells is not read as one. A string
// is copied whole: the "/*" of "image/*" does not begin a comment, and the specifier of an import is a string.
func withoutComments(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			end := strings.IndexByte(src[i:], '\n') // the line ends the comment, and stays
			if end < 0 {
				return b.String()
			}
			i += end
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			b.WriteByte(' ')
			i += 2 + end + 2
		case src[i] == '"' || src[i] == '\'' || src[i] == '`':
			end := stringEnd(src, i)
			b.WriteString(src[i:end])
			i = end
		default:
			b.WriteByte(src[i])
			i++
		}
	}

	return b.String()
}

// stringEnd is the index just past the string that opens at src[open]: after its closing quote, or, for a string of
// one quote that is not closed on its line, at the end of that line.
func stringEnd(src string, open int) int {
	quote := src[open]
	for i := open + 1; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\\':
			i++ // whatever it escapes
		case c == quote:
			return i + 1
		case c == '\n' && quote != '`':
			return i
		}
	}

	return len(src)
}
