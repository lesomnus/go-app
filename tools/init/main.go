// Command init turns this template into an app.
//
// It renames everything named after the template, leaves out the parts that
// were not asked for, and generates what is generated.
//
//	go run ./tools/init github.com/acme/my-service
//	go run ./tools/init --no-db github.com/acme/my-service svc
//
// # Why this is Go and not a shell script
//
// It used to be `scripts/init.sh`, and the reason it is not any more is not
// that it grew. It is that **the variants have to be tested**: a template that
// offers to leave a part out has promised that what is left still builds, and
// the only way to keep that promise is to do it in CI, on every change. In Go
// that check is `go test ./tools/init` -- the tool copies the repository into a
// temporary directory, runs itself over it, and builds the result. A shell
// script would have needed a second thing to test it with.
//
// Two smaller reasons: `sed -i` is not the same program on macOS as on Linux,
// and a rewritten Go file can be handed to `go/parser` here to check that the
// rename did not corrupt it.
//
// # It has no dependencies, on purpose
//
// Nothing outside the standard library. It lives in the module it is about to
// rewrite, so anything it imported would become a dependency of somebody's app
// -- and it removes itself at the end, which is a poor time to discover that
// `go.mod` still names something only it used.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "\ninit: %s\n", err)
		os.Exit(1)
	}
}

type options struct {
	// root is the repository to work on, and is the working directory when it
	// is empty. Only the tests set it: everything else runs `go run
	// ./tools/init` from the root, which is where the tool checks it is.
	root string

	module   string
	app      string
	without  []string
	generate bool
	keep     bool
	dry      bool
}

func run(args []string, out io.Writer) error {
	o, err := parse(args, out)
	if err != nil {
		return err
	}

	return initialize(o, out)
}

// parse reads the command line, and is separate from [initialize] so that a
// test can say what it wants without going through a string.
func parse(args []string, out io.Writer) (options, error) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		noGenerate = fs.Bool("no-generate", false, "rename only; the tree will not run until the generators have been run by hand")
		keep       = fs.Bool("keep", false, "do not remove this tool when it is done")
		dry        = fs.Bool("dry-run", false, "say what would happen and change nothing")
	)

	// One switch per variant, so `--help` lists what can be left out.
	names := make([]string, 0, len(variants))
	for name := range variants {
		names = append(names, name)
	}
	sort.Strings(names)

	without := make(map[string]*bool, len(variants))
	for _, name := range names {
		without[name] = fs.Bool("no-"+name, false, variants[name].Brief)
	}

	fs.Usage = func() {
		fmt.Fprintf(out, "Usage: go run ./tools/init [flags] <module-path> [app-name]\n\n")
		fmt.Fprintf(out, "  module-path   the Go module path of the new app\n")
		fmt.Fprintf(out, "  app-name      the binary, the config file and the proto package\n")
		fmt.Fprintf(out, "                (default: the last segment of the module path)\n\n")
		fmt.Fprintf(out, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(out, "\nExample:\n")
		fmt.Fprintf(out, "  go run ./tools/init github.com/acme/my-service\n")
		fmt.Fprintf(out, "  go run ./tools/init --no-db github.com/acme/my-service svc\n")
	}
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return options{}, fmt.Errorf("no module path")
	}

	o := options{
		module:   fs.Arg(0),
		generate: !*noGenerate,
		keep:     *keep,
		dry:      *dry,
	}
	if fs.NArg() > 1 {
		o.app = fs.Arg(1)
	}
	for _, name := range names {
		if *without[name] {
			o.without = append(o.without, name)
		}
	}

	return o, nil
}

