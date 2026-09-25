package cli_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func oneWorkItem(duration, date string) string {
	return `[{"$type":"IssueWorkItem","id":"199-6","duration":` + duration + `,"date":` + date + `}]`
}

// The duration as YouTrack answers with it: the minutes, the same length written in the language of the
// instance, and the id, which is the minutes as text.
func receivedDuration(minutes, presentation string) string {
	return `{"$type":"DurationValue","minutes":` + minutes + `,"presentation":"` + presentation +
		`","id":"` + minutes + `"}`
}

// A duration is printed as the one length it is, so no name stands under it: neither the minutes it is read
// from, nor the two forms the server writes beside them. Nothing of this costs a request.
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
			server := serveNothing(t)

			got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", tc.expression)

			want := faultDocument{code: "bad_usage"}
			assert.Equal(t, want, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The minutes are what the period is made of, whatever the server writes beside them: hours alone, minutes
// alone, both, a whole day and the largest length the server takes all read the one way.
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
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,duration")

			want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" +
				`  - {id: "199-6", duration: "` + tc.want + `"}` + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
			requests := server.requests()
			require.Len(t, requests, 1)
			assert.Equal(t, url.Values{"fields": {"id,duration(minutes)"}, "$top": {"50"}}, requests[0].URL.Query())
			assert.NotContains(t, got.stdout, "presentation")
			assert.NotContains(t, got.stdout, "1ч")
			assert.NotContains(t, got.stdout, `"`+tc.minutes+`"`)
		})
	}
}

// The minutes are the whole of what a duration is read from, so a duration that arrives without them, or
// with something other than a whole number of them, is a refusal and no document.
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
			server := serve(t, respondWith(http.StatusOK, oneWorkItem(tc.received, "1788220800000")))

			got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,duration")

			assert.Equal(t, "upstream_invalid", requireRefusal(t, got).code)
			assert.Empty(t, got.stdout)
		})
	}
}

// A duration the issue holds none of is printed empty, as every other name asked for and empty is: an
// absent length is not a lie about one.
func TestTimeListPrintsADurationTheWorkItemHasNone(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, oneWorkItem("null", "null")))

	got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,duration,date")

	want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" +
		`  - {id: "199-6", duration: null, date: null}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

// The day is printed as the moment it arrived, in UTC and in no zone of the reader: YouTrack keeps midnight
// UTC of the calendar day it was written against, and a moment that is not midnight is printed as it stands
// rather than moved to one.
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
			server := serve(t, respondWith(http.StatusOK, oneWorkItem(receivedDuration("90", "1ч 30м"), tc.received)))

			got := runWith(t, server.env(), "time", "list", "DEV-1", "--fields", "id,date")

			want := "total: 1\nreturned: 1\ntruncated: false\nworkItems:\n" +
				`  - {id: "199-6", date: "` + tc.want + `"}` + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}
