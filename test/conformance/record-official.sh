#!/usr/bin/env bash
# Records what the official devcontainers/cli makes of each case in
# testdata/official/, the fixtures official_test.go compares dcx with.
# Run it by hand (it needs Docker and network access) when a case is added
# or the reference CLI version changes, and review the diff:
#
#   test/conformance/record-official.sh            # every case
#   test/conformance/record-official.sh features   # one case
#
# The CLI runs in a container (no Node.js on the host); each case is copied
# to a temporary folder that is mounted at the same path, so the paths it
# reports are host paths, which record-official.js replaces with
# <workspace>. Per case it writes:
#
#   official.json                      install order, merged configuration
#   features.json                      devcontainer-feature.json by manifest digest
#   image-metadata.json                devcontainer.metadata by image reference
#   compose-config.json                docker compose config (compose cases)
#   .devcontainer/devcontainer-lock.json  `devcontainer upgrade` (feature cases)
set -euo pipefail

CLI_VERSION=0.90.0
IMAGE=dcx-official-cli:$CLI_VERSION

here=$(cd "$(dirname "$0")" && pwd)
cases_dir=$here/testdata/official

docker build --network host -q -t "$IMAGE" - >/dev/null <<EOF
FROM docker:29-cli
RUN apk add --no-cache nodejs npm && npm install -g @devcontainers/cli@$CLI_VERSION
EOF

if [ $# -eq 0 ]; then
	set -- $(cd "$cases_dir" && ls)
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for name in "$@"; do
	src=$cases_dir/$name
	[ -d "$src/.devcontainer" ] || { echo "no case $name" >&2; exit 1; }
	echo "recording $name" >&2
	ws=$tmp/$name
	mkdir -p "$ws"
	cp -R "$src/.devcontainer" "$ws/"
	rm -f "$ws/.devcontainer/devcontainer-lock.json"

	# As root (the CLI asks for the user's name); the files it writes are
	# handed back to the caller.
	docker run --rm --network host -e CLI_VERSION="$CLI_VERSION" \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$ws:$ws" -v "$here/record-official.js:/record-official.js:ro" \
		-w "$ws" --entrypoint sh "$IMAGE" -c \
		'status=0; node /record-official.js "$1" || status=$?; chown -R "$2" "$1"; exit $status' \
		sh "$ws" "$(id -u):$(id -g)"

	rm -f "$src/official.json" "$src/features.json" "$src/image-metadata.json" "$src/compose-config.json" "$src/.devcontainer/devcontainer-lock.json"
	for f in official.json features.json image-metadata.json compose-config.json; do
		if [ -f "$ws/$f" ]; then cp "$ws/$f" "$src/$f"; fi
	done
	if [ -f "$ws/.devcontainer/devcontainer-lock.json" ]; then
		cp "$ws/.devcontainer/devcontainer-lock.json" "$src/.devcontainer/"
	fi
done
