package cli_test

import (
	"cmp"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// What the answer to a write holds where the caller writes no expression of their own, which is what the help
// of the command names.
const workItemWriteFields = "id,duration,type(name),attributes,author(login),date,issue(idReadable,customFields),text"

// What goes out for the same expression: the minutes filled in under the duration, and the custom fields of the
// issue composed the way a show of an issue composes them.
const sentWorkItemWriteFields = "id,duration(minutes),type(name),attributes(id,name,value(id,name)),author(login),date," +
	"issue(idReadable," + customFieldsFields + "),text"

// The request a work item goes out as, which is the one a refusal about it names.
func workItemWriteRequest(address, issue, fields string) string {
	return "POST " + address + workItemsPath(issue) + "?fields=" + fields
}

// The work item a write answers with, under the expression that goes out. The members are JSON already, so a
// scenario may send a shape the specification does not allow; what a scenario leaves out is what a work item
// written by the admin comes back as.
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

// writingTime is the server of a work item write: the POST is the whole command, so a scenario says what that
// one request was answered with.
func writingTime(t *testing.T, write http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method, "a work item is written by one POST and nothing else") {
			return
		}
		write(w, r)
	})
}

// sentWorkItem is the body of the one request that went out, read as JSON reads it: the keys are held to what
// the body carries rather than to the bytes of it, since the order of members is the encoder's business.
func sentWorkItem(t *testing.T, u *upstream) map[string]any {
	t.Helper()
	asks := u.asks()
	require.Len(t, asks, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(asks[0]), &body), "the body that went out: %s", asks[0])
	return body
}

// Every form of a duration YouTrack itself would show, or would take as something other than hours and
// minutes, is refused before the network, and the refusal names both the value and the form that is written.
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
		// A fullwidth digit is written in bytes: it looks like the ASCII one it is not.
		{name: "a fullwidth digit", spent: "PT\xef\xbc\x91H"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "time", "create", "DEV-1", tc.spent)

			found := requireRefusal(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.requests())
		})
	}
}

// A duration longer than the minutes YouTrack counts them in is refused here rather than sent: the server
// would overflow the number and answer that the duration is negative or empty, which names neither the value
// nor what is wrong with it.
func TestTimeCreateRefusesADurationLongerThanTheServerCounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		spent string
	}{
		{name: "one minute past the largest", spent: "PT2147483648M"},
		{name: "one hour past the largest", spent: "PT35791395H"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), "time", "create", "DEV-1", tc.spent)

			want := refusal{code: "bad_usage"}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// What a write takes: the issue and the duration as arguments, and the day, the text and the expression as
// flags written once each. There is no flag under which a duration reaches the server the way it shows one.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// A day is written as a calendar day or as midnight UTC of one, and nothing else reaches the server: a
// moment of a day would be filed under the calendar day of a time zone the caller has no way to know.
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

			found := requireRefusal(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Empty(t, server.requests())
		})
	}
}

// Midnight UTC carried in an offset of its own names no time of day, so the refusal hands back the two forms
// a day is written in instead of saying it does.
func TestTimeCreateRefusesMidnightUTCCarriedInAnOffset(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H", "--date", "2026-08-31T21:00:00-03:00")

	assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

func TestTimeCreateHelpNamesItsDefault(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"time", "create", "--help"})

	assert.Equal(t, 0, got.code)
	assert.Empty(t, got.stderr)
	assert.Contains(t, got.stdout, workItemWriteFields)
}

// The body is the minutes and nothing else: the ISO period is the caller's way of saying a length and the
// minutes are the server's, and the id and the presentation YouTrack writes beside them are never sent.
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
			server := writingTime(t, answer(http.StatusOK, answeredWorkItem{
				duration: `{"$type":"DurationValue","minutes":` + strconv.FormatFloat(tc.minutes, 'f', -1, 64) + `}`,
			}.json()))

			got := runWith(t, server.env(), "time", "create", "dev-1", tc.spent)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, map[string]any{"duration": map[string]any{"minutes": tc.minutes}}, sentWorkItem(t, server))
			assert.Equal(t, []string{workItemsPath("dev-1")}, server.sentPaths())
		})
	}
}

// A day goes out as noon UTC of it, whichever of the two forms it was written in: every time zone from
// UTC−12 to UTC+11:59 reads that moment as the day that was written.
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
			server := writingTime(t, answer(http.StatusOK, answeredWorkItem{}.json()))

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--date", tc.day)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			want := map[string]any{"duration": map[string]any{"minutes": float64(90)}, "date": float64(1788264000000)}
			assert.Equal(t, want, sentWorkItem(t, server))
		})
	}
}

