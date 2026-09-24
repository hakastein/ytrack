package cli_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A command that addresses one issue by a string the caller wrote, with whatever else it needs to get as far
// as the form of that string. The id stands behind the separator because pflag reads a leading dash as a flag
// wherever the word stands, so without it the id of one case would never reach the form at all.
type idCommand struct {
	name string
	argv func(id string) []string
}

func issueCommands() []idCommand {
	return []idCommand{
		{name: "show", argv: func(id string) []string { return []string{"issue", "show", "--", id} }},
		{name: "update", argv: func(id string) []string {
			return []string{"issue", "update", "--summary", "x", "--", id}
		}},
		{name: "delete", argv: func(id string) []string { return []string{"issue", "delete", "--", id} }},
		// The work items of an issue hang from it and are reached through its own API, so the string is held to
		// the form of an issue here as well, whether they are read or written.
		{name: "time list", argv: func(id string) []string { return []string{"time", "list", "--", id} }},
		{name: "time create", argv: func(id string) []string {
			return []string{"time", "create", "--", id, "PT1H"}
		}},
		{name: "time update", argv: func(id string) []string {
			return []string{"time", "update", "--text", "x", "--", id, "199-6"}
		}},
		{name: "time delete", argv: func(id string) []string {
			return []string{"time", "delete", "--", id, "199-6"}
		}},
	}
}

// One parse settles what every command of issues was given, and the refusal names the class the string
// belongs to rather than only the form it is not. Nothing of this costs a request.
func TestIssueCommandsRefuseAnIDOfAnyOtherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an article", id: "DEV-A-1"},
		{name: "an article whose code is in lower case", id: "dev-A-1"},
		{name: "an internal id", id: "3-19"},
		{name: "an internal id of an article", id: "177-1"},
		{name: "an internal id with a leading zero", id: "02-1"},
		{name: "empty", id: ""},
		{name: "a dot", id: "."},
		{name: "two dots", id: ".."},
		{name: "a code alone", id: "DEV"},
		{name: "a code and a dash", id: "DEV-"},
		{name: "a number without a code", id: "-1"},
		{name: "a dash in place of the number", id: "DEV--1"},
		{name: "an article without a number", id: "DEV-A-"},
		{name: "the marker twice", id: "DEV-A-A-1"},
		{name: "a letter after the number", id: "DEV-1x"},
		{name: "a digit first in the code", id: "1DEV-1"},
		{name: "an underscore first in the code", id: "_DEV-1"},
		{name: "a sign before the number", id: "DEV-+1"},
		{name: "a path after the id", id: "DEV-1/.."},
		{name: "an escape in place of the number", id: "DEV-%31"},
		{name: "a space before the id", id: " DEV-1"},
		{name: "a line ending after the id", id: "DEV-1\n"},
		// The marker of an article is the Latin letter and no other: the server answers 404 for the Cyrillic
		// one, which is written in bytes here because the two letters look alike.
		{name: "a Cyrillic marker", id: "ДЕВ-\xd0\x90-1"},
		{name: "bytes that are no UTF-8", id: "\xff-1"},
		{name: "the marker in lower case", id: "DEV-a-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range issueCommands() {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serveNothing(t)

					got := runWith(t, server.env(), command.argv(tc.id)...)

					assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
					assert.Empty(t, server.requests())
				})
			}
		})
	}
}

// An id of the form of an issue is sent to the issues and nowhere else, and the server settles what it
// names: a code in any letter case, holding an underscore, digits or letters outside ASCII, and a number with
// leading zeros are all the form, and a 404 is an answer rather than a reason to ask the articles instead.
func TestIssueCommandsSendEveryFormOfAnIssueToTheIssues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "a code in upper case", id: "DEV-1"},
		{name: "a code in lower case", id: "dev-1"},
		{name: "a number with a leading zero", id: "DEV-01"},
		{name: "an underscore in the code", id: "Dev_X-1"},
		{name: "digits in the code", id: "Api_32-143"},
		{name: "letters in both cases", id: "HDold-7"},
		{name: "letters outside ASCII", id: "ДЕВ-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range issueCommands() {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serve(t, answer(http.StatusNotFound, entityNotFound(tc.id)))

					got := runWith(t, server.env(), command.argv(tc.id)...)

					assert.Equal(t, "not_found", requireRefusal(t, got).code)
					paths := server.sentPaths()
					require.Len(t, paths, 1)
					assert.True(t, strings.HasPrefix(paths[0], "/api/issues/"), "the request went to %s", paths[0])
					assert.NotContains(t, strings.Join(paths, " "), "/api/articles")
				})
			}
		})
	}
}

