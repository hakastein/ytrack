package cli_test

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sortableInstant(printed string) string {
	moment, fraction, split := strings.Cut(strings.TrimSuffix(printed, "Z"), ".")
	if !split {
		fraction = ""
	}
	return moment + "." + fraction + strings.Repeat("0", 3-len(fraction))
}

const mixedFixture = "DEV-1"

const editedFixture = "DEV-7"

const resolvedFixture = "DEV-5"

func devInstanceCategories() []string {
	return slices.DeleteFunc(strings.Split(activityCategories, ","), func(category string) bool {
		return category == "VcsChangeCategory"
	})
}

func fixtureCarrying(category string) string {
	switch category {
	case "CommentTextCategory", "DescriptionCategory", "SummaryCategory", "TagsCategory":
		return editedFixture
	case "IssueResolvedCategory":
		return resolvedFixture
	}
	return mixedFixture
}

func activitiesPathOf(issue string) string {
	return "/api/issues/" + issue + "/activities"
}

func editingActivities(edit func(url.Values)) func(*url.URL) {
	return func(u *url.URL) {
		if !strings.HasSuffix(u.Path, "/activities") {
			return
		}
		asked := u.Query()
		edit(asked)
		u.RawQuery = asked.Encode()
	}
}

func sentValues(t *testing.T, dev *upstream, name string) []any {
	t.Helper()
	requests, answers := dev.requests(), dev.answers()
	require.Len(t, answers, len(requests))
	for at, request := range requests {
		if !strings.HasSuffix(request.URL.Path, "/activities") {
			continue
		}
		var received []map[string]any
		require.NoError(t, json.Unmarshal(answers[at], &received), "the answer with activities: %s", answers[at])
		held := make([]any, 0, len(received))
		for _, activity := range received {
			held = append(held, activity[name])
		}
		return held
	}
	require.FailNow(t, "no request reached the activities of an issue")
	return nil
}

func TestActivityFindsTheDevInstanceAsksForTheCategories(t *testing.T) {
	t.Parallel()
	dev := devInstanceWithRewrite(t, editingActivities(func(asked url.Values) { asked.Del("categories") }))

	got := runWith(t, dev.env(), "activity", "list", mixedFixture)

	found := requireFault(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, 400, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, "No requested categories specified as a filter parameter",
		detailNamed(t, found, "upstream_message"))
	assert.NotContains(t, activitySent(t, dev), "categories")
	assert.Equal(t, 1, sentTo(dev, activitiesPathOf(mixedFixture)))
}

func TestActivityFindsTheDevInstanceAnswersACategoryItDoesNotKnowWithNoActivity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		categories string
	}{
		{name: "a category of no instance at all", categories: "BogusCategory"},
		{name: "a category of the list in another letter case", categories: "linkscategory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstanceWithRewrite(t, editingActivities(func(asked url.Values) { asked.Set("categories", tc.categories) }))

			got := runWith(t, dev.env(), "activity", "list", mixedFixture)

			assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nactivities: []\n"}, got)
			assert.Equal(t, []string{tc.categories}, activitySent(t, dev)["categories"])
		})
	}
}

func TestActivityChecksEachCategoryOfTheListAgainstTheDevInstance(t *testing.T) {
	t.Parallel()
	for _, category := range devInstanceCategories() {
		t.Run(category, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			got := runWith(t, dev.env(), "activity", "list", fixtureCarrying(category),
				"--category", category, "--limit", "100000", "--fields", "category")

			printed := requireActivityListing(t, got)
			assert.False(t, printed.Truncated)
			assert.GreaterOrEqual(t, printed.Returned, 1)
			for _, record := range activities(t, got) {
				assert.Equal(t, []string{"category"}, recordKeys(record))
				assert.Equal(t, category, nodeAt(t, record, "category").Value)
			}
		})
	}
}

func TestActivityHelpNamesTheCategoriesCheckedAgainstTheDevInstance(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"activity", "list", "--help"})

	require.Equal(t, 0, got.code)
	for _, category := range append(devInstanceCategories(), "VcsChangeCategory") {
		assert.Contains(t, got.stdout, category)
	}
}

func TestActivityPrintsTheMixedActivitiesOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", mixedFixture, "--limit", "100000")

	printed := requireActivityListing(t, got)
	assert.False(t, printed.Truncated)
	require.NotNil(t, printed.Total)
	assert.Equal(t, printed.Returned, *printed.Total)
	before := ""
	byCategory := map[string][]map[string]any{}
	for at, record := range activities(t, got) {
		assert.Equal(t, []string{"timestamp", "author", "category", "field", "added", "removed"}, recordKeys(record))
		moment := nodeAt(t, record, "timestamp")
		assert.Regexp(t, instantForm, moment.Value)
		if before != "" {
			assert.GreaterOrEqual(t, before, sortableInstant(moment.Value), "the activities arrived out of order")
		}
		before = sortableInstant(moment.Value)
		one := printed.Activities[at]
		assert.IsType(t, []any{}, one["added"], "added is no list: %v", one)
		assert.IsType(t, []any{}, one["removed"], "removed is no list: %v", one)
		category, _ := one["category"].(string)
		byCategory[category] = append(byCategory[category], one)
	}
	for _, category := range []string{"AttachmentsCategory", "CommentsCategory", "CustomFieldCategory",
		"IssueCreatedCategory", "LinksCategory", "WorkItemCategory"} {
		assert.NotEmpty(t, byCategory[category], "no activity of %s was printed", category)
	}
	spent := slices.IndexFunc(byCategory["CustomFieldCategory"], func(record map[string]any) bool {
		return record["field"] == "Затраченное время"
	})
	require.GreaterOrEqual(t, spent, 0, "no change of the time spent was printed")
	assert.Equal(t, []any{"PT1H30M"}, byCategory["CustomFieldCategory"][spent]["added"])
	assert.Equal(t, []any{}, byCategory["CustomFieldCategory"][spent]["removed"])
	for _, record := range byCategory["AttachmentsCategory"] {
		added := record["added"].([]any)
		require.Len(t, added, 1)
		assert.Equal(t, "заметка-полигона.txt", added[0].(map[string]any)["name"])
	}
	for _, category := range []string{"CommentsCategory", "WorkItemCategory"} {
		for _, record := range byCategory[category] {
			assert.Equal(t, []any{map[string]any{"id": record["added"].([]any)[0].(map[string]any)["id"]}}, record["added"],
				"a comment and a work item are named by the id alone: %v", record)
			assert.Regexp(t, `^[0-9]+-[0-9]+$`, record["added"].([]any)[0].(map[string]any)["id"])
		}
	}
	for _, record := range byCategory["IssueCreatedCategory"] {
		assert.Nil(t, record["field"])
		assert.Equal(t, []any{}, record["added"])
	}
	for _, unwanted := range []string{"target", "$type", "Зависит", "163-"} {
		assert.NotContains(t, got.stdout, unwanted)
	}
	assert.Equal(t, 1, sentTo(dev, activitiesPathOf(mixedFixture)))
}

func TestActivityPrintsTheLinksOfTheDevInstanceByTheirPhrases(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", mixedFixture, "--category", "LinksCategory",
		"--limit", "100000", "--fields", "field,added(idReadable),removed(idReadable)")

	printed := requireActivityListing(t, got)
	assert.False(t, printed.Truncated)
	for _, want := range []map[string]any{
		{"field": "relates to", "added": []any{map[string]any{"idReadable": "DEV-2"}}, "removed": []any{}},
		{"field": "depends on", "added": []any{map[string]any{"idReadable": "DEV-3"}}, "removed": []any{}},
		{"field": "subtask of", "added": []any{map[string]any{"idReadable": "DEV-4"}}, "removed": []any{}},
		{"field": "is duplicated by", "added": []any{map[string]any{"idReadable": "DEV-5"}}, "removed": []any{}},
		{"field": "Скопирована в", "added": []any{map[string]any{"idReadable": "DEV-6"}}, "removed": []any{}},
	} {
		assert.Contains(t, printed.Activities, want)
	}
	assert.Equal(t, 1, sentTo(dev, linkTypesPath))
}

func TestActivityPrintsTheNamesOfEachTypeOfValueOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", mixedFixture, "--limit", "100000",
		"--fields", "category,added(idReadable,login,name)")

	printed := requireActivityListing(t, got)
	assert.Contains(t, printed.Activities, map[string]any{
		"category": "AttachmentsCategory", "added": []any{map[string]any{"name": "заметка-полигона.txt"}},
	})
	assert.Contains(t, printed.Activities, map[string]any{
		"category": "LinksCategory", "added": []any{map[string]any{"idReadable": "DEV-2"}},
	})
	assert.Contains(t, printed.Activities, map[string]any{"category": "CommentsCategory", "added": []any{map[string]any{}}})
	assert.Contains(t, printed.Activities, map[string]any{"category": "CustomFieldCategory", "added": []any{"PT1H30M"}})
}

