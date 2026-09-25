package cli_test

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

type scalarRow struct {
	name      string
	valueType string
	sent      string
}

func scalarRows() []scalarRow {
	return []scalarRow{
		{name: "Плановая дата решения", valueType: "date", sent: "DateIssueCustomField"},
		{name: "Дата начала работы", valueType: "date and time", sent: "SimpleIssueCustomField"},
		{name: "Порядок реализации", valueType: "integer", sent: "SimpleIssueCustomField"},
		{name: "Коэффициент", valueType: "float", sent: "SimpleIssueCustomField"},
		{name: "Внешний номер", valueType: "string", sent: "SimpleIssueCustomField"},
		{name: "Примечание", valueType: "text", sent: "TextIssueCustomField"},
		{name: "Оценка", valueType: "period", sent: "PeriodIssueCustomField"},
	}
}

func (r scalarRow) binding() string {
	return "187-" + strconv.Itoa(len(r.name))
}

func scalarProject() string {
	fields := []writableField{{id: "180-15", name: "Type", valueType: "enum", canBeEmpty: true}}
	for _, row := range scalarRows() {
		fields = append(fields, writableField{id: row.binding(), kind: kindOfBinding(row.valueType),
			name: row.name, valueType: row.valueType, canBeEmpty: true})
	}
	return projectResponse(fields...)
}

func (r scalarRow) received(value string) receivedField {
	return receivedField{name: r.name, valueType: r.valueType, ordinal: strconv.Itoa(len(r.name)),
		binding: r.binding(), value: value}
}

func scalarRowNamed(t *testing.T, name string) scalarRow {
	t.Helper()
	for _, row := range scalarRows() {
		if row.name == name {
			return row
		}
	}
	require.FailNow(t, "no row of the table holds "+name)
	return scalarRow{}
}

func periodValue(minutes int, isoDuration string) string {
	return fmt.Sprintf(`{"$type":"PeriodValue","minutes":%d,"id":%q,"presentation":"%dм"}`,
		minutes, isoDuration, minutes)
}

func textValue(text string) string {
	renderedHTML := "<div>" + text + "</div>"
	return `{"$type":"TextFieldValue","id":"text","text":` + asJSON(text) +
		`,"markdownText":` + asJSON(renderedHTML) + `}`
}

func withValidUTF8(text string) string {
	return strings.ToValidUTF8(text, string(utf8.RuneError))
}

type invalidField struct {
	field string
	value any
}

func requireInvalidFields(t *testing.T, found faultDocument, want []invalidField) {
	t.Helper()
	entries, ok := detailNamed(t, found, "invalid").([]any)
	require.True(t, ok, "invalid: %v", found.details)
	require.Len(t, entries, len(want))
	for at, entry := range entries {
		pairs, ok := entry.([]detail)
		require.True(t, ok, "invalid[%d]: %v", at, entry)
		require.Len(t, pairs, 3)
		assert.Equal(t, detail{"field", want[at].field}, pairs[0])
		assert.Equal(t, detail{"value", want[at].value}, pairs[1])
		assert.Equal(t, "reason", pairs[2].key)
		assert.NotEmpty(t, pairs[2].value)
	}
}

