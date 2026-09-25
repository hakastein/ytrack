package cli_test

import (
	"cmp"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const sentWorkItemWriteFields = "id,duration(minutes),type(name),attributes(id,name,value(id,name)),author(login),date," +
	"issue(idReadable," + customFieldsFields + "),text"

func workItemWriteRequest(address, issue, fields string) string {
	return "POST " + address + workItemsPath(issue) + "?fields=" + fields
}

type answeredWorkItem struct {
	id         string
	duration   string
	workType   string
	date       string
	text       string
	fields     string
	attributes string
}

func (a answeredWorkItem) json() string {
	return `{"$type":"IssueWorkItem","id":` + strconv.Quote(cmp.Or(a.id, "199-7")) +
		`,"duration":` + cmp.Or(a.duration, `{"$type":"DurationValue","minutes":90}`) +
		`,"type":` + cmp.Or(a.workType, "null") + `,` + cmp.Or(a.attributes, sentNoAttribute) +
		`,"author":{"$type":"User","login":"admin"}` +
		`,"date":` + cmp.Or(a.date, "1788220800000") +
		`,"issue":{"$type":"Issue","idReadable":"DEV-1","customFields":[` + a.fields + `]}` +
		`,"text":` + cmp.Or(a.text, "null") + `}`
}

func writingTime(t *testing.T, write http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method, "a work item is written by one POST and nothing else") {
			return
		}
		write(w, r)
	})
}

func sentWorkItem(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	asks := u.asks()
	require.Len(t, asks, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(asks[0]), &body), "the body that went out: %s", asks[0])
	return body
}

func TestTimeCreateRefusesADurationItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		spent string
	}{
		{name: "the way the server shows one", spent: "1ч 30м"},
		{name: "the same in Latin letters", spent: "1h 30m"},
		{name: "hours in the language of the server", spent: "3ч"},
		{name: "a bare number", spent: "90"},
		{name: "minutes with no period at all", spent: "90m"},
		{name: "a fraction of an hour", spent: "1.5h"},
		{name: "a day", spent: "P1D"},
		{name: "a week", spent: "P1W"},
		{name: "a day and hours", spent: "P1DT2H"},
		{name: "a fraction of an ISO hour", spent: "PT1.5H"},
		{name: "seconds", spent: "PT30S"},
		{name: "hours, minutes and seconds", spent: "PT1H30M15S"},
		{name: "in lower case", spent: "pt1h"},
		{name: "the marker alone", spent: "PT"},
		{name: "the period marker alone", spent: "P"},
		{name: "nothing at all", spent: ""},
		{name: "a space before it", spent: " PT1H"},
		{name: "a fullwidth digit", spent: "PT" + string(rune(0xFF11)) + "H"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "time", "create", "DEV-1", tc.spent)

			found := requireFault(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestTimeCreateRefusesADurationLongerThanTheServerCounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		spent string
	}{
		{name: "one minute past the largest", spent: "PT" + strconv.FormatInt(math.MaxInt32+1, 10) + "M"},
		{name: "one hour past the largest", spent: "PT35791395H"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "time", "create", "DEV-1", tc.spent)

			want := faultDocument{code: "bad_usage"}
			assert.Equal(t, want, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTimeCreateRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{
			name: "the day twice",
			argv: []string{"time", "create", "DEV-1", "PT1H", "--date", "2026-09-01", "--date", "2026-09-02"},
		},
		{name: "the text twice", argv: []string{"time", "create", "DEV-1", "PT1H", "--text", "a", "--text", "b"}},
		{
			name: "the expression twice",
			argv: []string{"time", "create", "DEV-1", "PT1H", "--fields", "id", "--fields", "text"},
		},
		{
			name: "a name under the duration",
			argv: []string{"time", "create", "DEV-1", "PT1H", "--fields", "duration(minutes)"},
		},
		{
			name: "a custom field of the issue named",
			argv: []string{"time", "create", "DEV-1", "PT1H", "--fields", "+issue(customFields(State))"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestTimeCreateRefusesADayItCannotSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		day  string
	}{
		{name: "the day first", day: "01.09.2026"},
		{name: "the month first", day: "09/01/2026"},
		{name: "a word for today", day: "today"},
		{name: "a word for the day before", day: "yesterday"},
		{name: "a month and a day of one digit", day: "2026-9-1"},
		{name: "a day the month has none of", day: "2026-02-30"},
		{name: "a time of day", day: "2026-09-01T15:30:00Z"},
		{name: "midnight of another time zone", day: "2026-09-01T00:00:00+03:00"},
		{name: "a moment with no offset", day: "2026-09-01T00:00:00"},
		{name: "nothing at all", day: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H", "--date", tc.day)

			found := requireFault(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.requests())
		})
	}
}

func TestTimeCreateRefusesMidnightUTCCarriedInAnOffset(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H", "--date", "2026-08-31T21:00:00-03:00")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}

func TestTimeCreateSendsTheMinutesOfTheDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spent   string
		minutes float64
	}{
		{name: "hours and minutes", spent: "PT1H30M", minutes: 90},
		{name: "minutes alone", spent: "PT90M", minutes: 90},
		{name: "hours alone", spent: "PT24H", minutes: 1440},
		{name: "none at all", spent: "PT0M", minutes: 0},
		{name: "the largest the server counts", spent: "PT2147483647M", minutes: 2147483647},
		{name: "the same written in hours and minutes", spent: "PT35791394H7M", minutes: 2147483647},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{
				duration: `{"$type":"DurationValue","minutes":` + strconv.FormatFloat(tc.minutes, 'f', -1, 64) + `}`,
			}.json()))

			got := runWith(t, server.env(), "time", "create", "dev-1", tc.spent)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, map[string]any{"duration": map[string]any{"minutes": tc.minutes}}, sentWorkItem(t, server))
			assert.Equal(t, []string{workItemsPath("dev-1")}, server.sentPaths())
		})
	}
}

