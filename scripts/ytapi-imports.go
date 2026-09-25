//go:build ignore

package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	ytapiDir    = "internal/ytapi"
	adapterFile = "internal/youtrack/ytapi.go"
)

type violation struct {
	file    string
	line    int
	message string
}

type sourceFile struct {
	path    string
	pkg     string
	build   constraint.Expr
	ignored bool
}

type platform struct {
	GOOS   string
	GOARCH string
}

type platforms []platform

type listRun struct {
	env  []string
	args []string
}

type listedPackage struct {
	ImportPath string
	DepOnly    bool
	Dir        string
	GoFiles    []string
	ImportMap  map[string]string
	Export     string
}

func main() {
	violations, err := findViolations()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot judge:", err)
		os.Exit(2)
	}
	for _, v := range violations {
		fmt.Printf("%s:%d: %s\n", v.file, v.line, v.message)
	}
	if len(violations) > 0 {
		os.Exit(1)
	}
}

func findViolations() ([]violation, error) {
	modules, err := goJSON[struct{ Path, Dir string }](nil, "list", "-m", "-json")
	if err != nil {
		return nil, err
	}
	if len(modules) != 1 {
		return nil, fmt.Errorf("go list -m lists %d main modules instead of one", len(modules))
	}
	module := modules[0]
	ytapiPath := module.Path + "/" + ytapiDir
	if err := os.Chdir(module.Dir); err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	files, violations, err := walkModule(fset, ytapiPath)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(files, func(f sourceFile) bool { return f.path == adapterFile && !f.ignored }) {
		return nil, fmt.Errorf("module %s has no file %s", module.Path, adapterFile)
	}
	runs, err := listRuns(files)
	if err != nil {
		return nil, err
	}
	checked := map[string]bool{}
	ytapiListed := false
	for _, run := range runs {
		args := slices.Concat([]string{"list", "-deps", "-test", "-export", "-json=ImportPath,DepOnly,Dir,GoFiles,ImportMap,Export"}, run.args)
		packages, err := goJSON[listedPackage](run.env, args...)
		if err != nil {
			return nil, err
		}
		exports := map[string]string{}
		for _, p := range packages {
			exports[p.ImportPath] = p.Export
		}
		for _, p := range packages {
			path, _, _ := strings.Cut(p.ImportPath, " ")
			if !p.DepOnly && path == ytapiPath {
				ytapiListed = true
			}
			generatedTestMain := strings.HasSuffix(path, ".test")
			if p.DepOnly || path == ytapiPath || generatedTestMain {
				continue
			}
			found, typed, err := useViolations(fset, module.Dir, ytapiPath, p, exports)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p.ImportPath, err)
			}
			violations = append(violations, found...)
			for _, file := range typed {
				checked[file] = true
			}
		}
	}
	if !ytapiListed {
		return nil, fmt.Errorf("module %s has no package %s", module.Path, ytapiDir)
	}
	var unchecked []string
	for _, f := range files {
		if filepath.Dir(f.path) != ytapiDir && (!f.ignored || f.isStandaloneProgram()) && !checked[f.path] {
			unchecked = append(unchecked, f.path)
		}
	}
	if len(unchecked) > 0 {
		return nil, fmt.Errorf("no build configuration type-checks %s", strings.Join(unchecked, ", "))
	}
	slices.SortFunc(violations, func(a, b violation) int {
		return cmp.Or(strings.Compare(a.file, b.file), cmp.Compare(a.line, b.line), strings.Compare(a.message, b.message))
	})
	return slices.Compact(violations), nil
}

func walkModule(fset *token.FileSet, ytapiPath string) ([]sourceFile, []violation, error) {
	var files []sourceFile
	var imports []violation
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		name := entry.Name()
		ignoredByGo := strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
		if entry.IsDir() {
			if ignoredByGo || name == "testdata" || isFile(filepath.Join(path, "go.mod")) {
				return filepath.SkipDir
			}
			return nil
		}
		if ignoredByGo || filepath.Ext(name) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			return err
		}
		build, err := buildConstraint(file)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		files = append(files, sourceFile{path, file.Name.Name, build, requiresIgnore(build)})
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if importPath != ytapiPath || path == adapterFile {
				continue
			}
			message := "imports " + ytapiDir
			if spec.Name != nil {
				message += " as " + spec.Name.Name
			}
			imports = append(imports, violation{path, fset.Position(spec.Pos()).Line, message})
		}
		return nil
	})
	return files, imports, err
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func buildConstraint(file *ast.File) (constraint.Expr, error) {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if comment.Pos() < file.Package && constraint.IsGoBuild(comment.Text) {
				return constraint.Parse(comment.Text)
			}
		}
	}
	return nil, nil
}

func (f sourceFile) isStandaloneProgram() bool {
	return f.ignored && f.pkg == "main"
}

func requiresIgnore(x constraint.Expr) bool {
	switch x := x.(type) {
	case *constraint.TagExpr:
		return x.Tag == "ignore"
	case *constraint.AndExpr:
		return requiresIgnore(x.X) || requiresIgnore(x.Y)
	}
	return false
}

