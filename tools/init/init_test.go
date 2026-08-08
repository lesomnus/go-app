package main

import (
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewNames(t *testing.T) {
	t.Run("takes the app name from the module path", func(t *testing.T) {
		n, err := newNames("github.com/acme/my-service", "")
		if err != nil {
			t.Fatal(err)
		}
		if n.Kebab != "my-service" || n.Snake != "my_service" || n.Upper != "MY_SERVICE" {
			t.Fatalf("%+v", n)
		}
		if n.OrgRepo != "acme/my-service" {
			t.Fatalf("org/repo: %q", n.OrgRepo)
		}
	})

	t.Run("takes the app name it is given", func(t *testing.T) {
		n, err := newNames("github.com/acme/my-service", "svc")
		if err != nil {
			t.Fatal(err)
		}
		if n.Kebab != "svc" || n.Snake != "svc" || n.Upper != "SVC" {
			t.Fatalf("%+v", n)
		}
	})

	t.Run("refuses a name that cannot be a package", func(t *testing.T) {
		for _, app := range []string{"my.service", "my service", "my/service", "9lives"} {
			if _, err := newNames("github.com/acme/x", app); err == nil {
				t.Errorf("%q was accepted", app)
			}
		}
	})

	t.Run("refuses a module path it cannot use", func(t *testing.T) {
		for _, module := range []string{"", "github.com/acme/", "with space/x"} {
			if _, err := newNames(module, ""); err == nil {
				t.Errorf("%q was accepted", module)
			}
		}
	})
}

func TestSubstitutionOrder(t *testing.T) {
	n, err := newNames("github.com/acme/my-service", "")
	if err != nil {
		t.Fatal(err)
	}

	// The order is what makes these come out right, and each of them is a way
	// it has gone wrong: the shorthand eating half the module path, and the
	// upper-case prefix left half-replaced by a match on the lower-case one.
	for _, tc := range []struct{ in, want string }{
		{"github.com/lesomnus/go-app/internal/ent", "github.com/acme/my-service/internal/ent"},
		{"ghcr.io/lesomnus/go-app:latest", "ghcr.io/acme/my-service:latest"},
		{"GO_APP_SERVER_ADDR", "MY_SERVICE_SERVER_ADDR"},
		{`package go_app`, `package my_service`},
		{"go-app.yaml", "my-service.yaml"},
		{`import "go_app/coffee.proto"`, `import "my_service/coffee.proto"`},
	} {
		if got := n.apply(tc.in); got != tc.want {
			t.Errorf("%q -> %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStrip(t *testing.T) {
	db := map[string]bool{"db": true}
	none := map[string]bool{}

	t.Run("removes a block when its variant goes", func(t *testing.T) {
		src := "a\n//template:begin db\nb\n//template:end db\nc\n"
		got, err := strip(src, db)
		if err != nil {
			t.Fatal(err)
		}
		if got != "a\nc\n" {
			t.Fatalf("%q", got)
		}
	})

	t.Run("keeps a block when its variant stays, without the markers", func(t *testing.T) {
		src := "a\n//template:begin db\nb\n//template:end db\nc\n"
		got, err := strip(src, none)
		if err != nil {
			t.Fatal(err)
		}
		if got != "a\nb\nc\n" {
			t.Fatalf("%q", got)
		}
	})

	t.Run("a no- block is the other side of the same coin", func(t *testing.T) {
		src := "//template:begin db\nkeep-with-db\n//template:end db\n" +
			"//template:begin no-db\nkeep-without-db\n//template:end no-db\n"

		got, err := strip(src, db)
		if err != nil {
			t.Fatal(err)
		}
		if got != "keep-without-db\n" {
			t.Fatalf("with --no-db: %q", got)
		}

		got, err = strip(src, none)
		if err != nil {
			t.Fatal(err)
		}
		if got != "keep-with-db\n" {
			t.Fatalf("without --no-db: %q", got)
		}
	})

	t.Run("removes a marked line, or its marker", func(t *testing.T) {
		src := "\tDb DbConfig //template:db\n\tGreet GreetConfig\n"

		got, err := strip(src, db)
		if err != nil {
			t.Fatal(err)
		}
		if got != "\tGreet GreetConfig\n" {
			t.Fatalf("%q", got)
		}

		got, err = strip(src, none)
		if err != nil {
			t.Fatal(err)
		}
		if got != "\tDb DbConfig\n\tGreet GreetConfig\n" {
			t.Fatalf("%q", got)
		}
	})

	t.Run("reads the comment leader the file uses", func(t *testing.T) {
		for _, src := range []string{
			"#template:begin db\nb\n#template:end db\n",
			"# template:begin db\nb\n# template:end db\n",
			"<!--template:begin db-->\nb\n<!--template:end db-->\n",
		} {
			got, err := strip(src, db)
			if err != nil {
				t.Fatalf("%q: %s", src, err)
			}
			if got != "" {
				t.Errorf("%q -> %q", src, got)
			}
		}
	})

	t.Run("is not fooled by the word appearing in prose", func(t *testing.T) {
		// It has to be behind a comment leader, or this document would lose a
		// line to its own explanation.
		src := "The marker is template:begin db and it goes in a comment.\n"
		got, err := strip(src, db)
		if err != nil {
			t.Fatal(err)
		}
		if got != src {
			t.Fatalf("%q", got)
		}
	})

	t.Run("reports a marker that does not close", func(t *testing.T) {
		for _, src := range []string{
			"//template:begin db\nb\n",
			"//template:end db\n",
			// Closed in the wrong order.
			"//template:begin db\n//template:begin ui\n//template:end db\n//template:end ui\n",
		} {
			if _, err := strip(src, db); err == nil {
				t.Errorf("%q was accepted", src)
			}
		}
	})

	t.Run("a block being removed swallows whatever is inside it", func(t *testing.T) {
		// Which is what lets the documentation of this format sit inside a
		// block of its own, examples and all.
		src := "keep\n//template:begin self\n" +
			"Db DbConfig //template:db\n" +
			"//template:begin db\nb\n//template:end db\n" +
			"//template:end self\nkeep too\n"

		got, err := strip(src, map[string]bool{"self": true})
		if err != nil {
			t.Fatal(err)
		}
		if got != "keep\nkeep too\n" {
			t.Fatalf("%q", got)
		}
	})
}

// TestVariantsBuild is the promise this tool makes, kept.
//
// A template that offers to leave a part out has said that what is left still
// builds. Nothing but running it proves that, so this copies the repository,
// runs the tool over the copy, and builds the result -- which is also why the
// tool is Go and not a shell script.
//
// It is skipped under -short, since it compiles the tree once per variant.
func TestVariantsBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the whole tree, once per variant")
	}

	for _, tc := range []struct {
		name string
		args []string
		// gone is what must not be in the tree afterwards.
		gone []string
		// kept is what must be.
		kept []string
	}{
		{
			name: "whole",
			// A name of the same length as the template's, so the protobuf
			// descriptors in the committed generated code stay valid without
			// having to run the generators here -- they need buf, which a unit
			// test should not.
			args: []string{"--no-generate", "github.com/acme/gg-bpp"},
			kept: []string{"internal/ent", "server/bare", "migrations", "gg_bpp", "gg-bpp.yaml"},
			gone: []string{"go_app", "go-app.yaml", "tools/init", "scripts/init.sh"},
		},
		{
			name: "no-db",
			args: []string{"--no-generate", "--no-db", "github.com/acme/svc"},
			gone: []string{
				"internal/ent", "server/bare", "migrations", "internal/migrate",
				"server/watch", "server/spin", "proto.svc", "svc", "cmd/migrate.go",
				"cmd/config/db.go", "buf.gen.svc.yaml", "internal/ox/server.go",
				"server/core/coffee.go", "server/gate/gate.go", "tools/init",
			},
			kept: []string{
				"server/auth", "server/gate", "internal/grpcx", "internal/httpx",
				"internal/ox/stack.go", "cmd/serve.go", "svc.yaml", "ui/src/App.tsx",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			copyRepo(t, repoRoot(t), dir)

			o, err := parse(tc.args, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			// Pointed at the copy rather than chdir'd into it, so these can run
			// in parallel.
			o.root = dir
			if err := initialize(o, io.Discard); err != nil {
				t.Fatalf("init: %s", err)
			}

			for _, p := range tc.gone {
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
					t.Errorf("%s is still there", p)
				}
			}
			for _, p := range tc.kept {
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
					t.Errorf("%s is gone", p)
				}
			}

			// No marker survives into somebody's app.
			if found := grepTree(t, dir, markerWord); len(found) > 0 {
				t.Errorf("markers left behind: %v", found)
			}
			// Nor does the template's own name.
			if found := grepTree(t, dir, fromModule); len(found) > 0 {
				t.Errorf("the template's module path is still in: %v", found)
			}

			build(t, dir)
		})
	}
}

// repoRoot is the repository this test is part of, which is two directories up
// from the package it lives in.
func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Dir(filepath.Dir(wd))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s", root)
	}

	return root
}

func copyRepo(t *testing.T, from, to string) {
	t.Helper()

	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return filepath.SkipDir
			}

			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}

		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return os.WriteFile(filepath.Join(to, rel), b, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// grepTree answers with the files that still contain `what`.
func grepTree(t *testing.T, root, what string) []string {
	t.Helper()

	var found []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return filepath.SkipDir
			}

			return nil
		}
		if !isText(p) {
			return nil
		}

		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), what) {
			rel, _ := filepath.Rel(root, p)
			found = append(found, rel)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return found
}

func build(t *testing.T, dir string) {
	t.Helper()

	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %s:\n%s", strings.Join(args, " "), out)
		}
	}
}
