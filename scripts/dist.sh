#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

readonly version=${1:?"версия: v<ГГ>.<М>.<Д>.<номер запуска CI>"}
readonly targets=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64)

rm -rf dist
mkdir dist
for target in "${targets[@]}"; do
	goos=${target%/*}
	goarch=${target#*/}
	name=ytrack-$goos-$goarch
	if [[ $goos == windows ]]; then
		name+=.exe
	fi
	CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
		go build -trimpath -ldflags "-X main.version=$version" -o "dist/$name" ./cmd/ytrack
done
(cd dist && sha256sum ytrack-* >SHA256SUMS)