// A code with an underscore is a code like any other, and the polygon has no project under it: the form
// sends the id to the issues and the server answers for itself.
func TestIssueShowSendsACodeWithAnUnderscoreToTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "issue", "show", "Dev_X-1")

	found := requireRefusal(t, got)
	want := noSuchIssue(dev.url, "Dev_X-1")
	assert.Equal(t, want.code, found.code)
	assert.Equal(t, want.details, found.details)
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, "/api/issues/Dev_X-1", dev.sentPaths()[0])
}

func articleCommands() []idCommand {
	return []idCommand{
		{name: "show", argv: func(id string) []string { return []string{"article", "show", "--", id} }},
		{name: "update", argv: func(id string) []string {
			return []string{"article", "update", "--summary", "x", "--", id}
		}},
		{name: "delete", argv: func(id string) []string { return []string{"article", "delete", "--", id} }},
		// The one article a creation names is the parent, and it is named by a flag, so the separator has
		// nothing to part here.
		{name: "create --parent", argv: func(id string) []string {
			return []string{"article", "create", "DEV", "--summary", "x", "--parent", id}
		}},
	}
}

// The same parse settles what every command of articles was given, and a string of the form of an issue is
// refused here rather than sent: the API of articles answers 404 for it, which would name the wrong reason.
func TestArticleCommandsRefuseAnIDOfAnyOtherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an issue", id: "DEV-1"},
		{name: "an issue whose code is in lower case", id: "dev-1"},
		{name: "an internal id of an article", id: "177-1"},
		{name: "an internal id of an issue", id: "3-19"},
		{name: "a sign before the number", id: "DEV-A-+1"},
		{name: "a fraction for a number", id: "DEV-A-1.0"},
		{name: "two dots", id: ".."},
		{name: "the marker in lower case", id: "DEV-a-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range articleCommands() {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serveNothing(t)

					got := runWith(t, server.env(), command.argv(tc.id)...)

					assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
					assert.Empty(t, server.requests())
				})
			}
		})
	}
}

// An id of the form of an article is sent to the articles and nowhere else, and the server settles what it
// names: a code in any letter case or holding digits and an underscore, and a number with leading zeros, are
// all the form, and a 404 is an answer rather than a reason to ask the issues instead.
func TestArticleCommandsSendEveryFormOfAnArticleToTheArticles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "a code in upper case", id: "DEV-A-1"},
		{name: "a code in lower case", id: "dev-A-1"},
		{name: "a number with a leading zero", id: "DEV-A-01"},
		{name: "digits and an underscore in the code", id: "Api_32-A-3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range articleCommands() {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serve(t, answer(http.StatusNotFound, entityNotFound(tc.id)))

					got := runWith(t, server.env(), command.argv(tc.id)...)

					assert.Equal(t, "not_found", requireRefusal(t, got).code)
					paths := server.sentPaths()
					require.Len(t, paths, 1)
					assert.True(t, strings.HasPrefix(paths[0], "/api/articles/"), "the request went to %s", paths[0])
					assert.NotContains(t, strings.Join(paths, " "), "/api/issues")
				})
			}
		})
	}
}

// The commands that name an entity of either kind, since what they write hangs from an issue and from an
// article alike. One parse settles which of the two the string is, and no command of this kind ever asks the
// other API about it.
func ownerCommands(t *testing.T) []idCommand {
	t.Helper()
	attached := aFileToAttach(t)
	return []idCommand{
		{name: "comment create", argv: func(id string) []string {
			return []string{"comment", "create", "--text", "x", "--", id}
		}},
		{name: "comment update", argv: func(id string) []string {
			return []string{"comment", "update", "--text", "x", "--", id, "7-1"}
		}},
		{name: "comment delete", argv: func(id string) []string {
			return []string{"comment", "delete", "--", id, "7-1"}
		}},
		{name: "attachment list", argv: func(id string) []string {
			return []string{"attachment", "list", "--", id}
		}},
		{name: "attachment create", argv: func(id string) []string {
			return []string{"attachment", "create", "--", id, attached}
		}},
		{name: "attachment delete", argv: func(id string) []string {
			return []string{"attachment", "delete", "--", id, "12-1"}
		}},
		// A tagging reads the owner before it resolves the name, so the one request of the form is the owner's
		// and the catalogue of tags is never asked for here.
		{name: "tag add", argv: func(id string) []string {
			return []string{"tag", "add", "--name", "x", "--", id}
		}},
		{name: "tag remove", argv: func(id string) []string {
			return []string{"tag", "remove", "--name", "x", "--", id}
		}},
	}
}

// A string of neither form is refused before any request, and the refusal names the class it belongs to:
// here an internal id is no address at all, although it is the one thing the comment itself is addressed by.
func TestOwnerCommandsRefuseAStringOfNeitherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "an internal id of an issue", id: "3-19"},
		{name: "an internal id of an article", id: "177-1"},
		{name: "empty", id: ""},
		{name: "a dot", id: "."},
		{name: "two dots", id: ".."},
		{name: "a code alone", id: "DEV"},
		{name: "the marker in lower case", id: "DEV-a-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range ownerCommands(t) {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serveNothing(t)

					got := runWith(t, server.env(), command.argv(tc.id)...)

					assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
					assert.Empty(t, server.requests())
				})
			}
		})
	}
}

