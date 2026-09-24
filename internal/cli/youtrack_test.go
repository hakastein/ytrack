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

// token goes out as YTRACK_TOKEN, so a test can look for it where it must not appear.
const token = "perm-ytrack-test-token"

// A cassette matches a request by its URL and host, so every request goes out to this one address,
// the polygon's port in dev/.env.example, whatever port the test's own server listens on.
const devInstanceURL = "http://localhost:8091"

// upstream is the server behind YTRACK_URL, keeping every request it was sent.
type upstream struct {
	url    string
	token  string
	server *httptest.Server
	mu     sync.Mutex
	// What the address and the body of a request are edited into on their way in, or nil where the request
	// stands as ytrack sent it; see devInstanceRewriting and (*upstream).replacing.
	rewrite  func(*url.URL)
	replace  func(*http.Request, []byte) []byte
	received []*http.Request
	asked    [][]byte
	answered [][]byte
}

// The tokens make install leaves for the polygon's users; no cassette may hold one.
type polygonTokens struct {
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
	return u.start(t, httptest.NewUnstartedServer(u.keeping(t, handler)))
}

// appending is the rewrite that writes added onto the raw query of every request. The text is raw, spelled as
// it has to reach the server rather than as url.Values would escape it, and an empty one is no rewrite at all:
// a scenario that names none sends what ytrack built.
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

// serveWithoutKeepAlive is serve for a scenario that turns on what the caller does between two requests: every
// answer ends the connection it went out over, so the next request has to dial the server again.
func serveWithoutKeepAlive(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	u := &upstream{token: token}
	server := httptest.NewUnstartedServer(u.keeping(t, handler))
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

// stopListening frees the port and leaves the connections open, so an answer already on its way still arrives
// while a request sent after it is refused. Closing the socket is not the end of it: for some milliseconds
// the kernel goes on completing handshakes on the port, so the refusal itself is what is waited for.
func (u *upstream) stopListening(t *testing.T) {
	t.Helper()
	address := u.server.Listener.Addr().String()
	assert.NoError(t, u.server.Listener.Close())
	assert.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	}, time.Minute, time.Millisecond, "%s is still taking connections", address)
}

// keeping is handler with every request it is sent edited and then written down, both before anything else
// reads it, so what is written down, what a cassette keeps and what the handler is sent are one request and
// not three.
func (u *upstream) keeping(t *testing.T, handler http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		rewrite, replace := u.editing()
		if rewrite != nil {
			rewrite(r.URL)
		}
		// The body is read here rather than left to the handler: a request that asks its question in a body is
		// held to that question by scenarios whose handler is the forwarder of a cassette.
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err, "reading the body of %s %s", r.Method, r.URL)
		if replace != nil {
			body = replace(r, body)
			r.ContentLength = int64(len(body))
		}
		u.mu.Lock()
		u.received = append(u.received, r.Clone(r.Context()))
		u.asked = append(u.asked, body)
		u.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler(w, r)
	}
}

// editing is the pair of edits in force, taken together so a scenario that puts one on mid-run is read by the
// server rather than raced with it.
func (u *upstream) editing() (func(*url.URL), func(*http.Request, []byte) []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.rewrite, u.replace
}

// rewriting puts an edit of the address in force, or takes the one in force off again where it is handed nil,
// so one scenario asks the polygon about an address ytrack sends and about one it never would.
func (u *upstream) rewriting(rewrite func(*url.URL)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.rewrite = rewrite
}

// replacing is rewriting for the body a request carries: it is handed the request the body belongs to, so a
// scenario edits the write alone and leaves the read before it as ytrack sent it.
func (u *upstream) replacing(replace func(*http.Request, []byte) []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.replace = replace
}

// devInstance is the polygon as its admin sees it: replayed from the cassette named after the test,
// or, under the contract tag, the polygon itself, which rewrites that cassette.
func devInstance(t *testing.T) *upstream {
	t.Helper()
	return devInstanceRewriting(t, nil)
}

