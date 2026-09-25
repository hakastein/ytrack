#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

url=${YOUTRACK_URL:-http://localhost:8091}
dev_image_tag=$(sed -n 's/^YOUTRACK_VERSION=//p' dev/.env.example)

config=$(curl -fsS "$url/api/config?fields=version,build")
version=$(sed -n 's/.*"version":"\([^"]*\)".*/\1/p' <<<"$config")
build=$(sed -n 's/.*"build":"\([^"]*\)".*/\1/p' <<<"$config")
if [[ $version.$build != "$dev_image_tag" ]]; then
	echo "$url: YouTrack $version.$build, а в dev/.env.example запинен $dev_image_tag" >&2
	exit 1
fi

part=api/openapi.json.part
trap 'rm -f "$part"' EXIT
curl -fsS --create-dirs -o "$part" "$url/api/openapi.json"
mv "$part" api/openapi.json
