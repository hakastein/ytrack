package cli_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/ytrack/internal/fake"
)

const bogusToken = "perm-bogus"

func meRequest(address string) string {
	return "GET " + address + "/api/users/me?fields=login,fullName"
}

func TestNoCommandNamesWhereTheTokenTheServerRefusedCameFrom(t *testing.T) {
	t.Parallel()
	stated, scope := here(t)
	tests := []struct {
		name            string
		status          int
		upstreamError   string
		upstreamMessage string
		where           func(t *testing.T, address string) (env []string, from detail)
	}{
		{
			name:            "a token of the environment the server does not know",
			status:          http.StatusUnauthorized,
			upstreamError:   "Unauthorized",
			upstreamMessage: "Invalid token",
			where: func(_ *testing.T, address string) ([]string, detail) {
				return []string{"YTRACK_URL=" + address, "YTRACK_TOKEN=" + bogusToken}, authFromEnv()
			},
		},
		{
			name:            "a token of the global record",
			status:          http.StatusForbidden,
			upstreamError:   "Forbidden",
			upstreamMessage: "Access to the project is denied",
			where: func(t *testing.T, address string) ([]string, detail) {
				home, _ := homeWith(t, globalRecord(address, bogusToken))
				return []string{"HOME=" + home}, authFromSettings()
			},
		},
		{
			name:            "a token of the record for the directory of the call",
			status:          http.StatusForbidden,
			upstreamError:   "Forbidden",
			upstreamMessage: "Access to the project is denied",
			where: func(t *testing.T, address string) ([]string, detail) {
				home, _ := homeWith(t, recordFile(scopedRecord(scope, address, bogusToken)))
				return []string{"HOME=" + home, "PWD=" + stated}, authFromSettings()
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`{"error":%q,"error_description":%q}`, tc.upstreamError, tc.upstreamMessage)
			server := fake.Serve(t, fake.JSON(tc.status, body))
			env, from := tc.where(t, server.URL)

			got := runWith(t, env, "project", "show", "DEV")

			want := faultDocument{
				code: "denied",
				details: []detail{
					{"request", showRequest(server.URL, "DEV")},
					{"upstream_status", tc.status},
					{"upstream_error", tc.upstreamError},
					{"upstream_message", tc.upstreamMessage},
					from,
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assertNoToken(t, got, bogusToken)
			assert.Len(t, server.Requests(), 1)
		})
	}
}
