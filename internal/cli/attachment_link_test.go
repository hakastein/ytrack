package cli_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const signedLinkForm = `^/api/files/[0-9]+-[0-9]+\?sign=[A-Za-z0-9_-]+&updated=[0-9]+$`

const previewLinkForm = `^/api/files/[0-9]+-[0-9]+\?sign=[A-Za-z0-9_-]+$`

const issueWithAttachments = `{"$type":"Issue","attachments":[` +
	`{"$type":"IssueAttachment","id":"12-2","url":"/api/files/12-2?sign=Ab-_9&updated=1","thumbnailURL":null},` +
	`{"$type":"IssueAttachment","id":"12-3","url":"/api/files/12-3?sign=x","thumbnailURL":"/noPreview.svg"}]}`

func TestIssueShowPrintsTheLinksOfAnAttachmentWhole(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, issueWithAttachments))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0",
		"--fields", "attachments(id,url,thumbnailURL)")

	want := "attachments:\n" +
		`  - {id: "12-2", url: "` + server.url + `/api/files/12-2?sign=Ab-_9&updated=1", thumbnailURL: null}` + "\n" +
		`  - {id: "12-3", url: "` + server.url + `/api/files/12-3?sign=x", thumbnailURL: "` + server.url + `/noPreview.svg"}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestIssueShowResolvesAnAttachmentLinkAgainstTheHostAndNotThePathOfTheAddress(t *testing.T) {
	t.Parallel()
	server := serve(t, respondWith(http.StatusOK, issueWithAttachments))

	got := runWith(t, []string{"YTRACK_URL=" + server.url + "/ctx", "YTRACK_TOKEN=" + token},
		"issue", "show", "DEV-1", "--comments=0", "--fields", "attachments(url)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	want := "attachments:\n" +
		`  - {url: "` + server.url + `/api/files/12-2?sign=Ab-_9&updated=1"}` + "\n" +
		`  - {url: "` + server.url + `/api/files/12-3?sign=x"}` + "\n"
	assert.Equal(t, want, got.stdout)
	assert.Equal(t, []string{"/ctx/api/issues/DEV-1"}, server.sentPaths())
}

func TestArticleShowPrintsTheLinksOfAnAttachmentWhole(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Article","attachments":[{"$type":"ArticleAttachment",` +
		`"url":"/api/files/522-4?sign=z&updated=2","thumbnailURL":"/api/files/211-3?sign=y"}]}`
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "article", "show", "DEV-A-1", "--comments=0",
		"--fields", "attachments(url,thumbnailURL)")

	want := "attachments:\n" +
		`  - {url: "` + server.url + `/api/files/522-4?sign=z&updated=2", thumbnailURL: "` +
		server.url + `/api/files/211-3?sign=y"}` + "\n"
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
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0",
				"--fields", "attachments(url)")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", issueRequest(server.url, "DEV-1", "attachments(url)")},
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
			server := serve(t, respondWith(http.StatusOK, records))

			got := runWith(t, server.env(), "attachment", "list", tc.owner)

			want := "total: 1\nreturned: 1\ntruncated: false\nattachments:\n" +
				`  - {id: "12-2", name: "a.txt", size: 1, mimeType: "text/plain", url: "` +
				server.url + `/api/files/12-2?sign=s&updated=1"}` + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
			assert.Equal(t, []string{tc.path}, server.sentPaths())
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
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "attachment", "list", "DEV-1")

			assert.Equal(t, faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", attachmentsRequest(server.url, "issues", "DEV-1", attachmentFields, "50")},
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
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "attachments(url)")

	want := "attachments:\n" +
		`  - {url: "` + server.url + `/api/files/12-2?sign=Ab-_9&updated=1"}` + "\n"
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
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "project", "show", "DEV", "--fields", "customFields(url)")

			want := "customFields:\n  - " + strings.Replace(tc.printed, "%s", server.url, 1) + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}

func TestIssueShowPrintsTheLinkOfAnotherSchemaAsReceived(t *testing.T) {
	t.Parallel()
	const body = `{"$type":"Issue","externalIssue":{"$type":"ExternalIssue","url":"https://jira.example/X-1"}}`
	server := serve(t, respondWith(http.StatusOK, body))

	got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "externalIssue(url)")

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
			server := serve(t, respondWith(http.StatusOK, body))

			got := runWith(t, server.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "reporter(avatarUrl)")

			want := "reporter:\n  avatarUrl: \"" + server.url + "/hub/api/rest/avatar/u?s=48\"\n"
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
			server := serve(t, respondWith(http.StatusOK, `{"$type":"Project","iconUrl":`+tc.received+`}`))

			got := runWith(t, server.env(), "project", "show", "DEV", "--fields", "iconUrl")

			want := "iconUrl: " + strings.Replace(tc.printed, "%s", server.url, 1) + "\n"
			assert.Equal(t, outcome{stdout: want}, got)
		})
	}
}

func TestIssueListPrintsAnAttachmentLinkWholeInARecord(t *testing.T) {
	t.Parallel()
	record := listedRecord("DEV-1", namedFieldsOfTheDefault(),
		`"attachments":[{"$type":"IssueAttachment","url":"/api/files/12-2?sign=Ab-_9&updated=1"}]`)
	server := searching(t, respondWith(http.StatusOK, `[`+record+`]`))

	got := runWith(t, server.env(), "issue", "list", "--query", "x", "--fields", "+attachments(url)")

	lines := requireRecordsOnLines(t, got, 1)
	assert.Contains(t, lines[0], `attachments: [{url: "`+server.url+`/api/files/12-2?sign=Ab-_9&updated=1"}]`)
}

