package cli_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func fetching(body string) map[string]string {
	return map[string]string{"run.js": lines(
		`exports.definition = { short: "Run", long: "Run it.", args: [{ name: "url", type: "string", usage: "url" }] };`,
		body,
	)}
}

const fetched = `exports.command = (url) => { const answer = fetch(url, { timeout: 5000, maxBytes: 64, redirect: "follow" }); ` +
	`return { status: answer.status, ok: answer.ok, statusText: answer.statusText, text: answer.text() }; };`

func TestScriptFetchesAnAnswerOfAnyStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{name: "a success", status: http.StatusOK, want: "status: 200\nok: true\nstatusText: \"OK\"\ntext: \"said\"\n"},
		{name: "a missing page", status: http.StatusNotFound,
			want: "status: 404\nok: false\nstatusText: \"Not Found\"\ntext: \"said\"\n"},
		{name: "a failure of the server", status: http.StatusBadGateway,
			want: "status: 502\nok: false\nstatusText: \"Bad Gateway\"\ntext: \"said\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := fake.Serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("said"))
			})

			got := runScripts(t, fake.ServeNothing(t), fetching(fetched), "run", target.URL+"/page")

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestScriptReadsTheBodyAndHeadersOfAFetch(t *testing.T) {
	t.Parallel()
	target := fake.Serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("X-Kind", "one")
		w.Header().Add("X-Kind", "two")
		_, _ = w.Write([]byte(`{"id":"DEV-1","list":[1,2]}`))
	})
	body := `exports.command = (url) => { const answer = fetch(url, { timeout: 5000, maxBytes: 64, redirect: "follow" }); ` +
		`const bytes = answer.arrayBuffer(); const seen = []; answer.headers.forEach((value, name) => seen.push(name)); ` +
		`return { json: answer.json(), bytes: bytes.byteLength, first: new Uint8Array(bytes)[0], ` +
		`buffer: Buffer.from(answer.arrayBuffer()).toString("utf8").length, type: answer.headers.get("content-type"), ` +
		`kind: answer.headers.get("X-KIND"), absent: answer.headers.get("x-none"), has: answer.headers.has("x-kind"), ` +
		`seen: seen.filter((name) => name.startsWith("x-") || name === "content-type") }; };`

	got := runScripts(t, fake.ServeNothing(t), fetching(body), "run", target.URL)

	want := "json:\n  id: \"DEV-1\"\n  list:\n    - 1\n    - 2\nbytes: 27\nfirst: 123\nbuffer: 27\n" +
		"type: \"application/json\"\nkind: \"one, two\"\nabsent: null\nhas: true\nseen:\n  - \"content-type\"\n  - \"x-kind\"\n"
	assert.Equal(t, outcome{stdout: want}, got)
}

func TestScriptFollowsARedirectOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		redirect string
		want     string
	}{
		{name: "followed", redirect: "follow", want: "status: 200\nredirected: true\nto: \"/to\"\nlocation: null\n"},
		{name: "answered as it is", redirect: "manual", want: "status: 302\nredirected: false\nto: \"/from\"\nlocation: \"to\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/from" {
					w.Header().Set("Location", "to")
					w.WriteHeader(http.StatusFound)
				}
			})
			body := `exports.command = (url) => { const answer = fetch(url, { timeout: 5000, maxBytes: 64, redirect: "` +
				tc.redirect + `" }); return { status: answer.status, redirected: answer.redirected, ` +
				`to: answer.url.slice(answer.url.lastIndexOf("/")), location: answer.headers.get("location") }; };`

			got := runScripts(t, fake.ServeNothing(t), fetching(body), "run", target.URL+"/from")

			assert.Equal(t, outcome{stdout: tc.want}, got)
		})
	}
}

