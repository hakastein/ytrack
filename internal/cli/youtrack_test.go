package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/cli"
)

const token = "perm-ytrack-test-token"

type upstream struct {
	url      string
	token    string
	server   *httptest.Server
	mu       sync.Mutex
	received []*http.Request
	asked    [][]byte
}

func serve(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	return (&upstream{token: token}).serving(t, handler)
}

func (u *upstream) serving(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	return u.start(t, httptest.NewUnstartedServer(u.recording(t, handler)))
}

func serveWithoutKeepAlive(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	u := &upstream{token: token}
	server := httptest.NewUnstartedServer(u.recording(t, handler))
	server.Config.SetKeepAlivesEnabled(false)
	return u.start(t, server)
}

func (u *upstream) start(t *testing.T, server *httptest.Server) *upstream {
	t.Helper()
	server.Start()
	t.Cleanup(server.Close)
	u.url = server.URL
	u.server = server
	return u
}

func (u *upstream) stopListening(t *testing.T) {
	t.Helper()
	address := u.server.Listener.Addr().String()
	assert.NoError(t, u.server.Listener.Close())
	assert.Eventually(t, func() bool { return dialRefused(address) }, time.Minute, time.Millisecond,
		"the kernel still completes handshakes on %s after Close", address)
}

func dialRefused(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		return true
	}
	_ = conn.Close()
	return false
}

func (u *upstream) recording(t *testing.T, handler http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err, "reading the body of %s %s", r.Method, r.URL)
		u.logRequest(r, body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler(w, r)
	}
}

func (u *upstream) logRequest(r *http.Request, body []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.received = append(u.received, r.Clone(r.Context()))
	u.asked = append(u.asked, body)
}

const assistPath = "/api/search/assist"

const markupFields = "query,styleRanges(start,length,style)"

func searching(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, markingUp(t, handler))
}

func markingUp(t *testing.T, handler http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != assistPath {
			handler(w, r)
			return
		}
		respondWith(http.StatusOK, markup(t, searchAsked(t, r)))(w, r)
	}
}

func searchAsked(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	assert.NoError(t, err)
	var asked struct {
		Query string `json:"query"`
	}
	assert.NoError(t, json.Unmarshal(body, &asked), "the body of %s %s: %s", r.Method, r.URL, body)
	return asked.Query
}

func markup(t *testing.T, query string, ranges ...string) string {
	t.Helper()
	echoed, err := json.Marshal(query)
	assert.NoError(t, err)
	return `{"$type":"SearchSuggestions","query":` + string(echoed) + `,"styleRanges":[` + strings.Join(ranges, ",") + `]}`
}

func styled(startInUTF16, lengthInUTF16 int, style string) string {
	return fmt.Sprintf(`{"$type":"SearchStyleRange","start":%d,"length":%d,"style":%q}`,
		startInUTF16, lengthInUTF16, style)
}

func requireMarkedUpFirst(t *testing.T, server *upstream, query string) {
	t.Helper()
	requests := server.requests()
	require.NotEmpty(t, requests, "no request reached the server")
	assert.Equal(t, assistPath, requests[0].URL.Path)
	assert.Equal(t, 1, sentTo(server, assistPath))
	assert.Equal(t, http.MethodPost, requests[0].Method)
	assert.Equal(t, "application/json", requests[0].Header.Get("Content-Type"))
	assert.Equal(t, markupFields, requests[0].URL.Query().Get("fields"))
	asked, err := json.Marshal(struct {
		Query string `json:"query"`
	}{Query: query})
	require.NoError(t, err)
	assert.Equal(t, string(asked), server.asks()[0])
}

func serveNothing(t *testing.T) *upstream {
	t.Helper()
	return serve(t, func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
	})
}

func respondWith(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func (u *upstream) env() []string {
	return []string{"YTRACK_URL=" + u.url, "YTRACK_TOKEN=" + u.token}
}

func (u *upstream) requests() []*http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.received)
}

func (u *upstream) asks() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	bodies := make([]string, 0, len(u.asked))
	for _, body := range u.asked {
		bodies = append(bodies, string(body))
	}
	return bodies
}

func lastAsk(u *upstream) string {
	asked := u.asks()
	if len(asked) == 0 {
		return ""
	}
	return asked[len(asked)-1]
}

func (u *upstream) sentFields() []string {
	var fields []string
	for _, request := range u.requests() {
		fields = append(fields, request.URL.Query().Get("fields"))
	}
	return fields
}

func (u *upstream) sentPaths() []string {
	var paths []string
	for _, request := range u.requests() {
		paths = append(paths, request.URL.Path)
	}
	return paths
}

func (u *upstream) sentTargets() []string {
	var targets []string
	for _, request := range u.requests() {
		query, err := url.QueryUnescape(request.URL.RawQuery)
		if err != nil {
			query = request.URL.RawQuery
		}
		targets = append(targets, request.URL.Path+"?"+query)
	}
	return targets
}

func (u *upstream) sentQueries() []url.Values {
	var queries []url.Values
	for _, request := range u.requests() {
		queries = append(queries, request.URL.Query())
	}
	return queries
}

func runWith(t *testing.T, env []string, argv ...string) outcome {
	t.Helper()
	return runOn(t, nil, env, argv...)
}

func runOn(t *testing.T, stdin *os.File, env []string, argv ...string) outcome {
	t.Helper()
	return runBuiltFrom(t, nil, stdin, env, argv...)
}

func runBuiltFrom(t *testing.T, build *debug.BuildInfo, stdin *os.File, env []string, argv ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(t.Context(), argv, env, build, stdin, &stdout, &stderr)
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

const multipartForm = "multipart/form-data"

type formPart struct {
	field       string
	file        string
	content     string
	disposition string
}

func partsOf(body []byte, contentType string) ([]formPart, bool) {
	boundary, isForm := formBoundary(contentType)
	if !isForm {
		return nil, false
	}
	form := multipart.NewReader(bytes.NewReader(body), boundary)
	read := []formPart{}
	for {
		part, err := form.NextPart()
		if errors.Is(err, io.EOF) {
			return read, true
		}
		if err != nil {
			return nil, false
		}
		content, err := io.ReadAll(part)
		if err != nil {
			return nil, false
		}
		read = append(read, formPart{
			field:       part.FormName(),
			file:        part.FileName(),
			content:     string(content),
			disposition: part.Header.Get("Content-Disposition"),
		})
	}
}

func formBoundary(contentType string) (string, bool) {
	kind, params, err := mime.ParseMediaType(contentType)
	if err != nil || kind != multipartForm {
		return "", false
	}
	boundary, given := params["boundary"]
	return boundary, given
}

func sentParts(t *testing.T, u *upstream, at int) []formPart {
	t.Helper()
	requests, asked := u.requests(), u.asks()
	require.Greater(t, len(requests), at, "the server was sent no request %d", at)
	parts, isForm := partsOf([]byte(asked[at]), requests[at].Header.Get("Content-Type"))
	require.True(t, isForm, "the body of %s %s is no multipart form: %q",
		requests[at].Method, requests[at].URL, asked[at])
	return parts
}
