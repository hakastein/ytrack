package youtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const jsonContentType = "application/json"

// searchBody is the body of a read that asks its question in JSON rather than in the query of a URL. The text
// is encoded as a JSON value rather than spliced into one, so a quote or a backslash of the caller's reaches
// the server as they wrote it; encoding a string is the one marshalling that cannot fail.
func searchBody(query string) []byte {
	body, _ := json.Marshal(struct {
		Query string `json:"query"`
	}{Query: query})
	return body
}

type decodedResponse struct {
	objects      []map[string]any
	httpResponse *http.Response
	body         []byte
	// The schema of the specification the objects stand at, "" where the call named none.
	schema string
	// The catalogue the command was built with, so that reading the objects needs no second one.
	schemas *schemas
	// The address the client was built from. An address YouTrack sends is a path of its own instance and no use
	// to anyone not holding that address, so printing one takes both.
	address *url.URL
}

func (c *Client) read(ctx context.Context, spec *schemas, responseSchema string, requested []requestedField, call func(ctx context.Context, fields string) (*http.Response, error)) ([]*render.Node, *diag.Fault) {
	decoded, fault := c.request(ctx, spec, responseSchema, requested, call)
	if fault != nil {
		return nil, fault
	}
	return newConverter(decoded, blockLayout).objectsAt(decoded.schema, requested, decoded.objects)
}

// The two trees of one request: what goes out, which may hold names the tool fills in itself, and what a record
// of the answer prints, which is what the caller asked for.
type requestFields struct {
	sent   []requestedField
	output []requestedField
}

// readList is pass for the readList of a list, and it settles that one record is one line of the document.
func (c *Client) readList(ctx context.Context, spec *schemas, responseSchema string, of requestFields, call func(ctx context.Context, fields string) (*http.Response, error)) ([]*render.Node, *diag.Fault) {
	decoded, fault := c.request(ctx, spec, responseSchema, of.sent, call)
	if fault != nil {
		return nil, fault
	}
	return newConverter(decoded, inlineLayout).objectsAt(decoded.schema, of.output, decoded.objects)
}

func writeEmpty(ctx context.Context, call func(ctx context.Context) (*http.Response, error)) *diag.Fault {
	response, fault := send(ctx, call)
	if fault != nil {
		return fault
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return truncatedWriteResponse(response, body, err)
	}
	if fault := writeFailure(response, body); fault != nil {
		return fault
	}
	if len(body) > 0 {
		message := "the answer carries a body, and this call is answered with none"
		return markWritten(response, shapeFailure(response, body, message))
	}
	return nil
}

// write is pass for a write the server answers with the entity it wrote. The check of that answer and the
// document it becomes are both parameters rather than steps the caller runs afterwards: ADR-0005 puts them on
// the way back from a write, and here there is no way back around them.
//
// The answer never leaves this function, which is what makes the mark below impossible to forget: printing is
// a refusal of its own — an instant of the wrong kind, a block of custom fields or of links of the wrong shape
// — and a caller handed the answer to print would raise that one unmarked.
//
// Every refusal from the status onwards is marked as following a write the server carried out, so the exit
// code says the instance changed without the document being read.
func (c *Client) write(ctx context.Context, spec *schemas, responseSchema string, requested []requestedField, call func(ctx context.Context, fields string) (*http.Response, error), confirm func(decodedResponse) *diag.Fault, output func(decodedResponse) (*render.Node, *diag.Fault)) (*render.Node, *diag.Fault) {
	response, fault := send(ctx, func(ctx context.Context) (*http.Response, error) {
		return call(ctx, formatFields(requested))
	})
	if fault != nil {
		return nil, fault
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, truncatedWriteResponse(response, body, err)
	}
	if fault := writeFailure(response, body); fault != nil {
		return nil, fault
	}
	tree, isJSON := decode(body)
	if !isJSON {
		return nil, markWritten(response, shapeFailure(response, body, notOneValue))
	}
	decoded, fault := c.validateResponse(spec, responseSchema, requested, response, body, tree)
	if fault != nil {
		return nil, markWritten(response, fault)
	}
	if fault := confirm(decoded); fault != nil {
		return nil, markWritten(response, fault)
	}
	node, fault := output(decoded)
	if fault != nil {
		return nil, markWritten(response, fault)
	}
	return node, nil
}

