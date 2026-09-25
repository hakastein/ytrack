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

const workItemWriteFields = "id,duration,type(name),attributes,author(login),date,issue(idReadable,customFields),text"

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
		{name: "no issue at all", argv: []string{"time", "create"}},
		{name: "no duration", argv: []string{"time", "create", "DEV-1"}},
		{name: "a word after the duration", argv: []string{"time", "create", "DEV-1", "PT1H", "Разработка"}},
		{name: "the duration as the server shows it", argv: []string{"time", "create", "DEV-1", "--presentation", "3ч"}},
		{name: "the duration in minutes", argv: []string{"time", "create", "DEV-1", "--minutes", "90"}},
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

func TestTimeCreateHelpNamesItsDefault(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"time", "create", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, workItemWriteFields)
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
		{name: "a word pflag would read as a flag", text: "-x"},
		{name: "the word that asks for help", text: "--help"},
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

func contractWorkItemIssue(t *testing.T, dev *upstream, role string) string {
	t.Helper()
	argv := append([]string{"issue", "create", "DEV", "--summary",
		"ytrack contract " + t.Name() + " " + role}, devRequired()...)
	got := runWith(t, dev.env(), argv...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

func TestTimeCreateWritesTimeAgainstAnIssueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	const text = "ytrack contract первая\rвторая   "

	got := runWith(t, dev.env(), "time", "create", issue, "PT1H30M", "--date", "2026-09-01", "--text", text)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Empty(t, got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Regexp(t, internalIDForm, nodeAt(t, mapping, "id").Value)
	assert.Equal(t, "PT1H30M", nodeAt(t, mapping, "duration").Value)
	assert.Equal(t, "2026-09-01T00:00:00Z", nodeAt(t, mapping, "date").Value)
	assert.Equal(t, "admin", nodeAt(t, mapping, "author", "login").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "type")))
	assert.Equal(t, issue, nodeAt(t, mapping, "issue", "idReadable").Value)
	assert.Equal(t, "PT1H30M", nodeAt(t, mapping, "issue", "customFields", "Затраченное время").Value)
	written := nodeAt(t, mapping, "text")
	assert.Equal(t, text, written.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, written.Style)

	second := runWith(t, dev.env(), "time", "create", issue, "PT30M")
	require.Equal(t, 0, second.code, "stderr: %s", second.stderr)
	again := requireMapping(t, "stdout", second.stdout)
	assert.Equal(t, "PT2H", nodeAt(t, again, "issue", "customFields", "Затраченное время").Value)
	assert.Regexp(t, `^[0-9]{4}-[0-9]{2}-[0-9]{2}T00:00:00Z$`, nodeAt(t, again, "date").Value,
		"without --date the server takes its own today")

	listed := runWith(t, dev.env(), "time", "list", issue, "--limit", "1")
	printed := requireWorkItemListing(t, listed)
	assert.Equal(t, 2, printed.Total)
	assert.True(t, printed.Truncated)
}

func TestTimeCreateWritesTheDayNamedByTheMemberOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member},
		"time", "create", issue, "PT1H", "--date", "2026-09-01", "--text", "ytrack contract zone")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "2026-09-01T00:00:00Z", nodeAt(t, mapping, "date").Value,
		"the member's profile is UTC+10, where noon UTC is 22:00 of the named day")
	assert.Equal(t, "dev.member", nodeAt(t, mapping, "author", "login").Value)
}

func atThreePM(_ *http.Request, body []byte) []byte {
	var written struct {
		Duration struct {
			Minutes int64 `json:"minutes"`
		} `json:"duration"`
	}
	if json.Unmarshal(body, &written) != nil {
		return body
	}
	return []byte(`{"duration":{"minutes":` + strconv.FormatInt(written.Duration.Minutes, 10) +
		`},"date":` + strconv.FormatInt(threePMUTC, 10) + `}`)
}

const threePMUTC = 1788274800000

func TestTimeCreateFindsTheDayOfAMomentComesFromTheProfileOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	dev.replacing(atThreePM)
	defer dev.replacing(nil)

	got := runWith(t, dev.env(), "time", "create", issue, "PT15M", "--date", "2026-09-01")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "2026-09-01T00:00:00Z", nodeAt(t, requireMapping(t, "stdout", got.stdout), "date").Value,
		"15:00 UTC falls on 1 September in the admin's Europe/Moscow")

	byTheMember := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member},
		"time", "create", issue, "PT15M", "--date", "2026-09-01")

	found := requireUncertainty(t, byTheMember)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, []any{[]detail{
		{"field", "date"},
		{"expected", "2026-09-01"},
		{"actual", "2026-09-02T00:00:00Z"},
	}}, detailNamed(t, found, "mismatch"), "15:00 UTC falls on 2 September in the member's Asia/Vladivostok")
}

