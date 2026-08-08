#!/usr/bin/env bash

set -o errexit
set -o pipefail

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(cd "$(dirname "${__dir}")" && pwd)"

# The TypeScript half of the contract: the same protos, read by protoc-gen-es
# into `ui/src/gen`.
#
# The plugin comes out of the UI's own node_modules rather than off PATH, so
# that the version generating the code is the version the app is built against
# -- the generated files import from `@bufbuild/protobuf`, and a plugin newer
# than the runtime emits calls the runtime does not have.
cd "${__root}"

if [ ! -x ui/node_modules/.bin/protoc-gen-es ]; then
	echo "ui/node_modules is not installed; run: cd ui && npm install" >&2
	exit 1
fi

rm -rf ui/src/gen
buf generate --template buf.gen.ui.yaml

echo ""
echo "Done."
