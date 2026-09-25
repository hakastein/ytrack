package cli_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/cli"
	"github.com/hakastein/ytrack/internal/fake"
)

const gatewayPage = "<html><head><title>Bad Gateway</title></head><body>The server is not answering</body></html>"

const partialAnswer = "abc"

func gateway(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, gatewayPage)
	}
}

func breakOff(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func brokenOffUnder(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(partialAnswer)+7))
		w.WriteHeader(status)
		_, _ = io.WriteString(w, partialAnswer)
	}
}

func cancellingOnArrival(cancel context.CancelFunc) http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}
}

func runInContext(t *testing.T, ctx context.Context, env []string, argv ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(ctx, argv, env, nil, nil, &stdout, &stderr)
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func deletionRequest(address string) string {
	return "DELETE " + address + "/api/issues/DEV-7"
}

func readDEV7() http.HandlerFunc {
	return fake.JSON(http.StatusOK, issueNamed("DEV-7"))
}

func answeringTheDeletionWith(deletion http.HandlerFunc) func(*testing.T, context.CancelFunc) *fake.Server {
	return func(t *testing.T, _ context.CancelFunc) *fake.Server {
		return deleting(t, readDEV7(), deletion)
	}
}

func TestIssueDeleteExitsByWhetherTheDeletionMayHaveHappened(t *testing.T) {
	t.Parallel()
	const failed = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	const refused = `{"error":"bad_request","error_description":"Bad Request"}`
	tests := []struct {
		name    string
		serve   func(t *testing.T, cancel context.CancelFunc) *fake.Server
		want    func(address string) faultDocument
		exit    int
		methods []string
	}{
		{
			name:  "the answer never came",
			serve: answeringTheDeletionWith(breakOff),
			want: func(address string) faultDocument {
				return faultDocument{code: "write_uncertain", details: []detail{{"request", deletionRequest(address)}}}
			},
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "the deletion never left",
			serve: func(t *testing.T, _ context.CancelFunc) *fake.Server {
				var server *fake.Server
				server = fake.ServeAlone(t, readThenDeletion(func(w http.ResponseWriter, r *http.Request) {
					server.StopListening(t)
					readDEV7()(w, r)
				}, fake.Unexpected(t)))
				return server
			},
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_failed", details: []detail{{"request", deletionRequest(address)}}}
			},
			exit:    1,
			methods: []string{http.MethodGet},
		},
		{
			name: "the call was cancelled before the deletion was sent",
			serve: func(t *testing.T, cancel context.CancelFunc) *fake.Server {
				return deleting(t, cancellingOnArrival(cancel), fake.Unexpected(t))
			},
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_failed", details: []detail{
					{"request", issueRequest(address, "DEV-7", deletedFields)},
				}}
			},
			exit:    1,
			methods: []string{http.MethodGet},
		},
		{
			name: "the call was cancelled after the deletion was sent",
			serve: func(t *testing.T, cancel context.CancelFunc) *fake.Server {
				return deleting(t, readDEV7(), cancellingOnArrival(cancel))
			},
			want: func(address string) faultDocument {
				return faultDocument{code: "write_uncertain", details: []detail{{"request", deletionRequest(address)}}}
			},
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:  "YouTrack failed the deletion",
			serve: answeringTheDeletionWith(fake.JSON(http.StatusInternalServerError, failed)),
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_failed", details: []detail{
					{"request", deletionRequest(address)},
					{"upstream_status", 500},
					{"upstream_error", "server_error"},
					{"upstream_message", "java.lang.NullPointerException"},
				}}
			},
			exit:    1,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:  "a gateway answered a page under a 502",
			serve: answeringTheDeletionWith(gateway(http.StatusBadGateway)),
			want: func(address string) faultDocument {
				return faultDocument{code: "write_uncertain", details: []detail{
					{"request", deletionRequest(address)},
					{"upstream_status", 502},
					{"upstream_body", gatewayPage},
				}}
			},
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:  "the answer broke off under a 200",
			serve: answeringTheDeletionWith(brokenOffUnder(http.StatusOK)),
			want: func(address string) faultDocument {
				return faultDocument{code: "write_uncertain", details: []detail{
					{"request", deletionRequest(address)},
					{"upstream_status", 200},
					{"upstream_body", partialAnswer},
				}}
			},
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:  "the answer broke off under a 500",
			serve: answeringTheDeletionWith(brokenOffUnder(http.StatusInternalServerError)),
			want: func(address string) faultDocument {
				return faultDocument{code: "write_uncertain", details: []detail{
					{"request", deletionRequest(address)},
					{"upstream_status", 500},
					{"upstream_body", partialAnswer},
				}}
			},
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:  "the answer broke off under a 400",
			serve: answeringTheDeletionWith(brokenOffUnder(http.StatusBadRequest)),
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_failed", details: []detail{
					{"request", deletionRequest(address)},
					{"upstream_status", 400},
				}}
			},
			exit:    1,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:  "the server refused the deletion",
			serve: answeringTheDeletionWith(fake.JSON(http.StatusBadRequest, refused)),
			want: func(address string) faultDocument {
				return faultDocument{code: "rejected", details: []detail{
					{"request", deletionRequest(address)},
					{"upstream_status", 400},
					{"upstream_error", "bad_request"},
					{"upstream_message", "Bad Request"},
				}}
			},
			exit:    1,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:  "the deletion was answered with a body",
			serve: answeringTheDeletionWith(fake.JSON(http.StatusOK, `{"x":1}`)),
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_invalid", details: []detail{
					{"request", deletionRequest(address)},
					{"upstream_status", 200},
					{"upstream_body", `{"x":1}`},
				}}
			},
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := tc.serve(t, cancel)

			got := runInContext(t, ctx, server.Env(), "issue", "delete", "DEV-7")

			assert.Equal(t, tc.want(server.URL), requireFaultDocument(t, got))
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tc.methods, server.Methods())
		})
	}
}

