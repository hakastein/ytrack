package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/youtrack"
)

func TestShowIssueSendsEveryFormOfAnIssueAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "a code in upper case", id: "DEV-1"},
		{name: "a code in lower case", id: "dev-1"},
		{name: "a number with a leading zero", id: "DEV-01"},
		{name: "digits and an underscore in the code", id: "Dev_32-1"},
		{name: "letters outside ASCII", id: "ДЕВ-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Issue","id":"2-1"}`))
			call, fault := youtrack.ShowIssue(tc.id, new("id"), youtrack.Comments{})
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, "/api/issues/"+tc.id, server.Request(t, 0).URL.Path)
		})
	}
}

func TestShowIssueRefusesAnIDOfAnyOtherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an article", id: "DEV-A-1"},
		{name: "an internal id", id: "3-19"},
		{name: "two dots", id: ".."},
		{name: "a code and a dash", id: "DEV-"},
		{name: "a number without a code", id: "-1"},
		{name: "a digit first in the code", id: "1DEV-1"},
		{name: "an underscore first in the code", id: "_DEV-1"},
		{name: "a space before the code", id: " DEV-1"},
		{name: "a sign before the number", id: "DEV-+1"},
		{name: "a path after the number", id: "DEV-1/.."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.ShowIssue(tc.id, new("id"), youtrack.Comments{})

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestShowArticleSendsEveryFormOfAnArticleAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "a code in upper case", id: "DEV-A-1"},
		{name: "a code in lower case", id: "dev-A-1"},
		{name: "a number with a leading zero", id: "DEV-A-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Article","id":"3-1"}`))
			call, fault := youtrack.ShowArticle(tc.id, new("id"), youtrack.Comments{})
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, "/api/articles/"+tc.id, server.Request(t, 0).URL.Path)
		})
	}
}

func TestShowArticleRefusesAnIDOfAnyOtherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an issue", id: "DEV-1"},
		{name: "the marker in lower case", id: "DEV-a-1"},
		{name: "a dash in place of the marker", id: "DEV--1"},
		{name: "the marker twice", id: "DEV-A-A-1"},
		{name: "no number after the marker", id: "DEV-A-"},
		{name: "a sign before the number", id: "DEV-A-+1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.ShowArticle(tc.id, new("id"), youtrack.Comments{})

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestListCommentsSendsEachFormOfAnOwnerToTheAPIOfItsKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
		path string
	}{
		{name: "an issue", id: "DEV-1", path: "/api/issues/DEV-1/comments"},
		{name: "an article", id: "DEV-A-1", path: "/api/articles/DEV-A-1/comments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))
			call, fault := youtrack.ListComments(tc.id, new("id"), youtrack.Page{Limit: 1})
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, tc.path, server.Request(t, 0).URL.Path)
		})
	}
}

func TestListCommentsRefusesAnOwnerOfNeitherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an internal id", id: "3-19"},
		{name: "two dots", id: ".."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.ListComments(tc.id, new("id"), youtrack.Page{Limit: 1})

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestDeleteCommentSendsAnInternalIDAsTheSegmentUnderItsOwner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "two numbers", id: "7-1"},
		{name: "leading zeros in both numbers", id: "07-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, ``))
			call, fault := youtrack.DeleteComment("DEV-1", tc.id)
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, "/api/issues/DEV-1/comments/"+tc.id, server.Request(t, 0).URL.Path)
		})
	}
}

func TestDeleteCommentRefusesAnIDThatIsNoInternalID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "two dots", id: ".."},
		{name: "a number alone", id: "7"},
		{name: "no number before the dash", id: "-1"},
		{name: "no number after the dash", id: "7-"},
		{name: "a path after the id", id: "7-1/.."},
		{name: "the readable id of an issue", id: "DEV-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.DeleteComment("DEV-1", tc.id)

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestShowProjectSendsEveryFormOfACodeAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
	}{
		{name: "upper case", code: "DEV"},
		{name: "lower case", code: "dev"},
		{name: "digits and an underscore after the first letter", code: "Dev_32"},
		{name: "letters outside ASCII", code: "ДЕВ"},
		{name: "a number that is no decimal digit", code: "D²"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Project","shortName":"DEV"}`))
			call, fault := youtrack.ShowProject(tc.code, "shortName")
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, "/api/admin/projects/"+tc.code, server.Request(t, 0).URL.Path)
		})
	}
}

func TestShowProjectRefusesACodeOfAnotherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
	}{
		{name: "empty", code: ""},
		{name: "two dots", code: ".."},
		{name: "a digit first", code: "1DEV"},
		{name: "an underscore first", code: "_DEV"},
		{name: "a slash after the first letter", code: "a/b"},
		{name: "the readable id of an issue", code: "DEV-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.ShowProject(tc.code, "shortName")

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}

func TestShowUserSendsALoginAsOneSegment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		login   string
		escaped string
	}{
		{name: "a slash", login: "a/b", escaped: "a%2Fb"},
		{name: "me in upper case", login: "ME", escaped: "ME"},
		{name: "an internal id with a letter after it", login: "2-1x", escaped: "2-1x"},
		{name: "hex digits as many as the first group of a Hub id", login: "deadbeef", escaped: "deadbeef"},
		{name: "a Hub id without the dashes", login: "7fae4e4101f842c09cc4960c478d8a72", escaped: "7fae4e4101f842c09cc4960c478d8a72"},
		{name: "a Hub id one digit short", login: "7fae4e41-01f8-42c0-9cc4-960c478d8a7", escaped: "7fae4e41-01f8-42c0-9cc4-960c478d8a7"},
		{name: "a Hub id with a letter past f", login: "7fae4e41-01f8-42c0-9cc4-960c478d8a7g", escaped: "7fae4e41-01f8-42c0-9cc4-960c478d8a7g"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"User","login":"first"}`))
			call, fault := youtrack.ShowUser(tc.login, "login")
			require.Nil(t, fault)

			_, fault = call(t.Context(), client(t, server))

			require.Nil(t, fault)
			assert.Equal(t, "/api/users/"+tc.escaped, server.Last(t).URL.EscapedPath())
		})
	}
}

func TestShowUserRefusesEveryFormTheServerReadsAsSomethingOtherThanALogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		login string
	}{
		{name: "empty", login: ""},
		{name: "a dot", login: "."},
		{name: "two dots", login: ".."},
		{name: "a full name", login: "First Last"},
		{name: "an internal id", login: "2-1"},
		{name: "a Hub id", login: "7fae4e41-01f8-42c0-9cc4-960c478d8a72"},
		{name: "a Hub id in upper case", login: "7FAE4E41-01F8-42C0-9CC4-960C478D8A72"},
		{name: "me", login: "me"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.ShowUser(tc.login, "login")

			assert.Equal(t, diag.Fault{Code: diag.BadUsage}, refusal(t, fault))
		})
	}
}
