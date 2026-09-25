package cli_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const issueWithAttachments = `{"$type":"Issue","attachments":[` +
	`{"$type":"IssueAttachment","id":"12-2","url":"/api/files/12-2?sign=Ab-_9&updated=1","thumbnailURL":null},` +
	`{"$type":"IssueAttachment","id":"12-3","url":"/api/files/12-3?sign=x","thumbnailURL":"/noPreview.svg"}]}`

func TestIssueShowPrintsTheLinksOfAnAttachmentWhole(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueWithAttachments))

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0",
		"--fields", "attachments(id,url,thumbnailURL)")

	want := "attachments:\n" +
		`  - {id: "12-2", url: "` + server.Origin + `/api/files/12-2?sign=Ab-_9&updated=1", thumbnailURL: null}` + "\n" +
		`  - {id: "12-3", url: "` + server.Origin + `/api/files/12-3?sign=x", thumbnailURL: "` + server.Origin + `/noPreview.svg"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestIssueShowResolvesAnAttachmentLinkAgainstTheHostAndNotThePathOfTheAddress(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, issueWithAttachments))

	got := runWith(t, []string{"YTRACK_URL=" + server.URL + "/ctx", "YTRACK_TOKEN=" + fake.Token},
		"issue", "show", "DEV-1", "--comments=0", "--fields", "attachments(url)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	want := "attachments:\n" +
		`  - {url: "` + server.Origin + `/api/files/12-2?sign=Ab-_9&updated=1"}` + "\n" +
		`  - {url: "` + server.Origin + `/api/files/12-3?sign=x"}` + "\n"
	assert.Equal(t, want, got.stdout)
	assert.Equal(t, []string{"/ctx/api/issues/DEV-1"}, server.Paths())
}

func TestArticleShowPrintsTheLinksOfAnAttachmentWhole(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Article","attachments":[{"$type":"ArticleAttachment",` +
		`"url":"/api/files/522-4?sign=z&updated=2","thumbnailURL":"/api/files/211-3?sign=y"}]}`
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "article", "show", "DEV-A-1", "--comments=0",
		"--fields", "attachments(url,thumbnailURL)")

	want := "attachments:\n" +
		`  - {url: "` + server.Origin + `/api/files/522-4?sign=z&updated=2", thumbnailURL: "` +
		server.Origin + `/api/files/211-3?sign=y"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestIssueShowRefusesAnAttachmentLinkThatIsNoAbsolutePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
	}{
		{name: "a host of its own", received: "https://evil/api/files/1"},
		{name: "an authority and no scheme", received: "//evil/x"},
		{name: "a user and no host", received: "//u@/p"},
		{name: "a relative path", received: "api/files/1"},
		{name: "an opaque reference", received: "mailto:a@b"},
		{name: "nothing at all", received: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sent, err := json.Marshal(tc.received)
			require.NoError(t, err)
			body := `{"$type":"Issue","attachments":[{"$type":"IssueAttachment","url":` + string(sent) + `}]}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0",
				"--fields", "attachments(url)")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.URL, "DEV-1", "attachments(url)")},
					{"field", "attachments(url)"},
					{"upstream_value", tc.received},
				},
			}, requireFault(t, got))
		})
	}
}

func TestAttachmentListPrintsTheLinkOfAnAttachmentTheServerNamedNothing(t *testing.T) {
	t.Parallel()
	const records = `[{"id":"12-2","name":"a.txt","size":1,"mimeType":"text/plain",` +
		`"url":"/api/files/12-2?sign=s&updated=1"}]`
	tests := []struct {
		name  string
		owner string
		path  string
	}{
		{name: "an issue", owner: "DEV-1", path: "/api/issues/DEV-1/attachments"},
		{name: "an article", owner: "DEV-A-7", path: "/api/articles/DEV-A-7/attachments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, records))

			got := runWith(t, server.Env(), "attachment", "list", tc.owner)

			want := "total: 1\nreturned: 1\ntruncated: false\nattachments:\n" +
				`  - {id: "12-2", name: "a.txt", size: 1, mimeType: "text/plain", url: "` +
				server.Origin + `/api/files/12-2?sign=s&updated=1"}` + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
			assert.Equal(t, []string{tc.path}, server.Paths())
		})
	}
}

