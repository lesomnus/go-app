package core

import (
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AliasMaxLen is the longest an alias can be.
const AliasMaxLen = 63

// aliasPattern describes a slug: lowercase alphanumerics in groups separated by
// a single hyphen.
var aliasPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ParseAlias normalizes `v` into an alias, or reports why it cannot be one.
// Surrounding spaces are dropped and the case is folded, so "  Acme " and
// "acme" name the same entity.
func ParseAlias(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case v == "":
		return "", status.Error(codes.InvalidArgument, "alias: must not be empty")
	case len(v) > AliasMaxLen:
		return "", status.Errorf(codes.InvalidArgument, "alias: must be at most %d characters", AliasMaxLen)
	case !aliasPattern.MatchString(v):
		return "", status.Error(codes.InvalidArgument, "alias: must be alphanumerics joined by a hyphen, such as \"acme-corp\"")
	}

	return v, nil
}
