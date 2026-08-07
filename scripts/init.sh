#!/usr/bin/env bash

set -o errexit
set -o pipefail

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(cd "$(dirname "${__dir}")" && pwd)"
__self="${BASH_SOURCE[0]}"

# Current template placeholders
CURRENT_MODULE="github.com/lesomnus/go-app"
CURRENT_ORG_REPO="lesomnus/go-app"
CURRENT_APP_KEBAB="go-app"
CURRENT_APP_UPPER="GO_APP"
CURRENT_APP_SNAKE="go_app" # proto package name and its directory (proto/go_app, proto.svc/go_app)

usage() {
	echo "Usage: $0 [--no-generate] <module-path> [app-name]"
	echo ""
	echo "  module-path   New Go module path (e.g. github.com/my-org/my-app)"
	echo "  app-name      Binary/service name (default: last segment of module-path)"
	echo ""
	echo "  --no-generate  Rename only. THE TREE WILL NOT RUN until the code"
	echo "                 generation below has been run by hand -- see the note"
	echo "                 where this script does it."
	echo ""
	echo "Example:"
	echo "  $0 github.com/acme/my-service"
	echo "  $0 github.com/acme/my-service svc"
}

GENERATE=1
if [ "${1:-}" = "--no-generate" ]; then
	GENERATE=0
	shift
fi

if [ $# -lt 1 ]; then
	usage >&2
	exit 1
fi

NEW_MODULE="$1"
APP_NAME="${2:-${NEW_MODULE##*/}}"

# org/repo from new module path (strip first component, e.g. "github.com/")
NEW_ORG_REPO="${NEW_MODULE#*/}"

# UPPER_CASE variant: replace hyphens with underscores, then uppercase
APP_NAME_UPPER="${APP_NAME//-/_}"
APP_NAME_UPPER="${APP_NAME_UPPER^^}"

# snake_case variant: lowercase of the UPPER_CASE variant (used as proto package name)
APP_NAME_SNAKE="${APP_NAME_UPPER,,}"

echo "Module   : $CURRENT_MODULE  →  $NEW_MODULE"
echo "Org/Repo : $CURRENT_ORG_REPO  →  $NEW_ORG_REPO"
echo "App      : $CURRENT_APP_KEBAB / $CURRENT_APP_UPPER  →  $APP_NAME / $APP_NAME_UPPER"
echo "Proto    : $CURRENT_APP_SNAKE  →  $APP_NAME_SNAKE"
echo ""

# Find all text source files, excluding this script itself
_find_files() {
	find "$__root" -type f ! -path "$__self" \( \
		-name "*.go"        \
		-o -name "go.mod"   \
		-o -name "*.proto"  \
		-o -name "*.sh"     \
		-o -name "*.yml"    \
		-o -name "*.yaml"   \
		-o -name "*.hcl"    \
		-o -name "*.json"   \
		-o -name "*.md"     \
		-o -name "Dockerfile"     \
		-o -name "Dockerfile.*"   \
		-o -name ".dockerignore"  \
	\)
}

# 1. Full module path (handles Go imports and https://github.com/... URLs via substring)
_find_files | xargs sed -i "s|${CURRENT_MODULE}|${NEW_MODULE}|g"

# 2. org/repo shorthand remaining after step 1 (ghcr.io image names, devcontainer name, etc.)
_find_files | xargs sed -i "s|${CURRENT_ORG_REPO}|${NEW_ORG_REPO}|g"

# 3. UPPER_CASE env var prefix — before snake/kebab replacement to avoid partial-match issues
_find_files | xargs sed -i "s|${CURRENT_APP_UPPER}|${APP_NAME_UPPER}|g"

# 4. snake_case proto package name and its import paths (package go_app; import "go_app/...")
_find_files | xargs sed -i "s|${CURRENT_APP_SNAKE}|${APP_NAME_SNAKE}|g"

# 5. kebab-case binary/service name (Dockerfile paths, root command Name, config file keys)
_find_files | xargs sed -i "s|${CURRENT_APP_KEBAB}|${APP_NAME}|g"

# 6. Rename config files (go-app.yaml / go-app.yml → <app-name>.yaml / <app-name>.yml)
for ext in yaml yml; do
	src="$__root/${CURRENT_APP_KEBAB}.${ext}"
	dst="$__root/${APP_NAME}.${ext}"
	if [ -f "$src" ] && [ "$src" != "$dst" ]; then
		mv "$src" "$dst"
		echo "Renamed: ${CURRENT_APP_KEBAB}.${ext}  →  ${APP_NAME}.${ext}"
	fi
done

# 7. Rename proto package directories (proto/go_app, proto.svc/go_app → .../<snake>)
if [ "$CURRENT_APP_SNAKE" != "$APP_NAME_SNAKE" ]; then
	while IFS= read -r -d '' dir; do
		dst="$(dirname "$dir")/${APP_NAME_SNAKE}"
		if [ "$dir" != "$dst" ]; then
			mv "$dir" "$dst"
			echo "Renamed: ${dir#"$__root/"}  →  ${dst#"$__root/"}"
		fi
	done < <(find "$__root" -depth -type d -name "$CURRENT_APP_SNAKE" -print0)
fi

# 8. Generate everything that is generated, again.
#
# This is not a convenience. The substitutions above rewrote the *.pb.go files
# too, and a compiled protobuf descriptor is a length-prefixed byte string with
# the proto package name inside it -- so replacing `go_app` with a name of a
# different length leaves a descriptor whose prefixes say the old lengths. The
# result compiles and panics on the first init, with a slice bounds error a long
# way from anything anybody wrote.
#
# So the generated files are made again from the sources, which are text and
# were rewritten correctly. Nothing here is optional; `--no-generate` exists for
# somebody who has to run these later or elsewhere, and leaves a tree that does
# not run until they do.
if [ "$GENERATE" -eq 1 ]; then
	echo ""
	echo "Generating..."

	if ! (
		cd "$__root" &&
			buf generate --template buf.gen.svc.yaml &&
			"$__dir/gen-service.sh" &&
			"$__dir/gen-go.sh" &&
			"$__dir/gen-ent.sh"
	); then
		echo "" >&2
		echo "The renaming is done and the code generation is not." >&2
		echo "The tree will not run until these have: " >&2
		echo "  buf generate --template buf.gen.svc.yaml" >&2
		echo "  ./scripts/gen-service.sh" >&2
		echo "  ./scripts/gen-go.sh" >&2
		echo "  ./scripts/gen-ent.sh" >&2
		exit 1
	fi

	# And the check that the whole of the above worked, which is worth the few
	# seconds: the failure this catches is one that only shows up at run time.
	echo ""
	echo "Building..."
	(cd "$__root" && go build ./...)
fi

echo ""
echo "Done."
if [ "$GENERATE" -eq 1 ]; then
	echo "Next: cd $__root && go test ./..."
else
	echo "Next: the four generation steps above, then go build ./..."
fi
