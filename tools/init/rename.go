package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// What this template calls itself. These are the strings the rename replaces,
// and the reason the tool is excluded from its own walk: they have to survive
// in here for it to be run at all.
const (
	fromModule   = "github.com/lesomnus/go-app"
	fromOrgRepo  = "lesomnus/go-app"
	fromAppKebab = "go-app"
	fromAppUpper = "GO_APP"
	fromAppSnake = "go_app"
)

// names is what the template is being renamed to.
type names struct {
	Module  string
	OrgRepo string
	Kebab   string
	Upper   string
	Snake   string
}

func newNames(module, app string) (names, error) {
	if module == "" {
		return names{}, fmt.Errorf("no module path")
	}
	if strings.Contains(module, " ") {
		return names{}, fmt.Errorf("module path %q contains a space", module)
	}
	if app == "" {
		app = module[strings.LastIndex(module, "/")+1:]
	}
	if app == "" {
		return names{}, fmt.Errorf("module path %q ends in a slash, so there is no name to take from it", module)
	}

	upper := strings.ToUpper(strings.ReplaceAll(app, "-", "_"))

	// The environment prefix and the proto package are built from the app name,
	// so a name that is not a Go identifier once the hyphens are gone would
	// produce a package nothing can compile.
	for _, r := range upper {
		ok := r == '_' || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return names{}, fmt.Errorf("app name %q cannot be a package name: %q is not a letter, a digit or a hyphen", app, r)
		}
	}
	if upper[0] >= '0' && upper[0] <= '9' {
		return names{}, fmt.Errorf("app name %q starts with a digit, so it cannot be a package name", app)
	}

	org := module
	if i := strings.Index(module, "/"); i >= 0 {
		org = module[i+1:]
	}

	return names{
		Module:  module,
		OrgRepo: org,
		Kebab:   app,
		Upper:   upper,
		Snake:   strings.ToLower(upper),
	}, nil
}

// substitutions are the replacements, in the order they must happen.
//
// The order is load-bearing and was found the hard way. The full module path
// goes first so that the `org/repo` shorthand does not eat half of it; the
// UPPER_CASE prefix goes before the lower-case forms so that `GO_APP` is not
// left half-replaced by a match on `go_app`.
func (n names) substitutions() [][2]string {
	return [][2]string{
		{fromModule, n.Module},
		{fromOrgRepo, n.OrgRepo},
		{fromAppUpper, n.Upper},
		{fromAppSnake, n.Snake},
		{fromAppKebab, n.Kebab},
	}
}

func (n names) apply(s string) string {
	for _, sub := range n.substitutions() {
		s = strings.ReplaceAll(s, sub[0], sub[1])
	}

	return s
}

// textExt is what gets rewritten. A file that is not one of these is left
// alone, which is what keeps the walk away from binaries and lock files.
var textExt = map[string]bool{
	".go": true, ".mod": true, ".proto": true, ".sh": true,
	".yml": true, ".yaml": true, ".hcl": true, ".json": true,
	".md": true, ".ts": true, ".tsx": true, ".html": true, ".css": true,
	".sql": true,
}

// textName is the same for the files that have no extension.
var textName = map[string]bool{
	"Dockerfile": true, ".dockerignore": true, ".gitignore": true, ".nvmrc": true,
}

// skipDir is never walked into: somebody else's files, a great many of them,
// and rewriting inside one is both wrong and slow.
var skipDir = map[string]bool{
	".git": true, "node_modules": true, "dist": true, ".gen": true,
}

func isText(path string) bool {
	base := filepath.Base(path)
	if textName[base] {
		return true
	}
	if strings.HasPrefix(base, "Dockerfile.") {
		return true
	}

	return textExt[filepath.Ext(base)]
}

// tree is the repository being worked on.
type tree struct {
	root string

	// self is the tool's own directory, relative to root. It is skipped by
	// every rewrite: the strings above have to survive in it.
	self string

	dry bool
	log func(format string, a ...any)
}

// walkText calls `fn` with the contents of every text file, and writes back
// whatever it answers with.
func (t *tree) walkText(fn func(path string, src string) (string, error)) error {
	return filepath.WalkDir(t.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return filepath.SkipDir
			}
			if rel, _ := filepath.Rel(t.root, path); rel == t.self {
				return filepath.SkipDir
			}

			return nil
		}
		if !isText(path) {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		out, err := fn(path, string(b))
		if err != nil {
			rel, _ := filepath.Rel(t.root, path)
			return fmt.Errorf("%s: %w", rel, err)
		}
		if out == string(b) {
			return nil
		}
		if t.dry {
			rel, _ := filepath.Rel(t.root, path)
			t.log("would rewrite %s", rel)
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return os.WriteFile(path, []byte(out), info.Mode().Perm())
	})
}