func listRuns(files []sourceFile) ([]listRun, error) {
	hosts, err := goJSON[platform](nil, "env", "-json", "GOOS", "GOARCH")
	if err != nil {
		return nil, err
	}
	lists, err := goJSON[[]platform](nil, "tool", "dist", "list", "-json")
	if err != nil {
		return nil, err
	}
	host, known := hosts[0], platforms(slices.Concat(lists...))
	oses, tags := buildTerms(files, known)
	envs := [][]string{nil}
	for _, goos := range oses {
		if goos == host.GOOS {
			continue
		}
		arch := host.GOARCH
		if !slices.Contains(known, platform{goos, arch}) {
			arch = known.firstArch(goos)
		}
		envs = append(envs, []string{"GOOS=" + goos, "GOARCH=" + arch})
	}
	tagFlags := [][]string{nil}
	if len(tags) > 0 {
		tagFlags = append(tagFlags, []string{"-tags=" + strings.Join(tags, ",")})
	}
	var runs []listRun
	for _, env := range envs {
		for _, flags := range tagFlags {
			runs = append(runs, listRun{env, slices.Concat(flags, []string{"./..."})})
		}
	}
	for _, f := range files {
		if f.isStandaloneProgram() {
			runs = append(runs, listRun{nil, []string{f.path}})
		}
	}
	return runs, nil
}

func buildTerms(files []sourceFile, known platforms) (oses, tags []string) {
	osSet, tagSet := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		if f.ignored {
			continue
		}
		if goos := fileOS(filepath.Base(f.path), known); goos != "" {
			osSet[goos] = true
		}
		for _, tag := range constraintTags(f.build) {
			if known.hasOS(tag) {
				osSet[tag] = true
			} else if !goSetsTag(tag, known) {
				tagSet[tag] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(osSet)), slices.Sorted(maps.Keys(tagSet))
}

func constraintTags(x constraint.Expr) []string {
	switch x := x.(type) {
	case *constraint.TagExpr:
		return []string{x.Tag}
	case *constraint.NotExpr:
		return constraintTags(x.X)
	case *constraint.AndExpr:
		return append(constraintTags(x.X), constraintTags(x.Y)...)
	case *constraint.OrExpr:
		return append(constraintTags(x.X), constraintTags(x.Y)...)
	}
	return nil
}

func fileOS(name string, known platforms) string {
	stem, _, _ := strings.Cut(name, ".")
	parts := strings.Split(strings.TrimSuffix(stem, "_test"), "_")[1:]
	switch n := len(parts); {
	case n >= 2 && known.hasOS(parts[n-2]) && known.hasArch(parts[n-1]):
		return parts[n-2]
	case n >= 1 && known.hasOS(parts[n-1]):
		return parts[n-1]
	}
	return ""
}

func goSetsTag(tag string, known platforms) bool {
	arch, _, _ := strings.Cut(tag, ".")
	return known.hasOS(tag) || known.hasArch(arch) || slices.Contains([]string{"unix", "cgo", "gc", "gccgo"}, tag) ||
		strings.HasPrefix(tag, "go1.") || strings.HasPrefix(tag, "goexperiment.")
}

func (known platforms) hasOS(name string) bool {
	return slices.ContainsFunc(known, func(p platform) bool { return p.GOOS == name })
}

func (known platforms) hasArch(name string) bool {
	return slices.ContainsFunc(known, func(p platform) bool { return p.GOARCH == name })
}

func (known platforms) firstArch(goos string) string {
	return known[slices.IndexFunc(known, func(p platform) bool { return p.GOOS == goos })].GOARCH
}

func useViolations(fset *token.FileSet, root, ytapiPath string, p listedPackage, exports map[string]string) ([]violation, []string, error) {
	dir, err := filepath.Rel(root, p.Dir)
	if err != nil {
		return nil, nil, err
	}
	var files []*ast.File
	var paths []string
	for _, name := range p.GoFiles {
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, nil, err
		}
		files, paths = append(files, file), append(paths, path)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		return os.Open(exports[p.listedImportPath(path)])
	})}
	info := types.Info{Uses: map[*ast.Ident]types.Object{}}
	if _, err := conf.Check(p.ImportPath, fset, files, &info); err != nil {
		return nil, nil, err
	}
	var found []violation
	for id, obj := range info.Uses {
		pos := fset.Position(id.Pos())
		if obj.Pkg() != nil && obj.Pkg().Path() == ytapiPath && pos.Filename != adapterFile && !allowed(obj) {
			found = append(found, violation{pos.Filename, pos.Line, "uses " + obj.Name() + " from " + ytapiDir})
		}
	}
	return found, paths, nil
}

func (p listedPackage) listedImportPath(sourceImport string) string {
	return cmp.Or(p.ImportMap[sourceImport], sourceImport)
}

func allowed(obj types.Object) bool {
	switch obj := obj.(type) {
	case *types.TypeName:
		return slices.Contains([]string{"Client", "RequestEditorFn", "HttpRequestDoer"}, obj.Name()) || strings.HasSuffix(obj.Name(), "Params")
	case *types.Func:
		return obj.Name() == "NewClient" && obj.Signature().Recv() == nil
	case *types.Var:
		return obj.IsField() && declaredInParams(obj)
	}
	return false
}

func declaredInParams(field *types.Var) bool {
	scope := field.Pkg().Scope()
	for _, name := range scope.Names() {
		if !strings.HasSuffix(name, "Params") {
			continue
		}
		if st, ok := scope.Lookup(name).Type().Underlying().(*types.Struct); ok && slices.Contains(slices.Collect(st.Fields()), field) {
			return true
		}
	}
	return false
}

func goJSON[T any](env []string, args ...string) ([]T, error) {
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(slices.Concat(env, cmd.Args), " "), err)
	}
	var values []T
	for decoder := json.NewDecoder(bytes.NewReader(out)); decoder.More(); {
		var value T
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}
