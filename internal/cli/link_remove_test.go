package cli_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const removedLinkPath = "/api/issues/DEV-1/links/5-1t/issues/3-2"

func removalNames() []detail {
	return []detail{{"issue", "DEV-1"}, {"phrase", "needs"}, {"target", "DEV-2"}}
}

func TestLinkRemoveTakesTheLinkAwayBySlotAndInternalID(t *testing.T) {
	t.Parallel()
	server := linking(t, deletionDone())

	got := runWith(t, server.Env(), "link", "remove", "DEV-1", "needs", "DEV-2")

	assert.Equal(t, outcome{stdout: "idReadable: \"DEV-1\"\nremoved:\n  \"needs\":\n    - {idReadable: \"DEV-2\"}\n"}, got)
	assert.Equal(t, []string{http.MethodGet, http.MethodGet, http.MethodDelete}, server.Methods())
	assert.Equal(t, []string{
		"/api/issues/DEV-1?fields=" + addSourceFields,
		"/api/issues/DEV-2?fields=" + addTargetFields,
		removedLinkPath + "?",
	}, server.Targets(t))
	assert.Equal(t, []string{"", "", ""}, server.Bodies(), "no request of a removal carries a body")
}

func TestLinkRemoveRefusesALinkTheIssueDoesNotHave(t *testing.T) {
	t.Parallel()
	server := linking(t, fake.JSON(http.StatusNotFound, entityNotFound("3-2")))

	got := runWith(t, server.Env(), "link", "remove", "DEV-1", "needs", "DEV-2")

	assert.Equal(t, faultDocument{
		code: "not_found",
		details: append([]detail{{"request", "DELETE " + server.URL + removedLinkPath}},
			append(removalNames(),
				detail{"upstream_status", 404},
				detail{"upstream_error", "Not Found"},
				detail{"upstream_message", "Entity with id 3-2 not found"})...),
	}, requireFault(t, got))
}