// rename rewrites every text file and then renames the paths named after the
// template.
func (t *tree) rename(n names) error {
	err := t.walkText(func(path, src string) (string, error) {
		out := n.apply(src)

		// A Go file that stopped parsing is a substitution that hit something
		// it should not have. Better to say so here than to hand somebody a
		// tree that does not build and a rename to blame it on.
		if filepath.Ext(path) == ".go" && out != src {
			fset := token.NewFileSet()
			if _, err := parser.ParseFile(fset, path, out, parser.SkipObjectResolution); err != nil {
				return "", fmt.Errorf("the rename left this unparseable, which is a bug in the tool: %w", err)
			}
		}

		return out, nil
	})
	if err != nil {
		return err
	}

	// The config file, which is named after the app.
	for _, ext := range []string{"yaml", "yml"} {
		from := filepath.Join(t.root, fromAppKebab+"."+ext)
		to := filepath.Join(t.root, n.Kebab+"."+ext)
		if from == to {
			continue
		}
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if err := t.move(from, to); err != nil {
			return err
		}
	}

	// And the directories. The snake_case ones are the proto package and the
	// Go package it becomes; the kebab-case one is the skill, whose directory
	// has to keep matching the `name:` in it.
	if err := t.renameDirs(fromAppSnake, n.Snake); err != nil {
		return err
	}

	return t.renameDirs(fromAppKebab, n.Kebab)
}

// renameDirs renames every directory called `from`, deepest first so that a
// parent moving does not invalidate the path of a child.
func (t *tree) renameDirs(from, to string) error {
	if from == to {
		return nil
	}

	var found []string
	err := filepath.WalkDir(t.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if skipDir[d.Name()] {
			return filepath.SkipDir
		}
		if d.Name() == from {
			found = append(found, path)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Deepest first.
	sort.Slice(found, func(i, j int) bool { return len(found[i]) > len(found[j]) })
	for _, dir := range found {
		if err := t.move(dir, filepath.Join(filepath.Dir(dir), to)); err != nil {
			return err
		}
	}

	return nil
}

func (t *tree) move(from, to string) error {
	rel := func(p string) string {
		v, _ := filepath.Rel(t.root, p)
		return v
	}
	if t.dry {
		t.log("would rename %s -> %s", rel(from), rel(to))
		return nil
	}

	t.log("rename %s -> %s", rel(from), rel(to))
	return os.Rename(from, to)
}

// write copies everything in `src` into the tree, overwriting what is there. It
// is how a variant replaces a file it could not simply mark.
//
// A nil `src` is a variant with nothing to replace, which is most of them.
func (t *tree) write(src fs.FS) error {
	if src == nil {
		return nil
	}

	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		rel := filepath.FromSlash(p)
		if t.dry {
			t.log("would write %s", filepath.ToSlash(rel))
			return nil
		}

		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}

		dst := filepath.Join(t.root, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		t.log("write %s", filepath.ToSlash(rel))
		return os.WriteFile(dst, b, 0o644)
	})
}

// has reports whether any of `paths` is in the tree, which is how a variant
// says whether it applies here at all.
func (t *tree) has(paths []string) bool {
	for _, p := range paths {
		if _, err := os.Lstat(filepath.Join(t.root, filepath.FromSlash(p))); err == nil {
			return true
		}
	}

	return false
}

// removeIfEmpty deletes a directory that has nothing left in it.
func (t *tree) removeIfEmpty(rel string) error {
	p := filepath.Join(t.root, filepath.FromSlash(rel))
	es, err := os.ReadDir(p)
	if err != nil || len(es) > 0 {
		return nil //nolint:nilerr // not there, or not empty
	}
	if t.dry {
		t.log("would remove %s", filepath.ToSlash(rel))
		return nil
	}

	t.log("remove %s", filepath.ToSlash(rel))
	return os.Remove(p)
}

// remove deletes a path, and says nothing about one that is already not there.
func (t *tree) remove(rel string) error {
	p := filepath.Join(t.root, filepath.FromSlash(rel))
	if _, err := os.Lstat(p); err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}
	if t.dry {
		t.log("would remove %s", rel)
		return nil
	}

	t.log("remove %s", rel)
	return os.RemoveAll(p)
}