// Every form of an issue goes to the issues and every form of an article to the articles, each in one
// request: the form settles the API, and a 404 from it is an answer rather than a reason to ask the other.
func TestOwnerCommandsSendEachFormToTheAPIOfItsKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		id    string
		under string
		apart string
	}{
		{name: "an issue in upper case", id: "DEV-1", under: "/api/issues/", apart: "/api/articles"},
		{name: "an issue in lower case", id: "dev-1", under: "/api/issues/", apart: "/api/articles"},
		{name: "an issue numbered with a leading zero", id: "DEV-01", under: "/api/issues/", apart: "/api/articles"},
		{name: "an issue of a code with an underscore", id: "Dev_X-1", under: "/api/issues/", apart: "/api/articles"},
		{name: "an issue of a code outside ASCII", id: "ДЕВ-1", under: "/api/issues/", apart: "/api/articles"},
		{name: "an article in upper case", id: "DEV-A-1", under: "/api/articles/", apart: "/api/issues"},
		{name: "an article in lower case", id: "dev-A-1", under: "/api/articles/", apart: "/api/issues"},
		{name: "an article numbered with a leading zero", id: "dev-A-01", under: "/api/articles/", apart: "/api/issues"},
		{name: "an article of a code with digits", id: "Api_32-A-3", under: "/api/articles/", apart: "/api/issues"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range ownerCommands(t) {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serve(t, answer(http.StatusNotFound, entityNotFound(tc.id)))

					got := runWith(t, server.env(), command.argv(tc.id)...)

					assert.Equal(t, "not_found", requireRefusal(t, got).code)
					paths := server.sentPaths()
					require.Len(t, paths, 1)
					assert.True(t, strings.HasPrefix(paths[0], tc.under), "the request went to %s", paths[0])
					assert.NotContains(t, strings.Join(paths, " "), tc.apart)
				})
			}
		})
	}
}

// A command that addresses one child of an issue or of an article, beside the collection that child stands
// under: the form of the id is one rule for every kind of child, and where the request lands is what says the
// id reached the entity it addresses rather than another endpoint.
type childCommand struct {
	idCommand
	under string
}

// The commands that address one child, which carries no readable id of its own and is named by the internal id
// instead. The owner stands in the argument before it, so a command that works on either kind is written once
// per kind.
func childCommands() []childCommand {
	return []childCommand{
		{under: "/comments/", idCommand: idCommand{name: "comment update on an issue", argv: func(id string) []string {
			return []string{"comment", "update", "--text", "x", "--", "DEV-1", id}
		}}},
		{under: "/comments/", idCommand: idCommand{name: "comment update on an article", argv: func(id string) []string {
			return []string{"comment", "update", "--text", "x", "--", "DEV-A-1", id}
		}}},
		{under: "/comments/", idCommand: idCommand{name: "comment delete on an issue", argv: func(id string) []string {
			return []string{"comment", "delete", "--", "DEV-1", id}
		}}},
		{under: "/comments/", idCommand: idCommand{name: "comment delete on an article", argv: func(id string) []string {
			return []string{"comment", "delete", "--", "DEV-A-1", id}
		}}},
		{under: "/timeTracking/workItems/", idCommand: idCommand{name: "time update", argv: func(id string) []string {
			return []string{"time", "update", "--text", "x", "--", "DEV-1", id}
		}}},
		{under: "/timeTracking/workItems/", idCommand: idCommand{name: "time delete", argv: func(id string) []string {
			return []string{"time", "delete", "--", "DEV-1", id}
		}}},
		{under: "/" + attachmentsCollection + "/", idCommand: idCommand{name: "attachment delete on an issue", argv: func(id string) []string {
			return []string{"attachment", "delete", "--", "DEV-1", id}
		}}},
		{under: "/" + attachmentsCollection + "/", idCommand: idCommand{name: "attachment delete on an article", argv: func(id string) []string {
			return []string{"attachment", "delete", "--", "DEV-A-1", id}
		}}},
	}
}

const attachmentsCollection = "attachments"

// A string that is no internal id is refused before any request, whichever kind of owner it was written
// under: the generated client would resolve it against the server, and an empty id or a traversal reaches an
// endpoint other than the one child that was named.
func TestChildCommandsRefuseAStringThatIsNoInternalID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "a dot", id: "."},
		{name: "two dots", id: ".."},
		{name: "a number alone", id: "7"},
		{name: "a path after the id", id: "7-1/.."},
		{name: "the readable id of an issue", id: "DEV-1"},
		{name: "letters", id: "abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range childCommands() {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serveNothing(t)

					got := runWith(t, server.env(), command.argv(tc.id)...)

					assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
					assert.Empty(t, server.requests())
				})
			}
		})
	}
}