func TestAttachmentListRefusesALinkOfAnAttachmentTheServerNamedNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
	}{
		{name: "a relative path", received: "api/files/12-2"},
		{name: "a host of its own", received: "https://evil/api/files/12-2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sent, err := json.Marshal(tc.received)
			require.NoError(t, err)
			body := `[{"id":"12-2","name":"a.txt","size":1,"mimeType":"text/plain","url":` + string(sent) + `}]`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			got := runWith(t, server.Env(), "attachment", "list", "DEV-1")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", attachmentsRequest(server.URL, "issues", "DEV-1", attachmentFields, "50")},
					{"field", "url"},
					{"upstream_value", tc.received},
				},
			}, requireFault(t, got))
		})
	}
}

func TestIssueShowPrintsTheLinkOfAnAttachmentTheServerNamedNothing(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Issue","attachments":[{"url":"/api/files/12-2?sign=Ab-_9&updated=1"}]}`
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "attachments(url)")

	want := "attachments:\n" +
		`  - {url: "` + server.Origin + `/api/files/12-2?sign=Ab-_9&updated=1"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestProjectShowReadsALinkOfANodeOfNoSchemaByTheNameTheServerGaveIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		object  string
		printed string
	}{
		{
			name:    "an object the server named",
			object:  `{"$type":"IssueAttachment","url":"/api/files/12-2?sign=s"}`,
			printed: `{url: "%s/api/files/12-2?sign=s"}`,
		},
		{
			name:    "an object the server named nothing",
			object:  `{"url":"/api/files/12-2?sign=s"}`,
			printed: `{url: "/api/files/12-2?sign=s"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Project","customFields":[` + tc.object + `]}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			got := runWith(t, server.Env(), "project", "show", "DEV", "--fields", "customFields(url)")

			want := "customFields:\n  - " + strings.Replace(tc.printed, "%s", server.Origin, 1) + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}

func TestIssueShowPrintsTheLinkOfAnotherSchemaAsReceived(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Issue","externalIssue":{"$type":"ExternalIssue","url":"https://jira.example/X-1"}}`
	server := fake.Serve(t, fake.JSON(http.StatusOK, body))

	got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "externalIssue(url)")

	assert.Equal(t, outcome{stdout: "externalIssue:\n  url: \"https://jira.example/X-1\"\n"}, got)
}

func TestIssueShowPrintsTheAvatarOfAUserWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		named string
	}{
		{name: "the schema itself", named: "User"},
		{name: "a subtype of it", named: "Me"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"$type":"Issue","reporter":{"$type":"` + tc.named + `","avatarUrl":"/hub/api/rest/avatar/u?s=48"}}`
			server := fake.Serve(t, fake.JSON(http.StatusOK, body))

			got := runWith(t, server.Env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "reporter(avatarUrl)")

			want := "reporter:\n  avatarUrl: \"" + server.Origin + "/hub/api/rest/avatar/u?s=48\"\n"
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}

func TestProjectShowPrintsTheIconOfTheProjectWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
		printed  string
	}{
		{name: "an icon", received: `"/api/entityIcons/0-3"`, printed: `"%s/api/entityIcons/0-3"`},
		{name: "no icon", received: "null", printed: "null"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Project","iconUrl":`+tc.received+`}`))

			got := runWith(t, server.Env(), "project", "show", "DEV", "--fields", "iconUrl")

			want := "iconUrl: " + strings.Replace(tc.printed, "%s", server.Origin, 1) + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}

func TestIssueListPrintsAnAttachmentLinkWholeInARecord(t *testing.T) {
	t.Parallel()
	record := listedRecord("DEV-1", namedFieldsOfTheDefault(),
		`"attachments":[{"$type":"IssueAttachment","url":"/api/files/12-2?sign=Ab-_9&updated=1"}]`)
	server := fake.Serve(t, fake.Searching(t, fake.JSON(http.StatusOK, `[`+record+`]`)))

	got := runWith(t, server.Env(), "issue", "list", "--query", "x", "--fields", "+attachments(url)")

	lines := requireRecordsOnLines(t, got, 1)
	assert.Contains(t, lines[0], `attachments: [{url: "`+server.Origin+`/api/files/12-2?sign=Ab-_9&updated=1"}]`)
}

type printedAttachment struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Size     int    `yaml:"size"`
	MimeType string `yaml:"mimeType"`
	URL      string `yaml:"url"`
}