func contractIssueWithTimeTrackingOff(t *testing.T, dev *upstream) string {
	t.Helper()
	got := runWith(t, dev.env(), "issue", "create", "DOCS", "--summary",
		"ytrack contract "+t.Name())
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DOCS-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

func TestTimeCreateWritesNoTimeInTheProjectOfTheDevInstanceThatHasTimeTrackingOff(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractIssueWithTimeTrackingOff(t, dev)
	before := len(dev.requests())

	got := runWith(t, dev.env(), "time", "create", issue, "PT30M")

	found := requireFault(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, "Forbidden", detailNamed(t, found, "upstream_error"))
	assert.Equal(t, "HTTP 403 Forbidden", detailNamed(t, found, "upstream_message"))
	assert.Equal(t, []string{workItemsPath(issue)}, pathsSince(dev, before))

	byTheMember := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member},
		"time", "create", issue, "PT30M")
	assert.Equal(t, "denied", requireFault(t, byTheMember).code)

	listed := runWith(t, dev.env(), "time", "list", issue)
	assert.Equal(t, 0, requireWorkItemListing(t, listed).Total)
}

func TestTimeCreateBreaksTheSumOfTheDevInstanceOnePastTheLargestDuration(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")

	got := runWith(t, dev.env(), "time", "create", issue, "PT2147483647M")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	filed := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "PT35791394H7M", nodeAt(t, filed, "duration").Value)
	assert.Equal(t, "PT35791394H7M", nodeAt(t, filed, "issue", "customFields", "Затраченное время").Value)

	oneMore := runWith(t, dev.env(), "time", "create", issue, "PT1M")

	require.Equal(t, 0, oneMore.code, "stderr: %s", oneMore.stderr)
	assert.Empty(t, oneMore.stderr)
	broken := requireMapping(t, "stdout", oneMore.stdout)
	assert.Equal(t, "PT1M", nodeAt(t, broken, "duration").Value)
	assert.NotContains(t, keysOf(nodeAt(t, broken, "issue", "customFields")), "Затраченное время",
		"the server sums the minutes in an int32 and sends null past it")

	afterwards := runWith(t, dev.env(), "issue", "show", issue, "--fields", "customFields", "--comments=0")
	require.Equal(t, 0, afterwards.code, "stderr: %s", afterwards.stderr)
	assert.NotContains(t, keysOf(nodeAt(t, requireMapping(t, "stdout", afterwards.stdout), "customFields")),
		"Затраченное время")
}

func TestTimeCreateWritesNoWorkItemOfNoLength(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")

	got := runWith(t, dev.env(), "time", "create", issue, "PT0M")

	found := requireFault(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, "Длительность работы не может быть отрицательной или пустой",
		detailNamed(t, found, "upstream_message"))

	listed := runWith(t, dev.env(), "time", "list", issue)
	assert.Equal(t, 0, requireWorkItemListing(t, listed).Total)
}

func asDurationID(_ *http.Request, body []byte) []byte {
	var written struct {
		Duration struct {
			Minutes int64 `json:"minutes"`
		} `json:"duration"`
	}
	if json.Unmarshal(body, &written) != nil {
		return body
	}
	return []byte(`{"duration":{"id":"PT` + strconv.FormatInt(written.Duration.Minutes/60, 10) + `H"}}`)
}

func asPresentation(*http.Request, []byte) []byte {
	return []byte(`{"duration":{"presentation":"1д"}}`)
}

func TestTimeCreateIsRefusedTheDurationWrittenAsAnID(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	dev.replacing(asDurationID)
	defer dev.replacing(nil)

	got := runWith(t, dev.env(), "time", "create", issue, "PT2H")

	found := requireFault(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, "Для единицы работы должна быть задана длительность",
		detailNamed(t, found, "upstream_message"))

	dev.replacing(nil)
	listed := runWith(t, dev.env(), "time", "list", issue)
	assert.Equal(t, 0, requireWorkItemListing(t, listed).Total)
}

func TestTimeCreateFindsADayOfTheDevInstanceIsEightHours(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	dev.replacing(asPresentation)
	defer dev.replacing(nil)

	got := runWith(t, dev.env(), "time", "create", issue, "PT8H")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "PT8H", nodeAt(t, requireMapping(t, "stdout", got.stdout), "duration").Value)
}

func TestTimeCreateWritesNothingForAnIssueTheDevInstanceDoesNotShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		issue string
		token func(*testing.T) string
	}{
		{
			name:  "an issue the dev instance has none of",
			issue: "DEV-99999",
			token: func(t *testing.T) string { t.Helper(); return devTokens(t).admin },
		},
		{
			name:  "an issue hidden from the limited token",
			issue: "DEV-1",
			token: func(t *testing.T) string { t.Helper(); return devTokens(t).limited },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + tc.token(t)},
				"time", "create", tc.issue, "PT1H")

			found := requireFault(t, got)
			assert.Equal(t, "not_found", found.code)
			assert.Equal(t, "Entity with id "+tc.issue+" not found", detailNamed(t, found, "upstream_message"))
			assert.Equal(t, []string{workItemsPath(tc.issue)}, dev.sentPaths())
		})
	}
}

func TestTimeCreateRefusesAnExpressionItCannotSend(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H", "--fields", "a,,b")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.requests())
}