func initialize(o options, out io.Writer) error {
	root, self, err := locate(o.root)
	if err != nil {
		return err
	}

	n, err := newNames(o.module, o.app)
	if err != nil {
		return err
	}

	t := &tree{
		root: root,
		self: self,
		dry:  o.dry,
		log:  func(format string, a ...any) { fmt.Fprintf(out, "  "+format+"\n", a...) },
	}

	fmt.Fprintf(out, "Module   : %s  ->  %s\n", fromModule, n.Module)
	fmt.Fprintf(out, "App      : %s / %s  ->  %s / %s\n", fromAppKebab, fromAppUpper, n.Kebab, n.Upper)
	fmt.Fprintf(out, "Proto    : %s  ->  %s\n", fromAppSnake, n.Snake)
	if len(o.without) > 0 {
		fmt.Fprintf(out, "Without  : %s\n", strings.Join(o.without, ", "))
	}
	fmt.Fprintln(out)

	// The variant first, and the rename second. What a variant names are the
	// template's own paths, which is what they still are at this point.
	if len(o.without) > 0 {
		fmt.Fprintln(out, "Leaving out...")
		if err := leaveOut(t, o.without); err != nil {
			return err
		}
		fmt.Fprintln(out)
	}

	// Always, and not only when something was left out: the markers are the
	// template's own bookkeeping and no app should inherit them.
	if err := stripAll(t, tagsOf(o.without, !o.keep)); err != nil {
		return err
	}

	fmt.Fprintln(out, "Renaming...")
	if err := t.rename(n); err != nil {
		return err
	}

	if o.generate && !o.dry {
		fmt.Fprintln(out, "\nGenerating...")
		if err := generate(root, out); err != nil {
			return err
		}

		fmt.Fprintln(out, "\nBuilding...")
		if err := command(root, out, "go", "build", "./..."); err != nil {
			return err
		}
		// And vet, which compiles the tests as well. That is not thoroughness
		// for its own sake: the app name becomes the alias of the message
		// package, so choosing one that the sample uses as a *variable* shadows
		// it -- and only in the tests, which `go build` never looks at. Found
		// here it is a name to change; found later it is a puzzle.
		if err := command(root, out, "go", "vet", "./..."); err != nil {
			return fmt.Errorf("the tree builds but does not vet, which is usually the app name shadowing the message package it is also the alias of; pick another name: %w", err)
		}
	}

	if !o.keep && !o.dry {
		// Last, so that a failure above leaves something to run again.
		if err := t.remove(self); err != nil {
			return err
		}
		// And the directory that held it, if this was the only thing in it. An
		// empty `tools/` is a thing somebody would have to wonder about.
		if err := t.removeIfEmpty(filepath.Dir(self)); err != nil {
			return err
		}
	}

	fmt.Fprintln(out, "\nDone.")
	for _, name := range o.without {
		if note := variants[name].Note; note != "" {
			fmt.Fprintf(out, "\n--no-%s\n\n%s\n", name, indent(note))
		}
	}
	if !o.generate {
		fmt.Fprintln(out, "\nThe generators have not run, so the tree does not build yet.")
	}

	return nil
}

// locate finds the repository root and this tool's place in it.
//
// It is the directory of the tool's own source, two levels up: the tool is at
// `tools/init` by construction, and `go run` compiles it somewhere else
// entirely -- so the binary's own path says nothing about where the repository
// is.
func locate(root string) (string, string, error) {
	wd := root
	if wd == "" {
		var err error
		if wd, err = os.Getwd(); err != nil {
			return "", "", err
		}
	}

	// `go run ./tools/init` runs with the working directory of whoever invoked
	// it, which is the repository root in every documented use. Verified rather
	// than assumed, because being wrong here deletes the wrong tree.
	for _, want := range []string{"go.mod", "tools/init/main.go"} {
		if _, err := os.Stat(filepath.Join(wd, filepath.FromSlash(want))); err != nil {
			return "", "", fmt.Errorf("run this from the repository root (%s is not there)", want)
		}
	}

	return wd, filepath.FromSlash("tools/init"), nil
}