// devInstanceAsking is devInstance with added written onto the query of every request that goes through it. It
// is how a scenario holds the polygon to a parameter ytrack has no way to send: the specification declares none
// such for the operation, so the generated client carries no field for it either, and the only place left to
// put it is the wire.
func devInstanceAsking(t *testing.T, added string) *upstream {
	t.Helper()
	return devInstanceRewriting(t, appending(added))
}

// devInstanceRewriting is devInstance with rewrite standing between ytrack and the recorder: it is handed the
// address of every request before that request is written down or sent on, so a scenario can hold the polygon
// to a request ytrack itself will not send. The cassette keeps what the polygon was asked, so the recording and
// the replay ask the same thing, and so does the journal.
func devInstanceRewriting(t *testing.T, rewrite func(*url.URL)) *upstream {
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
	t.Cleanup(func() {
		// A scenario that failed has confirmed nothing it recorded, so its cassette stays as it was.
		if !t.Failed() {
			assert.NoError(t, rec.Stop())
		}
	})
	// The handler reads dev after serving returned it, so a scenario against real data can hold the document
	// against what the polygon said rather than against a copy of the fixture.
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

func scrub(tokens polygonTokens) recorder.HookFunc {
	return func(i *cassette.Interaction) error {
		i.Request.Headers.Del("Authorization")
		// All that differs between two recordings of the same answer: X-Version changes with a polygon
		// installed anew on the same build, and the web page's Last-Modified is a moment of its run.
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
				return fmt.Errorf("interaction %d holds a token of the polygon", i.ID)
			}
		}
		return nil
	}
}

func TestCassetteRefusesATokenOfThePolygon(t *testing.T) {
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

			assert.EqualError(t, scrub(tokens)(interaction), "interaction 0 holds a token of the polygon")
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
	// The transport ungzips only what it asked for itself: a forwarded Accept-Encoding would leave the
	// body in the cassette as !!binary gzip.
	out.Header.Del("Accept-Encoding")
	return rec.RoundTrip(out)
}

// Where a search is marked up, which every selection asks for before it runs the search itself.
const assistPath = "/api/search/assist"

// The names of the markup ytrack asks for, none of which the specification declares.
const markupFields = "query,styleRanges(start,length,style)"

// searching is the server of a selection: it marks every search up as carrying no styled range at all and
// leaves every other request to handler, so a scenario that says nothing of the markup runs against one that
// passes.
func searching(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, markingUp(t, handler))
}

// markingUp answers a request that marks a search up with the search itself, styled nowhere, and leaves every
// other request to handler.
func markingUp(t *testing.T, handler http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != assistPath {
			handler(w, r)
			return
		}
		answer(http.StatusOK, markup(t, searchAsked(t, r)))(w, r)
	}
}

// searchAsked is the search a request that marks one up was sent.
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

// markup is an answer of the server that marks a search up: the search it read back, and the ranges of that
// text it gave a style to.
func markup(t *testing.T, query string, ranges ...string) string {
	t.Helper()
	echoed, err := json.Marshal(query)
	assert.NoError(t, err)
	return `{"$type":"SearchSuggestions","query":` + string(echoed) + `,"styleRanges":[` + strings.Join(ranges, ",") + `]}`
}

// styled is one range of a markup: where it begins and how far it runs, both in units of UTF-16, and the style
// the server gave it.
func styled(start, length int, style string) string {
	return fmt.Sprintf(`{"$type":"SearchStyleRange","start":%d,"length":%d,"style":%q}`, start, length, style)
}

// requireMarkedUpFirst holds a selection to the request it opens with: the search is marked up before anything
// is selected, once, and the words the markup is asked about are the words of the search itself.
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

// serveNothing stands for the network a refusal must not reach.
func serveNothing(t *testing.T) *upstream {
	t.Helper()
	return serve(t, func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a request reached the server", "%s %s", r.Method, r.URL)
	})
}

