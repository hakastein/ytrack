#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

readonly imports_cannot_judge=2
readonly tree=$PWD

work=$(mktemp -d "${TMPDIR:-/tmp}/ytapi.XXXXXXXXXX")
trap 'rm -rf "$work"' EXIT
readonly copy=$work/copy

existing_only() {
	while IFS= read -r -d '' path; do
		if [[ -e $path || -L $path ]]; then
			printf '%s\0' "$path"
		fi
	done
}

mkdir "$copy"
git ls-files -z --cached --others --exclude-standard | existing_only |
	tar --null -cf - -T - | tar -xf - -C "$copy"
cd "$copy"

rm -f internal/youtrack/catalogue.gen.go
if ! go generate ./...; then
	echo "FAIL  go generate"
	exit 1
fi

failed=0
verdict() {
	local name=$1 code=$2
	if ((code == 0)); then
		echo "ok    $name"
	else
		echo "FAIL  $name"
		failed=1
	fi
}

check() {
	local name=$1 code=0
	shift
	"$@" || code=$?
	verdict "$name" "$code"
}

check "каталог в дереве совпадает с тем, что даёт спецификация модуля" \
	diff -q "$tree/internal/youtrack/catalogue.gen.go" "$copy/internal/youtrack/catalogue.gen.go"

# go run сводит любой ненулевой код выхода к 1.
imports=$imports_cannot_judge
if go build -o "$work/ytapi-imports" scripts/ytapi-imports.go; then
	imports=0
	"$work/ytapi-imports" || imports=$?
fi
if ((imports >= imports_cannot_judge)); then
	verdict "проверка шва не смогла судить" "$imports"
else
	verdict "ytapi виден только из адаптера" "$imports"
fi
exit "$failed"
