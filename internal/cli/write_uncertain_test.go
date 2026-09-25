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

// What a gateway in front of YouTrack answers with when the server behind it says nothing: a page, under a
// status YouTrack writes only as JSON.
const gatewayPage = "<html><head><title>Bad Gateway</title></head><body>The server is not answering</body></html>"

// The request the deletion of DEV-7 goes out as, which is the one a refusal about it names.
func deletionRequest(address string) string {
	return "DELETE " + address + "/api/issues/DEV-7"
}

// gateway is an answer of something other than YouTrack: a status of its own and a page under it.
func gateway(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, gatewayPage)
	}
}

// blockingHandler is a handler that tells the test its request arrived whole and then waits for the caller to go away,
// which is what a server taking its time over a write looks like from outside.
func blockingHandler(reached chan<- struct{}) http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) {
		close(reached)
		<-r.Context().Done()
	}
}

// runInContext is runWith on a context of the scenario's own: only one that cancels the call needs it.
func runInContext(t *testing.T, ctx context.Context, env []string, argv ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(ctx, argv, env, nil, nil, &stdout, &stderr)
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// The deletion left whole and the connection went away before an answer: the server may have carried it
// out and may never have started, and nothing ytrack could send afterwards tells the two apart — a repeat
// would delete an issue somebody else filed under the same id, and a read would answer about a deletion that
// may still be under way. So the caller is told that much, and nothing else goes out.
func TestIssueDeleteIsUncertainWhereTheAnswerNeverCame(t *testing.T) {
	t.Parallel()
	server := deleting(t, respondWith(http.StatusOK, issueNamed("DEV-7")), breakOff)

	got := runWith(t, server.env(), "issue", "delete", "DEV-7")

	// The word for a connection that went away comes from the kernel and differs between them.
	found := requireUncertainty(t, got)
	assert.Equal(t, "write_uncertain", found.code)
	assert.Equal(t, []detail{{"request", deletionRequest(server.url)}}, found.details)
	assert.Equal(t, []string{http.MethodGet, http.MethodDelete}, sentMethods(server))
}

// Nothing was listening by the time the deletion was sent, so it never left: the issue stands where it
// was, and the caller may send the same call again once the server is back.
func TestIssueDeleteFailsWhereTheDeletionNeverLeft(t *testing.T) {
	t.Parallel()
	var server *upstream
	// Without keep alives the answer to the read ends the connection it came back over, so the deletion has to
	// dial the server that is no longer there instead of going out over a connection that is already open.
	server = serveWithoutKeepAlive(t, readThenDeletion(func(w http.ResponseWriter, r *http.Request) {
		server.stopListening(t)
		respondWith(http.StatusOK, issueNamed("DEV-7"))(w, r)
	}, noDeletion(t)))

	got := runWith(t, server.env(), "issue", "delete", "DEV-7")

	// The word for a connection nothing accepts is the kernel's too.
	found := requireRefusal(t, got)
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
			// Cancelled once the request is with the server, so which of the two ytrack was in the middle of
			// is settled by the scenario rather than by which goroutine ran first.
			go func() {
				<-reached
				cancel()
			}()

			got := runInContext(t, ctx, server.env(), "issue", "delete", "DEV-7")

			want := faultDocument{
				code:    tc.code,
				details: []detail{{"request", tc.request(server.url)}},
			}
			assert.Equal(t, want, requireRefusalDocument(t, got))
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// A 5xx that is not YouTrack's own word about a failure was written by something between ytrack and the
// instance, which may well have passed the deletion on; YouTrack's own says the server got as far as refusing
// it, and that leaves the issue where it was.
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

			found := requireRefusalDocument(t, got)
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

// The status of the deletion arrived and the rest of the answer did not. The server had taken the request
// by the time it answered, so the issue may well be gone, whatever the part of the answer that never came was
// going to say.
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

// The border is the same for the write that carries a body as for the one that carries none, and it is
// where the request left rather than what the error says. An update left whole may well have been carried
// out; one that never left leaves the issue exactly as the read before it found it.
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
		// Without keep alives the answer to the read ends the connection it came back over, so the write has to
		// dial the server that is no longer there instead of going out over one that is already open.
		server = serveWithoutKeepAlive(t, readThenUpdate(func(w http.ResponseWriter, r *http.Request) {
			server.stopListening(t)
			read(w, r)
		}, noUpdate(t)))

		got := runWith(t, server.env(), "issue", "update", "DEV-7", "--field", "Type=Task")

		found := requireRefusal(t, got)
		assert.Equal(t, "upstream_failed", found.code)
		assert.Equal(t, []detail{{"request", updateRequest(server.url, "DEV-7", askedIssueFields)}}, found.details)
		assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
	})
}

