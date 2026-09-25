package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

func oneWorkItem(duration, date string) string {
	return `[{"$type":"IssueWorkItem","id":"199-6","duration":` + duration + `,"date":` + date + `}]`
}

func receivedDuration(minutes, presentation string) string {
	return `{"$type":"DurationValue","minutes":` + minutes + `,"presentation":"` + presentation +
		`","id":"` + minutes + `"}`
}

func TestTimeListRefusesANameUnderTheDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
	}{
		{name: "the minutes it is read from", expression: "duration(minutes)"},
		{name: "the form the server writes for a human", expression: "duration(presentation)"},
		{name: "the id, which is the minutes as text", expression: "+duration(id)"},
		{name: "the whole of what a duration arrives with", expression: "id,duration(minutes,presentation,id)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, server.Env(), "time", "list", "DEV-1", "--fields", tc.expression)

			want := faultDocument{code: "bad_usage"}
			assert.Equal(t, want, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}

func TestTimeListPrintsTheDurationAsAPeriodOfTheMinutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		minutes string
		want    string
	}{
		{name: "hours and minutes", minutes: "90", want: "PT1H30M"},
		{name: "a whole hour", minutes: "60", want: "PT1H"},
		{name: "one minute", minutes: "1", want: "PT1M"},
		{name: "a day of the clock", minutes: "1440", want: "PT24H"},
		{name: "the largest the server takes", minutes: "2147483647", want: "PT35791394H7M"},
		{name: "nothing at all", minutes: "0", want: "PT0M"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := oneWorkItem(receivedDuration(tc.minutes, "1ч 30м"), "1788220800000")
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			got := runWith(t, server.Env(), "time", "list", "DEV-1", "--fields", "id,duration")

			want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" +
				`  - {id: "199-6", duration: "` + tc.want + `"}` + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
			requests := server.Requests()
			require.Len(t, requests, 1)
			assert.Equal(t, url.Values{"fields": {"id,duration(minutes)"}, "$top": {"50"}}, requests[0].URL.Query())
			assert.NotContains(t, got.stdout, "presentation")
			assert.NotContains(t, got.stdout, "1ч")
			assert.NotContains(t, got.stdout, `"`+tc.minutes+`"`)
		})
	}
}

func TestTimeListRefusesADurationItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
	}{
		{name: "no minutes at all", received: `{"$type":"DurationValue","presentation":"1ч 30м"}`},
		{name: "a fraction of a minute", received: `{"$type":"DurationValue","minutes":1.5}`},
		{name: "the minutes as text", received: `{"$type":"DurationValue","minutes":"90"}`},
		{name: "a duration that is no object", received: `"PT1H30M"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, oneWorkItem(tc.received, "1788220800000")))

			got := runWith(t, server.Env(), "time", "list", "DEV-1", "--fields", "id,duration")

			assert.Equal(t, "upstream_invalid", requireFault(t, got).code)
			assert.Empty(t, got.stdout)
		})
	}
}

func TestTimeListPrintsADurationTheWorkItemHasNone(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, oneWorkItem("null", "null")))

	got := runWith(t, server.Env(), "time", "list", "DEV-1", "--fields", "id,duration,date")

	want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" +
		`  - {id: "199-6", duration: null, date: null}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestTimeListPrintsTheDayOfAWorkItemInUTC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
		want     string
	}{
		{name: "midnight UTC, which is what the server keeps", received: "1788220800000", want: "2026-09-01T00:00:00Z"},
		{name: "noon UTC of the same day", received: "1788264000000", want: "2026-09-01T12:00:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, oneWorkItem(receivedDuration("90", "1ч 30м"), tc.received)))

			got := runWith(t, server.Env(), "time", "list", "DEV-1", "--fields", "id,date")

			want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" +
				`  - {id: "199-6", date: "` + tc.want + `"}` + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}