func TestIssueCreateRefusesAScalarValueTheServerWouldRewrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		field        string
		given        string
		shownInError string
	}{
		{name: "a period of days", field: "Оценка", given: "P1D"},
		{name: "a period of a fraction of an hour", field: "Оценка", given: "PT1.5H"},
		{name: "a period of seconds", field: "Оценка", given: "PT30S"},
		{name: "a period in lower case", field: "Оценка", given: "pt1h"},
		{name: "a period of no part at all", field: "Оценка", given: "PT"},
		{name: "a period of hours with no count before the mark", field: "Оценка", given: "PTH"},
		{name: "a period of minutes with no count before the mark", field: "Оценка", given: "PTM"},
		{name: "a period of nothing at all", field: "Оценка", given: ""},
		{name: "a date of nothing at all", field: "Плановая дата решения", given: ""},
		{name: "a period past what the field holds", field: "Оценка", given: "PT2147483648M"},
		{name: "a date written the way a human writes one", field: "Плановая дата решения", given: "16.09.2026"},
		{name: "a date carrying a moment of the day", field: "Плановая дата решения",
			given: "2026-09-16T00:00:00Z"},
		{name: "a moment with no offset from UTC", field: "Дата начала работы", given: "2026-08-31T00:00:00"},
		{name: "a moment finer than a millisecond", field: "Дата начала работы",
			given: "2026-08-31T00:00:00.0001Z"},
		{name: "a whole number that is a fraction", field: "Порядок реализации", given: "2.5"},
		{name: "a whole number past what the field holds", field: "Порядок реализации", given: "2147483648"},
		{name: "a whole number written in hexadecimal", field: "Порядок реализации", given: "0x10"},
		{name: "a number that is not one", field: "Коэффициент", given: "NaN"},
		{name: "a number past every number", field: "Коэффициент", given: "Inf"},
		{name: "a number written in hexadecimal", field: "Коэффициент", given: "0x1p-2"},
		{name: "a number written with a comma", field: "Коэффициент", given: "1,5"},
		{name: "a string that begins with a space", field: "Внешний номер", given: " EXT-1"},
		{name: "a string that ends with a tab", field: "Внешний номер", given: "EXT-1\t"},
		{name: "a string holding a line separator", field: "Внешний номер", given: "a\xe2\x80\xa8b"},
		{name: "a string holding a NEL", field: "Внешний номер", given: "a\xc2\x85b"},
		{name: "a string that is no UTF-8", field: "Внешний номер", given: "a\xffb",
			shownInError: withValidUTF8("a\xffb")},
		{name: "a text that is no UTF-8", field: "Примечание", given: "a\xffb",
			shownInError: withValidUTF8("a\xffb")},
		{name: "a text of nothing at all", field: "Примечание", given: ""},
		{name: "a name of nothing at all", field: "Type", given: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creating(t, fake.JSON(http.StatusOK, scalarProject()), noCreation(t))

			got := runWith(t, server.Env(), "issue", "create", "DEV", "--summary", "x",
				"--field", tc.field+"="+tc.given)

			found := requireFault(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Equal(t, []detail{
				{"request", writeMetadataRequest(server.URL, "DEV")},
				{"project", "DEV"},
			}, found.details[:2])
			shown := cmp.Or(tc.shownInError, tc.given)
			requireInvalidFields(t, found, []invalidField{{field: tc.field, value: shown}})
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestIssueCreateNamesEveryValueItCannotSendAtOnce(t *testing.T) {
	t.Parallel()
	refusedInProjectOrder := []struct{ field, given string }{
		{field: "Type", given: ""},
		{field: "Плановая дата решения", given: "16.09.2026"},
		{field: "Дата начала работы", given: "2026-08-31T00:00:00"},
		{field: "Порядок реализации", given: "2.5"},
		{field: "Коэффициент", given: "1,5"},
		{field: "Внешний номер", given: " EXT-1"},
		{field: "Примечание", given: ""},
		{field: "Оценка", given: "P1D"},
	}
	argv := []string{"issue", "create", "DEV", "--summary", "x"}
	for _, row := range slices.Backward(refusedInProjectOrder) {
		argv = append(argv, "--field", row.field+"="+row.given)
	}
	server := creating(t, fake.JSON(http.StatusOK, scalarProject()), noCreation(t))

	got := runWith(t, server.Env(), argv...)

	want := make([]invalidField, 0, len(refusedInProjectOrder))
	for _, row := range refusedInProjectOrder {
		want = append(want, invalidField{field: row.field, value: row.given})
	}
	found := requireFault(t, got)
	assert.Equal(t, "bad_usage", found.code)
	requireInvalidFields(t, found, want)
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestIssueCreateWritesAScalarValueAsItsFieldTypeExpects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		field    string
		given    string
		sentJSON string
		received string
	}{
		{name: "a period of hours and minutes", field: "Оценка", given: "PT1H30M", sentJSON: `{"minutes":90}`,
			received: periodValue(90, "PT1H30M")},
		{name: "a period of no time at all", field: "Оценка", given: "PT0M", sentJSON: `{"minutes":0}`,
			received: periodValue(0, "PT0S")},
		{name: "a period of minutes alone", field: "Оценка", given: "PT90M", sentJSON: `{"minutes":90}`,
			received: periodValue(90, "PT1H30M")},
		{name: "a day, which the server keeps at noon UTC", field: "Плановая дата решения", given: "2026-09-16",
			sentJSON: "1789560000000", received: "1789560000000"},
		{name: "a moment in an offset of its own", field: "Дата начала работы",
			given: "2026-08-31T03:00:00.123+03:00", sentJSON: "1788134400123", received: "1788134400123"},
		{name: "a whole number written with leading zeroes", field: "Порядок реализации", given: "007",
			sentJSON: "7", received: "7"},
		{name: "a number written with an exponent", field: "Коэффициент", given: "1e3", sentJSON: "1000",
			received: "1000"},
		{name: "a string", field: "Внешний номер", given: "EXT-1", sentJSON: `"EXT-1"`, received: `"EXT-1"`},
		{name: "a text holding a carriage return and a CRLF", field: "Примечание",
			given: "первая\r\nвторая\rтретья", sentJSON: `{"text":"первая\r\nвторая\rтретья"}`,
			received: textValue("первая\r\nвторая\rтретья")},
		{name: "a text of two lines", field: "Примечание", given: "a\nb", sentJSON: `{"text":"a\nb"}`,
			received: textValue("a\nb")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := scalarRowNamed(t, tc.field)
			held := receivedFields(row.received(tc.received))
			server := creating(t, fake.JSON(http.StatusOK, scalarProject()),
				fake.JSON(http.StatusOK, createdIssueWith("DEV-7", "x", "null", held)))

			got := runWith(t, server.Env(), "issue", "create", "DEV", "--summary", "x",
				"--field", tc.field+"="+tc.given)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			want := `{"project":{"id":"0-1"},"summary":"x","customFields":[{"$type":` + strconv.Quote(row.sent) +
				`,"name":` + strconv.Quote(tc.field) + `,"value":` + tc.sentJSON + `}]}`
			assert.JSONEq(t, want, server.Bodies()[1])
		})
	}
}

func TestIssueCreateChecksAScalarResponseAgainstTheValueItWrote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		field    string
		given    string
		received string
		mismatch []detail
	}{
		{
			name: "a day the server keeps at noon of that day", field: "Плановая дата решения",
			given: "2026-09-16", received: "1789560000000",
		},
		{
			name: "a day the server keeps at another day", field: "Плановая дата решения", given: "2026-09-16",
			received: "1789646400000",
			mismatch: []detail{{"field", "Плановая дата решения"}, {"expected", "2026-09-16"}, {"actual", "2026-09-17"}},
		},
		{
			name: "a moment the server keeps in UTC", field: "Дата начала работы",
			given: "2026-08-31T03:00:00.123+03:00", received: "1788134400123",
		},
		{
			name: "a moment the server keeps a millisecond off", field: "Дата начала работы",
			given: "2026-08-31T03:00:00.123+03:00", received: "1788134400124",
			mismatch: []detail{{"field", "Дата начала работы"},
				{"expected", "2026-08-31T03:00:00.123+03:00"}, {"actual", "2026-08-31T00:00:00.124Z"}},
		},
		{
			name: "a number the server keeps to the digits a float64 holds", field: "Коэффициент",
			given: "123456789.123456789", received: "123456789.12345679",
		},
		{
			name: "a number the server keeps as another", field: "Коэффициент", given: "1.5", received: "1.75",
			mismatch: []detail{{"field", "Коэффициент"}, {"expected", "1.5"}, {"actual", "1.75"}},
		},
		{
			name: "a period the server kept nothing of", field: "Оценка", given: "PT1H30M", received: "null",
			mismatch: []detail{{"field", "Оценка"}, {"expected", "PT1H30M"}, {"actual", nil}},
		},
		{
			name: "a period the server rounded to the hour", field: "Оценка", given: "PT1H30M",
			received: periodValue(60, "PT1H"),
			mismatch: []detail{{"field", "Оценка"}, {"expected", "PT1H30M"}, {"actual", "PT1H"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := scalarRowNamed(t, tc.field)
			held := receivedFields(row.received(tc.received))
			server := creating(t, fake.JSON(http.StatusOK, scalarProject()),
				fake.JSON(http.StatusOK, createdIssueWith("DEV-7", "x", "null", held)))

			got := runWith(t, server.Env(), "issue", "create", "DEV", "--summary", "x",
				"--field", tc.field+"="+tc.given)

			if tc.mismatch == nil {
				require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				assert.Empty(t, got.stderr)
				return
			}
			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", creationRequest(server.URL, askedIssueFields)},
					{"issue", "DEV-7"},
					{"mismatch", []any{tc.mismatch}},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

func TestIssueShowRefusesAScalarOfAnotherShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		valueType string
		value     string
	}{
		{name: "a whole number that is text", valueType: "integer", value: `"42"`},
		{name: "a number that is text", valueType: "float", value: `"1.5"`},
		{name: "a string that is a number", valueType: "string", value: "42"},
		{name: "a day that is a fraction", valueType: "date", value: "1.5"},
		{name: "minutes that are text", valueType: "period", value: `{"$type":"PeriodValue","minutes":"90"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := issueWithFields(receivedField{name: "Field", valueType: tc.valueType, value: tc.value})
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields")

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
		})
	}
}