// Every internal id goes to the server as it was written, in one request: the class is not read — the
// instance numbers its own entities and the classes of two instances already differ — and a leading zero is
// the server's to refuse, which it does by matching the id exactly.
func TestChildCommandsSendEveryInternalIDToTheServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "the class of a comment of an issue", id: "7-1"},
		{name: "the class of a comment of an article", id: "8-1"},
		{name: "a class the polygon has none of", id: "42-1"},
		{name: "leading zeros in both numbers", id: "07-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range childCommands() {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serve(t, answer(http.StatusNotFound, entityNotFound(tc.id)))

					got := runWith(t, server.env(), command.argv(tc.id)...)

					assert.Equal(t, "not_found", requireRefusal(t, got).code)
					paths := server.sentPaths()
					require.Len(t, paths, 1)
					assert.True(t, strings.HasSuffix(paths[0], command.under+tc.id), "the request went to %s", paths[0])
				})
			}
		})
	}
}

// The commands that name a project by its code. Each is given whatever else it needs to get as far as the form
// of that code: a creation with no title is refused for the missing title before the code is ever looked at.
func codeCommands() []idCommand {
	return []idCommand{
		{name: "project show", argv: func(code string) []string { return []string{"project", "show", "--", code} }},
		{name: "field list", argv: func(code string) []string { return []string{"field", "list", "--", code} }},
		{name: "field show", argv: func(code string) []string { return []string{"field", "show", "--", code, "Type"} }},
		{name: "issue create", argv: func(code string) []string {
			return []string{"issue", "create", "--summary", "x", "--", code}
		}},
	}
}

// The code of a project is held to the grammar that parts every readable id from an internal one, and to
// the same one in each command that takes a code. Nothing of this costs a request.
func TestProjectCodeCommandsRefuseACodeOfAnotherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
	}{
		{name: "a digit first", code: "1DEV"},
		{name: "an underscore first", code: "_DEV"},
		{name: "digits alone", code: "123"},
		{name: "an internal id", code: "0-1"},
		{name: "the readable id of an issue", code: "DEV-1"},
		{name: "empty", code: ""},
		{name: "a dot", code: "."},
		{name: "two dots", code: ".."},
		{name: "a slash", code: "a/b"},
		{name: "a query", code: "DEV?x=1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range codeCommands() {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serveNothing(t)

					got := runWith(t, server.env(), command.argv(tc.code)...)

					assert.Equal(t, "bad_usage", requireRefusal(t, got).code)
					assert.Empty(t, server.requests())
				})
			}
		})
	}
}

// A string of the form of a code goes to the projects and the server settles what it names: any letter
// case, an underscore, digits after the first letter and letters outside ASCII are all the form, and a 404 is
// an answer rather than a reason to ask anywhere else. A creation refused here has written nothing.
func TestProjectCodeCommandsSendEveryFormOfACodeToTheProjects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
	}{
		{name: "upper case", code: "DEV"},
		{name: "lower case", code: "dev"},
		{name: "digits and an underscore", code: "Api_32"},
		{name: "letters in both cases", code: "HDold"},
		{name: "letters outside ASCII", code: "\xd0\x94\xd0\x95\xd0\x92"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range codeCommands() {
				t.Run(command.name, func(t *testing.T) {
					t.Parallel()
					server := serve(t, answer(http.StatusNotFound, entityNotFound(tc.code)))

					got := runWith(t, server.env(), command.argv(tc.code)...)

					assert.Equal(t, "not_found", requireRefusal(t, got).code)
					paths := server.sentPaths()
					require.Len(t, paths, 1)
					assert.True(t, strings.HasPrefix(paths[0], "/api/admin/projects/"), "the request went to %s", paths[0])
					assert.NotContains(t, strings.Join(paths, " "), "/api/issues")
				})
			}
		})
	}
}

// A code holding digits and an underscore is a code like any other, and the polygon has no project under
// it: the form sends it to the projects and the server answers for itself.
func TestProjectShowSendsACodeWithDigitsToTheDevInstance(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)

	got := runWith(t, dev.env(), "project", "show", "Api_32")

	assert.Equal(t, refusal{
		code: "not_found",
		details: []detail{
			{"request", showRequest(dev.url, "Api_32")},
			{"upstream_status", 404},
			{"upstream_error", "Not Found"},
			{"upstream_message", "Entity with id Api_32 not found"},
		},
	}, requireRefusal(t, got))
	require.Len(t, dev.requests(), 1)
	assert.Equal(t, "/api/admin/projects/Api_32", dev.sentPaths()[0])
}
