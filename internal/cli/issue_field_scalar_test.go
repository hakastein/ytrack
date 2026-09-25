package cli_test

import (
	"cmp"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One of the seven types ytrack reads a value of itself: the field of DEV that holds it, what the type is
// called and the class the body names the field by. Every other type holds values that are names, and what a
// name stands for is the server's to say.
type scalarRow struct {
	name      string
	valueType string
	sent      string
}

// The seven rows, in the order the project of the scenarios below binds them, which is the order a refusal
// naming several of them names them in.
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

// The project of these scenarios: the seven fields whose values ytrack reads, and one enum ahead of them, so
// that a value of nothing at all is asked of a type that names its values as well as of one that does not.
func scalarProject() string {
	fields := []writableField{{id: "180-15", name: "Type", valueType: "enum", canBeEmpty: true}}
	for _, row := range scalarRows() {
		fields = append(fields, writableField{id: row.binding(), kind: kindOfBinding(row.valueType),
			name: row.name, valueType: row.valueType, canBeEmpty: true})
	}
	return projectResponse(fields...)
}

// The field as the new issue comes back holding it, with the value written as the server sends it.
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

// A period as the server sends one back, with the ISO duration it writes beside the minutes.
func periodValue(minutes int, id string) string {
	return fmt.Sprintf(`{"$type":"PeriodValue","minutes":%d,"id":%q,"presentation":"%dм"}`, minutes, id, minutes)
}

// A text value as the server sends one back, with the HTML it shows a human beside the text itself.
func textValue(text string) string {
	return `{"$type":"TextFieldValue","id":"text","text":` + asJSON(text) +
		`,"markdownText":` + asJSON("<div>"+text+"</div>") + `}`
}

// The text as a document can carry it: a byte that is no UTF-8 is no character of one.
func replaced(text string) string {
	return strings.ToValidUTF8(text, string(utf8.RuneError))
}

// invalidField is a value a call refuses to send: the field it names and the value the document shows for it.
// The reason ytrack gives for it is not held to, only that the refusal carries one.
type invalidField struct {
	field string
	value any
}

// requireInvalidFields holds a refusal's invalid detail to the fields and values it names, in the order they
// were given, without holding the reason named beside each to its own wording.
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

// Every value the call cannot send, one field at a time: nothing reaches the server, so the caller may fix
// the flag and send the call again.
func TestIssueCreateRefusesAScalarValueTheServerWouldRewrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field string
		given string
		// What the document says the value was, where that is not what was written: a byte that is no UTF-8
		// reaches no document, so the refusal over one shows the replacement character in its place.
		shown string
	}{
		{name: "a period of days", field: "Оценка", given: "P1D"},
		{name: "a period of a fraction of an hour", field: "Оценка", given: "PT1.5H"},
		{name: "a period of seconds", field: "Оценка", given: "PT30S"},
		{name: "a period in lower case", field: "Оценка", given: "pt1h"},
		{name: "a period of no part at all", field: "Оценка", given: "PT"},
		// A mark with no digits before it would otherwise read as none of that part and go out as PT0M.
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
		{name: "a string that is no UTF-8", field: "Внешний номер", given: "a\xffb", shown: replaced("a\xffb")},
		{name: "a text that is no UTF-8", field: "Примечание", given: "a\xffb", shown: replaced("a\xffb")},
		{name: "a text of nothing at all", field: "Примечание", given: ""},
		{name: "a name of nothing at all", field: "Type", given: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := creating(t, respondWith(http.StatusOK, scalarProject()), noCreation(t))

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x",
				"--field", tc.field+"="+tc.given)

			found := requireRefusal(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Equal(t, []detail{
				{"request", writeMetadataRequest(server.url, "DEV")},
				{"project", "DEV"},
			}, found.details[:2])
			requireInvalidFields(t, found, []invalidField{{field: tc.field, value: cmp.Or(tc.shown, tc.given)}})
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

// Every value the call cannot send is named in one refusal, in the order the project puts its fields in: a
// caller fixing one flag per attempt would read the project as many times over.
func TestIssueCreateNamesEveryValueItCannotSendAtOnce(t *testing.T) {
	t.Parallel()
	// One bad value for each field of the project, in the order the project binds them.
	refused := []struct{ field, given string }{
		{field: "Type", given: ""},
		{field: "Плановая дата решения", given: "16.09.2026"},
		{field: "Дата начала работы", given: "2026-08-31T00:00:00"},
		{field: "Порядок реализации", given: "2.5"},
		{field: "Коэффициент", given: "1,5"},
		{field: "Внешний номер", given: " EXT-1"},
		{field: "Примечание", given: ""},
		{field: "Оценка", given: "P1D"},
	}
	// Written back to front, so that the order the refusal names them in is the project's own.
	argv := []string{"issue", "create", "DEV", "--summary", "x"}
	for at := len(refused) - 1; at >= 0; at-- {
		argv = append(argv, "--field", refused[at].field+"="+refused[at].given)
	}
	server := creating(t, respondWith(http.StatusOK, scalarProject()), noCreation(t))

	got := runWith(t, server.env(), argv...)

	want := make([]invalidField, 0, len(refused))
	for _, row := range refused {
		want = append(want, invalidField{field: row.field, value: row.given})
	}
	found := requireRefusal(t, got)
	assert.Equal(t, "bad_usage", found.code)
	requireInvalidFields(t, found, want)
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

// What the body carries for a value ytrack read itself: the minutes of a period and nothing beside them,
// the milliseconds of a day at noon UTC and of a moment in the offset it was written in, the number the digits
// stand for, and text byte for byte.
func TestIssueCreateWritesAScalarValueAsItsFieldTypeExpects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field string
		given string
		// The value the body carries for the field, as JSON.
		value string
		// The value the new issue comes back holding, as the server sends it.
		received string
	}{
		{name: "a period of hours and minutes", field: "Оценка", given: "PT1H30M", value: `{"minutes":90}`,
			received: periodValue(90, "PT1H30M")},
		{name: "a period of no time at all", field: "Оценка", given: "PT0M", value: `{"minutes":0}`,
			received: periodValue(0, "PT0S")},
		{name: "a period of minutes alone", field: "Оценка", given: "PT90M", value: `{"minutes":90}`,
			received: periodValue(90, "PT1H30M")},
		{name: "a day, which the server keeps at noon UTC", field: "Плановая дата решения", given: "2026-09-16",
			value: "1789560000000", received: "1789560000000"},
		{name: "a moment in an offset of its own", field: "Дата начала работы",
			given: "2026-08-31T03:00:00.123+03:00", value: "1788134400123", received: "1788134400123"},
		{name: "a whole number written with leading zeroes", field: "Порядок реализации", given: "007",
			value: "7", received: "7"},
		{name: "a number written with an exponent", field: "Коэффициент", given: "1e3", value: "1000",
			received: "1000"},
		{name: "a string", field: "Внешний номер", given: "EXT-1", value: `"EXT-1"`, received: `"EXT-1"`},
		{name: "a text holding a carriage return and a CRLF", field: "Примечание",
			given: "первая\r\nвторая\rтретья", value: `{"text":"первая\r\nвторая\rтретья"}`,
			received: textValue("первая\r\nвторая\rтретья")},
		{name: "a text of two lines", field: "Примечание", given: "a\nb", value: `{"text":"a\nb"}`,
			received: textValue("a\nb")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := scalarRowNamed(t, tc.field)
			held := receivedFields(row.received(tc.received))
			server := creating(t, respondWith(http.StatusOK, scalarProject()),
				respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", held)))

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x",
				"--field", tc.field+"="+tc.given)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			want := `{"project":{"id":"0-1"},"summary":"x","customFields":[{"$type":` + strconv.Quote(row.sent) +
				`,"name":` + strconv.Quote(tc.field) + `,"value":` + tc.value + `}]}`
			assert.JSONEq(t, want, server.asks()[1])
		})
	}
}

// A value ytrack read itself is held against the answer by what the value is rather than by what it was
// written as: a day is a day whatever moment of it comes back, a number is the number its digits stand for,
// and a field the write filled and the answer holds nothing in is the write disagreeing with itself.
func TestIssueCreateChecksAScalarResponseAgainstTheValueItWrote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		field    string
		given    string
		received string
		// What the refusal names, empty where the answer agrees with the write.
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
			server := creating(t, respondWith(http.StatusOK, scalarProject()),
				respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", held)))

			got := runWith(t, server.env(), "issue", "create", "DEV", "--summary", "x",
				"--field", tc.field+"="+tc.given)

			if tc.mismatch == nil {
				require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				assert.Empty(t, got.stderr)
				return
			}
			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", creationRequest(server.url, askedIssueFields)},
					{"issue", "DEV-7"},
					{"mismatch", []any{tc.mismatch}},
				},
			}
			assert.Equal(t, want, requireUncertainty(t, got))
			assert.Empty(t, got.stdout)
		})
	}
}

// A value of a type whose identity is no text is held to the shape its type gives it on the way back as much
// as on the way out: what the catalogue of the instance says the field holds is what the answer has to carry.
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
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "customFields")

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
		})
	}
}
