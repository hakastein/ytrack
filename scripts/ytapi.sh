#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

readonly min_methods=335
readonly grep_no_match=1
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

rm -rf internal/ytapi internal/youtrack/catalogue.gen.go
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

stub_applied() {
	local code=0
	grep -rq ClientWithResponses internal/ytapi || code=$?
	[[ $code -eq $grep_no_match ]]
}

interface_floor() {
	local methods
	methods=$(go doc ./internal/ytapi ClientInterface | grep -cE '^[[:space:]]+[A-Z][A-Za-z0-9_]*\(ctx context\.Context') || true
	echo "ClientInterface: $methods"
	((methods >= min_methods))
}

outputs_match() {
	local code=0
	diff -rq "$tree/internal/ytapi" "$copy/internal/ytapi" || code=1
	diff -q "$tree/internal/youtrack/catalogue.gen.go" "$copy/internal/youtrack/catalogue.gen.go" || code=1
	return "$code"
}

# oapi-codegen выходит с 0 и на несобираемом пакете, и на пустом ClientInterface.
check "go build ./internal/ytapi" go build ./internal/ytapi
check "выхлоп в дереве совпадает с тем, что дают входы" outputs_match
check "ClientWithResponses в выхлопе нет" stub_applied
check "ClientInterface не меньше $min_methods методов" interface_floor

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
