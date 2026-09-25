package youtrack

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	yt "github.com/hakastein/youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

func transportFailure(err error) *diag.Fault {
	fault := &diag.Fault{Code: diag.UpstreamFailed, Message: err.Error()}
	var failed *url.Error
	if errors.As(err, &failed) {
		fault.Message = failed.Err.Error()
		method := strings.ToUpper(failed.Op)
		fault.Details = []render.Pair{requestDetail(method, failed.URL)}
	}
	return fault
}

func readFailure(response *http.Response, err error) *diag.Fault {
	return &diag.Fault{Code: diag.UpstreamFailed, Message: err.Error(), Details: responseDetails(response)}
}

func truncatedWriteResponse(response *http.Response, body []byte, err error) *diag.Fault {
	fault := readFailure(response, err)
	if statusRejectsWrite(response.StatusCode) {
		return fault
	}
	fault.Code = diag.WriteUncertain
	fault.Details = append(fault.Details, bodyDetail(body))
	return fault
}

func statusRejectsWrite(status int) bool {
	return !is2xx(status) && !is5xx(status)
}

func writeFailure(response *http.Response, body []byte) *diag.Fault {
	if response.StatusCode == http.StatusOK {
		return nil
	}
	tree, isJSON := decode(body)
	answeredByProxy := is5xx(response.StatusCode) && !isYouTrackError(tree)
	if answeredByProxy {
		message := fmt.Sprintf("something other than YouTrack answered the write with status %d", response.StatusCode)
		details := append(responseDetails(response), bodyDetail(body))
		return &diag.Fault{Code: diag.WriteUncertain, Message: message, Details: details}
	}
	if !isJSON && bodyMustBeJSON(response.StatusCode) {
		return markWritten(response, shapeFailure(response, body, notOneValue))
	}
	return markWritten(response, statusFailure(response, tree, body))
}

func isYouTrackError(tree any) bool {
	said, isObject := tree.(map[string]any)
	if !isObject {
		return false
	}
	_, named := said["error"].(string)
	return named
}

func markWritten(response *http.Response, fault *diag.Fault) *diag.Fault {
	fault.AfterWrite = is2xx(response.StatusCode)
	return fault
}

func is2xx(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func statusFailure(response *http.Response, tree any, body []byte) *diag.Fault {
	return statusFault(responseDetails(response), response.StatusCode, tree, body)
}

func statusFault(details []render.Pair, status int, tree any, body []byte) *diag.Fault {
	code, named := statusCode(status)
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
	message := fmt.Sprintf("the server answered with status %d", status)
	return &diag.Fault{Code: code, Message: message, Details: details}
}

func shapeFailure(response *http.Response, body []byte, message string) *diag.Fault {
	return shapeFault(responseDetails(response), body, message)
}

func shapeFault(details []render.Pair, body []byte, message string) *diag.Fault {
	return &diag.Fault{Code: diag.UpstreamInvalid, Message: message, Details: append(details, bodyDetail(body))}
}

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

func bodyMustBeJSON(status int) bool {
	return !is5xx(status)
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

func answerDetails(sent yt.Request, status int) []render.Pair {
	return []render.Pair{sentRequest(sent), {Key: "upstream_status", Value: intNode(status)}}
}

func sentRequest(sent yt.Request) render.Pair {
	return render.Pair{Key: requestKey, Value: render.NewString(sent.String())}
}

func moduleFailure(err error) *diag.Fault {
	var (
		argument  *yt.ArgumentError
		transport *yt.TransportError
		status    *yt.StatusError
		response  *yt.ResponseError
	)
	switch {
	case errors.As(err, &argument):
		return &diag.Fault{Code: diag.BadUsage, Message: argument.Error()}
	case errors.As(err, &transport):
		fault := &diag.Fault{Code: diag.UpstreamFailed, Message: transport.Err.Error()}
		if transport.Written {
			fault.Code = diag.WriteUncertain
		}
		if transport.Request.Method != "" {
			fault.Details = []render.Pair{sentRequest(transport.Request)}
		}
		return fault
	case errors.As(err, &status):
		tree, _ := decode(status.Body)
		return statusFault(answerDetails(status.Request, status.Status), status.Status, tree, status.Body)
	case errors.As(err, &response):
		return shapeFault(answerDetails(response.Request, response.Status), response.Body, response.Reason)
	}
	return &diag.Fault{Code: diag.UpstreamFailed, Message: err.Error()}
}

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

func readableQuery(query string) string {
	return strings.NewReplacer("%24", "$", "%28", "(", "%29", ")", "%2C", ",").Replace(query)
}
