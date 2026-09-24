package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An author with a whole tree of the catalogue under it, as the server would send one: the names a record of the
// journal is read by — field, added, removed — are declared by other schemas too, and every schema below
// is reachable from an activity by a path the specification itself draws.
func sentAuthorHolding(held string) string {
	return `{"$type":"User","login":"admin","savedQueries":[{"$type":"SavedQuery","issues":[{"$type":"Issue",` +
		held + `}]}]}`
}

// The custom field of a project, reached through the saved queries of the author: ProjectCustomField declares a
// field of its own, which is a CustomField and not the filter a record of the journal stands for.
const sentProjectField = `"customFields":[{"$type":"IssueCustomField","projectCustomField":` +
	`{"$type":"ProjectCustomField","field":{"$type":"CustomField","name":"Priority"}}}]`

// Whether an attachment of an issue was taken off, reached the same way: IssueAttachment declares removed as the
// flag it is, while a record of the journal holds the values a change took away under that name.
const sentAttachmentRemoved = `"attachments":[{"$type":"IssueAttachment","removed":false}]`

// The rules of the table of categories are the record's own: they say what field, added and removed hold
// on a record, and a name of the same spelling standing deeper in the expression belongs to the schema of its own
// place. Both paths below are ones the specification draws, so the judgment of names lets them through.
func TestActivityReadsTheNamesOfARecordAtTheRecordAndNowhereBelowIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		activity   string
		expression string
		want       string
	}{
		{
			name: "the field of a project's custom field, under the saved queries of the author",
			activity: sentActivity{
				kind: "IssueCreatedActivityItem", category: "IssueCreatedCategory", timestamp: middle,
				author: sentAuthorHolding(sentProjectField),
			}.sent(),
			expression: "author(savedQueries(issues(customFields(projectCustomField(field(name))))))",
			want: `author: {savedQueries: [{issues: [{customFields: [{projectCustomField: ` +
				`{field: {name: "Priority"}}}]}]}]}`,
		},
		{
			name: "the removed of an attachment, under the saved queries of the author",
			activity: sentActivity{
				kind: "LinksActivityItem", category: "LinksCategory", timestamp: middle,
				field: `{"$type":"LinkTypeFilterField","name":"Зависит от"}`, added: sentLinkedIssue,
				author: sentAuthorHolding(sentAttachmentRemoved),
			}.sent(),
			expression: "author(savedQueries(issues(attachments(removed))))",
			want:       `author: {savedQueries: [{issues: [{attachments: [{removed: false}]}]}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := journal(t, answer(http.StatusOK, `[`+tc.activity+`]`))

			got := runWith(t, server.env(), "activity", "list", journalIssue,
				"--fields", tc.expression)

			assert.Equal(t, outcome{stdout: oneRecord(tc.want)}, got)
		})
	}
}

// The names a caller is offered are the ones the place declares, so a caller near none of them is shown what
// there is: the family of the root is ActivityItem with every subtype of it, and no name of a schema below.
func TestActivityShowsTheNamesOfAnActivityToACallerNearNoneOfThem(t *testing.T) {
	t.Parallel()
	server := journal(t, answer(http.StatusOK, `[`+sentCreatedActivity(middle)+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--fields", "timestamp,zzzzzz")

	found := requireRefusal(t, got)
	assert.Equal(t, "unknown_name", found.code)
	entry, isEntry := detailNamed(t, found, "unknown").([]any)
	require.True(t, isEntry, "the unknown of the refusal: %v", found.details)
	require.Len(t, entry, 1)
	assert.Equal(t, []detail{
		{"field", "zzzzzz"},
		{"nearest", []any{"$type", "added", "author", "authorGroup", "category", "field", "id", "markup",
			"removed", "target", "targetMember", "targetSubMember", "timestamp"}},
	}, entry[0])
}
