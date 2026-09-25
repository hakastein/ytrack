package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
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
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"

	"github.com/hakastein/ytrack/internal/cli"
)

const token = "perm-ytrack-test-token"

const devInstanceURL = "http://localhost:8091"

type upstream struct {
	url      string
	token    string
	server   *httptest.Server
	mu       sync.Mutex
	rewrite  func(*url.URL)
	replace  func(*http.Request, []byte) []byte
	received []*http.Request
	asked    [][]byte
	answered [][]byte
}

type devInstanceTokens struct {
	admin   string
	limited string
	member  string
}

func serve(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	return (&upstream{token: token}).serving(t, handler)
}

func (u *upstream) serving(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	return u.start(t, httptest.NewUnstartedServer(u.recording(t, handler)))
}

func appending(added string) func(*url.URL) {
	if added == "" {
		return nil
	}
	return func(u *url.URL) {
		if u.RawQuery == "" {
			u.RawQuery = added
			return
		}
		u.RawQuery += "&" + added
	}
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
		body := u.edited(t, r)
		u.logRequest(r, body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler(w, r)
	}
}

func (u *upstream) edited(t *testing.T, r *http.Request) []byte {
	t.Helper()
	rewrite, replace := u.editing()
	if rewrite != nil {
		rewrite(r.URL)
	}
	body, err := io.ReadAll(r.Body)
	assert.NoError(t, err, "reading the body of %s %s", r.Method, r.URL)
	if replace != nil {
		body = replace(r, body)
		r.ContentLength = int64(len(body))
	}
	return body
}

func (u *upstream) logRequest(r *http.Request, body []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.received = append(u.received, r.Clone(r.Context()))
	u.asked = append(u.asked, body)
}

func (u *upstream) editing() (func(*url.URL), func(*http.Request, []byte) []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.rewrite, u.replace
}

func (u *upstream) rewriting(rewrite func(*url.URL)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.rewrite = rewrite
}

func (u *upstream) replacing(replace func(*http.Request, []byte) []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.replace = replace
}

func devInstance(t *testing.T) *upstream {
	t.Helper()
	return devInstanceWithRewrite(t, nil)
}

func devInstanceAsking(t *testing.T, added string) *upstream {
	t.Helper()
	return devInstanceWithRewrite(t, appending(added))
}

func devInstanceWithRewrite(t *testing.T, rewrite func(*url.URL)) *upstream {
	t.Helper()
	tokens := devTokens(t)
	rec, err := recorder.New("testdata/cassettes/"+t.Name(),
		recorder.WithMode(cassetteMode),
		recorder.WithRealTransport(realTransport(t)),
		recorder.WithSkipRequestLatency(true),
		recorder.WithMatcher(matchingMultipartByItsParts()),
		recorder.WithHook(scrub(tokens), recorder.BeforeSaveHook),
	)
	require.NoError(t, err)
	t.Cleanup(func() { writeTheCassetteUnlessFailed(t, rec) })
	dev := &upstream{token: tokens.admin, rewrite: rewrite}
	dev.serving(t, func(w http.ResponseWriter, r *http.Request) {
		response, err := forward(rec, r)
		if err != nil {
			t.Errorf("cassette %s: %v", t.Name(), err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Errorf("cassette %s: %v", t.Name(), err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		dev.keep(body)
		maps.Copy(w.Header(), response.Header)
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(body)
	})
	return dev
}

func writeTheCassetteUnlessFailed(t *testing.T, rec *recorder.Recorder) {
	t.Helper()
	if !t.Failed() {
		assert.NoError(t, rec.Stop())
	}
}

func scrub(tokens devInstanceTokens) recorder.HookFunc {
	return func(i *cassette.Interaction) error {
		i.Request.Headers.Del("Authorization")
		i.Response.Headers.Del("Date")
		i.Response.Headers.Del("Last-Modified")
		i.Response.Headers.Del("X-Version")
		i.Response.Duration = 0
		recorded, err := json.Marshal(i)
		if err != nil {
			return err
		}
		for _, secret := range []string{tokens.admin, tokens.limited, tokens.member} {
			if bytes.Contains(recorded, []byte(secret)) {
				return fmt.Errorf("interaction %d holds a token of the dev instance", i.ID)
			}
		}
		return nil
	}
}

func TestCassetteRefusesATokenOfTheDevInstance(t *testing.T) {
	t.Parallel()
	tokens := devTokens(t)
	tests := []struct {
		name  string
		token string
	}{
		{name: "the admin's", token: tokens.admin},
		{name: "the limited user's", token: tokens.limited},
		{name: "the member's", token: tokens.member},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			interaction := &cassette.Interaction{Response: cassette.Response{Body: `{"token":"` + tc.token + `"}`}}

			assert.EqualError(t, scrub(tokens)(interaction), "interaction 0 holds a token of the dev instance")
		})
	}
}

func forward(rec *recorder.Recorder, r *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	out, err := http.NewRequestWithContext(r.Context(), r.Method, devInstanceURL+r.URL.RequestURI(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header = r.Header.Clone()
	// net/http ungzips only what it asked for; a forwarded Accept-Encoding leaves !!binary gzip in the cassette.
	out.Header.Del("Accept-Encoding")
	return rec.RoundTrip(out)
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

func anonymousClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func fetched(t *testing.T, address, bearer string) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	require.NoError(t, err)
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := anonymousClient().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response, body
}

func (u *upstream) env() []string {
	return []string{"YTRACK_URL=" + u.url, "YTRACK_TOKEN=" + u.token}
}

func (u *upstream) requests() []*http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.received)
}

func (u *upstream) keep(body []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.answered = append(u.answered, body)
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

func (u *upstream) answers() [][]byte {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.answered)
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

// multipart.Writer draws a new random boundary on every send.
func matchingMultipartByItsParts() cassette.MatcherFunc {
	byDefault := cassette.NewDefaultMatcher(cassette.WithIgnoreAuthorization(), cassette.WithIgnoreUserAgent())
	return func(r *http.Request, i cassette.Request) bool {
		recorded, wasForm := partsOf([]byte(i.Body), i.Headers.Get("Content-Type"))
		sent, isForm := partsSent(r)
		if !wasForm || !isForm {
			return byDefault(r, i)
		}
		return r.Method == i.Method && r.URL.String() == i.URL && slices.Equal(sent, recorded)
	}
}

func partsSent(r *http.Request) ([]formPart, bool) {
	if _, isForm := formBoundary(r.Header.Get("Content-Type")); !isForm || r.Body == nil {
		return nil, false
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return partsOf(body, r.Header.Get("Content-Type"))
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