func TestIssueShowPrintsTheLinkOfTheDevInstanceAttachmentWhole(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "attachments(name,size,url)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	printed := requireAttachmentsPrinted(t, got.stdout)
	require.Len(t, printed, 1)
	assert.Equal(t, "заметка-полигона.txt", printed[0].Name)
	assert.Equal(t, 75, printed[0].Size)

	reference, found := strings.CutPrefix(printed[0].URL, dev.url)
	require.True(t, found, "%q does not begin with the address ytrack was given", printed[0].URL)
	assert.Equal(t, sentAttachmentLinks(t, dev)[0], reference)
	assert.Regexp(t, signedLinkForm, reference)
	assert.Len(t, dev.requests(), 1)
}

func TestUserShowPrintsTheAvatarOfTheDevInstanceWhole(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "user", "show", "admin", "--fields", "avatarUrl")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	var printed struct {
		AvatarURL string `yaml:"avatarUrl"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(got.stdout), &printed), "stdout: %s", got.stdout)
	assert.True(t, strings.HasPrefix(printed.AvatarURL, dev.url+"/"),
		"%q does not begin with the address ytrack was given", printed.AvatarURL)
	assert.Len(t, dev.requests(), 1)
}

func TestTheSignedLinkOfTheDevInstanceGivesTheFileWithoutAnAuthorizationHeader(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "DEV-1", "--comments=0", "--fields", "attachments(name,size,url)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	printed := requireAttachmentsPrinted(t, got.stdout)
	require.Len(t, printed, 1)
	asked := dev.requests()
	require.Len(t, asked, 1)
	assert.NotContains(t, asked[0].URL.Path, filesPath, "ytrack reached for the bytes of the file itself")
	link, err := url.Parse(printed[0].URL)
	require.NoError(t, err)

	t.Run("signed", func(t *testing.T) {
		before := len(dev.requests())

		response, body := fetched(t, link.String(), "")

		require.Equal(t, http.StatusOK, response.StatusCode, "body: %s", body)
		assert.Len(t, body, printed[0].Size, "the file is as long as the size that was printed with the link")
		encoded, found := strings.CutPrefix(response.Header.Get("Content-Disposition"), contentDisposition)
		require.True(t, found, "Content-Disposition: %q", response.Header.Get("Content-Disposition"))
		assert.NotEqual(t, printed[0].Name, encoded, "the name of the file went out unencoded")
		decoded, err := url.PathUnescape(encoded)
		require.NoError(t, err)
		assert.Equal(t, printed[0].Name, decoded)
		sent := dev.requests()[before:]
		require.Len(t, sent, 1)
		assert.Empty(t, sent[0].Header.Values("Authorization"), "the file was asked for as somebody")
		assert.Empty(t, sent[0].Header.Values("Cookie"), "the file was asked for in a session")
	})

	t.Run("unsigned", func(t *testing.T) {
		unsigned := *link
		query := unsigned.Query()
		query.Del("sign")
		unsigned.RawQuery = query.Encode()
		tests := []struct {
			name   string
			bearer string
		}{
			{name: "holding nothing"},
			{name: "holding the admins token", bearer: devTokens(t).admin},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				response, body := fetched(t, unsigned.String(), tc.bearer)

				require.Equal(t, http.StatusBadRequest, response.StatusCode, "body: %s", body)
				var refused struct {
					Description string `json:"error_description"`
				}
				require.NoError(t, json.Unmarshal(body, &refused), "body: %s", body)
				assert.Equal(t, "Sign parameter is required", refused.Description)
			})
		}
	})

	t.Run("spoiled", func(t *testing.T) {
		for _, sign := range []string{"abc", "ABC"} {
			t.Run(sign, func(t *testing.T) {
				spoiled := *link
				query := spoiled.Query()
				query.Set("sign", sign)
				spoiled.RawQuery = query.Encode()

				response, body := fetched(t, spoiled.String(), "")

				assert.Equal(t, http.StatusMovedPermanently, response.StatusCode)
				assert.Equal(t, "/issue/attachment", response.Header.Get("Location"))
				assert.Empty(t, body)
			})
		}
	})
}

const filesPath = "/api/files/"

const contentDisposition = `attachment; filename*=UTF-8''`

type printedAttachment struct {
	ID           string        `yaml:"id"`
	Name         string        `yaml:"name"`
	Size         int           `yaml:"size"`
	MimeType     string        `yaml:"mimeType"`
	URL          string        `yaml:"url"`
	ThumbnailURL string        `yaml:"thumbnailURL"`
	Comment      *printedOwner `yaml:"comment"`
}

type printedOwner struct {
	ID string `yaml:"id"`
}

func requireAttachmentsPrinted(t *testing.T, stdout string) []printedAttachment {
	t.Helper()
	var printed struct {
		Attachments []printedAttachment `yaml:"attachments"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(stdout))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&printed), "stdout: %s", stdout)
	return printed.Attachments
}

func sentAttachmentLinks(t *testing.T, u *upstream) []string {
	t.Helper()
	answers := u.answers()
	require.NotEmpty(t, answers, "the server answered nothing")
	var received struct {
		Attachments []struct {
			URL string `json:"url"`
		} `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(answers[0], &received), "the answer: %s", answers[0])
	links := make([]string, 0, len(received.Attachments))
	for _, attachment := range received.Attachments {
		links = append(links, attachment.URL)
	}
	require.NotEmpty(t, links, "the answer carries no attachment: %s", answers[0])
	return links
}
