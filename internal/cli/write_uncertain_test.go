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
)

const gatewayPage = "<html><head><title>Bad Gateway</title></head><body>The server is not answering</body></html>"

func deletionRequest(address string) string {
	return "DELETE " + address + "/api/issues/DEV-7"
}

func gateway(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, gatewayPage)
	}
}

func blockingHandler(reached chan<- struct{}) http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) {
		close(reached)
		<-r.Context().Done()
	}
}

func cancelOnceReached(reached <-chan struct{}, cancel context.CancelFunc) {
	<-reached
	cancel()
}

func runInContext(t *testing.T, ctx context.Context, env []string, argv ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(ctx, argv, env, nil, nil, &stdout, &stderr)
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestIssueDeleteIsUncertainWhereTheAnswerNeverCame(t *testing.T) {
	t.Parallel()
	server := deleting(t, respondWith(http.StatusOK, issueNamed("DEV-7")), breakOff)

	got := runWith(t, server.env(), "issue", "delete", "DEV-7")

	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", deletionRequest(server.url)}}, found.details)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
}

func TestIssueDeleteFailsWhereTheDeletionNeverLeft(t *testing.T) {
	t.Parallel()
	var server *upstream
	server = serveWithoutKeepAlive(t, readThenDeletion(func(w http.ResponseWriter, r *http.Request) {
		server.stopListening(t)
		respondWith(http.StatusOK, issueNamed("DEV-7"))(w, r)
	}, noDeletion(t)))

	got := runWith(t, server.env(), "issue", "delete", "DEV-7")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_failed", found.code)
	assert.Equal(t, []detail{{"request", deletionRequest(server.url)}}, found.details)
	assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
}

func TestIssueDeleteClassifiesACancelledCallByWhetherTheRequestWasSent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		held    func(t *testing.T, reached chan<- struct{}) (read, deletion http.HandlerFunc)
		code    string
		exit    int
		request func(address string) string
		methods []string
	}{
		{
			name: "the deletion is held",
			held: func(_ *testing.T, reached chan<- struct{}) (http.HandlerFunc, http.HandlerFunc) {
				return respondWith(http.StatusOK, issueNamed("DEV-7")), blockingHandler(reached)
			},
			code:    "write_uncertain",
			exit:    2,
			request: deletionRequest,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "the read before it is held",
			held: func(t *testing.T, reached chan<- struct{}) (http.HandlerFunc, http.HandlerFunc) {
				return blockingHandler(reached), noDeletion(t)
			},
			code:    "upstream_failed",
			exit:    1,
			request: func(address string) string { return issueRequest(address, "DEV-7", deletedFields) },
			methods: []string{http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reached := make(chan struct{})
			read, deletion := tc.held(t, reached)
			server := deleting(t, read, deletion)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go cancelOnceReached(reached, cancel)

			got := runInContext(t, ctx, server.env(), "issue", "delete", "DEV-7")

			want := faultDocument{
				code:    tc.code,
				details: []detail{{"request", tc.request(server.url)}},
			}
			assert.Equal(t, want, requireFaultDocument(t, got))
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestIssueDeleteReadsA5xxByWhoWroteIt(t *testing.T) {
	t.Parallel()
	const failed = `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
	tests := []struct {
		name     string
		status   int
		deletion http.HandlerFunc
		code     string
		exit     int
		details  []detail
	}{
		{
			name:     "a gateway that reached nobody",
			status:   http.StatusBadGateway,
			deletion: gateway(http.StatusBadGateway),
			code:     "write_uncertain",
			exit:     2,
			details:  []detail{{"upstream_body", gatewayPage}},
		},
		{
			name:     "a gateway that waited for an answer",
			status:   http.StatusGatewayTimeout,
			deletion: gateway(http.StatusGatewayTimeout),
			code:     "write_uncertain",
			exit:     2,
			details:  []detail{{"upstream_body", gatewayPage}},
		},
		{
			name:     "YouTrack itself",
			status:   http.StatusInternalServerError,
			deletion: respondWith(http.StatusInternalServerError, failed),
			code:     "upstream_failed",
			exit:     1,
			details: []detail{
				{"upstream_error", "server_error"},
				{"upstream_message", "java.lang.NullPointerException"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := deleting(t, respondWith(http.StatusOK, issueNamed("DEV-7")), tc.deletion)

			got := runWith(t, server.env(), "issue", "delete", "DEV-7")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			want := append([]detail{
				{"request", deletionRequest(server.url)},
				{"upstream_status", tc.status},
			}, tc.details...)
			assert.Equal(t, want, found.details)
			assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
		})
	}
}

func TestIssueDeleteIsUncertainWhereTheAnswerBreaksOff(t *testing.T) {
	t.Parallel()
	const sent = "abc"
	server := deleting(t, respondWith(http.StatusOK, issueNamed("DEV-7")), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(sent)+7))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, sent)
	})

	got := runWith(t, server.env(), "issue", "delete", "DEV-7")

	want := faultDocument{
		code: "write_uncertain",
		details: []detail{
			{"request", deletionRequest(server.url)},
			{"upstream_status", 200},
			{"upstream_body", sent},
		},
	}
	assert.Equal(t, want, requireUncertainty(t, got))
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
}

func TestIssueUpdateClassifiesALostResponseByWhetherTheWriteWasSent(t *testing.T) {
	t.Parallel()
	project := projectResponse(writableField{id: "180-15", name: "Type", valueType: "enum", canBeEmpty: true})
	read := respondWith(http.StatusOK, issueToUpdate("DEV-7", project))

	t.Run("the answer to the write never came", func(t *testing.T) {
		t.Parallel()
		server := updating(t, read, breakOff)

		got := runWith(t, server.env(), "issue", "update", "DEV-7", "--field", "Type=Task")

		found := requireUncertainty(t, got)
		assert.Equal(t, "write_uncertain", found.code)
		assert.Equal(t, detail{"request", updateRequest(server.url, "DEV-7", askedIssueFields)}, found.details[0])
		assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
	})
	t.Run("the write never left", func(t *testing.T) {
		t.Parallel()
		var server *upstream
		server = serveWithoutKeepAlive(t, readThenUpdate(func(w http.ResponseWriter, r *http.Request) {
			server.stopListening(t)
			read(w, r)
		}, noUpdate(t)))

		got := runWith(t, server.env(), "issue", "update", "DEV-7", "--field", "Type=Task")

		found := requireFault(t, got)
		assert.Equal(t, "upstream_failed", found.code)
		assert.Equal(t, []detail{{"request", updateRequest(server.url, "DEV-7", askedIssueFields)}}, found.details)
		assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	})
}

func TestArticleDeleteExitsByWhetherTheDeletionMayHaveHappened(t *testing.T) {
	t.Parallel()
	read := respondWith(http.StatusOK, articleNamed("DEV-A-7"))
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return deleting(t, read, breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:    "a gateway answered a page under a 502",
			server:  func(t *testing.T) *upstream { return deleting(t, read, gateway(http.StatusBadGateway)) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "the deletion never left",
			server: func(t *testing.T) *upstream {
				var server *upstream
				server = serveWithoutKeepAlive(t, readThenDeletion(func(w http.ResponseWriter, r *http.Request) {
					server.stopListening(t)
					read(w, r)
				}, noDeletion(t)))
				return server
			},
			code:    "upstream_failed",
			exit:    1,
			methods: []string{http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "article", "delete", "DEV-A-7")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", articleDeletionRequest(server.url, "DEV-A-7")}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestCommentCreateExitsByWhetherTheWriteMayHaveHappened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return commenting(t, breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodPost},
		},
		{
			name:    "a gateway answered a page under a 502",
			server:  func(t *testing.T) *upstream { return commenting(t, gateway(http.StatusBadGateway)) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodPost},
		},
		{
			name: "the write never left",
			server: func(t *testing.T) *upstream {
				server := commenting(t, func(_ http.ResponseWriter, r *http.Request) {
					assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
				})
				server.stopListening(t)
				return server
			},
			code: "upstream_failed",
			exit: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "comment", "create", "DEV-7", "--text", "x")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", issueCommentRequest(server.url, "DEV-7", writtenCommentFields)},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestTimeCreateExitsByWhetherTheWriteMayHaveHappened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return writingTime(t, breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodPost},
		},
		{
			name:    "a gateway answered a page under a 502",
			server:  func(t *testing.T) *upstream { return writingTime(t, gateway(http.StatusBadGateway)) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodPost},
		},
		{
			name: "the write never left",
			server: func(t *testing.T) *upstream {
				server := writingTime(t, func(_ http.ResponseWriter, r *http.Request) {
					assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
				})
				server.stopListening(t)
				return server
			},
			code: "upstream_failed",
			exit: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "time", "create", "DEV-1", "PT1H30M")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", workItemWriteRequest(server.url, "DEV-1", sentWorkItemWriteFields)},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestTimeUpdateExitsByWhetherTheWriteMayHaveHappened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return writingTime(t, breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodPost},
		},
		{
			name:    "a gateway answered a page under a 502",
			server:  func(t *testing.T) *upstream { return writingTime(t, gateway(http.StatusBadGateway)) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodPost},
		},
		{
			name: "the write never left",
			server: func(t *testing.T) *upstream {
				server := writingTime(t, func(_ http.ResponseWriter, r *http.Request) {
					assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
				})
				server.stopListening(t)
				return server
			},
			code: "upstream_failed",
			exit: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "time", "update", "DEV-1", "199-6", "--text", "x")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request",
				workItemUpdateRequest(server.url, "DEV-1", "199-6", sentWorkItemWriteFields)}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestTimeDeleteExitsByWhetherTheRemovalMayHaveHappened(t *testing.T) {
	t.Parallel()
	read := respondWith(http.StatusOK, workItemOfAnIssue("199-7", "DEV-1"))
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		request func(address string) string
		methods []string
	}{
		{
			name:   "the answer to the removal never came",
			server: func(t *testing.T) *upstream { return removingTime(t, read, breakOff) },
			code:   "write_uncertain",
			exit:   2,
			request: func(address string) string {
				return workItemDeletionRequest(address, "DEV-1", "199-7")
			},
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:   "a gateway answered a page under a 502",
			server: func(t *testing.T) *upstream { return removingTime(t, read, gateway(http.StatusBadGateway)) },
			code:   "write_uncertain",
			exit:   2,
			request: func(address string) string {
				return workItemDeletionRequest(address, "DEV-1", "199-7")
			},
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "the removal never left",
			server: func(t *testing.T) *upstream {
				var server *upstream
				server = serveWithoutKeepAlive(t, readThenDeletion(func(w http.ResponseWriter, r *http.Request) {
					server.stopListening(t)
					read(w, r)
				}, noDeletion(t)))
				return server
			},
			code: "upstream_failed",
			exit: 1,
			request: func(address string) string {
				return workItemDeletionRequest(address, "DEV-1", "199-7")
			},
			methods: []string{http.MethodGet},
		},
		{
			name:   "the answer to the read never came",
			server: func(t *testing.T) *upstream { return removingTime(t, breakOff, noDeletion(t)) },
			code:   "upstream_failed",
			exit:   1,
			request: func(address string) string {
				return workItemReadRequest(address, "DEV-1", "199-7")
			},
			methods: []string{http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "time", "delete", "DEV-1", "199-7")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tc.request(server.url)}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestCommentUpdateExitsByWhetherTheWriteMayHaveHappened(t *testing.T) {
	t.Parallel()
	read := respondWith(http.StatusOK, commentDeletedState(false))
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		request func(address string) string
		methods []string
	}{
		{
			name:   "the answer to the write never came",
			server: func(t *testing.T) *upstream { return updatingAComment(t, read, breakOff) },
			code:   "write_uncertain",
			exit:   2,
			request: func(address string) string {
				return commentWriteRequest(address, "DEV-7", "7-12", writtenCommentFields)
			},
			methods: []string{http.MethodGet, http.MethodPost},
		},
		{
			name: "the write never left",
			server: func(t *testing.T) *upstream {
				var server *upstream
				server = serveWithoutKeepAlive(t, readThenUpdate(func(w http.ResponseWriter, r *http.Request) {
					server.stopListening(t)
					read(w, r)
				}, noUpdate(t)))
				return server
			},
			code: "upstream_failed",
			exit: 1,
			request: func(address string) string {
				return commentWriteRequest(address, "DEV-7", "7-12", writtenCommentFields)
			},
			methods: []string{http.MethodGet},
		},
		{
			name:    "the answer to the read never came",
			server:  func(t *testing.T) *upstream { return updatingAComment(t, breakOff, noUpdate(t)) },
			code:    "upstream_failed",
			exit:    1,
			request: func(address string) string { return commentReadRequest(address, "DEV-7", "7-12") },
			methods: []string{http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "comment", "update", "DEV-7", "7-12", "--text", "x")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tc.request(server.url)}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestCommentDeleteExitsByWhetherTheRemovalMayHaveHappened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return removingAComment(t, breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodDelete},
		},
		{
			name:    "a gateway answered a page under a 502",
			server:  func(t *testing.T) *upstream { return removingAComment(t, gateway(http.StatusBadGateway)) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodDelete},
		},
		{
			name: "the removal never left",
			server: func(t *testing.T) *upstream {
				server := removingAComment(t, func(_ http.ResponseWriter, r *http.Request) {
					assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
				})
				server.stopListening(t)
				return server
			},
			code: "upstream_failed",
			exit: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "comment", "delete", "DEV-7", "7-12")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", commentDeletionRequest(server.url, "DEV-7", "7-12")},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestIssueDeleteExitsByWhetherTheDeletionMayHaveHappened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		id     string
		server func(t *testing.T) *upstream
		code   string
		exit   int
	}{
		{
			name:   "a call no server could answer",
			id:     "..",
			server: serveNothing,
			code:   "bad_usage",
			exit:   1,
		},
		{
			name: "an issue the read does not find",
			id:   "DEV-7",
			server: func(t *testing.T) *upstream {
				return deleting(t, respondWith(http.StatusNotFound, entityNotFound("DEV-7")), noDeletion(t))
			},
			code: "not_found",
			exit: 1,
		},
		{
			name: "a readable id the deletion cannot be addressed by",
			id:   "DEV-7",
			server: func(t *testing.T) *upstream {
				return deleting(t, respondWith(http.StatusOK, issueNamed("..")), noDeletion(t))
			},
			code: "upstream_invalid",
			exit: 1,
		},
		{
			name: "a token that may read the issue and not delete it",
			id:   "DEV-7",
			server: func(t *testing.T) *upstream {
				said := `{"error":"Forbidden","error_description":"Insufficient rights"}`
				return deleting(t, respondWith(http.StatusOK, issueNamed("DEV-7")), respondWith(http.StatusForbidden, said))
			},
			code: "denied",
			exit: 1,
		},
		{
			name: "a deletion the server refused",
			id:   "DEV-7",
			server: func(t *testing.T) *upstream {
				said := `{"error":"bad_request","error_description":"Bad Request"}`
				return deleting(t, respondWith(http.StatusOK, issueNamed("DEV-7")), respondWith(http.StatusBadRequest, said))
			},
			code: "rejected",
			exit: 1,
		},
		{
			name: "a server that failed over the deletion",
			id:   "DEV-7",
			server: func(t *testing.T) *upstream {
				said := `{"error":"server_error","error_description":"java.lang.NullPointerException"}`
				return deleting(t, respondWith(http.StatusOK, issueNamed("DEV-7")), respondWith(http.StatusInternalServerError, said))
			},
			code: "upstream_failed",
			exit: 1,
		},
		{
			name: "an answer to the deletion that carries a body",
			id:   "DEV-7",
			server: func(t *testing.T) *upstream {
				return deleting(t, respondWith(http.StatusOK, issueNamed("DEV-7")), respondWith(http.StatusOK, `{"x":1}`))
			},
			code: "upstream_invalid",
			exit: 2,
		},
		{
			name: "an answer to the deletion that never came",
			id:   "DEV-7",
			server: func(t *testing.T) *upstream {
				return deleting(t, respondWith(http.StatusOK, issueNamed("DEV-7")), breakOff)
			},
			code: "write_uncertain",
			exit: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := runWith(t, tc.server(t).env(), "issue", "delete", tc.id)

			assert.Equal(t, tc.code, requireFaultDocument(t, got).code)
			assert.Equal(t, tc.exit, got.code)
		})
	}
}

func TestAttachmentDeleteExitsByWhetherTheDeletionMayHaveHappened(t *testing.T) {
	t.Parallel()
	read := respondWith(http.StatusOK, attachmentOfDEV7())
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return deleting(t, read, breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name:    "a gateway answered a page under a 502",
			server:  func(t *testing.T) *upstream { return deleting(t, read, gateway(http.StatusBadGateway)) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "the deletion never left",
			server: func(t *testing.T) *upstream {
				var server *upstream
				server = serveWithoutKeepAlive(t, readThenDeletion(func(w http.ResponseWriter, r *http.Request) {
					server.stopListening(t)
					read(w, r)
				}, noDeletion(t)))
				return server
			},
			code:    "upstream_failed",
			exit:    1,
			methods: []string{http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "attachment", "delete", "DEV-7", "12-5")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", attachmentDeletionRequest(server.url, "issues", "DEV-7", "12-5")},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestTagDeleteExitsByWhetherTheDeletionMayHaveHappened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return resolvingTags(t, tagsOfTwoOwners(), breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "a gateway answered a page under a 502",
			server: func(t *testing.T) *upstream {
				return resolvingTags(t, tagsOfTwoOwners(), gateway(http.StatusBadGateway))
			},
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodDelete},
		},
		{
			name: "the deletion never left",
			server: func(t *testing.T) *upstream {
				var server *upstream
				server = serveWithoutKeepAlive(t, readThenDeletion(func(w http.ResponseWriter, r *http.Request) {
					server.stopListening(t)
					respondWith(http.StatusOK, tagsOfTwoOwners())(w, r)
				}, noDeletion(t)))
				return server
			},
			code:    "upstream_failed",
			exit:    1,
			methods: []string{http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "tag", "delete", "--name", "ready")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tagDeletionRequest(server.url, "10-5")}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestTagAddExitsByWhetherTheTaggingMayHaveHappened(t *testing.T) {
	t.Parallel()
	owner := respondWith(http.StatusOK, issueNamed("DEV-7"))
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return addingATag(t, owner, shownTags(), breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodGet, http.MethodPost},
		},
		{
			name: "a gateway answered a page under a 502",
			server: func(t *testing.T) *upstream {
				return addingATag(t, owner, shownTags(), gateway(http.StatusBadGateway))
			},
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodGet, http.MethodPost},
		},
		{
			name: "the tagging never left",
			server: func(t *testing.T) *upstream {
				var server *upstream
				server = serveWithoutKeepAlive(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == tagsCollection {
						server.stopListening(t)
						shownTags()(w, r)
						return
					}
					owner(w, r)
				})
				return server
			},
			code:    "upstream_failed",
			exit:    1,
			methods: []string{http.MethodGet, http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "tag", "add", "DEV-7", "--name", "ready")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, taggingRequest(server.url, "issues", "DEV-7", resolvedTagFields),
				detailNamed(t, found, "request"))
			assert.Equal(t, []detail{{"issue", "DEV-7"}, {"tag", "ready"}}, found.details[1:3])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestTagRemoveExitsByWhetherTheRemovalMayHaveHappened(t *testing.T) {
	t.Parallel()
	owner := respondWith(http.StatusOK, issueNamed("DEV-7"))
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return takingATagOff(t, owner, shownTags(), breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodGet, http.MethodDelete},
		},
		{
			name: "a gateway answered a page under a 502",
			server: func(t *testing.T) *upstream {
				return takingATagOff(t, owner, shownTags(), gateway(http.StatusBadGateway))
			},
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodGet, http.MethodGet, http.MethodDelete},
		},
		{
			name: "the removal never left",
			server: func(t *testing.T) *upstream {
				var server *upstream
				server = serveWithoutKeepAlive(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == tagsCollection {
						server.stopListening(t)
						shownTags()(w, r)
						return
					}
					owner(w, r)
				})
				return server
			},
			code:    "upstream_failed",
			exit:    1,
			methods: []string{http.MethodGet, http.MethodGet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)

			got := runWith(t, server.env(), "tag", "remove", "DEV-7", "--name", "ready")

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tagRemovalRequest(server.url, "issues", "DEV-7", "10-5"), detailNamed(t, found, "request"))
			assert.Equal(t, []detail{{"issue", "DEV-7"}, {"tag", "ready"}}, found.details[1:3])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

func TestAttachmentCreateExitsByWhetherTheUploadMayHaveHappened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		server  func(t *testing.T) *upstream
		code    string
		exit    int
		methods []string
	}{
		{
			name:    "the answer never came",
			server:  func(t *testing.T) *upstream { return serve(t, breakOff) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodPost},
		},
		{
			name:    "a gateway answered a page under a 502",
			server:  func(t *testing.T) *upstream { return serve(t, gateway(http.StatusBadGateway)) },
			code:    "write_uncertain",
			exit:    2,
			methods: []string{http.MethodPost},
		},
		{
			name: "the upload never left",
			server: func(t *testing.T) *upstream {
				server := serve(t, func(_ http.ResponseWriter, r *http.Request) {
					assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
				})
				server.stopListening(t)
				return server
			},
			code: "upstream_failed",
			exit: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server(t)
			path := aFileToAttach(t)

			got := runWith(t, server.env(), "attachment", "create", "DEV-7", path)

			found := requireFaultDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", attachmentWriteRequest(server.url, "DEV-7", attachmentFields)},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}
