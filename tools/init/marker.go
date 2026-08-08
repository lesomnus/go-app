package main

import (
	"bufio"
	"fmt"
	"strings"
)

// A marker says that the lines it covers belong to one part of the template,
// and go away with it.
//
// The map of what is coupled to what lives **in the source**, at the place the
// coupling is, rather than in a document beside it. A document goes out of date
// the first time somebody moves a field and does not think to look; a marker
// cannot, because it is the thing being moved. `grep -rn "template:db"` is the
// whole of the map, and it is always true.
//
// Two shapes. A block, for a region:
//
//	//template:begin db
//	Db DbConfig
//	//template:end db
//
// and a line, for one line:
//
//	Db DbConfig //template:db
//
// The comment leader is whatever the file uses -- `//`, `#`, `<!--` -- so the
// same syntax works in Go, YAML, Dockerfiles and Markdown. That matters more
// than it sounds: the coupling this exists for is spread across all of them.
//
// # Saying what replaces something
//
// A tag spelled `no-<variant>` is the other side of the same coin: it is kept
// only when that variant is left out.
//
//	//template:begin db
//	go watchDb(ctx, health_srv, db, dbCheckInterval)
//	//template:end db
//	//template:begin no-db
//	health_srv.SetServingStatus(ServiceReadiness, ...)
//	//template:end no-db
//
// Without it, removing a part would often leave a hole rather than a working
// tree -- something has to be *there* instead. Keeping both halves next to each
// other is also the only way a reader can see what the variant actually
// changes.
const markerWord = "template:"

// negation is what marks a tag as the other side of a variant.
const negation = "no-"

// removes reports whether a marked region goes, given the variants being left
// out.
func removes(tag string, tags map[string]bool) bool {
	if inverse, ok := strings.CutPrefix(tag, negation); ok {
		// Kept when the variant is left out, so removed when it stays.
		return !tags[inverse]
	}

	return tags[tag]
}

type markerKind int

const (
	markerNone markerKind = iota
	markerBegin
	markerEnd
	markerLine
)

// leaders are the comment openers a marker may hide behind.
var leaders = []string{"//", "#", "<!--", "--"}

// parseMarker reads what a line says about a variant, if anything.
func parseMarker(line string) (markerKind, string) {
	i := strings.Index(line, markerWord)
	if i < 0 {
		return markerNone, ""
	}

	// What comes after the word: "begin db", "end db" or "db".
	rest := strings.TrimSpace(line[i+len(markerWord):])
	rest = strings.TrimSuffix(rest, "-->")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return markerNone, ""
	}

	// And what comes before it has to be a comment leader, so that the word
	// appearing in prose or in a string is not read as a marker.
	before := strings.TrimSpace(line[:i])
	isBlock := false
	for _, l := range leaders {
		if strings.HasSuffix(before, l) {
			isBlock = before == l
			goto found
		}
	}

	return markerNone, ""

found:
	switch {
	case strings.HasPrefix(rest, "begin "):
		if !isBlock {
			// A begin that shares its line with code has nothing to open.
			return markerNone, ""
		}

		return markerBegin, strings.TrimSpace(strings.TrimPrefix(rest, "begin "))

	case strings.HasPrefix(rest, "end "):
		if !isBlock {
			return markerNone, ""
		}

		return markerEnd, strings.TrimSpace(strings.TrimPrefix(rest, "end "))

	default:
		return markerLine, rest
	}
}

// strip removes every line covered by a marker for one of `tags`, and the
// markers of the tags that stay.
//
// The markers of tags that stay are removed as well, and that is deliberate:
// the app somebody ends up with should not carry the template's bookkeeping.
// What it costs is that the tool cannot be run twice, which is already true of
// the rename.
//
// Blocks may nest, and a block being removed takes whatever is inside it
// without reading it. That is not generality for its own sake: the
// documentation of this format contains examples of it, and has to be able to
// be inside a block of its own.
func strip(src string, tags map[string]bool) (string, error) {
	var (
		out   strings.Builder
		open  []string
		lines []int

		// cut is the depth at which removal started, and -1 when nothing is
		// being removed. Everything below it goes, markers and all -- which is
		// what lets a block that is on its way out contain marker syntax of its
		// own, as the documentation of this very format does.
		cut     = -1
		lineNum int
	)

	// Preserved so that a file with no trailing newline does not grow one.
	trailing := strings.HasSuffix(src, "\n")

	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		lineNum++

		switch kind, tag := parseMarker(line); kind {
		case markerBegin:
			open = append(open, tag)
			lines = append(lines, lineNum)
			if cut < 0 && removes(tag, tags) {
				cut = len(open) - 1
			}

		case markerEnd:
			if len(open) == 0 {
				return "", fmt.Errorf("line %d: %q ends without beginning", lineNum, tag)
			}
			if last := open[len(open)-1]; last != tag {
				return "", fmt.Errorf("line %d: %q ends while %q is open", lineNum, tag, last)
			}
			if cut == len(open)-1 {
				cut = -1
			}
			open = open[:len(open)-1]
			lines = lines[:len(lines)-1]

		case markerLine:
			if cut >= 0 || removes(tag, tags) {
				continue
			}

			// The tag stays, so the line stays -- without its marker.
			out.WriteString(strings.TrimRight(dropMarker(line), " \t"))
			out.WriteString("\n")

		default:
			if cut >= 0 {
				continue
			}

			out.WriteString(line)
			out.WriteString("\n")
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if len(open) > 0 {
		return "", fmt.Errorf("line %d: %q begins and never ends", lines[0], open[0])
	}

	s := out.String()
	if !trailing {
		s = strings.TrimSuffix(s, "\n")
	}

	return s, nil
}

// dropMarker removes a trailing line marker and the comment leader it hides
// behind, leaving the code that shared the line with it.
func dropMarker(line string) string {
	i := strings.Index(line, markerWord)
	if i < 0 {
		return line
	}

	head := line[:i]
	for _, l := range leaders {
		if strings.HasSuffix(strings.TrimSpace(head), l) {
			return strings.TrimSuffix(strings.TrimRight(head, " \t"), l)
		}
	}

	return line
}

// hasMarker reports whether `src` says anything about a variant at all, which
// is how the walk skips the files that have nothing to strip.
func hasMarker(src string) bool {
	return strings.Contains(src, markerWord)
}