// The text reaches the server byte for byte, whatever stands in it: YouTrack keeps every one of them, so
// nothing here is rewritten on the way out and nothing here is refused.
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
			server := writingTime(t, answer(http.StatusOK, answeredWorkItem{text: asJSON(tc.text)}.json()))

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--text", tc.text)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			want := map[string]any{"duration": map[string]any{"minutes": float64(90)}, "text": tc.text}
			assert.Equal(t, want, sentWorkItem(t, server))
		})
	}
}

// The one byte of a text a work item does not keep is a byte that is no UTF-8: the encoder would write it
// as U+FFFD, so the rewriting would be ytrack's own and the check of the write would report it over a work item
// that by then holds the rewritten text. It is refused before the network at either verb, as it is at a
// comment, and an empty text goes out all the same.
func TestTimeRefusesATextThatIsNoUTF8(t *testing.T) {
	t.Parallel()
	// A byte no rune of UTF-8 begins with, written as the byte it is.
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

			assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The document is the keys of the default in their order, with the duration read out of the minutes, the
// day printed as midnight UTC and the time spent on the issue among its custom fields.
func TestTimeCreatePrintsTheWorkItemTheServerKept(t *testing.T) {
	t.Parallel()
	spent := arrivedField{name: "Затраченное время", valueType: "period", value: `{"$type":"DurationValue","minutes":90}`}
	estimation := arrivedField{name: "Оценка", valueType: "period", ordinal: "2", binding: "180-2"}
	written := answeredWorkItem{
		workType: `{"$type":"WorkItemType","name":"Разработка"}`,
		text:     asJSON("первая\nвторая"),
		fields:   spent.sent() + "," + estimation.sent(),
	}
	server := writingTime(t, answer(http.StatusOK, written.json()))

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
	// A field the issue holds nothing in is left out of the block, so only the one that moved stands there.
	assert.Equal(t, []string{"Затраченное время"}, keysOf(nodeAt(t, mapping, "issue", "customFields")))
	assert.Equal(t, "PT1H30M", nodeAt(t, mapping, "issue", "customFields", "Затраченное время").Value)
	text := nodeAt(t, mapping, "text")
	assert.Equal(t, "первая\nвторая", text.Value)
	assert.Equal(t, yaml.LiteralStyle, text.Style)
}

// A text a literal block cannot carry is printed in double quotes, byte for byte, as it is wherever prose
// of the instance is printed.
func TestTimeCreatePrintsATextNoBlockCanCarryInQuotes(t *testing.T) {
	t.Parallel()
	server := writingTime(t, answer(http.StatusOK, answeredWorkItem{text: asJSON("первая\rвторая")}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--text", "первая\rвторая")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	text := nodeAt(t, requireMapping(t, "stdout", got.stdout), "text")
	assert.Equal(t, "первая\rвторая", text.Value)
	assert.Equal(t, yaml.DoubleQuotedStyle, text.Style)
}

// A 200 says the server took the body, not that it kept what went out, so every part the call wrote is held
// against the answer and a disagreement is a refusal naming both. The work item exists by then, which is what
// the exit code says without the document being read.
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
			mismatch: []any{[]detail{{"field", "duration"}, {"written", "PT1H30M"}, {"arrived", "PT1H"}}},
		},
		{
			name:     "no duration at all",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M"},
			answered: answeredWorkItem{duration: "null"},
			mismatch: []any{[]detail{{"field", "duration"}, {"written", "PT1H30M"}, {"arrived", nil}}},
		},
		{
			name:     "a day after the one written",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01"},
			answered: answeredWorkItem{date: "1788307200000"},
			mismatch: []any{[]detail{
				{"field", "date"},
				{"written", "2026-09-01"},
				{"arrived", "2026-09-02T00:00:00Z"},
			}},
		},
		{
			// The day is read off the milliseconds the server keeps it in, so a date said any other way is a
			// day that never arrived rather than a day of its own.
			name:     "a day that came back as something other than a number of milliseconds",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01"},
			answered: answeredWorkItem{date: asJSON("2026-09-01")},
			mismatch: []any{[]detail{{"field", "date"}, {"written", "2026-09-01"}, {"arrived", nil}}},
		},
		{
			name:     "no day at all",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01"},
			answered: answeredWorkItem{date: "null"},
			mismatch: []any{[]detail{{"field", "date"}, {"written", "2026-09-01"}, {"arrived", nil}}},
		},
		{
			name:     "a text the server rewrote",
			argv:     []string{"time", "create", "DEV-1", "PT1H30M", "--text", "первая\rвторая"},
			answered: answeredWorkItem{text: asJSON("первая\nвторая")},
			mismatch: []any{[]detail{
				{"field", "text"},
				{"written", "первая\rвторая"},
				{"arrived", "первая\nвторая"},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTime(t, answer(http.StatusOK, tc.answered.json()))

			got := runWith(t, server.env(), tc.argv...)

			found := requireUncertainty(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Equal(t, []string{"request", "issue", "id", "mismatch"}, detailKeys(found))
			assert.Equal(t, "DEV-1", detailNamed(t, found, "issue"))
			assert.Equal(t, "199-7", detailNamed(t, found, "id"))
			assert.Equal(t, tc.mismatch, detailNamed(t, found, "mismatch"))
		})
	}
}

// A duration that came back without the minutes it holds is caught before the check of the write is: the
// minutes were asked for and did not arrive, which is the judgment of names rather than a value the server
// kept otherwise. The work item exists by then either way.
func TestTimeCreateRefusesADurationThatArrivedWithoutItsMinutes(t *testing.T) {
	t.Parallel()
	server := writingTime(t, answer(http.StatusOK, answeredWorkItem{duration: `{"$type":"DurationValue"}`}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M")

	found := requireUncertainty(t, got)
	assert.Equal(t, "upstream_lied", found.code)
	assert.Equal(t, []any{[]detail{{"field", "duration(minutes)"}, {"type", "DurationValue"}}},
		detailNamed(t, found, "missing"))
}

// detailKeys is the keys a refusal printed after the message, in order.
func detailKeys(found refusal) []string {
	keys := make([]string, 0, len(found.details))
	for _, printed := range found.details {
		keys = append(keys, printed.key)
	}
	return keys
}

// The day is held to the calendar day of UTC and not to the millisecond: the body carries noon of it and
// the server keeps midnight, so the two moments differ and the day does not.
func TestTimeCreateTakesTheDayTheServerKeptForTheDayWritten(t *testing.T) {
	t.Parallel()
	server := writingTime(t, answer(http.StatusOK, answeredWorkItem{date: "1788220800000"}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--date", "2026-09-01")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "2026-09-01T00:00:00Z", nodeAt(t, requireMapping(t, "stdout", got.stdout), "date").Value)
}

// What the call named nothing for is held to nothing: the day YouTrack chose itself and the text it left
// empty are its answer about a work item, not a disagreement with a write that said neither.
func TestTimeCreateHoldsNothingItNeverWrote(t *testing.T) {
	t.Parallel()
	server := writingTime(t, answer(http.StatusOK, answeredWorkItem{date: "1788307200000", text: "null"}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "2026-09-02T00:00:00Z", nodeAt(t, mapping, "date").Value)
	assert.Nil(t, requireValue(t, nodeAt(t, mapping, "text")))
}

// What the server says about a write it refused passes on word for word, and the code is the status read
// as ADR-0005 reads one: nothing was written in any of these, so the caller may fix the call and send it again.
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
			server := writingTime(t, answer(tc.status, said))

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT0M")

			want := refusal{
				code: tc.code,
				details: append([]detail{
					{"request", workItemWriteRequest(server.url, "DEV-1", sentWorkItemWriteFields)},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
				}, tc.details...),
			}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Equal(t, []string{http.MethodPost}, sentMethods(server))
		})
	}
}

// What the tool needs of the answer goes out whatever the caller asked to print: the parts the check holds
// against what was written, and the pair a refusal names the work item by. The document is theirs alone.
func TestTimeCreateAsksForWhatItChecksWhateverWasAskedToPrint(t *testing.T) {
	t.Parallel()
	server := writingTime(t, answer(http.StatusOK, answeredWorkItem{}.json()))

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M", "--fields", "id")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, `id: "199-7"`+"\n", got.stdout)
	assert.Equal(t, []string{"id,duration(minutes),date,text,issue(idReadable)"}, server.sentFields())
}

// The issue a work item is written against is held to the form of an issue like any other, and the work item
// this scenario files is taken away with the issue it hangs from.
func contractWorkItemIssue(t *testing.T, dev *upstream, role string) string {
	t.Helper()
	argv := append([]string{"issue", "create", "DEV", "--summary",
		"ytrack contract " + t.Name() + " " + role}, devRequired()...)
	got := runWith(t, dev.env(), argv...)
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DEV-[0-9]+$`, readable)
	// Registered after the recorder's own cleanup, so the deletion runs first and the cassette records it.
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

// A work item written on the polygon for real: the duration goes out as minutes and comes back the period
// it was written as, the day is kept as the calendar day, the text byte for byte, and the time spent on the
// issue is in the answer to the write itself — a second one goes out and the sum moves again.
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
	// The day nobody named is today of the server, which is a moving number: only the form of it is held.
	assert.Regexp(t, `^[0-9]{4}-[0-9]{2}-[0-9]{2}T00:00:00Z$`, nodeAt(t, again, "date").Value)

	listed := runWith(t, dev.env(), "time", "list", issue, "--limit", "1")
	printed := requireWorkItemListing(t, listed)
	assert.Equal(t, 2, printed.Total)
	assert.True(t, printed.Truncated)
}

// A day written by a token whose profile stands ten hours east of UTC is kept as the day that was named:
// what goes out is noon UTC of it, which is 22:00 of that profile's zone and so still the same calendar day.
// Nothing but a token of another zone can say this — an instance where every profile sits at UTC+3 reads
// noon UTC and midnight UTC as one and the same day.
func TestTimeCreateWritesTheDayNamedByTheMemberOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member},
		"time", "create", issue, "PT1H", "--date", "2026-09-01", "--text", "ytrack contract zone")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	mapping := requireMapping(t, "stdout", got.stdout)
	assert.Equal(t, "2026-09-01T00:00:00Z", nodeAt(t, mapping, "date").Value)
	assert.Equal(t, "dev.member", nodeAt(t, mapping, "author", "login").Value)
}

// atThreePM is the replacement of the body of a write: the noon UTC ytrack sends for a day becomes three in the
// afternoon of it. ytrack has no way to send a moment — --date takes a day and refuses everything else — so the
// only place to put one is the wire, which is how the scenario asks the polygon whose time zone decides.
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

// Three in the afternoon of 1 September 2026, UTC: the moment DEV-7 is written against, kept by the polygon
// under 2 September.
const threePMUTC = 1788274800000

// Whose time zone a moment is filed under: the profile of the token that writes, not the zone of the
// instance. The same moment goes out twice — 15:00 UTC of 1 September — and comes back the day that
// was written for the admin, whose profile is Europe/Moscow, and the day after for the member, whose profile is
// Asia/Vladivostok. ytrack itself never sends such a moment, and the check of the write is what says so out
// loud on the second run.
func TestTimeCreateFindsTheDayOfAMomentComesFromTheProfileOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	dev.replacing(atThreePM)
	defer dev.replacing(nil)

	got := runWith(t, dev.env(), "time", "create", issue, "PT15M", "--date", "2026-09-01")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Equal(t, "2026-09-01T00:00:00Z", nodeAt(t, requireMapping(t, "stdout", got.stdout), "date").Value)

	byTheMember := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member},
		"time", "create", issue, "PT15M", "--date", "2026-09-01")

	found := requireUncertainty(t, byTheMember)
	assert.Equal(t, "upstream_lied", found.code)
	assert.Equal(t, []any{[]detail{
		{"field", "date"},
		{"written", "2026-09-01"},
		{"arrived", "2026-09-02T00:00:00Z"},
	}}, detailNamed(t, found, "mismatch"))
}

// The issue a scenario about a project with time tracking off files for itself. DOCS requires no custom field
// of a new issue, so nothing is named here: the defaults of the project are what DOCS-1 carries as well.
func contractDocsIssue(t *testing.T, dev *upstream) string {
	t.Helper()
	got := runWith(t, dev.env(), "issue", "create", "DOCS", "--summary",
		"ytrack contract "+t.Name())
	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	readable := nodeAt(t, requireMapping(t, "stdout", got.stdout), "idReadable").Value
	require.Regexp(t, `^DOCS-[0-9]+$`, readable)
	t.Cleanup(func() { removeIssue(t, dev, readable) })
	return readable
}

// A project whose time tracking is off answers a write with 403, and the admin is answered it as well: it
// is the project's setting rather than the caller's rights, so nothing ytrack could ask for would change it.
// The refusal passes on word for word, in one request, and the issue keeps no work item.
func TestTimeCreateWritesNoTimeInTheProjectOfTheDevInstanceThatHasTimeTrackingOff(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractDocsIssue(t, dev)
	before := len(dev.requests())

	got := runWith(t, dev.env(), "time", "create", issue, "PT30M")

	found := requireRefusal(t, got)
	assert.Equal(t, "denied", found.code)
	assert.Equal(t, 403, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, "Forbidden", detailNamed(t, found, "upstream_error"))
	assert.Equal(t, "HTTP 403 Forbidden", detailNamed(t, found, "upstream_message"))
	assert.Equal(t, []string{workItemsPath(issue)}, pathsSince(dev, before))

	byTheMember := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).member},
		"time", "create", issue, "PT30M")
	assert.Equal(t, "denied", requireRefusal(t, byTheMember).code)

	listed := runWith(t, dev.env(), "time", "list", issue)
	assert.Equal(t, 0, requireWorkItemListing(t, listed).Total)
}

// The server counts the minutes of an issue in an int32, so the largest duration ytrack will send passes and a
// single minute on top of it overflows the sum: Затраченное время comes back null, which the block of custom
// fields prints by leaving the key out altogether. Both writes answer 200 and exit 0 — the cascade is printed
// as it arrived and held against nothing, so this is what the overflow looks like from outside rather than a
// refusal.
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
	assert.NotContains(t, keysOf(nodeAt(t, broken, "issue", "customFields")), "Затраченное время")

	afterwards := runWith(t, dev.env(), "issue", "show", issue, "--fields", "customFields", "--comments=0")
	require.Equal(t, 0, afterwards.code, "stderr: %s", afterwards.stderr)
	assert.NotContains(t, keysOf(nodeAt(t, requireMapping(t, "stdout", afterwards.stdout), "customFields")),
		"Затраченное время")
}

// A duration of no length passes the grammar and the server refuses it in its own words: the refusal is
// not doubled locally, and nothing is written.
func TestTimeCreateWritesNoWorkItemOfNoLength(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")

	got := runWith(t, dev.env(), "time", "create", issue, "PT0M")

	found := requireRefusal(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, "Длительность работы не может быть отрицательной или пустой",
		detailNamed(t, found, "upstream_message"))

	listed := runWith(t, dev.env(), "time", "list", issue)
	assert.Equal(t, 0, requireWorkItemListing(t, listed).Total)
}

// asDurationID is the replacement of the body of a write: the minutes ytrack sends become the ISO period the
// server keeps as the id of a duration. It is how the scenario asks the polygon what it does with a duration
// written that way, over the write ytrack really sends.
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

// asPresentation is the same replacement with the length said the way the server shows one: a day, which is
// what TestTimeCreateFindsADayOfTheDevInstanceIsEightHours asks the polygon about.
func asPresentation(*http.Request, []byte) []byte {
	return []byte(`{"duration":{"presentation":"1д"}}`)
}

// ytrack sends the minutes and has no way to send the ISO period as the id of a duration, so that body is put
// on the wire between ytrack and the polygon: the server refuses it word for word rather than answering 200 and
// keeping nothing, which is what it does at a period field of an issue.
func TestTimeCreateIsRefusedTheDurationWrittenAsAnID(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	issue := contractWorkItemIssue(t, dev, "issue")
	dev.replacing(asDurationID)
	defer dev.replacing(nil)

	got := runWith(t, dev.env(), "time", "create", issue, "PT2H")

	found := requireRefusal(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, "Для единицы работы должна быть задана длительность",
		detailNamed(t, found, "upstream_message"))

	dev.replacing(nil)
	listed := runWith(t, dev.env(), "time", "list", issue)
	assert.Equal(t, 0, requireWorkItemListing(t, listed).Total)
}

// Why neither a day nor a week is written here. The polygon is sent the length as the server shows one, a
// day of it, and keeps eight hours: a day of YouTrack is the working day of the instance, so ytrack converting
// P1D would be ytrack agreeing with an arithmetic that is the instance's own and not ISO 8601's.
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

// An issue the polygon has none of and one this token may not see are the same 404 of the server, in one
// request, with nothing written: nothing of the issue is read before the write.
func TestTimeCreateWritesNothingForAnIssueTheDevInstanceDoesNotShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		issue string
		token func(*testing.T) string
	}{
		{
			name:  "an issue the polygon has none of",
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

			found := requireRefusal(t, got)
			assert.Equal(t, "not_found", found.code)
			assert.Equal(t, "Entity with id "+tc.issue+" not found", detailNamed(t, found, "upstream_message"))
			assert.Equal(t, []string{workItemsPath(tc.issue)}, dev.sentPaths())
		})
	}
}

// An expression that does not parse is refused where every other one is: before the network.
func TestTimeCreateRefusesAnExpressionItCannotSend(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H", "--fields", "a,,b")

	assert.Equal(t, refusal{code: "bad_usage"}, requireRefusal(t, got))
	assert.Empty(t, server.requests())
}
