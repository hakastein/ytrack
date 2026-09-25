package youtrack_test

import (
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/fake"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

// Under a zone of UTC a moment printed in the zone of the process reads as one printed in UTC.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("UTC+5", 5*60*60)
	os.Exit(m.Run())
}

const noMetadataCache = ""

func client(t *testing.T, server *fake.Server) *youtrack.Client {
	t.Helper()
	return youtrack.New(server.Address(t), fake.Token, noMetadataCache)
}

func callOn(t *testing.T, server *fake.Server) func(youtrack.Call, *diag.Fault) (*render.Node, *diag.Fault) {
	t.Helper()
	return func(call youtrack.Call, fault *diag.Fault) (*render.Node, *diag.Fault) {
		t.Helper()
		require.Nil(t, fault)
		return call(t.Context(), client(t, server))
	}
}

func faultOf(t *testing.T, fault *diag.Fault) diag.Fault {
	t.Helper()
	require.NotNil(t, fault, "the call did not fail")
	assert.NotEmpty(t, fault.Message)
	kept := *fault
	kept.Message = ""
	return kept
}

func requestTo(method string, server *fake.Server, target string) render.Pair {
	return render.Pair{Key: "request", Value: render.NewString(method + " " + server.URL + target)}
}

func lastRequest(t *testing.T, server *fake.Server) render.Pair {
	t.Helper()
	sent := server.Last(t)
	query, err := url.QueryUnescape(sent.URL.RawQuery)
	require.NoError(t, err)
	return requestTo(sent.Method, server, sent.URL.Path+"?"+query)
}

func unreadable(request render.Pair, body string) diag.Fault {
	return diag.Fault{Code: diag.UpstreamInvalid, Details: []render.Pair{
		request,
		{Key: "upstream_status", Value: render.NewNumber("200")},
		{Key: "upstream_body", Value: render.NewString(body)},
	}}
}

func mismatch(field string, expected, actual *render.Node) *render.Node {
	return render.NewMap(
		render.Pair{Key: "field", Value: render.NewString(field)},
		render.Pair{Key: "expected", Value: expected},
		render.Pair{Key: "actual", Value: actual})
}

func number(n int) *render.Node {
	return render.NewNumber(json.Number(strconv.Itoa(n)))
}

func texts(values ...string) *render.Node {
	items := make([]*render.Node, 0, len(values))
	for _, value := range values {
		items = append(items, render.NewString(value))
	}
	return render.NewList(items...)
}
