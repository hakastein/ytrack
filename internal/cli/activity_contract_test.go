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

// paddedInstant is the moment with its fraction filled out to milliseconds, so that two of them compare as text
// the way they compare as moments: the renderer writes .4Z where .400Z would sort after .875Z.
func paddedInstant(printed string) string {
	moment, fraction, split := strings.Cut(strings.TrimSuffix(printed, "Z"), ".")
	if !split {
		fraction = ""
	}
	return moment + "." + fraction + strings.Repeat("0", 3-len(fraction))
}

// The fixture whose journal mixes every kind of change the polygon carries but an edit: a link of each of the
// five types, a comment, an attachment, a work item, the time spent it moved and the filing.
const mixedFixture = "DEV-1"

// The fixture whose journal carries an edit of the summary, of the description, of the text of a comment and a
// tag put on and taken off again.
const editedFixture = "DEV-7"

// The fixture that was resolved, which is the one journal of the polygon holding a resolution and a change of a
// state.
const resolvedFixture = "DEV-5"

// polygonCategories is the categories of the table held to the polygon, each by a scenario of its own.
// VcsChangeCategory is not among them: the polygon files no commit, and the category is held to records
// captured from a live instance instead.
func polygonCategories() []string {
	return slices.DeleteFunc(strings.Split(activityCategories, ","), func(category string) bool {
		return category == "VcsChangeCategory"
	})
}

// fixtureCarrying is the issue to hold a row of the table against.
func fixtureCarrying(category string) string {
	switch category {
	case "CommentTextCategory", "DescriptionCategory", "SummaryCategory", "TagsCategory":
		return editedFixture
	case "IssueResolvedCategory":
		return resolvedFixture
	}
	return mixedFixture
}

// journalPath is where the journal of an issue of the polygon is asked for.
func journalPath(issue string) string {
	return "/api/issues/" + issue + "/activities"
}

// editingJournal is the rewrite of a request on its way to the polygon, and it leaves every request but the
// journal as it was: categories stands on the activities of an issue and nowhere else.
func editingJournal(edit func(url.Values)) func(*url.URL) {
	return func(u *url.URL) {
		if !strings.HasSuffix(u.Path, "/activities") {
			return
		}
		asked := u.Query()
		edit(asked)
		u.RawQuery = asked.Encode()
	}
}

// sentValues is what the answer of the journal held under name for each activity, as JSON read it: a text the
// polygon sent is held against the document by the words that went over the wire and not by a copy of them.
func sentValues(t *testing.T, dev *upstream, name string) []any {
	t.Helper()
	requests, answers := dev.requests(), dev.answers()
	require.Len(t, answers, len(requests))
	for at, request := range requests {
		if !strings.HasSuffix(request.URL.Path, "/activities") {
			continue
		}
		var arrived []map[string]any
		require.NoError(t, json.Unmarshal(answers[at], &arrived), "the answer of the journal: %s", answers[at])
		held := make([]any, 0, len(arrived))
		for _, activity := range arrived {
			held = append(held, activity[name])
		}
		return held
	}
	require.FailNow(t, "no request reached the activities of an issue")
	return nil
}

// The specification marks categories optional and the server requires it, so a journal that forgot it would come
// back a refusal rather than a journal of everything. ytrack sends it on every call, and what the server does
// without it is held by taking it off between ytrack and the polygon.
func TestActivityFindsTheDevInstanceAsksForTheCategories(t *testing.T) {
	t.Parallel()
	dev := devInstanceRewriting(t, editingJournal(func(asked url.Values) { asked.Del("categories") }))

	got := runWith(t, dev.env(), "activity", "list", mixedFixture)

	found := requireRefusal(t, got)
	assert.Equal(t, "rejected", found.code)
	assert.Equal(t, 400, detailNamed(t, found, "upstream_status"))
	assert.Equal(t, "No requested categories specified as a filter parameter",
		detailNamed(t, found, "upstream_message"))
	assert.NotContains(t, activitySent(t, dev), "categories")
	assert.Equal(t, 1, sentTo(dev, journalPath(mixedFixture)))
}

// A category the server does not know is answered with a journal of nothing rather than with a refusal, and the
// letter case is part of the name: both are why the name is resolved before the request and goes out as ytrack
// keeps it. Neither request is one ytrack sends, so both are made between it and the polygon.
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
			dev := devInstanceRewriting(t, editingJournal(func(asked url.Values) { asked.Set("categories", tc.categories) }))

			got := runWith(t, dev.env(), "activity", "list", mixedFixture)

			assert.Equal(t, outcome{stdout: "total: 0\nreturned: 0\ntruncated: false\nactivities: []\n"}, got)
			assert.Equal(t, []string{tc.categories}, activitySent(t, dev)["categories"])
		})
	}
}