func TestEveryWriteIsUncertainWhereItsAnswerNeverCame(t *testing.T) {
	t.Parallel()
	attached := aFileToAttach(t)
	readIssue := fake.JSON(http.StatusOK, issueToUpdate("DEV-7", projectRequiringNothing()))
	owner := fake.JSON(http.StatusOK, issueNamed("DEV-7"))
	tests := []struct {
		name  string
		argv  []string
		serve func(t *testing.T) *fake.Server
	}{
		{
			name: "issue create",
			argv: []string{"issue", "create", "DEV", "--summary", "x"},
			serve: func(t *testing.T) *fake.Server {
				return creating(t, fake.JSON(http.StatusOK, projectRequiringNothing()), breakOff)
			},
		},
		{
			name:  "issue update",
			argv:  []string{"issue", "update", "DEV-7", "--summary", "x"},
			serve: func(t *testing.T) *fake.Server { return updating(t, readIssue, breakOff) },
		},
		{
			name:  "issue delete",
			argv:  []string{"issue", "delete", "DEV-7"},
			serve: func(t *testing.T) *fake.Server { return deleting(t, owner, breakOff) },
		},
		{
			name:  "article create",
			argv:  []string{"article", "create", "DEV", "--summary", "x"},
			serve: func(t *testing.T) *fake.Server { return creatingAnArticle(t, breakOff) },
		},
		{
			name: "article update",
			argv: []string{"article", "update", "DEV-A-7", "--summary", "x"},
			serve: func(t *testing.T) *fake.Server {
				return updatingAnArticle(t, fake.JSON(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")), breakOff)
			},
		},
		{
			name: "article delete",
			argv: []string{"article", "delete", "DEV-A-7"},
			serve: func(t *testing.T) *fake.Server {
				return deleting(t, fake.JSON(http.StatusOK, articleNamed("DEV-A-7")), breakOff)
			},
		},
		{
			name:  "comment create",
			argv:  []string{"comment", "create", "DEV-7", "--text", "x"},
			serve: func(t *testing.T) *fake.Server { return commenting(t, breakOff) },
		},
		{
			name: "comment update",
			argv: []string{"comment", "update", "DEV-7", "7-12", "--text", "x"},
			serve: func(t *testing.T) *fake.Server {
				return updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)), breakOff)
			},
		},
		{
			name:  "comment delete",
			argv:  []string{"comment", "delete", "DEV-7", "7-12"},
			serve: func(t *testing.T) *fake.Server { return removingAComment(t, breakOff) },
		},
		{
			name:  "attachment create",
			argv:  []string{"attachment", "create", "DEV-7", attached},
			serve: func(t *testing.T) *fake.Server { return fake.Serve(t, breakOff) },
		},
		{
			name: "attachment delete",
			argv: []string{"attachment", "delete", "DEV-7", "12-5"},
			serve: func(t *testing.T) *fake.Server {
				return deleting(t, fake.JSON(http.StatusOK, attachmentOfDEV7()), breakOff)
			},
		},
		{
			name:  "tag create",
			argv:  []string{"tag", "create", "--name", "x"},
			serve: func(t *testing.T) *fake.Server { return creatingATag(t, breakOff) },
		},
		{
			name:  "tag delete",
			argv:  []string{"tag", "delete", "--name", "ready"},
			serve: func(t *testing.T) *fake.Server { return resolvingTags(t, tagsOfTwoOwners(), breakOff) },
		},
		{
			name:  "tag add",
			argv:  []string{"tag", "add", "DEV-7", "--name", "ready"},
			serve: func(t *testing.T) *fake.Server { return addingATag(t, owner, shownTags(), breakOff) },
		},
		{
			name:  "tag remove",
			argv:  []string{"tag", "remove", "DEV-7", "--name", "ready"},
			serve: func(t *testing.T) *fake.Server { return takingATagOff(t, owner, shownTags(), breakOff) },
		},
		{
			name:  "link add",
			argv:  []string{"link", "add", "DEV-1", "needs", "DEV-2"},
			serve: func(t *testing.T) *fake.Server { return linking(t, breakOff) },
		},
		{
			name:  "link remove",
			argv:  []string{"link", "remove", "DEV-1", "needs", "DEV-2"},
			serve: func(t *testing.T) *fake.Server { return linking(t, breakOff) },
		},
		{
			name:  "time create",
			argv:  []string{"time", "create", "DEV-1", "PT1H"},
			serve: func(t *testing.T) *fake.Server { return writingTime(t, breakOff) },
		},
		{
			name:  "time update",
			argv:  []string{"time", "update", "DEV-1", "199-6", "--text", "x"},
			serve: func(t *testing.T) *fake.Server { return writingTime(t, breakOff) },
		},
		{
			name: "time delete",
			argv: []string{"time", "delete", "DEV-1", "199-7"},
			serve: func(t *testing.T) *fake.Server {
				return removingTime(t, fake.JSON(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1")), breakOff)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.serve(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, "write_uncertain", requireUncertainty(t, got).code)
		})
	}
}

func TestEveryWriteExitsWith2WhereTheAnswerDisagreesWithTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		serve func(t *testing.T) *fake.Server
		argv  []string
		want  func(address string) faultDocument
	}{
		{
			name: "comment create",
			serve: func(t *testing.T) *fake.Server {
				return commenting(t, fake.JSON(http.StatusOK, createdComment("7-12", "text")))
			},
			argv: []string{"comment", "create", "DEV-7", "--text", "Text", "--fields", "id,text"},
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_invalid", details: []detail{
					{"request", issueCommentRequest(address, "DEV-7", "id,text")},
					{"comment", "7-12"},
					{"mismatch", []any{[]detail{{"field", "text"}, {"expected", "Text"}, {"actual", "text"}}}},
				}}
			},
		},
		{
			name: "comment update",
			serve: func(t *testing.T) *fake.Server {
				return updatingAComment(t, fake.JSON(http.StatusOK, commentDeletedState(false)),
					fake.JSON(http.StatusOK, createdComment("7-12", "text")))
			},
			argv: []string{"comment", "update", "DEV-7", "7-12", "--text", "Text", "--fields", "text"},
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_invalid", details: []detail{
					{"request", commentWriteRequest(address, "DEV-7", "7-12", "text")},
					{"comment", "7-12"},
					{"mismatch", []any{[]detail{{"field", "text"}, {"expected", "Text"}, {"actual", "text"}}}},
				}}
			},
		},
		{
			name: "issue create",
			serve: func(t *testing.T) *fake.Server {
				return creating(t, fake.JSON(http.StatusOK, projectRequiringNothing()),
					fake.JSON(http.StatusOK, createdIssueWith("DEV-7", "upper", "null", "[]")))
			},
			argv: []string{"issue", "create", "DEV", "--summary", "Upper", "--fields", "idReadable"},
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_invalid", details: []detail{
					{"request", creationRequest(address, "idReadable,summary")},
					{"issue", "DEV-7"},
					{"mismatch", []any{[]detail{{"field", "summary"}, {"expected", "Upper"}, {"actual", "upper"}}}},
				}}
			},
		},
		{
			name: "article create",
			serve: func(t *testing.T) *fake.Server {
				return creatingAnArticle(t, fake.JSON(http.StatusOK, createdArticle("DEV-A-7", "title", "null")))
			},
			argv: []string{"article", "create", "DEV", "--summary", "Title", "--fields", "idReadable"},
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_invalid", details: []detail{
					{"request", articleCreationRequest(address, "idReadable,summary,content,project(shortName)")},
					{"article", "DEV-A-7"},
					{"mismatch", []any{[]detail{{"field", "summary"}, {"expected", "Title"}, {"actual", "title"}}}},
				}}
			},
		},
		{
			name: "article update",
			serve: func(t *testing.T) *fake.Server {
				filed := answeredArticle{readable: "DEV-A-7", summary: "Title", content: asJSON("text")}
				return updatingAnArticle(t, fake.JSON(http.StatusOK, articleOfDEVToWrite("177-7", "DEV-A-7")),
					fake.JSON(http.StatusOK, filed.json()))
			},
			argv: []string{"article", "update", "DEV-A-7", "--content", "Text", "--fields", "idReadable"},
			want: func(address string) faultDocument {
				return faultDocument{code: "upstream_invalid", details: []detail{
					{"request", articleUpdateRequest(address, "DEV-A-7", "idReadable,content")},
					{"article", "DEV-A-7"},
					{"mismatch", []any{[]detail{{"field", "content"}, {"expected", "Text"}, {"actual", "text"}}}},
				}}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.serve(t)

			got := runWith(t, server.Env(), tc.argv...)

			assert.Equal(t, tc.want(server.URL), requireUncertainty(t, got))
		})
	}
}
