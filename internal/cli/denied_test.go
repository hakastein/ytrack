package cli_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

const bogusToken = "perm-bogus"

func TestNoCommandNamesWhereTheTokenTheServerRefusedCameFrom(t *testing.T) {
	t.Parallel()
	scope := here(t)
	tests := []struct {
		name            string
		status          int
		upstreamError   string
		upstreamMessage string
		where           func(t *testing.T, address string) (env []string, from string)
	}{
		{
			name:            "a token of the environment the server does not know",
			status:          http.StatusUnauthorized,
			upstreamError:   "Unauthorized",
			upstreamMessage: "Invalid token",
			where: func(_ *testing.T, address string) ([]string, string) {
				return []string{"YTRACK_URL=" + address, "YTRACK_TOKEN=" + bogusToken}, "environment"
			},
		},
		{
			name:            "a token of the global record",
			status:          http.StatusForbidden,
			upstreamError:   "Forbidden",
			upstreamMessage: "Access to the project is denied",
			where: func(t *testing.T, address string) ([]string, string) {
				home, _ := homeWith(t, globalRecord(address, bogusToken))
				return []string{"HOME=" + home}, "settings"
			},
		},
		{
			name:            "a token of the record for the directory of the call",
			status:          http.StatusForbidden,
			upstreamError:   "Forbidden",
			upstreamMessage: "Access to the project is denied",
			where: func(t *testing.T, address string) ([]string, string) {
				home, _ := homeWith(t, recordFile(scopedRecord(scope, address, bogusToken)))
				return []string{"HOME=" + home}, "settings"
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`{"error":%q,"error_description":%q}`, tc.upstreamError, tc.upstreamMessage)
			server := fake.Serve(t, fake.JSON(tc.status, body))
			env, from := tc.where(t, server.URL)

			got := runWith(t, env, showDEV...)

			found := requireFault(t, got)
			assert.Equal(t, "denied", found.code)
			assert.Equal(t, from, detailNamed(t, found, "auth_from"))
			assertNoToken(t, got, bogusToken)
			assert.Equal(t, []string{"/api/admin/projects/DEV"}, server.Paths())
		})
	}
}
