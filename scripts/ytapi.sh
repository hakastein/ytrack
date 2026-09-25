#!/usr/bin/env bash
# oapi-codegen выходит с 0 и на пакете, который не собирается, и на пустом ClientInterface:
# регенерацию проверяют утверждения о выхлопе, а не код возврата.
set -euo pipefail
cd "$(dirname "$0")/.."

readonly min_methods=335
readonly tree=$PWD

work=$(mktemp -d "${TMPDIR:-/tmp}/ytapi.XXXXXXXXXX")
trap 'rm -rf "$work"' EXIT
readonly copy=$work/copy

# Регенерация идёт в копии дерева вместе с незакоммиченными правками: само дерево
# не меняется ни при каком исходе.
mkdir "$copy"
git ls-files -z --cached --others --exclude-standard |
	while IFS= read -r -d '' path; do
		# --cached называет и отслеживаемые файлы, удалённые из дерева.
		if [[ -e $path || -L $path ]]; then
			printf '%s\0' "$path"
		fi
	done |
	tar --null -cf - -T - | tar -xf - -C "$copy"
cd "$copy"

# Без удаления не сработавшая директива оставила бы в копии выхлоп из дерева,
# и сравнение с деревом прошло бы.
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

# grep выходит с 2, когда каталога нет, и это провал, а не отсутствие строки.
stub_applied() {
	local code=0
	grep -rq ClientWithResponses internal/ytapi || code=$?
	[[ $code -eq 1 ]]
}

interface_floor() {
	local methods
	methods=$(go doc ./internal/ytapi ClientInterface | grep -cE '^[[:space:]]+[A-Z][A-Za-z0-9_]*\(ctx context\.Context') || true
	echo "ClientInterface: $methods"
	((methods >= min_methods))
}

# Каталог схем генерируется из той же спеки, что и internal/ytapi, и сверяется с деревом тем же утверждением.
outputs_match() {
	local code=0
	diff -rq "$tree/internal/ytapi" "$copy/internal/ytapi" || code=1
	diff -q "$tree/internal/youtrack/catalogue.gen.go" "$copy/internal/youtrack/catalogue.gen.go" || code=1
	return "$code"
}

check "go build ./internal/ytapi" go build ./internal/ytapi
check "выхлоп в дереве совпадает с тем, что дают входы" outputs_match
check "ClientWithResponses в выхлопе нет" stub_applied
check "ClientInterface не меньше $min_methods методов" interface_floor

# go run сводит любой ненулевой код к 1, а код 2 у проверки шва — отказ судить, а не утечка.
imports=2
if go build -o "$work/ytapi-imports" scripts/ytapi-imports.go; then
	imports=0
	"$work/ytapi-imports" || imports=$?
fi
if ((imports > 1)); then
	verdict "проверка шва не смогла судить" "$imports"
else
	verdict "ytapi виден только из адаптера" "$imports"
fi
exit "$failed"