func answer(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// theHolderOfALink is the client a signed link is fetched by: whoever came by the link and nothing else. It
// carries no Authorization, keeps no cookie jar and follows no redirect, so what comes back is the answer the
// link itself was given rather than one a session of its own earned it.
func theHolderOfALink() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// fetched is the answer and the body the holder of a link is given at address. A scenario that names a bearer
// sends it as the Authorization of the request, which is how the link is asked about by a caller who holds a
// token as well: the signature is what is read, and a token neither opens the link nor stands in for it.
func fetched(t *testing.T, address, bearer string) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	require.NoError(t, err)
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := theHolderOfALink().Do(request)
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

// asks is the body of each request the server was sent, in order; a request that carries none stands empty.
func (u *upstream) asks() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	bodies := make([]string, 0, len(u.asked))
	for _, body := range u.asked {
		bodies = append(bodies, string(body))
	}
	return bodies
}

// lastAsk is the body of the request the server was sent last, which is what a scenario that ran several
// commands holds the last of them to without counting the requests of the ones before it.
func lastAsk(u *upstream) string {
	asked := u.asks()
	if len(asked) == 0 {
		return ""
	}
	return asked[len(asked)-1]
}

// answers is the body of each answer the server sent, in order.
func (u *upstream) answers() [][]byte {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.answered)
}

// sentFields is the fields= of each request the server was sent, unescaped.
func (u *upstream) sentFields() []string {
	var fields []string
	for _, request := range u.requests() {
		fields = append(fields, request.URL.Query().Get("fields"))
	}
	return fields
}

// sentPaths is the path of each request the server was sent, in order.
func (u *upstream) sentPaths() []string {
	var paths []string
	for _, request := range u.requests() {
		paths = append(paths, request.URL.Path)
	}
	return paths
}

// sentTargets is the path and query of each request the server was sent, unescaped: everything a name that was
// resolved without the server must be absent from.
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

// sentQueries is the query of each request the server was sent, unescaped.
func (u *upstream) sentQueries() []url.Values {
	var queries []url.Values
	for _, request := range u.requests() {
		queries = append(queries, request.URL.Query())
	}
	return queries
}

// Only auth login looks at stdin, so every scenario but its own hands Run none.
func runWith(t *testing.T, env []string, argv ...string) outcome {
	t.Helper()
	return runOn(t, nil, env, argv...)
}

// Only --version looks at the stamp of the build, so every scenario but its own hands Run none of that either.
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

// The kind of body an upload carries, and the one a comparison of it reads part by part.
const multipartForm = "multipart/form-data"

// One part of a multipart body: the field it stands under, the file name it carries, its content, and the
// header the first two were written into, which is where a name is held to the byte.
type formPart struct {
	field       string
	file        string
	content     string
	disposition string
}

// matchingMultipartByItsParts is the matcher a cassette of this package is replayed by: go-vcr's own, except
// over a request whose body is a form. multipart.Writer draws a boundary at random, so the Content-Type header
// and every line of the body differ between two sends of the very same file; what the request says is the
// parts themselves, and those are what the two are compared by. A boundary fixed for the tests would be a seam
// in the tool put there for them.
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

// partsSent is the parts of the request's own body, with the body left where the next reader expects it.
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

// sentParts is the multipart body of the request the server was sent at that place in its log, read part by
// part: what a scenario holds an upload to, since the boundary between the parts is drawn afresh every time.
func sentParts(t *testing.T, u *upstream, at int) []formPart {
	t.Helper()
	requests, asked := u.requests(), u.asks()
	require.Greater(t, len(requests), at, "the server was sent no request %d", at)
	parts, isForm := partsOf([]byte(asked[at]), requests[at].Header.Get("Content-Type"))
	require.True(t, isForm, "the body of %s %s is no multipart form: %q",
		requests[at].Method, requests[at].URL, asked[at])
	return parts
}
