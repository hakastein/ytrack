package cli_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const cyrillicCapitalEs = "\u0421"

func everyCategory() []any {
	names := []any{}
	for _, category := range strings.Split(activityCategories, ",") {
		names = append(names, category)
	}
	return names
}

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

func TestActivityShowsEveryCategoryToACallerNearNoneOfThem(t *testing.T) {
	t.Parallel()
	server := serveNothing(t)

	got := runWith(t, server.env(), "activity", "list", activityIssue, "--category", "Bogus")

	assert.Equal(t, unknownCategories(categoryEntry("Bogus", everyCategory())), requireFault(t, got))
	assert.Empty(t, server.requests())
}

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
			name:  "a category holding a letter of another alphabet",
			flags: []string{"--category", "Links" + cyrillicCapitalEs + "ategory"},
			want:  unknownCategories(categoryEntry("Links"+cyrillicCapitalEs+"ategory", []any{"LinksCategory"})),
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

			got := runWith(t, server.env(), slices.Concat([]string{"activity", "list", activityIssue}, tc.flags)...)

			assert.Equal(t, tc.want, requireFault(t, got))
			assert.Empty(t, server.requests())
		})
	}
}

func TestActivityAsksForEachCategoryOnceInTheOrderOfTheList(t *testing.T) {
	t.Parallel()
	server := activityServer(t, respondWith(http.StatusOK, noActivities))

	got := runWith(t, server.env(), "activity", "list", activityIssue,
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
			server := activityServer(t, respondWith(http.StatusOK, `[`+tc.activity.sent()+`]`))

			got := runWith(t, server.env(), "activity", "list", activityIssue)

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Empty(t, got.stdout)
		})
	}
}
