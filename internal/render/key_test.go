package render_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/render"
)

func TestCheckKeyTakesANameOfLettersDigitsUnderscoresAndDollars(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"idReadable", "upstream_status", "$type", "_", "$", "az", "AZ", "a0", "_9"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, render.CheckKey(key))
		})
	}
}

func TestCheckKeyRefusesWhatIsNotAName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
	}{
		{name: "empty", key: ""},
		{name: "digit first", key: "1a"},
		{name: "only a digit", key: "9"},
		{name: "dash", key: "a-b"},
		{name: "space", key: "a b"},
		{name: "dot", key: "a.b"},
		{name: "quote", key: `"a"`},
		{name: "character before the digits", key: "a/"},
		{name: "character after the digits", key: "a:"},
		{name: "character before the capitals", key: "a@"},
		{name: "character after the capitals", key: "a["},
		{name: "character before the small letters", key: "a`"},
		{name: "character after the small letters", key: "a{"},
		{name: "line feed", key: "a\n"},
		{name: "tilde", key: "~"},
		{name: "letter beyond ASCII", key: "ключ"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Error(t, render.CheckKey(tc.key))
		})
	}
}

func TestCheckKeyRefusesAWordYAMLReadsAsABoolOrNull(t *testing.T) {
	t.Parallel()
	words := []string{
		"null", "Null", "NULL",
		"true", "True", "TRUE", "false", "False", "FALSE",
		"yes", "Yes", "YES", "no", "No", "NO",
		"on", "On", "ON", "off", "Off", "OFF",
		"y", "Y", "n", "N",
	}
	for _, key := range words {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			assert.Error(t, render.CheckKey(key))
		})
	}
}

func TestYAMLPrintsOwnKeysBareAndKeysFromDataQuoted(t *testing.T) {
	t.Parallel()
	first := render.NewString("First")
	tests := []struct {
		name     string
		document *render.Node
		want     string
	}{
		{name: "own key", document: render.NewMap(render.Pair{Key: "idReadable", Value: first}), want: lines(`idReadable: "First"`)},
		{name: "key from data", document: render.NewMap(render.FromData("State", first)), want: lines(`"State": "First"`)},
		{name: "key from data with a space", document: render.NewMap(render.FromData("First field", first)), want: lines(`"First field": "First"`)},
		{name: "key from data that reads as null", document: render.NewMap(render.FromData("null", first)), want: lines(`"null": "First"`)},
		{name: "empty key from data", document: render.NewMap(render.FromData("", first)), want: lines(`"": "First"`)},
		{name: "key from data beyond ASCII", document: render.NewMap(render.FromData("Статус", first)), want: lines(`"Статус": "First"`)},
		{name: "key from data YAML must escape", document: render.NewMap(render.FromData("a\"b\\c\nd", first)), want: lines(`"a\"b\\c\nd": "First"`)},
		{
			name:     "block under a key from data",
			document: render.NewMap(render.FromData("depends on", render.NewList(render.NewMap(render.Pair{Key: "idReadable", Value: first})))),
			want:     lines(`"depends on":`, `  - {idReadable: "First"}`),
		},
		{
			name:     "keys from data that differ in case",
			document: render.NewMap(render.FromData("State", first), render.FromData("state", render.NewString("Second"))),
			want:     lines(`"State": "First"`, `"state": "Second"`),
		},
		{
			name: "one key from data in two mappings",
			document: render.NewMap(
				render.Pair{Key: "first", Value: render.NewMap(render.FromData("State", first))},
				render.Pair{Key: "second", Value: render.NewMap(render.FromData("State", render.NewString("Second")))},
			),
			want: lines(`first:`, `  "State": "First"`, `second:`, `  "State": "Second"`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rendered(t, tc.document))
		})
	}
}

func TestYAMLRefusesAKeyItCannotPrint(t *testing.T) {
	t.Parallel()
	first, second := render.NewString("First"), render.NewString("Second")
	record := func(pairs ...render.Pair) *render.Node {
		return render.NewMap(render.Pair{Key: "records", Value: render.NewList(render.NewMap(pairs...))})
	}
	tests := []struct {
		name     string
		document *render.Node
	}{
		{name: "own key that is not a name", document: render.NewMap(render.Pair{Key: "First field", Value: first})},
		{name: "own key that reads as a bool", document: render.NewMap(render.Pair{Key: "yes", Value: first})},
		{name: "own key that is not a name in a nested mapping", document: render.NewMap(render.Pair{Key: "outer", Value: render.NewMap(render.Pair{Key: "a-b", Value: first})})},
		{name: "own key that is not a name in a record", document: record(render.Pair{Key: "a-b", Value: first})},
		{name: "own key that is not a name in a record with text", document: record(render.Pair{Key: "a-b", Value: render.NewText("First")})},
		{name: "two own keys", document: render.NewMap(render.Pair{Key: "name", Value: first}, render.Pair{Key: "name", Value: second})},
		{name: "two keys from data", document: render.NewMap(render.FromData("State", first), render.FromData("State", second))},
		{name: "own key and key from data", document: render.NewMap(render.Pair{Key: "name", Value: first}, render.FromData("name", second))},
		{name: "two keys from data in a nested mapping", document: render.NewMap(render.Pair{Key: "outer", Value: render.NewMap(render.FromData("State", first), render.FromData("State", second))})},
		{name: "two keys from data in a record", document: record(render.FromData("State", first), render.FromData("State", second))},
		{name: "two keys from data in a record with text", document: record(render.FromData("State", first), render.FromData("State", second), render.Pair{Key: "text", Value: render.NewText("First")})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refused(t, tc.document)
		})
	}
}
