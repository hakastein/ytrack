package cli_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// everyCategory is the whole list as a refusal prints it, which is what a caller near none of the names is
// shown: the names by code point, the order the list itself stands in.
func everyCategory() []any {
	names := []any{}
	for _, category := range strings.Split(activityCategories, ",") {
		names = append(names, category)
	}
	return names
}

// unknownCategories is the refusal a journal is stopped by before it reaches the network, with one entry per
// name that resolved to nothing, in the order those names were written.
func unknownCategories(entries ...[]detail) faultDocument {
	unknown := []any{}
	for _, entry := range entries {
		unknown = append(unknown, entry)
	}
	return faultDocument{
		code:    "unknown_name",
		details: []detail{{"unknown", unknown}},
	}
}

func categoryEntry(written string, nearest []any) []detail {
	return []detail{{"category", written}, {"nearest", nearest}}
}

// A caller nowhere near any of the names is shown the whole list: the names of the categories are ytrack's own
// writing, and there is nowhere else to read them, the server listing none of them and the specification
// declaring none either.
func TestActivityShowsEveryCategoryToACallerNearNoneOfThem(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--category", "Bogus")

	assert.Equal(t, unknownCategories(categoryEntry("Bogus", everyCategory())), requireRefusal(t, got))
	assert.Empty(t, server.requests())
}

// A category is resolved where it is written and not by the server: YouTrack answers a name of no category of
// its own with an empty journal, so a misspelling sent on would read as an issue nothing ever happened to.
func TestActivityRefusesANameOfNoCategoryBeforeItAsksForAnything(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
		want  faultDocument
	}{
		{
			name:  "a category a letter short",
			flags: []string{"--category", "LinksCategry"},
			want:  unknownCategories(categoryEntry("LinksCategry", []any{"LinksCategory"})),
		},
		{
			name: "a category holding a letter of another alphabet",
			// The Cyrillic С of U+0421, which reads as the Latin C it stands in for.
			flags: []string{"--category", "Links\xd0\xa1ategory"},
			want:  unknownCategories(categoryEntry("Links\xd0\xa1ategory", []any{"LinksCategory"})),
		},
		{
			name:  "two categories written as one name",
			flags: []string{"--category", "LinksCategory,CommentsCategory"},
			want:  unknownCategories(categoryEntry("LinksCategory,CommentsCategory", everyCategory())),
		},
		{
			name:  "one misspelling written in two letter cases",
			flags: []string{"--category", "Bogus", "--category", "BOGUS"},
			want:  unknownCategories(categoryEntry("Bogus", everyCategory())),
		},
		{
			name:  "two misspellings beside a category that resolves",
			flags: []string{"--category", "Bogus", "--category", "LinksCategory", "--category", "Nope"},
			want: unknownCategories(categoryEntry("Bogus", everyCategory()),
				categoryEntry("Nope", everyCategory())),
		},
		{
			name:  "a category of nothing at all",
			flags: []string{"--category", ""},
			want:  faultDocument{code: "bad_usage"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := serveNothing(t)

			got := runWith(t, server.env(), slices.Concat([]string{"activity", "list", journalIssue}, tc.flags)...)

			assert.Equal(t, tc.want, requireRefusal(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

// The letter case a caller wrote is theirs and the letter case that goes out is YouTrack's, so the same category
// written twice is asked for once, and the categories stand in the order of the list rather than in the order
// they were written.
func TestActivityAsksForEachCategoryOnceInTheOrderOfTheList(t *testing.T) {
	t.Parallel()
	server := journal(t, respondWith(http.StatusOK, noActivities))

	got := runWith(t, server.env(), "activity", "list", journalIssue,
		"--category", "linkscategory", "--category", "LINKSCATEGORY", "--category", "CommentsCategory")

	assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nactivities: []\n"}, got)
	assert.Equal(t, []string{"CommentsCategory,LinksCategory"}, activitySent(t, server)["categories"])
	assert.Equal(t, 1, sentTo(server, activitiesPath))
}

func TestActivityRefusesAnActivityItCannotReadTheCategoryOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		activity sentActivity
	}{
		{
			name: "a category that arrived as a list of one",
			activity: sentActivity{
				kind: "LinksActivityItem", timestamp: middle,
				categoryRaw: `[{"$type":"ActivityCategory","id":"LinksCategory"}]`,
			},
		},
		{
			name: "a category whose identifier arrived as a number",
			activity: sentActivity{
				kind: "LinksActivityItem", timestamp: middle,
				categoryRaw: `{"$type":"ActivityCategory","id":163}`,
			},
		},
		{
			name: "an activity whose moment arrived as the text of one",
			activity: sentActivity{
				kind: "IssueCreatedActivityItem", category: "IssueCreatedCategory",
				timestamp: `"2026-09-10T10:16:51Z"`,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := journal(t, respondWith(http.StatusOK, `[`+tc.activity.sent()+`]`))

			got := runWith(t, server.env(), "activity", "list", journalIssue)

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Empty(t, got.stdout)
		})
	}
}
