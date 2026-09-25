package cli_test

import (
	"net/http"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const (
	addSourceFields = "id,idReadable,links(id,direction,linkType(id,sourceToTarget,targetToSource," +
		"localizedSourceToTarget,localizedTargetToSource))"
	addTargetFields = "id,idReadable"
)

const (
	needsLinkType = `{"$type":"IssueLinkType","id":"5-1","sourceToTarget":"is needed by","targetToSource":"needs",` +
		`"localizedSourceToTarget":null,"localizedTargetToSource":null}`
	tiesLinkType = `{"$type":"IssueLinkType","id":"5-2","sourceToTarget":"ties","targetToSource":"",` +
		`"localizedSourceToTarget":null,"localizedTargetToSource":null}`
	linkSourceRead = `{"$type":"Issue","id":"3-1","idReadable":"DEV-1","links":[` +
		`{"$type":"IssueLink","id":"5-1t","direction":"INWARD","linkType":` + needsLinkType + `},` +
		`{"$type":"IssueLink","id":"5-1s","direction":"OUTWARD","linkType":` + needsLinkType + `},` +
		`{"$type":"IssueLink","id":"5-2","direction":"BOTH","linkType":` + tiesLinkType + `}]}`
	linkTargetRead = `{"$type":"Issue","id":"3-2","idReadable":"DEV-2"}`
)

func addWriteFields(target string) string {
	return "id,links(direction,linkType(id),issues(id,links(direction," +
		"linkType(id,sourceToTarget,targetToSource),issuesSize,issues(id," + target + "))))"
}

func linkWrittenTo(sourceLinks string) string {
	return `{"$type":"Issue","id":"3-2","links":[{"$type":"IssueLink","direction":"OUTWARD","linkType":` + needsLinkType +
		`,"issues":[{"$type":"Issue","id":"3-1","links":[` + sourceLinks + `]}]}]}`
}

func linking(t *testing.T, write http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method != http.MethodGet:
			write(w, r)
		case path.Base(r.URL.Path) == "DEV-1":
			fake.JSON(http.StatusOK, linkSourceRead)(w, r)
		default:
			fake.JSON(http.StatusOK, linkTargetRead)(w, r)
		}
	})
}

func noLinkWritten(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "a write reached the server", "%s %s", r.Method, r.URL)
	}
}

func TestLinkAddRefusesAnEmptyPhrase(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "link", "add", "DEV-1", "", "DEV-2")

	assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
	assert.Empty(t, server.Requests())
}

func TestLinkAddWritesTheLinkAndPrintsTheLinksOfTheIssue(t *testing.T) {
	t.Parallel()
	server := linking(t, fake.JSON(http.StatusOK, linkWrittenTo(
		`{"$type":"IssueLink","direction":"INWARD","linkType":`+needsLinkType+`,"issuesSize":1,"issues":[`+
			`{"$type":"Issue","id":"3-2","idReadable":"DEV-2","summary":"Second"}]},`+
			`{"$type":"IssueLink","direction":"BOTH","linkType":`+tiesLinkType+`,"issuesSize":1,"issues":[`+
			`{"$type":"Issue","id":"3-4","idReadable":"DEV-4","summary":"Fourth"}]}`)))

	got := runWith(t, server.Env(), "link", "add", "DEV-1", "needs", "DEV-2")

	assert.Equal(t, outcome{stdout: "total: 2\nreturned: 2\ntruncated: false\nlinks:\n" +
		"  \"needs\":\n    - {idReadable: \"DEV-2\", summary: \"Second\"}\n" +
		"  \"ties\":\n    - {idReadable: \"DEV-4\", summary: \"Fourth\"}\n"}, got)
	assert.Equal(t, []string{
		"/api/issues/DEV-1?fields=" + addSourceFields,
		"/api/issues/DEV-2?fields=" + addTargetFields,
		"/api/issues/DEV-1/links/5-1t/issues?fields=" + addWriteFields(linkListTarget),
	}, server.Targets())
	assert.Equal(t, []string{"", "", `{"id":"3-2"}`}, server.Bodies())
	assert.Equal(t, http.MethodPost, server.Last(t).Method)
}

func TestLinkAddRefusesAPhraseNoLinkGoesBy(t *testing.T) {
	t.Parallel()
	server := linking(t, noLinkWritten(t))

	got := runWith(t, server.Env(), "link", "add", "DEV-1", "Neds", "DEV-2")

	assert.Equal(t, faultDocument{
		code: "unknown_name",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", addSourceFields)},
			{"issue", "DEV-1"},
			{"unknown", []any{[]detail{{"phrase", "Neds"}, {"nearest", []any{"needs"}}}}},
		},
	}, requireFault(t, got))
	assert.Equal(t, []string{"/api/issues/DEV-1"}, server.Paths())
}

func TestLinkAddRefusesLinkingAnIssueToItself(t *testing.T) {
	t.Parallel()
	server := linking(t, noLinkWritten(t))

	got := runWith(t, server.Env(), "link", "add", "DEV-1", "ties", "DEV-1")

	assert.Equal(t, faultDocument{
		code: "bad_usage",
		details: []detail{
			{"request", issueRequest(server.URL, "DEV-1", addTargetFields)},
			{"issue", "DEV-1"},
			{"target", "DEV-1"},
		},
	}, requireFault(t, got))
	assert.Equal(t, []string{"/api/issues/DEV-1", "/api/issues/DEV-1"}, server.Paths())
}

func TestLinkAddRefusesAnAnswerWithoutTheLink(t *testing.T) {
	t.Parallel()
	server := linking(t, fake.JSON(http.StatusOK, linkWrittenTo("")))

	got := runWith(t, server.Env(), "link", "add", "DEV-1", "needs", "DEV-2")

	assert.Equal(t, faultDocument{
		code: "upstream_invalid",
		details: []detail{
			{"request", "POST " + server.URL + "/api/issues/DEV-1/links/5-1t/issues?fields=" +
				addWriteFields(linkListTarget)},
			{"issue", "DEV-1"},
			{"phrase", "needs"},
			{"target", "DEV-2"},
		},
	}, requireUncertainty(t, got))
}