func TestActivityPrintsTheResolutionAndTheStateOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", resolvedFixture, "--limit", "100000",
		"--category", "CustomFieldCategory", "--category", "IssueResolvedCategory", "--fields", "category,field,added(id,name),removed(id,name)")

	printed := requireActivityListing(t, got)
	var state, resolution map[string]any
	for _, record := range printed.Activities {
		switch {
		case record["field"] == "State":
			state = record
		case record["category"] == "IssueResolvedCategory":
			resolution = record
		}
	}
	require.NotNil(t, state, "no change of the state was printed: %s", got.stdout)
	require.NotNil(t, resolution, "no resolution was printed: %s", got.stdout)
	added, removed := state["added"].([]any), state["removed"].([]any)
	require.Len(t, added, 1)
	require.Len(t, removed, 1)
	assert.Equal(t, "Duplicate", added[0].(map[string]any)["name"])
	assert.Equal(t, "Новая", removed[0].(map[string]any)["name"])
	assert.Regexp(t, `^[0-9]+-[0-9]+$`, added[0].(map[string]any)["id"])
	assert.Nil(t, resolution["field"])
	require.Len(t, resolution["added"], 1)
	assert.Regexp(t, instantForm, resolution["added"].([]any)[0])
	assert.Equal(t, []any{}, resolution["removed"])
	assert.NotContains(t, got.stdout, "Состояние")
}

func TestActivityPrintsTheEditsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	for _, category := range []string{"DescriptionCategory", "SummaryCategory", "CommentTextCategory"} {
		t.Run(category, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)

			got := runWith(t, dev.env(), "activity", "list", editedFixture, "--category", category, "--limit", "100000")

			printed := requireActivityListing(t, got)
			require.Len(t, printed.Activities, 1)
			record := printed.Activities[0]
			assert.Nil(t, record["field"])
			assert.Equal(t, []any{sentValues(t, dev, "added")[0]}, record["added"])
			assert.Equal(t, []any{sentValues(t, dev, "removed")[0]}, record["removed"])
			assert.NotEmpty(t, sentValues(t, dev, "added")[0])
		})
	}
}

func TestActivityPrintsTheTagsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", editedFixture, "--category", "TagsCategory", "--limit", "100000")

	printed := requireActivityListing(t, got)
	require.Len(t, printed.Activities, 2)
	newer, older := printed.Activities[0], printed.Activities[1]
	assert.Equal(t, []any{}, newer["added"])
	assert.Equal(t, []any{}, older["removed"])
	assert.Equal(t, newer["removed"], older["added"])
	tag := older["added"].([]any)
	require.Len(t, tag, 1)
	assert.Equal(t, "история-полигона", tag[0].(map[string]any)["name"])
}

func TestActivityCutsTheActivitiesOfTheDevInstanceAtTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", mixedFixture, "--limit", "3")

	printed := requireActivityListing(t, got)
	assert.Equal(t, 3, printed.Returned)
	assert.True(t, printed.Truncated)
	assert.Nil(t, printed.Total)
	assert.Equal(t, []string{"4"}, activitySent(t, dev)["$top"])
}

func TestActivityRefusesANameTheSchemasOfTheDevInstanceDoNotDeclare(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", mixedFixture, "--fields", "timestamp,autor")

	found := requireFault(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, []any{unknownEntry("autor", "author")}, detailNamed(t, found, "unknown"))
	assert.Equal(t, "timestamp,autor,category(id)", detailNamed(t, found, "fields"))
	assert.Equal(t, 1, sentTo(dev, activitiesPathOf(mixedFixture)))
}

func TestActivityFindsNoIssueOfTheDevInstanceForTheLimitedToken(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"activity", "list", mixedFixture)

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, 404, detailNamed(t, found, "upstream_status"))
	assert.Empty(t, got.stdout)
	assert.Equal(t, 1, sentTo(dev, activitiesPathOf(mixedFixture)))
}
