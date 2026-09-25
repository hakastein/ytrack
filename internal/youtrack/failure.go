package youtrack

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// A read whose answer did not arrive changed nothing, so it is upstream_failed, not write_uncertain.
func transportFailure(err error) *diag.Fault {
	fault := &diag.Fault{Code: diag.UpstreamFailed, Message: err.Error()}
	var failed *url.Error
	if errors.As(err, &failed) {
		fault.Message = failed.Err.Error()
		// net/http names the operation after the method in title case: Get for GET.
		fault.Details = []render.Pair{requestDetail(strings.ToUpper(failed.Op), failed.URL)}
	}
	return fault
}

// The status arrived and the body broke off, so the status is named.
func readFailure(response *http.Response, err error) *diag.Fault {
	return &diag.Fault{Code: diag.UpstreamFailed, Message: err.Error(), Details: responseDetails(response)}
}

// A write whose request left whole and whose answer never came. Whether it happened is nobody's to say: ytrack
// neither sends it again, which would write twice, nor reads the entity back, which would answer about a write
// that may still be under way.
func uncertainWrite(err error) *diag.Fault {
	fault := transportFailure(err)
	fault.Code = diag.WriteUncertain
	return fault
}

// The status of a write arrived and the rest of the answer did not. Under a 2xx or a 5xx the server had taken
// the request by the time it answered, so what became of the write is unknown; under any other status it said
// it refused the write, and an answer cut short does not unsay that.
func truncatedWriteResponse(response *http.Response, body []byte, err error) *diag.Fault {
	fault := readFailure(response, err)
	if !is2xx(response.StatusCode) && !is5xx(response.StatusCode) {
		return fault
	}
	fault.Code = diag.WriteUncertain
	fault.Details = append(fault.Details, bodyDetail(body))
	return fault
}

// writeFailure is what a write makes of the status that came back, and nil where the server answered 200. A
// 5xx that is not YouTrack's own word about a failure was written by something between ytrack and YouTrack,
// which may well have passed the request on, so what happened is unknown; every other status is read as a
// status is read everywhere.
func writeFailure(response *http.Response, body []byte) *diag.Fault {
	if response.StatusCode == http.StatusOK {
		return nil
	}
	tree, isJSON := decode(body)
	if is5xx(response.StatusCode) && !isYouTrackError(tree) {
		message := fmt.Sprintf("something other than YouTrack answered the write with status %d", response.StatusCode)
		details := append(responseDetails(response), bodyDetail(body))
		return &diag.Fault{Code: diag.WriteUncertain, Message: message, Details: details}
	}
	// Only a 5xx keeps its code over a body that is not JSON, as everywhere else a status is read.
	if !isJSON && !is5xx(response.StatusCode) {
		return markWritten(response, shapeFailure(response, body, notOneValue))
	}
	return markWritten(response, statusFailure(response, tree, body))
}

// YouTrack's own word about a failure: a JSON object carrying a string error, the shape every refusal of the
// API arrives in.
func isYouTrackError(tree any) bool {
	said, isObject := tree.(map[string]any)
	if !isObject {
		return false
	}
	_, named := said["error"].(string)
	return named
}

// A refusal about a write the server answered 2xx: it took the request and carried it out, so the instance
// changed however the refusal reads, and the exit code says so without the document being read.
func markWritten(response *http.Response, fault *diag.Fault) *diag.Fault {
	fault.AfterWrite = is2xx(response.StatusCode)
	return fault
}

func is2xx(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

// What the server said about a status other than 200 passes on verbatim. The raw body goes too for a status ADR-0005
// does not name, for a body that is no JSON object and for one holding more than string error and error_description.
func statusFailure(response *http.Response, tree any, body []byte) *diag.Fault {
	code, named := statusCode(response.StatusCode)
	details := responseDetails(response)
	said, _ := tree.(map[string]any)
	carried := 0
	for _, member := range []struct{ name, key string }{{"error", "upstream_error"}, {"error_description", "upstream_message"}} {
		if text, ok := said[member.name].(string); ok {
			details = append(details, render.Pair{Key: member.key, Value: render.NewString(text)})
			carried++
		}
	}
	if !named || said == nil || len(said) > carried {
		details = append(details, bodyDetail(body))
	}
	message := fmt.Sprintf("the server answered with status %d", response.StatusCode)
	return &diag.Fault{Code: code, Message: message, Details: details}
}

func shapeFailure(response *http.Response, body []byte, message string) *diag.Fault {
	details := append(responseDetails(response), bodyDetail(body))
	return &diag.Fault{Code: diag.UpstreamInvalid, Message: message, Details: details}
}

// ADR-0005's table. A status it does not name is upstream_failed with the body attached, never the
// nearest plausible code.
func statusCode(status int) (code diag.Code, named bool) {
	switch {
	case status == http.StatusBadRequest:
		return diag.Rejected, true
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return diag.Denied, true
	case status == http.StatusNotFound:
		return diag.NotFound, true
	case is5xx(status):
		return diag.UpstreamFailed, true
	}
	return diag.UpstreamFailed, false
}

func is5xx(status int) bool {
	return status >= 500 && status < 600
}

func bodyDetail(body []byte) render.Pair {
	return render.Pair{Key: "upstream_body", Value: render.NewString(string(body))}
}

func responseDetails(response *http.Response) []render.Pair {
	return []render.Pair{
		requestDetail(response.Request.Method, response.Request.URL.Redacted()),
		{Key: "upstream_status", Value: intNode(response.StatusCode)},
	}
}

// The address is the one that went out: an escape in the path or in the text searched for stands as the server
// received it, so sending the printed address again reaches what ytrack reached.
func requestDetail(method, address string) render.Pair {
	if path, query, split := strings.Cut(address, "?"); split {
		address = path + "?" + readableQuery(query)
	}
	return render.Pair{Key: requestKey, Value: render.NewString(method + " " + address)}
}

const requestKey = "request"

func insertAfterRequest(details []render.Pair, own ...render.Pair) []render.Pair {
	at := slices.IndexFunc(details, func(pair render.Pair) bool { return pair.Key == requestKey })
	return slices.Insert(slices.Clone(details), at+1, own...)
}

// url.Values escapes the "," "(" ")" and "$" a fields= expression is written with, although a query carries all
// four literally and no parser of one reads them as a delimiter. Unwrapping just these four keeps the expression
// readable while a space stays the "+" it went as and "&" and "#" stay escaped.
func readableQuery(query string) string {
	return strings.NewReplacer("%24", "$", "%28", "(", "%29", ")", "%2C", ",").Replace(query)
}