func TestTimeCreateSendsTheDayAsNoonUTC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		day  string
	}{
		{name: "a calendar day", day: "2026-09-01"},
		{name: "midnight UTC as ytrack prints it", day: "2026-09-01T00:00:00.000Z"},
		{name: "midnight UTC written with an offset of none", day: "2026-09-01T00:00:00+00:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{}.json()))

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--date", tc.day)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			want := map[string]any{"duration": map[string]any{"minutes": float64(90)}, "date": float64(1788264000000)}
			assert.Equal(t, want, sentWorkItem(t, server))
		})
	}
}

func TestTimeCreateSendsTheTextByteForByte(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{name: "a carriage return", text: "первая\rвторая"},
		{name: "a line ending of two bytes", text: "первая\r\nвторая"},
		{name: "a line separator", text: "первая\xe2\x80\xa8вторая"},
		{name: "a byte of nothing", text: "первая\x00вторая"},
		{name: "brackets a reader might take for markup", text: "[bug] fix login"},
		{name: "nothing at all", text: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{text: asJSON(tc.text)}.json()))

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--text", tc.text)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			want := map[string]any{"duration": map[string]any{"minutes": float64(90)}, "text": tc.text}
			assert.Equal(t, want, sentWorkItem(t, server))
		})
	}
}

func TestTimeRefusesATextThatIsNoUTF8(t *testing.T) {
	t.Parallel()
	const written = "bad\xffbyte"
	tests := []struct {
		name string
		argv []string
	}{
		{name: "a creation", argv: []string{"time", "create", "DEV-1", "PT1H", "--text", written}},
		{name: "an update", argv: []string{"time", "update", "DEV-1", "199-6", "--text", written}},
		{
			name: "an update that writes the day as well",
			argv: []string{"time", "update", "DEV-1", "199-6", "--date", "2026-09-01", "--text", written},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), tc.argv...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.requests(), "encoding/json would send the bad byte as U+FFFD")
		})
	}
}

func TestTimeCreatePrintsTheWorkItemTheServerKept(t *testing.T) {
	t.Parallel()
	spent := receivedField{name: "Затраченное время", valueType: "period", value: `{"$type":"DurationValue","minutes":90}`}
	estimation := receivedField{name: "Оценка", valueType: "period", ordinal: "2", binding: "180-2"}
	written := answeredWorkItem{
		workType: `{"$type":"WorkItemType","name":"Разработка"}`,
		text:     asJSON("первая\nвторая"),
		fields:   spent.sent() + "," + estimation.sent(),
	}
	server := writingTime(t, respondWith(http.StatusOK, written.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01",
		"--text", "первая\nвторая")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	assert.Equal(t, []string{sentWorkItemWriteFields}, server.sentFields())
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, []string{"id", "duration", "type", "attributes", "author", "date", "issue", "text"}, keysOf(mapping))
	assert.Equal(t, "PT1H30M", nodeAt(t, mapping, "duration").Value)
	assert.Equal(t, "2026-09-01T00:00:00Z", nodeAt(t, mapping, "date").Value)
	assert.Equal(t, "DEV-1", nodeAt(t, mapping, "issue", "idReadable").Value)
	assert.Equal(t, []string{"Затраченное время"}, keysOf(nodeAt(t, mapping, "issue", "customFields")))
	assert.Equal(t, "PT1H30M", nodeAt(t, mapping, "issue", "customFields", "Затраченное время").Value)
	text := nodeAt(t, mapping, "text")
	assert.Equal(t, "первая\nвторая", text.Value)
	assert.Equal(t, yaml.LiteralStyle, text.Style)
}

func TestTimeCreatePrintsATextNoBlockCanCarryInQuotes(t *testing.T) {
	t.Parallel()
	server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{text: asJSON("первая\rвторая")}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--text", "первая\rвторая")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	text := nodeAt(t, requireMapping(t, "stdout", got.stdout), "text")
	assert.Equal(t, "первая\rвторая", text.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, text.Style)
}