// The border holds wherever a deletion goes out, and the deletion of an article is where it costs most: a
// subtree the caller cannot see the end of may be gone or may be standing, and nothing ytrack could send
// afterwards tells the two apart. A deletion that never left leaves the tree exactly as the read found it.
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
				// Without keep alives the answer to the read ends the connection it came back over, so the
				// deletion has to dial the server that is no longer there.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", articleDeletionRequest(server.url, "DEV-A-7")}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The border holds where the write is the whole command and no read stands before it: a POST left whole
// may well have added a comment nobody can see from here, and one that never left added none. A page under a
// 5xx was written by something between ytrack and the instance, which may have passed the write on.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", issueCommentRequest(server.url, "DEV-7", writtenCommentFields)},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// A page under a 5xx was written by an intermediary that may have passed the write on.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", workItemWriteRequest(server.url, "DEV-1", sentWorkItemWriteFields)},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// A work item has no readable id to read back, so a POST that left whole may have changed one.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request",
				workItemUpdateRequest(server.url, "DEV-1", "199-6", sentWorkItemWriteFields)}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The read before DELETE tells the cases apart: a failed read leaves the item in place, a DELETE left
// whole may have removed it.
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
				// Without keep alives the answer to the read ends the connection it came back over, so the
				// removal has to dial the server that is no longer there.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tc.request(server.url)}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The border holds where a read stands before the write: the POST left whole may well have changed a
// comment nobody can see from here, while anything that goes wrong during the read before it leaves the
// comment exactly as it was and costs the caller nothing but sending the call again.
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
				// Without keep alives the answer to the read ends the connection it came back over, so the write
				// has to dial the server that is no longer there instead of going out over one already open.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tc.request(server.url)}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The border holds where the write is a removal and no read stands before it: a DELETE left whole may
// well have taken the comment away, and one that never left took none. The comment carries no readable id to
// read back afterwards, so what the border says is the whole of what the caller has.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", commentDeletionRequest(server.url, "DEV-7", "7-12")},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The table of exit codes over the one write there is. A refusal that leaves the instance as it was exits 1,
// whatever it is about; a refusal the caller cannot answer by sending the call again — the answer never came,
// or it came and says the deletion was carried out — exits 2. Nothing but the code is read to tell them apart.
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

			assert.Equal(t, tc.code, requireRefusalDocument(t, got).code)
			assert.Equal(t, tc.exit, got.code)
		})
	}
}

// The border holds where the deletion stands behind a read: a DELETE left whole may well have taken the
// file away, and the signed link that was printed for it with it, while anything that goes wrong before the
// request leaves asks the caller for nothing but the same call again.
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
				// Without keep alives the answer to the read ends the connection it came back over, so the
				// deletion has to dial the server that is no longer there.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", attachmentDeletionRequest(server.url, "issues", "DEV-7", "12-5")},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The border holds where the deletion stands behind a read of ytrack's own making: the DELETE left whole
// may well have destroyed a tag for its owner and for everyone it was shared with, and one that never left
// destroyed none. Nothing ytrack could send afterwards tells the two apart — a repeat would destroy a tag
// somebody has since made under the same name.
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
				// Without keep alives the answer to the read ends the connection it came back over, so the
				// deletion has to dial the server that is no longer there.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", tagDeletionRequest(server.url, "10-5")}, found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The border holds where two reads stand before the write: a POST left whole may well have hung the tag,
// and one that never left hung none. Nothing ytrack could send afterwards tells the two apart — the answer to
// a second tagging reads the same whether the first went through or not.
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
				// Without keep alives the answer to each read ends the connection it came back over, so the
				// write has to dial the server that is no longer there.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, taggingRequest(server.url, "issues", "DEV-7", resolvedTagFields),
				detailNamed(t, found, "request"))
			assert.Equal(t, []detail{{"issue", "DEV-7"}, {"tag", "ready"}}, found.details[1:3])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The border holds where the write is a removal that carries no body at all: a DELETE left whole may well
// have taken the tag off, and one that never left took none. A page under a 5xx was written by something
// between ytrack and the instance, which may have passed the removal on.
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
				// Without keep alives the answer to each read ends the connection it came back over, so the
				// removal has to dial the server that is no longer there.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, tagRemovalRequest(server.url, "issues", "DEV-7", "10-5"), detailNamed(t, found, "request"))
			assert.Equal(t, []detail{{"issue", "DEV-7"}, {"tag", "ready"}}, found.details[1:3])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}

// The border holds where the write carries a file rather than a body ytrack built, and the file is what
// makes it worth holding: a POST left whole may well have attached megabytes nobody can see from here, and one
// that never left attached none. The bytes are read into the request as it goes out, so a request the server
// took whole before it stopped answering is a request that left.
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

			found := requireRefusalDocument(t, got)
			assert.Equal(t, tc.code, found.code)
			assert.Equal(t, tc.exit, got.code)
			assert.Equal(t, detail{"request", attachmentWriteRequest(server.url, "DEV-7", attachmentFields)},
				found.details[0])
			assert.Equal(t, tc.methods, sentMethods(server))
		})
	}
}