// leaveOut removes the paths of the variants not wanted, writes whatever
// replaces them, and strips the marked regions of everything that stays.
func leaveOut(t *tree, without []string) error {
	for _, name := range without {
		v, ok := variants[name]
		if !ok {
			return fmt.Errorf("there is no variant called %q", name)
		}
		if !t.has(v.Paths) {
			// Asking for a tree to be without something it is already without
			// is a request that has been met. `main` carries no database half
			// at all, and writing the files that stand in for one would build
			// half of something nobody asked for.
			t.log("there is no %s in this tree to leave out", name)
			continue
		}

		for _, p := range v.Paths {
			if err := t.remove(p); err != nil {
				return err
			}
		}
		if err := t.write(v.Files); err != nil {
			return err
		}
	}

	return nil
}

// stripAll removes every marked region belonging to a variant being left out,
// and the markers of the ones that stay.
func stripAll(t *tree, tags map[string]bool) error {
	return t.walkText(func(_ string, src string) (string, error) {
		if !hasMarker(src) {
			return src, nil
		}

		return strip(src, tags)
	})
}

// generate runs what makes the generated code.
//
// Which steps there are is read from the tree rather than assumed, because the
// tree differs: `main` carries no protos at all, a variant may have taken the
// entity codegen away, and an app that deleted the UI has no TypeScript half.
// A step whose script is not there is a step this tree does not have.
//
// Running it is not a convenience. The rename rewrote the *.pb.go files too,
// and a compiled protobuf descriptor is a length-prefixed byte string with the
// proto package name inside it -- so replacing the old name with one of a
// different length leaves a descriptor whose prefixes say the old lengths. The
// result compiles and panics on the first init, with a slice bounds error a
// long way from anything anybody wrote.
//
// So the generated files are made again from the sources, which are text and
// were rewritten correctly.
func generate(root string, out io.Writer) error {
	here := func(p string) bool {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(p)))
		return err == nil
	}

	// Nothing to generate *from* is not a failure. `main` has no protos at all,
	// and a variant that took the sample away leaves the directory empty --
	// which buf reports as an error ("had no .proto files") rather than as
	// nothing to do, so the emptiness is checked here instead.
	if !anyProto(filepath.Join(root, "proto")) {
		fmt.Fprintln(out, "  there are no protos here yet, so there is nothing to generate")
		return nil
	}

	steps := []struct {
		needs string
		argv  []string
	}{
		{"buf.gen.svc.yaml", []string{"buf", "generate", "--template", "buf.gen.svc.yaml"}},
		{"scripts/gen-service.sh", []string{"./scripts/gen-service.sh"}},
		{"scripts/gen-go.sh", []string{"./scripts/gen-go.sh"}},
		{"scripts/gen-ent.sh", []string{"./scripts/gen-ent.sh"}},
	}
	for _, s := range steps {
		if !here(s.needs) {
			continue
		}
		if err := command(root, out, s.argv[0], s.argv[1:]...); err != nil {
			return fmt.Errorf("the renaming is done and the generation is not; run %q and the rest by hand: %w", strings.Join(s.argv, " "), err)
		}
	}

	if !here("scripts/gen-ui.sh") {
		return nil
	}

	// The TypeScript half needs the UI's own node_modules, so a fresh clone has
	// to install before it can be generated. Saying so is better than a failure
	// that reads as the rename having gone wrong.
	if !here("ui/node_modules/.bin/protoc-gen-es") {
		fmt.Fprintln(out, "  ui/node_modules is not installed, so ui/src/gen was not regenerated.")
		fmt.Fprintln(out, "  Before running the UI: cd ui && npm install && cd .. && ./scripts/gen-ui.sh")
		return nil
	}

	return command(root, out, "./scripts/gen-ui.sh")
}

// anyProto reports whether there is anything under `dir` for buf to read.
func anyProto(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // a missing directory is simply no protos
		}
		if !d.IsDir() && filepath.Ext(d.Name()) == ".proto" {
			found = true
			return filepath.SkipAll
		}

		return nil
	})

	return found
}

func command(dir string, out io.Writer, name string, args ...string) error {
	fmt.Fprintf(out, "  %s %s\n", name, strings.Join(args, " "))

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func indent(s string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(s, "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}

		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	return strings.TrimSuffix(b.String(), "\n")
}
