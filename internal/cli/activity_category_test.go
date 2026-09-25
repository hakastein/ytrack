package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

func TestActivityRefusesANameOfNoCategoryBeforeItAsksForAnything(t *testing.T) {
	t.Parallel()
	server := fake.ServeNothing(t)

	got := runWith(t, server.Env(), "activity", "list", activityIssue, "--category", "LinksCategry")

	want := faultDocument{
		code:    "unknown_name",
		details: []detail{{"unknown", []any{[]detail{{"category", "LinksCategry"}, {"nearest", []any{"LinksCategory"}}}}}},
	}
	assert.Equal(t, want, requireFault(t, got))
	assert.Empty(t, server.Requests())
}
