package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

func sentAuthorWith(held string) string {
	return `{"$type":"User","login":"admin","savedQueries":[{"$type":"SavedQuery","issues":[{"$type":"Issue",` +
		held + `}]}]}`
}

const sentProjectField = `"customFields":[{"$type":"IssueCustomField","projectCustomField":` +
	`{"$type":"ProjectCustomField","field":{"$type":"CustomField","name":"Priority"}}}]`

const sentAttachmentRemoved = `"attachments":[{"$type":"IssueAttachment","removed":false}]`

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
				author: sentAuthorWith(sentProjectField),
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
				author: sentAuthorWith(sentAttachmentRemoved),
			}.sent(),
			expression: "author(savedQueries(issues(attachments(removed))))",
			want:       `author: {savedQueries: [{issues: [{attachments: [{removed: false}]}]}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, fake.JSON(http.StatusOK, `[`+tc.activity+`]`))

			got := runWith(t, server.Env(), "activity", "list", activityIssue,
				"--fields", tc.expression)

			assert.Equal(t, outcome{stdout: oneRecord(tc.want)}, got)
		})
	}
}

func TestActivityShowsTheNamesOfAnActivityToACallerNearNoneOfThem(t *testing.T) {
	t.Parallel()
	server := activityServer(t, fake.JSON(http.StatusOK, `[`+sentCreatedActivity(middle)+`]`))

	got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", "timestamp,zzzzzz")

	found := requireFault(t, got)
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
