package main

import (
	"fmt"
	"io/fs"
)

// A variant is a part of the template that can be left out.
//
// # Why there are none in this file
//
// This repository keeps a branch per kind of app, and they do not have the same
// parts to leave out: `main` is a CLI with no server in it at all, so there is
// no database half there to remove.
//
// If the list lived here it would differ per branch, which would put the
// difference in a shared file -- the one place every merge from `main` into a
// `kind/*` branch is guaranteed to land. So the list starts empty and each
// branch adds its own in a file of its own (`variant_db.go` on `kind/server`),
// which is a file that simply does not exist on the branches without it.
// Nothing has to be reconciled, this file is identical everywhere, and `--help`
// lists what the branch you are on actually has.
//
// # What a variant is made of
//
//   - Paths, for whole files and directories. Named there rather than marked,
//     because a file that exists only to serve one part of the template has
//     nothing to mark.
//   - A tag, for the places inside the files that stay. Those are marked at the
//     coupling itself; see marker.go.
//   - Files, for the rare case where something has to be *replaced*.
type variant struct {
	// Tag is what its markers say, and is the flag name without `--no-`.
	Tag string

	// Brief is one line, for `--help`.
	Brief string

	// Note is printed after the tool has run, and says what somebody is now
	// holding. A variant that removes the sample leaves questions the README
	// cannot answer generically.
	Note string

	// Paths go entirely. A directory takes what is in it.
	Paths []string

	// Files are written over the tree once Paths are gone, and are nil for a
	// variant that only removes.
	//
	// A marker cannot do this job. The template has to compile with every
	// marker still in it, so a marked region can never be a second declaration
	// of something -- two `App` components or two `connect` functions do not
	// both compile, whichever one is about to be deleted.
	//
	// It is a cost worth naming: these are files kept working alongside the
	// ones they replace, and CI builds the variant precisely so that they are.
	// Keep them few. Everything that can be a marker should be one.
	Files fs.FS
}

// variants is every part that can be left out, by flag name. What is in it is
// whatever this branch registered; see the note above.
var variants = map[string]variant{}

// register adds a variant, and is called from the `init` of the file that
// defines it.
func register(name string, v variant) {
	if _, dup := variants[name]; dup {
		panic(fmt.Sprintf("init: two variants are called %q", name))
	}

	variants[name] = v
}

// selfTag marks what belongs to the template rather than to any app made from
// it: this tool, and the CI job that checks the variants still build.
//
// It goes whenever the tool removes itself, which is always unless somebody
// asked otherwise -- there is no app for which `go run ./tools/init` is a thing
// that exists, so a CI job calling it would fail on the first push.
const selfTag = "self"

// tagsOf is the set of tags to strip for the variants being left out.
func tagsOf(left []string, self bool) map[string]bool {
	tags := make(map[string]bool, len(left)+1)
	for _, name := range left {
		tags[variants[name].Tag] = true
	}
	if self {
		tags[selfTag] = true
	}

	return tags
}
