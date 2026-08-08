#!/usr/bin/env bash

set -o errexit
set -o pipefail
# set -o xtrace

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" # Directory where this script exists.
__root="$(cd "$(dirname "${__dir}")" && pwd)"         # Root directory of project.

SVC_DIR="${SVC_DIR:-${__root}/proto.svc}"
OUT_DIR="${OUT_DIR:-${__root}/proto}"

# Command used to merge a base proto with its overlay.
# Override with PROTOBUF_MERGE=protobuf-merge if the binary is on PATH.
#
# `go tool`, the way every other generator here is reached, and not
# `go run ...@latest`: the version is then pinned by go.mod like the rest of
# them. It was `@latest` once, and what that means is that the generation
# breaks on a day nobody changed anything -- which is what happened when a
# release of it started requiring a newer Go than go.mod asked for.
PROTOBUF_MERGE="${PROTOBUF_MERGE:-go tool github.com/protobuf-orm/protobuf-merge}"

# Merge every generated service body with its extension (if any) and emit the
# result under OUT_DIR, keeping the same package sub-directory layout.
#
#   proto.svc/go_app/user_svc.g.proto  +  proto.svc/go_app/user_svc.ext.proto
#     ->  proto/go_app/user_svc.proto
#
# A body without a matching *.ext.proto is emitted as-is. Imports that point at
# other service bodies (*_svc.g.proto) are rewritten to their merged names
# (*_svc.proto) so the output set stays self-consistent.
while IFS= read -r -d '' base; do
	rel="${base#"${SVC_DIR}/"}"          # e.g. app/user_svc.g.proto
	ext="${base%.g.proto}.ext.proto"     # e.g. .../user_svc.ext.proto
	out="${OUT_DIR}/${rel%.g.proto}.proto"

	mkdir -p "$(dirname "${out}")"

	if [ -f "${ext}" ]; then
		echo "merge : ${rel}  +  ${ext#"${SVC_DIR}/"}  ->  ${out#"${__root}/"}"
		${PROTOBUF_MERGE} "${base}" "${ext}" \
			| sed -E 's|(import ")([^"]*)_svc\.g\.proto(";)|\1\2_svc.proto\3|' >"${out}"
	else
		echo "copy  : ${rel}  ->  ${out#"${__root}/"}"
		sed -E 's|(import ")([^"]*)_svc\.g\.proto(";)|\1\2_svc.proto\3|' "${base}" >"${out}"
	fi
done < <(find "${SVC_DIR}" -type f -name '*_svc.g.proto' -print0)

echo "Done."