// Every row of the table held to the polygon is an assertion about the instance and not a copy of the
// specification, which declares none of them: the polygon answers each row with activities, and with activities
// of that row alone.
func TestActivityHoldsEachCategoryOfTheListToTheDevInstance(t *testing.T) {
	t.Parallel()
	for _, category := range polygonCategories() {
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

// The help is where a caller reads the list, so it is the list the contract holds and not a second one: every
// row of it is held either to the polygon or, for the commits the polygon cannot file, to records of a live
// instance.
func TestActivityHelpNamesTheCategoriesHeldToTheDevInstance(t *testing.T) {
	t.Parallel()

	got := run(t, []string{"activity", "list", "--help"})

	require.Equal(t, 0, got.code)
	for _, category := range append(polygonCategories(), "VcsChangeCategory") {
		assert.Contains(t, got.stdout, category)
	}
}

// The journal of the mixed fixture under the default: every record is one change, printed without the issue it
// belongs to, and whatever a change put there or took away stands as a list — the minutes of the time spent as
// the period they make, a link as the issue at its other end, a comment, an attachment and a work item as the
// entities they are, each by the names the default asks of a value and it has.
func TestActivityPrintsTheMixedJournalOfTheDevInstance(t *testing.T) {
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
			assert.GreaterOrEqual(t, before, paddedInstant(moment.Value), "the activities arrived out of order")
		}
		before = paddedInstant(moment.Value)
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
	assert.Equal(t, 1, sentTo(dev, journalPath(mixedFixture)))
}

// Both ends of every link of the mixed fixture, each named by the phrase of the end the fixture stands at and by
// the issue at the other end: five links of five types, and the phrase of each is the untranslated one,
// whatever language the polygon writes its records in.
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

// A polymorphic request over the mixed journal: each name is printed on the values whose type has it and left
// out of the rest, and none of them is refused, though no type has all three.
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

// The fixture that was resolved: its state changed from one value of a bundle to another, each printed as the
// tree the default asks of a value, and its resolution holds the moment it happened as the one item of a list.
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

// An edit of the text of the edited fixture prints the text the polygon sent, both what the edit put there and
// what it took away, byte for byte, as the one item of each list.
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

// A tag put on and taken off again are two records, each holding the tag at the end it stands at and nothing at
// the other.
func TestActivityPrintsTheTagsOfTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", editedFixture, "--category", "TagsCategory", "--limit", "100000")

	printed := requireActivityListing(t, got)
	require.Len(t, printed.Activities, 2)
	// Newest first, so the tag comes off in the first record and went on in the second.
	assert.Equal(t, []any{}, printed.Activities[0]["added"])
	assert.Equal(t, []any{}, printed.Activities[1]["removed"])
	assert.Equal(t, printed.Activities[0]["removed"], printed.Activities[1]["added"])
	tag := printed.Activities[1]["added"].([]any)
	require.Len(t, tag, 1)
	assert.Equal(t, "история-полигона", tag[0].(map[string]any)["name"])
}

func TestActivityCutsTheJournalOfTheDevInstanceAtTheLimit(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", mixedFixture, "--limit", "3")

	printed := requireActivityListing(t, got)
	assert.Equal(t, 3, printed.Returned)
	assert.True(t, printed.Truncated)
	assert.Nil(t, printed.Total)
	assert.Equal(t, []string{"4"}, activitySent(t, dev)["$top"])
}

// The caller's own names are judged as they are everywhere else: a name no schema of its place declares is
// theirs to fix, with the names of the place to fix it by.
func TestActivityRefusesANameTheSchemasOfTheDevInstanceDoNotDeclare(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "activity", "list", mixedFixture, "--fields", "timestamp,autor")

	found := requireRefusal(t, got)
	assert.Equal(t, "unknown_name", found.code)
	assert.Equal(t, []any{unknownEntry("autor", "author")}, detailNamed(t, found, "unknown"))
	// The expression of the refusal is the one that went out, so the names ytrack merged in stand in it too.
	assert.Equal(t, "timestamp,autor,category(id)", detailNamed(t, found, "fields"))
	assert.Equal(t, 1, sentTo(dev, journalPath(mixedFixture)))
}

// The limited token is on no project, so the polygon answers the journal of an issue it cannot see as it answers
// an issue nobody has: 404.
func TestActivityFindsNoIssueOfTheDevInstanceForTheLimitedToken(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, []string{"YTRACK_URL=" + dev.url, "YTRACK_TOKEN=" + devTokens(t).limited},
		"activity", "list", mixedFixture)

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, 404, detailNamed(t, found, "upstream_status"))
	assert.Empty(t, got.stdout)
	assert.Equal(t, 1, sentTo(dev, journalPath(mixedFixture)))
}