// send is where a write that never left is told from one that left with no answer coming back, and the
// border is whether the request went out whole rather than what the error says: net/http draws it in the same
// place to decide whether a request may be sent again, by a nothingWrittenError it does not export, so from
// outside it is visible only through httptrace. WroteRequest runs once the body and the final flush are
// through, and a request cut short before that is one no server acts on: its Content-Length does not add up.
func send(ctx context.Context, call func(ctx context.Context) (*http.Response, error)) (*http.Response, *diag.Fault) {
	// Written by the goroutine of the transport, which outlives a call that failed.
	var left atomic.Bool
	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(wrote httptrace.WroteRequestInfo) {
			if wrote.Err == nil {
				left.Store(true)
			}
		},
	})
	response, err := call(traced)
	switch {
	case err == nil:
		return response, nil
	case left.Load():
		return nil, uncertainWrite(err)
	}
	return nil, transportFailure(err)
}

// request is pass for a command that needs the values of the answer as well: it asks for names of its own, and
// what it prints is not one node per object.
func (c *Client) request(ctx context.Context, spec *schemas, responseSchema string, requested []requestedField, call func(ctx context.Context, fields string) (*http.Response, error)) (decodedResponse, *diag.Fault) {
	response, err := call(ctx, formatFields(requested))
	if err != nil {
		return decodedResponse{}, transportFailure(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return decodedResponse{}, readFailure(response, err)
	}
	tree, isJSON := decode(body)
	// Only a 5xx keeps its code over a body that is not JSON: it already says the server failed,
	// while a 200 or a 404 over such a body may be a login page's or a proxy's.
	if !isJSON && !is5xx(response.StatusCode) {
		return decodedResponse{}, shapeFailure(response, body, notOneValue)
	}
	if response.StatusCode != http.StatusOK {
		return decodedResponse{}, statusFailure(response, tree, body)
	}
	return c.validateResponse(spec, responseSchema, requested, response, body, tree)
}

const notOneValue = "the answer is not one JSON value"

// validateResponse is what a decoded body under a 200 becomes once it stands where the call said it would and carries
// every name that was asked of it.
func (c *Client) validateResponse(spec *schemas, responseSchema string, requested []requestedField, response *http.Response, body []byte, tree any) (decodedResponse, *diag.Fault) {
	expected := parseTypeRef(responseSchema)
	objects, ok := decodeObjects(tree, expected.list)
	switch {
	case !ok && expected.list:
		return decodedResponse{}, shapeFailure(response, body, "the answer is not a JSON array of objects")
	case !ok:
		return decodedResponse{}, shapeFailure(response, body, "the answer is not a JSON object")
	}
	if fault := checkMissingFields(spec, response, expected.schema, requested, tree); fault != nil {
		return decodedResponse{}, fault
	}
	return decodedResponse{objects: objects, httpResponse: response, body: body, schema: expected.schema, schemas: spec, address: c.address}, nil
}

func decodeObjects(tree any, isList bool) ([]map[string]any, bool) {
	items := []any{tree}
	if isList {
		list, ok := tree.([]any)
		if !ok {
			return nil, false
		}
		items = list
	}
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		objects = append(objects, object)
	}
	return objects, true
}

// JSON allows only space, tab, LF and CR around a value, so any other byte after it is a tail.
func decode(body []byte) (tree any, isJSON bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&tree) != nil || len(bytes.TrimLeft(body[decoder.InputOffset():], " \t\r\n")) > 0 {
		return nil, false
	}
	return tree, true
}
