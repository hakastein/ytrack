package cli_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

func breakOff(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func TestAWriteExitsWith2WhereItMayHaveHappened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write http.HandlerFunc
		argv  []string
		code  string
	}{
		{
			name:  "the answer to the write never came",
			write: breakOff,
			argv:  []string{"time", "create", "DEV-1", "PT1H"},
			code:  "write_uncertain",
		},
		{
			name:  "the answer disagrees with the write",
			write: fake.JSON(http.StatusOK, answeredWorkItem{duration: `{"$type":"DurationValue","minutes":60}`}.json()),
			argv:  []string{"time", "create", "DEV-1", "PT1H30M"},
			code:  "upstream_invalid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := writingTimeAgainstTheSettings(t, tc.write)

			got := runWith(t, envOf(server), tc.argv...)

			assert.Equal(t, tc.code, requireUncertainty(t, got).code)
		})
	}
}
