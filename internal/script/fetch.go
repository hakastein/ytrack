package script

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
)

const maxRedirects = 10

type fetchOptions struct {
	timeout  time.Duration
	maxBytes int
	follow   bool
}

// fetch of Node returns a promise; here the answer, since a script runs with no event loop.
func (e *engine) fetch(call goja.FunctionCall) goja.Value {
	if !goja.IsString(call.Argument(0)) {
		panic(e.vm.NewTypeError("fetch takes its url as a string"))
	}
	target, err := url.Parse(call.Argument(0).String())
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		panic(e.vm.NewTypeError(fmt.Sprintf("fetch takes an absolute http or https url, not %q", call.Argument(0).String())))
	}
	options := e.fetchOptions(call.Argument(1))
	ctx, cancel := context.WithTimeout(e.ctx, options.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		panic(e.vm.NewTypeError(err.Error()))
	}
	// Proxies of the environment are not taken: a script reads no environment. With no connection kept, the transport
	// has none to send the request again on.
	transport := &http.Transport{ForceAttemptHTTP2: true, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if !options.follow {
			return http.ErrUseLastResponse
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	}}
	answer, err := client.Do(request)
	if err != nil {
		panic(e.throw(e.fetchFault(ctx, target, options.timeout, err)))
	}
	defer answer.Body.Close()
	body, err := io.ReadAll(io.LimitReader(answer.Body, int64(options.maxBytes)+1))
	if err != nil {
		panic(e.throw(e.fetchFault(ctx, target, options.timeout, err)))
	}
	if len(body) > options.maxBytes {
		panic(e.throw(e.fetchFault(ctx, target, options.timeout, fmt.Errorf("the body is longer than maxBytes %d", options.maxBytes))))
	}
	return e.response(answer, request, body)
}

func (e *engine) fetchOptions(value goja.Value) fetchOptions {
	const takes = "{ timeout, maxBytes, redirect, method? }"
	given, isObject := value.(*goja.Object)
	if !isObject || given.ClassName() != "Object" {
		panic(e.vm.NewTypeError("fetch takes its options as an object: " + takes))
	}
	for _, key := range given.Keys() {
		if !slices.Contains([]string{"timeout", "maxBytes", "redirect", "method"}, key) {
			panic(e.vm.NewTypeError(fmt.Sprintf("fetch of ytrack takes no option %q: it takes %s", key, takes)))
		}
	}
	if method := given.Get("method"); method != nil && !goja.IsUndefined(method) &&
		(!goja.IsString(method) || !strings.EqualFold(method.String(), http.MethodGet)) {
		panic(e.vm.NewTypeError("fetch of ytrack sends only GET"))
	}
	timeout := e.fetchWhole(given, "timeout", 1)
	maxBytes := e.fetchWhole(given, "maxBytes", 0)
	redirect := given.Get("redirect")
	if redirect == nil || !goja.IsString(redirect) || (redirect.String() != "follow" && redirect.String() != "manual") {
		panic(e.vm.NewTypeError(`fetch takes the option redirect: "follow" or "manual"`))
	}
	return fetchOptions{timeout: time.Duration(timeout) * time.Millisecond, maxBytes: maxBytes,
		follow: redirect.String() == "follow"}
}

func (e *engine) fetchWhole(given *goja.Object, key string, least int) int {
	value := given.Get(key)
	if value == nil || goja.IsUndefined(value) {
		panic(e.vm.NewTypeError(fmt.Sprintf("fetch takes the option %s, which has no default", key)))
	}
	read, reason := readWhole(value)
	if reason == "" && read.(int) < least {
		reason = fmt.Sprintf("is below %d", least)
	}
	if reason != "" {
		panic(e.vm.NewTypeError(fmt.Sprintf("fetch: the option %s %s", key, reason)))
	}
	return read.(int)
}

func (e *engine) fetchFault(ctx context.Context, target *url.URL, timeout time.Duration, err error) *diag.Fault {
	message := fmt.Sprintf("GET %s: %v", target.Redacted(), unwrapURL(err))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && e.ctx.Err() == nil {
		message = fmt.Sprintf("GET %s: no whole answer within the timeout of %d ms", target.Redacted(),
			timeout.Milliseconds())
	}
	return &diag.Fault{Code: youtrack.CodeUpstreamFailed, Message: message,
		Details: []youtrack.Pair{{Key: "url", Value: youtrack.NewString(target.Redacted())}}}
}

func unwrapURL(err error) error {
	var failed *url.Error
	if errors.As(err, &failed) {
		return failed.Err
	}
	return err
}

func (e *engine) response(answer *http.Response, request *http.Request, body []byte) *goja.Object {
	response := e.vm.NewObject()
	e.set(response,
		property{"status", answer.StatusCode},
		property{"ok", answer.StatusCode >= 200 && answer.StatusCode < 300},
		property{"statusText", strings.TrimPrefix(answer.Status, strconv.Itoa(answer.StatusCode)+" ")},
		property{"url", answer.Request.URL.String()},
		property{"redirected", answer.Request != request},
		property{"headers", e.headers(answer.Header)},
		property{"text", func() string { return text(body) }},
		property{"json", func() goja.Value {
			parsed, err := e.parseJSON(goja.Undefined(), e.vm.ToValue(text(body)))
			if err != nil {
				panic(err)
			}
			return parsed
		}},
		property{"arrayBuffer", func() goja.ArrayBuffer { return e.vm.NewArrayBuffer(slices.Clone(body)) }},
	)
	return response
}

// A body not in UTF-8 reads with U+FFFD in place of what does not decode, and without a BOM, as in fetch.
func text(body []byte) string {
	return strings.TrimPrefix(strings.ToValidUTF8(string(body), "\ufffd"), "\ufeff")
}

func (e *engine) headers(header http.Header) *goja.Object {
	names := make([]string, 0, len(header))
	joined := map[string]string{}
	for name, values := range header {
		lower := strings.ToLower(name)
		names = append(names, lower)
		joined[lower] = strings.Join(values, ", ")
	}
	slices.Sort(names)
	entries := func() [][2]string {
		pairs := make([][2]string, 0, len(names))
		for _, name := range names {
			pairs = append(pairs, [2]string{name, joined[name]})
		}
		return pairs
	}
	headers := e.vm.NewObject()
	e.set(headers,
		property{"get", func(name string) goja.Value {
			value, held := joined[strings.ToLower(name)]
			if !held {
				return goja.Null()
			}
			return e.vm.ToValue(value)
		}},
		property{"has", func(name string) bool {
			_, held := joined[strings.ToLower(name)]
			return held
		}},
		property{"getSetCookie", func() []string { return slices.Clone(header.Values("Set-Cookie")) }},
		property{"keys", func() []string { return slices.Clone(names) }},
		property{"values", func() []string {
			held := make([]string, 0, len(names))
			for _, name := range names {
				held = append(held, joined[name])
			}
			return held
		}},
		property{"entries", entries},
		property{"forEach", func(each goja.Callable) {
			for _, pair := range entries() {
				if _, err := each(goja.Undefined(), e.vm.ToValue(pair[1]), e.vm.ToValue(pair[0]), headers); err != nil {
					panic(err)
				}
			}
		}},
	)
	return headers
}
