package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	bundleValues = "bundle(values(name,archived))"
	bundleUsers  = "bundle(aggregatedUsers(login))"
)

func fieldShowDefault(tail string) string {
	if tail == "" {
		return fieldListDefault
	}
	return fieldListDefault + "," + tail
}

const (
	stateOfMany = `{"$type":"StateProjectCustomField","id":"180-1","field":{"$type":"CustomField","name":"State",` +
		`"localizedName":null,"fieldType":{"$type":"FieldType","valueType":"state","isMultiValue":true}}}`
	answeredStateOfMany = `{"$type":"StateProjectCustomField","field":{"$type":"CustomField","name":"State",` +
		`"localizedName":null,"fieldType":{"$type":"FieldType","valueType":"state","isMultiValue":true}}}`
)

func TestFieldShowRefusesTheDefaultOfATypeOutsideTheCatalogue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
	}{
		{name: "no fields of the caller's own", argv: []string{"field", "show", "DEV", "State"}},
		{name: "fields added to the default", argv: []string{"field", "show", "DEV", "State", "--fields", "+canBeEmpty"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := projectMetadata(stateOfMany)
			server := serveTheProject(t, metadata, noField(t))

			got := runWith(t, server.env(), tc.argv...)

			want := faultDocument{
				code: "upstream_invalid",
				details: []detail{
					{"request", metadataRequest(server.url, "DEV")},
					{"upstream_status", 200},
					{"upstream_body", metadata},
				},
			}
			assert.Equal(t, want, requireFault(t, got))
			assert.Len(t, server.requests(), 1)
		})
	}
}

func TestFieldShowPrintsAFieldOfATypeOutsideTheCatalogueTheCallerAsksFor(t *testing.T) {
	t.Parallel()
	field := respondWith(http.StatusOK, answeredStateOfMany)
	server := serveTheProject(t, projectMetadata(stateOfMany), field)

	got := runWith(t, server.env(), "field", "show", "DEV", "State", "--fields", "field(name)")

	assert.Equal(t, outcome{stdout: "field:\n  name: \"State\"\n"}, got)
	assert.Len(t, server.requests(), 2)
}