func TestTimeCreateRefusesWhatTheServerKeptOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		argv     []string
		answered answeredWorkItem
		mismatch []any
	}{
		{
			name:     "a duration of another length",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M"},
			answered: answeredWorkItem{duration: `{"$type":"DurationValue","minutes":60}`},
			mismatch: []any{[]detail{{"field", "duration"}, {"expected", "PT1H30M"}, {"actual", "PT1H"}}},
		},
		{
			name:     "no duration at all",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M"},
			answered: answeredWorkItem{duration: "null"},
			mismatch: []any{[]detail{{"field", "duration"}, {"expected", "PT1H30M"}, {"actual", nil}}},
		},
		{
			name:     "a day after the one written",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01"},
			answered: answeredWorkItem{date: "1788307200000"},
			mismatch: []any{[]detail{
				{"field", "date"},
				{"expected", "2026-09-01"},
				{"actual", "2026-09-02T00:00:00Z"},
			}},
		},
		{
			name:     "a day that came back as something other than a number of milliseconds",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01"},
			answered: answeredWorkItem{date: asJSON("2026-09-01")},
			mismatch: []any{[]detail{{"field", "date"}, {"expected", "2026-09-01"}, {"actual", nil}}},
		},
		{
			name:     "no day at all",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01"},
			answered: answeredWorkItem{date: "null"},
			mismatch: []any{[]detail{{"field", "date"}, {"expected", "2026-09-01"}, {"actual", nil}}},
		},
		{
			name:     "a text the server rewrote",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M", "--text", "первая\rвторая"},
			answered: answeredWorkItem{text: asJSON("первая\nвторая")},
			mismatch: []any{[]detail{
				{"field", "text"},
				{"expected", "первая\rвторая"},
				{"actual", "первая\nвторая"},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTime(t, respondWith(http.StatusOK, tc.answered.json()))

			got := runWith(t, server.env(), tc.argv...)

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, []string{"request", "issue", "id", "mismatch"}, detailKeys(found))
			assert.Equal(t, "DEV-1", detailNamed(t, found, "issue"))
			assert.Equal(t, "199-7", detailNamed(t, found, "id"))
			assert.Equal(t, tc.mismatch, detailNamed(t, found, "mismatch"))
		})
	}
}

func TestTimeCreateRefusesADurationReceivedWithoutItsMinutes(t *testing.T) {
	t.Parallel()
	server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{duration: `{"$type":"DurationValue"}`}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, []any{[]detail{{"field", "duration(minutes)"}, {"type", "DurationValue"}}},
		detailNamed(t, found, "missing"))
}

func detailKeys(found faultDocument) []string {
	keys := make([]string, 0, len(found.details))
	for _, printed := range found.details {
		keys = append(keys, printed.key)
	}
	return keys
}

func TestTimeCreateTakesTheDayTheServerKeptForTheDayWritten(t *testing.T) {
	t.Parallel()
	keptAtMidnightUTC := answeredWorkItem{date: "1788220800000"}
	server := writingTime(t, respondWith(http.StatusOK, keptAtMidnightUTC.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "2026-09-01T00:00:00Z", nodeAt(t, requireMapping(t, "stdout", got.stdout), "date").Value)
}

func TestTimeCreateChecksNothingItNeverWrote(t *testing.T) {
	t.Parallel()
	server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{date: "1788307200000", text: "null"}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "2026-09-02T00:00:00Z", nodeAt(t, mapping, "date").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "text")))
}

func TestTimeCreateRefusesWhatTheServerRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		status          int
		code            string
		upstreamError   string
		upstreamMessage string
		details         []detail
	}{
		{
			name:            "a duration of no length",
			status:          http.StatusBadRequest,
			code:            "rejected",
			upstreamError:   "invalid_properties",
			upstreamMessage: "Длительность работы не может быть отрицательной или пустой",
		},
		{
			name:            "an issue the instance has none of",
			status:          http.StatusNotFound,
			code:            "not_found",
			upstreamError:   "Not Found",
			upstreamMessage: "Entity with id DEV-1 not found",
		},
		{
			name:            "a token that may read the issue and not write time against it",
			status:          http.StatusForbidden,
			code:            "denied",
			upstreamError:   "Forbidden",
			upstreamMessage: "HTTP 403 Forbidden",
			details:         []detail{authFromEnv()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			said := `{"error":` + strconv.Quote(tc.upstreamError) + `,"error_description":` +
				strconv.Quote(tc.upstreamMessage) + `}`
			server := writingTime(t, respondWith(tc.status, said))

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT0M")

			want := faultDocument{
				code: tc.code,
				details: append([]detail{
					{"request", workItemWriteRequest(server.url, "DEV-1", sentWorkItemWriteFields)},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
				}, tc.details...),
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
		})
	}
}

func TestTimeCreateAsksForWhatItChecksWhateverWasAskedToPrint(t *testing.T) {
	t.Parallel()
	server := writingTime(t, respondWith(http.StatusOK, answeredWorkItem{}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--fields", "id")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, `id: "199-7"`+"\n", got.stdout)
	assert.Equal(t, []string{"id,duration(minutes),date,text,issue(idReadable)"}, server.sentFields())
}

func TestTimeCreateRefusesAnExpressionItCannotSend(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H", "--fields", "a,,b")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}