func TestFetchThatGetsNoWholeAnswerIsUpstreamFailed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		options string
		target  func(t *testing.T) string
	}{
		{name: "a body longer than maxBytes", options: `{ timeout: 5000, maxBytes: 3, redirect: "follow" }`,
			target: func(t *testing.T) string {
				return fake.Serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("four")) }).URL
			}},
		{name: "an answer slower than the timeout", options: `{ timeout: 50, maxBytes: 64, redirect: "follow" }`,
			target: func(t *testing.T) string {
				return fake.Serve(t, func(_ http.ResponseWriter, r *http.Request) {
					select {
					case <-r.Context().Done():
					case <-time.After(5 * time.Second):
					}
				}).URL
			}},
		{name: "more than ten redirects", options: `{ timeout: 5000, maxBytes: 64, redirect: "follow" }`,
			target: func(t *testing.T) string {
				return fake.Serve(t, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Location", "again")
					w.WriteHeader(http.StatusFound)
				}).URL + "/loop"
			}},
		{name: "a server nobody serves", options: `{ timeout: 5000, maxBytes: 64, redirect: "follow" }`,
			target: func(*testing.T) string { return fake.NobodyListens }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `exports.command = (url) => fetch(url, ` + tc.options + `);`

			got := runScripts(t, fake.ServeNothing(t), fetching(body), "run", tc.target(t))

			found := requireFault(t, got)
			assert.Equal(t, "upstream_failed", found.code)
			assert.True(t, strings.HasPrefix(detailNamed(t, found, "url").(string), "http://"))
		})
	}
}

func TestFetchSendsNoTokenEvenToTheInstance(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "kept"})
		_, _ = w.Write([]byte("said"))
	})
	body := `const { address } = require("ytrack/v1");` + "\n" +
		`exports.command = () => { const options = { timeout: 5000, maxBytes: 64, redirect: "follow" }; ` +
		`fetch(address + "/one", options); fetch(address + "/two", options); return { done: true }; };`

	got := runWith(t, atHome(server, scriptsHome(t, fetching(body))), "run", "unused")

	assert.Equal(t, outcome{stdout: "done: true\n"}, got)
	for _, sent := range server.Requests() {
		assert.Empty(t, sent.Header.Values("Authorization"))
		assert.Empty(t, sent.Header.Values("Cookie"))
	}
	assert.Len(t, server.Requests(), 2)
}

func TestScriptCallingFetchWrongIsScriptFailed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call string
	}{
		{name: "no options", call: `fetch(url)`},
		{name: "no timeout", call: `fetch(url, { maxBytes: 64, redirect: "follow" })`},
		{name: "no maxBytes", call: `fetch(url, { timeout: 5000, redirect: "follow" })`},
		{name: "no redirect", call: `fetch(url, { timeout: 5000, maxBytes: 64 })`},
		{name: "a timeout of no time", call: `fetch(url, { timeout: 0, maxBytes: 64, redirect: "follow" })`},
		{name: "a negative maxBytes", call: `fetch(url, { timeout: 5000, maxBytes: -1, redirect: "follow" })`},
		{name: "a redirect fetch of ytrack does not take", call: `fetch(url, { timeout: 5000, maxBytes: 64, redirect: "error" })`},
		{name: "a method other than GET", call: `fetch(url, { method: "POST", timeout: 5000, maxBytes: 64, redirect: "follow" })`},
		{name: "headers of the caller", call: `fetch(url, { headers: { A: "b" }, timeout: 5000, maxBytes: 64, redirect: "follow" })`},
		{name: "a url of another scheme", call: `fetch("ftp://example.com/file", { timeout: 5000, maxBytes: 64, redirect: "follow" })`},
		{name: "a url that does not parse", call: `fetch("http://[::1", { timeout: 5000, maxBytes: 64, redirect: "follow" })`},
		{name: "a url that is no string", call: `fetch(1, { timeout: 5000, maxBytes: 64, redirect: "follow" })`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := fake.ServeNothing(t)

			got := runScripts(t, fake.ServeNothing(t), fetching(`exports.command = (url) => { `+tc.call+`; return {}; };`),
				"run", target.URL)

			assert.Equal(t, "script_failed", requireFault(t, got).code)
		})
	}
}
