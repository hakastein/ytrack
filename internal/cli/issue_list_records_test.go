package cli_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func listedRecord(id string, fields []receivedField, keys ...string) string {
	members := []string{
		`"$type":"Issue"`,
		`"idReadable":` + strconv.Quote(id),
		`"summary":"summary of ` + id + `"`,
		`"created":1789035410875`,
		`"customFields":` + receivedFields(fields...),
	}
	return "{" + strings.Join(append(members, keys...), ",") + "}"
}

func namedFieldsOfTheDefault() []receivedField {
	return []receivedField{
		{name: namedState, valueType: "state", ordinal: "8", binding: "180-14", value: bundleElement("In Progress")},
		{name: namedType, valueType: "enum", ordinal: "1", binding: "180-15", value: bundleElement("Task")},
	}
}

func requireRecordsOnLines(t *testing.T, got outcome, count int) []string {
	t.Helper()
	const countLines, issuesKeyLine = 3, 1
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
	require.Len(t, lines, countLines+issuesKeyLine+count, "stdout: %q", got.stdout)
	recordLines := lines[countLines+issuesKeyLine:]
	for _, line := range recordLines {
		assert.True(t, strings.HasPrefix(line, "  - {"), "a record stands on more than its line: %q", line)
	}
	return recordLines
}
