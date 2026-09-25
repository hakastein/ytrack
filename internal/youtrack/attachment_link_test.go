package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

func attachmentLinks(key string, links ...*render.Node) *render.Node {
	items := make([]*render.Node, 0, len(links))
	for _, link := range links {
		items = append(items, render.NewMap(render.Pair{Key: key, Value: link}))
	}
	return render.NewList(items...)
}

func TestAnAddressOfTheInstanceIsPrintedAbsolute(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func() (youtrack.Call, *diag.Fault)
		body string
		want func(origin string) *render.Node
	}{
		{
			name: "the link and the preview of an attachment of an issue",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowIssue("DEV-1", new("attachments(url,thumbnailURL)"), youtrack.Comments{})
			},
			body: `{"$type":"Issue","attachments":[{"$type":"IssueAttachment",` +
				`"url":"/api/files/12-2?sign=s&updated=1","thumbnailURL":"/api/files/12-3?sign=t"}]}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "attachments", Value: render.NewList(render.NewMap(
					render.Pair{Key: "url", Value: render.NewString(origin + "/api/files/12-2?sign=s&updated=1")},
					render.Pair{Key: "thumbnailURL", Value: render.NewString(origin + "/api/files/12-3?sign=t")}))})
			},
		},
		{
			name: "no preview",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowIssue("DEV-1", new("attachments(thumbnailURL)"), youtrack.Comments{})
			},
			body: `{"$type":"Issue","attachments":[{"$type":"IssueAttachment","thumbnailURL":null}]}`,
			want: func(string) *render.Node {
				return render.NewMap(render.Pair{Key: "attachments", Value: attachmentLinks("thumbnailURL", render.NewNull())})
			},
		},
		{
			name: "an attachment of an article",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowArticle("DEV-A-1", new("attachments(thumbnailURL)"), youtrack.Comments{})
			},
			body: `{"$type":"Article","attachments":[{"$type":"ArticleAttachment","thumbnailURL":"/api/files/12-3?sign=t"}]}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "attachments",
					Value: attachmentLinks("thumbnailURL", render.NewString(origin+"/api/files/12-3?sign=t"))})
			},
		},
		{
			name: "an attachment of an issue the server named nothing",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowIssue("DEV-1", new("attachments(url)"), youtrack.Comments{})
			},
			body: `{"$type":"Issue","attachments":[{"url":"/api/files/12-2?sign=s"}]}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "attachments",
					Value: attachmentLinks("url", render.NewString(origin+"/api/files/12-2?sign=s"))})
			},
		},
		{
			name: "a listed attachment the server named nothing",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ListAttachments("DEV-1", new("url"), youtrack.Page{Limit: 50})
			},
			body: `[{"url":"/api/files/12-2?sign=s"}]`,
			want: func(origin string) *render.Node {
				return render.NewMap(
					render.Pair{Key: "total", Value: render.NewNumber("1")},
					render.Pair{Key: "returned", Value: render.NewNumber("1")},
					render.Pair{Key: "truncated", Value: render.NewBool(false)},
					render.Pair{Key: "attachments", Value: attachmentLinks("url", render.NewString(origin+"/api/files/12-2?sign=s"))})
			},
		},
		{
			name: "an object of no declared schema the server named an attachment",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "customFields(url)") },
			body: `{"$type":"Project","customFields":[{"$type":"IssueAttachment","url":"/api/files/12-2?sign=s"}]}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "customFields",
					Value: attachmentLinks("url", render.NewString(origin+"/api/files/12-2?sign=s"))})
			},
		},
		{
			name: "an object of no declared schema the server named nothing",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "customFields(url)") },
			body: `{"$type":"Project","customFields":[{"url":"/api/files/12-2?sign=s"}]}`,
			want: func(string) *render.Node {
				return render.NewMap(render.Pair{Key: "customFields",
					Value: attachmentLinks("url", render.NewString("/api/files/12-2?sign=s"))})
			},
		},
		{
			name: "a link of another schema",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowIssue("DEV-1", new("externalIssue(url)"), youtrack.Comments{})
			},
			body: `{"$type":"Issue","externalIssue":{"$type":"ExternalIssue","url":"/browse/X-1"}}`,
			want: func(string) *render.Node {
				return render.NewMap(render.Pair{Key: "externalIssue",
					Value: render.NewMap(render.Pair{Key: "url", Value: render.NewString("/browse/X-1")})})
			},
		},
		{
			name: "the avatar of a user",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowIssue("DEV-1", new("reporter(avatarUrl)"), youtrack.Comments{})
			},
			body: `{"$type":"Issue","reporter":{"$type":"User","avatarUrl":"/hub/api/rest/avatar/u?s=48"}}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "reporter", Value: render.NewMap(
					render.Pair{Key: "avatarUrl", Value: render.NewString(origin + "/hub/api/rest/avatar/u?s=48")})})
			},
		},
		{
			name: "the avatar of a subtype of a user",
			call: func() (youtrack.Call, *diag.Fault) {
				return youtrack.ShowIssue("DEV-1", new("reporter(avatarUrl)"), youtrack.Comments{})
			},
			body: `{"$type":"Issue","reporter":{"$type":"Me","avatarUrl":"/hub/api/rest/avatar/u?s=48"}}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "reporter", Value: render.NewMap(
					render.Pair{Key: "avatarUrl", Value: render.NewString(origin + "/hub/api/rest/avatar/u?s=48")})})
			},
		},
		{
			name: "the icon of a project",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "iconUrl") },
			body: `{"$type":"Project","iconUrl":"/api/entityIcons/0-3"}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "iconUrl", Value: render.NewString(origin + "/api/entityIcons/0-3")})
			},
		},
		{
			name: "no icon",
			call: func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "iconUrl") },
			body: `{"$type":"Project","iconUrl":null}`,
			want: func(string) *render.Node {
				return render.NewMap(render.Pair{Key: "iconUrl", Value: render.NewNull()})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))
			call, fault := tc.call()
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.want(server.Origin), node)
		})
	}
}

func TestAnAddressOfTheInstanceThatIsNoAbsolutePathIsRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		received string
		value    *render.Node
	}{
		{name: "a host of its own", received: `"https://elsewhere/api/files/1"`, value: render.NewString("https://elsewhere/api/files/1")},
		{name: "an authority and no scheme", received: `"//elsewhere/x"`, value: render.NewString("//elsewhere/x")},
		{name: "a user and no host", received: `"//u@/p"`, value: render.NewString("//u@/p")},
		{name: "a relative path", received: `"api/files/1"`, value: render.NewString("api/files/1")},
		{name: "an opaque reference", received: `"mailto:a@b"`, value: render.NewString("mailto:a@b")},
		{name: "nothing at all", received: `""`, value: render.NewString("")},
		{name: "a number", received: `5`, value: render.NewNumber("5")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK,
				`{"$type":"Issue","attachments":[{"$type":"IssueAttachment","url":`+tc.received+`}]}`))
			call, fault := youtrack.ShowIssue("DEV-1", new("attachments(url)"), youtrack.Comments{})
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				{Key: "request", Value: render.NewString("GET " + server.URL + "/api/issues/DEV-1?fields=attachments(url)")},
				{Key: "field", Value: render.NewString("attachments(url)")},
				{Key: "upstream_value", Value: tc.value},
			}}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}
