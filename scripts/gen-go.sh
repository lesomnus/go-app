#!/usr/bin/env bash

set -o errexit
set -o pipefail
# set -o xtrace

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" # Directory where this script exists.
__root="$(cd "$(dirname "${__dir}")" && pwd)"         # Root directory of project.

MODULE="${MODULE:-github.com/lesomnus/go-app}"

# Staging directory `buf generate` writes into. Its content is moved into the
# repository by this script and the directory itself is removed.
GEN_DIR="${GEN_DIR:-${__root}/.gen}"

# The plugins emit paths relative to the `go_package` of each proto file, which
# is the module root, so the messages, the gRPC stubs and the query helpers land
# at the top level of the staging directory. They cannot stay at the repository
# root, which is `package main`, so they are moved into PKG_DIR and every import
# of the module root is rewritten to point there.
#
# Everything else (internal/ent, server/bare, ...) is already rooted at the
# repository root and is moved as-is.
PKG_DIR="${PKG_DIR%/}"
PKG_DIR="${PKG_DIR:-go_app}"
case "${PKG_DIR}" in
"." | ".." | /*)
	echo "PKG_DIR must be a directory below the repository root" >&2
	exit 1
	;;
esac

# Go package name of the generated messages. protoc-gen-go derives it from the
# last segment of `go_package`, e.g. "go-app" becomes "go_app".
SRC_PKG="$(basename "${MODULE}" | sed 's|[^a-zA-Z0-9_]|_|g')"
PKG_NAME="${PKG_NAME:-${SRC_PKG}}"

echo "module : ${MODULE}"
echo "package: ${PKG_DIR}  (package ${PKG_NAME})"
echo ""

rm -rf "${GEN_DIR}"
(cd "${__root}" && buf generate)

# Drop the previously generated files so that renamed or removed entities do not
# leave stale code behind. The ent runtime (see `scripts/gen-ent.sh`) is left
# alone; only the files owned by this pipeline are removed.
rm -rf "${__root:?}/${PKG_DIR}" "${__root}/internal/ent/schema"
for d in "${__root}/internal/ent" "${__root}/server/bare"; do
	if [ -d "${d}" ]; then
		find "${d}" -maxdepth 1 -type f -name '*.g.go' -delete
	fi
done

# Move the messages and their helpers into PKG_DIR.
mkdir -p "${__root}/${PKG_DIR}"
find "${GEN_DIR}" -maxdepth 1 -type f -name '*.go' -exec mv {} "${__root}/${PKG_DIR}/" \;

# Move the remaining trees as-is.
for d in "${GEN_DIR}"/*/; do
	[ -d "${d}" ] || continue

	name="$(basename "${d}")"
	mkdir -p "${__root}/${name}"
	cp -r "${d}." "${__root}/${name}/"
	echo "move  : ${name}/"
done

rm -rf "${GEN_DIR}"

# Collect the generated trees for the rewrites below.
_targets() {
	local ds=("${__root}/${PKG_DIR}")
	for d in "${__root}/internal/ent" "${__root}/server/bare"; do
		if [ -d "${d}" ]; then
			ds+=("${d}")
		fi
	done

	find "${ds[@]}" -type f -name '*.go' -print0
}

# Rewrite the import of the module root, which is where the plugins think the
# messages are, to PKG_DIR. Imports that only share the prefix, like
# ".../internal/ent", are not affected since the closing quote is part of the
# pattern.
_targets | xargs -0 -r sed -i "s|\"${MODULE}\"|\"${MODULE}/${PKG_DIR}\"|g"

# Rename the package if a different name is requested. Import aliases in the
# other generated files keep working since they are explicit.
if [ "${PKG_NAME}" != "${SRC_PKG}" ]; then
	find "${__root}/${PKG_DIR}" -type f -name '*.go' \
		-exec sed -i "s|^package ${SRC_PKG}$|package ${PKG_NAME}|" {} +
fi

echo ""
echo "Done."
echo "Next: ${__dir#"${__root}/"}/gen-ent.sh"
