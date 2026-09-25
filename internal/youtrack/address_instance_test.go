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

func TestAnAddressOfTheInstanceIsResolvedFromTheAddressOfTheLogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		call   func() (youtrack.Call, *diag.Fault)
		answer string
		want   func(origin string) *render.Node
	}{
		{
			name:   "the icon of a project",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "iconUrl") },
			answer: `{"$type":"Project","iconUrl":"/icon/1?sign=a"}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "iconUrl", Value: render.NewString(origin + "/icon/1?sign=a")})
			},
		},
		{
			name:   "the avatar of a user",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.ShowUser("first", "avatarUrl") },
			answer: `{"$type":"User","avatarUrl":"/avatar/1"}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "avatarUrl", Value: render.NewString(origin + "/avatar/1")})
			},
		},
		{
			name:   "the avatar of a user under a project",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "leader(avatarUrl)") },
			answer: `{"$type":"Project","leader":{"$type":"User","avatarUrl":"/avatar/1"}}`,
			want: func(origin string) *render.Node {
				leader := render.NewMap(render.Pair{Key: "avatarUrl", Value: render.NewString(origin + "/avatar/1")})
				return render.NewMap(render.Pair{Key: "leader", Value: leader})
			},
		},
		{
			name:   "the avatar of a subtype of a user the server named",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.ShowUser("first", "avatarUrl") },
			answer: `{"$type":"Me","avatarUrl":"/avatar/1"}`,
			want: func(origin string) *render.Node {
				return render.NewMap(render.Pair{Key: "avatarUrl", Value: render.NewString(origin + "/avatar/1")})
			},
		},
		{
			name:   "an attachment the server named where no attachment is declared",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "customFields(url)") },
			answer: `{"$type":"Project","customFields":[{"$type":"IssueAttachment","url":"/file/1"}]}`,
			want: func(origin string) *render.Node {
				attachment := render.NewMap(render.Pair{Key: "url", Value: render.NewString(origin + "/file/1")})
				return render.NewMap(render.Pair{Key: "customFields", Value: render.NewList(attachment)})
			},
		},
		{
			name:   "a url of a node the server named nothing where no attachment is declared",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "customFields(url)") },
			answer: `{"$type":"Project","customFields":[{"url":"/file/1"}]}`,
			want: func(string) *render.Node {
				field := render.NewMap(render.Pair{Key: "url", Value: render.NewString("/file/1")})
				return render.NewMap(render.Pair{Key: "customFields", Value: render.NewList(field)})
			},
		},
		{
			name:   "an icon that is null",
			call:   func() (youtrack.Call, *diag.Fault) { return youtrack.ShowProject("DEV", "iconUrl") },
			answer: `{"$type":"Project","iconUrl":null}`,
			want: func(string) *render.Node {
				return render.NewMap(render.Pair{Key: "iconUrl", Value: render.NewNull()})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.answer))
			call, fault := tc.call()
			require.Nil(t, fault)

			node, fault := call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.want(server.Origin), node)
		})
	}
}

func TestShowProjectRefusesAnAddressOfTheInstanceThatIsNoAbsolutePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fields   string
		answer   string
		field    string
		received *render.Node
	}{
		{
			name:     "a relative path",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"icon/1"}`,
			field:    "iconUrl",
			received: render.NewString("icon/1"),
		},
		{
			name:     "an empty string",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":""}`,
			field:    "iconUrl",
			received: render.NewString(""),
		},
		{
			name:     "another authority",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"//elsewhere/icon/1"}`,
			field:    "iconUrl",
			received: render.NewString("//elsewhere/icon/1"),
		},
		{
			name:     "another scheme",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"https://elsewhere/icon/1"}`,
			field:    "iconUrl",
			received: render.NewString("https://elsewhere/icon/1"),
		},
		{
			name:     "a scheme without a host",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"https:/icon/1"}`,
			field:    "iconUrl",
			received: render.NewString("https:/icon/1"),
		},
		{
			name:     "a user without a host",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"//first@/icon/1"}`,
			field:    "iconUrl",
			received: render.NewString("//first@/icon/1"),
		},
		{
			name:     "a broken escape",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":"/icon/%zz"}`,
			field:    "iconUrl",
			received: render.NewString("/icon/%zz"),
		},
		{
			name:     "a number",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":7}`,
			field:    "iconUrl",
			received: render.NewNumber("7"),
		},
		{
			name:     "a boolean",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":true}`,
			field:    "iconUrl",
			received: render.NewBool(true),
		},
		{
			name:     "an object",
			fields:   "iconUrl",
			answer:   `{"$type":"Project","iconUrl":{"path":"/icon/1"}}`,
			field:    "iconUrl",
			received: render.NewString(`{"path":"/icon/1"}`),
		},
		{
			name:     "the avatar of a user under the project",
			fields:   "leader(avatarUrl)",
			answer:   `{"$type":"Project","leader":{"$type":"User","avatarUrl":"avatar/1"}}`,
			field:    "leader(avatarUrl)",
			received: render.NewString("avatar/1"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.answer))
			call, fault := youtrack.ShowProject("DEV", tc.fields)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			want := diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
				{Key: "request", Value: render.NewString("GET " + server.URL + "/api/admin/projects/DEV?fields=" + tc.fields)},
				{Key: "field", Value: render.NewString(tc.field)},
				{Key: "upstream_value", Value: tc.received},
			}}
			assert.Equal(t, want, refusal(t, fault))
		})
	}
}
