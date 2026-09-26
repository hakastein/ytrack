package cli_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
)

type repeatedFlag struct {
	argv  []string
	flag  string
	value string
}

func TestEveryCommandRefusesAFlagOfOneValueGivenTwice(t *testing.T) {
	t.Parallel()
	attached := aFileToAttach(t)
	tests := []repeatedFlag{
		{argv: []string{"issue", "show", "DEV-1"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"issue", "show", "DEV-1"}, flag: "--comments", value: "1"},
		{argv: []string{"issue", "list"}, flag: "--query", value: "a"},
		{argv: []string{"issue", "list", "--query", "a"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"issue", "list", "--query", "a"}, flag: "--limit", value: "1"},
		{argv: []string{"issue", "list", "--query", "a"}, flag: "--skip", value: "1"},
		{argv: []string{"issue", "create", "DEV"}, flag: "--summary", value: "x"},
		{argv: []string{"issue", "create", "DEV", "--summary", "x"}, flag: "--description", value: "x"},
		{argv: []string{"issue", "create", "DEV", "--summary", "x"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"issue", "update", "DEV-1"}, flag: "--summary", value: "x"},
		{argv: []string{"issue", "update", "DEV-1"}, flag: "--description", value: "x"},
		{argv: []string{"issue", "update", "DEV-1", "--summary", "x"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"article", "show", "DEV-A-1"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"article", "show", "DEV-A-1"}, flag: "--comments", value: "1"},
		{argv: []string{"article", "list"}, flag: "--query", value: "a"},
		{argv: []string{"article", "list"}, flag: "--parent", value: "DEV-A-1"},
		{argv: []string{"article", "list", "--query", "a"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"article", "list", "--query", "a"}, flag: "--limit", value: "1"},
		{argv: []string{"article", "list", "--query", "a"}, flag: "--skip", value: "1"},
		{argv: []string{"article", "create", "DEV"}, flag: "--summary", value: "x"},
		{argv: []string{"article", "create", "DEV", "--summary", "x"}, flag: "--content", value: "x"},
		{argv: []string{"article", "create", "DEV", "--summary", "x"}, flag: "--parent", value: "DEV-A-1"},
		{argv: []string{"article", "create", "DEV", "--summary", "x"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"article", "update", "DEV-A-1"}, flag: "--summary", value: "x"},
		{argv: []string{"article", "update", "DEV-A-1"}, flag: "--content", value: "x"},
		{argv: []string{"article", "update", "DEV-A-1"}, flag: "--parent", value: "DEV-A-2"},
		{argv: []string{"article", "update", "DEV-A-1", "--summary", "x"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"comment", "list", "DEV-1"}, flag: "--fields", value: "id"},
		{argv: []string{"comment", "list", "DEV-1"}, flag: "--limit", value: "1"},
		{argv: []string{"comment", "list", "DEV-1"}, flag: "--skip", value: "1"},
		{argv: []string{"comment", "create", "DEV-1"}, flag: "--text", value: "x"},
		{argv: []string{"comment", "create", "DEV-1", "--text", "x"}, flag: "--fields", value: "id"},
		{argv: []string{"comment", "update", "DEV-1", "7-1"}, flag: "--text", value: "x"},
		{argv: []string{"comment", "update", "DEV-1", "7-1", "--text", "x"}, flag: "--fields", value: "id"},
		{argv: []string{"attachment", "list", "DEV-1"}, flag: "--fields", value: "id"},
		{argv: []string{"attachment", "list", "DEV-1"}, flag: "--limit", value: "1"},
		{argv: []string{"attachment", "list", "DEV-1"}, flag: "--skip", value: "1"},
		{argv: []string{"attachment", "create", "DEV-1", attached}, flag: "--fields", value: "id"},
		{argv: []string{"tag", "list"}, flag: "--fields", value: "name"},
		{argv: []string{"tag", "list"}, flag: "--limit", value: "1"},
		{argv: []string{"tag", "list"}, flag: "--skip", value: "1"},
		{argv: []string{"tag", "create"}, flag: "--name", value: "x"},
		{argv: []string{"tag", "create", "--name", "x"}, flag: "--fields", value: "name"},
		{argv: []string{"tag", "delete"}, flag: "--name", value: "x"},
		{argv: []string{"tag", "delete", "--name", "x"}, flag: "--owned-by", value: "first"},
		{argv: []string{"tag", "add", "DEV-1"}, flag: "--name", value: "x"},
		{argv: []string{"tag", "add", "DEV-1", "--name", "x"}, flag: "--owned-by", value: "first"},
		{argv: []string{"tag", "remove", "DEV-1"}, flag: "--name", value: "x"},
		{argv: []string{"tag", "remove", "DEV-1", "--name", "x"}, flag: "--owned-by", value: "first"},
		{argv: []string{"link", "list", "DEV-1"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"link", "add", "DEV-1", "needs", "DEV-2"}, flag: "--fields", value: "idReadable"},
		{argv: []string{"time", "list", "DEV-1"}, flag: "--fields", value: "id"},
		{argv: []string{"time", "list", "DEV-1"}, flag: "--limit", value: "1"},
		{argv: []string{"time", "list", "DEV-1"}, flag: "--skip", value: "1"},
		{argv: []string{"time", "create", "DEV-1", "PT1H"}, flag: "--date", value: "2026-09-01"},
		{argv: []string{"time", "create", "DEV-1", "PT1H"}, flag: "--type", value: "First"},
		{argv: []string{"time", "create", "DEV-1", "PT1H"}, flag: "--text", value: "x"},
		{argv: []string{"time", "create", "DEV-1", "PT1H"}, flag: "--fields", value: "id"},
		{argv: []string{"time", "update", "DEV-1", "199-6"}, flag: "--duration", value: "PT1H"},
		{argv: []string{"time", "update", "DEV-1", "199-6"}, flag: "--date", value: "2026-09-01"},
		{argv: []string{"time", "update", "DEV-1", "199-6"}, flag: "--type", value: "First"},
		{argv: []string{"time", "update", "DEV-1", "199-6"}, flag: "--text", value: "x"},
		{argv: []string{"time", "update", "DEV-1", "199-6", "--text", "x"}, flag: "--fields", value: "id"},
		{argv: []string{"activity", "list", "DEV-1"}, flag: "--fields", value: "timestamp"},
		{argv: []string{"activity", "list", "DEV-1"}, flag: "--limit", value: "1"},
		{argv: []string{"activity", "list", "DEV-1"}, flag: "--skip", value: "1"},
		{argv: []string{"field", "list", "DEV"}, flag: "--fields", value: "canBeEmpty"},
		{argv: []string{"field", "show", "DEV", "Type"}, flag: "--fields", value: "canBeEmpty"},
		{argv: []string{"project", "show", "DEV"}, flag: "--fields", value: "shortName"},
		{argv: []string{"project", "list"}, flag: "--fields", value: "shortName"},
		{argv: []string{"project", "list"}, flag: "--limit", value: "1"},
		{argv: []string{"project", "list"}, flag: "--skip", value: "1"},
		{argv: []string{"user", "show", "first"}, flag: "--fields", value: "login"},
		{argv: []string{"user", "list"}, flag: "--query", value: "a"},
		{argv: []string{"user", "list", "--query", "a"}, flag: "--fields", value: "login"},
		{argv: []string{"user", "list", "--query", "a"}, flag: "--limit", value: "1"},
		{argv: []string{"user", "list", "--query", "a"}, flag: "--skip", value: "1"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.argv[:2], " ")+" "+tc.flag, func(t *testing.T) {
			t.Parallel()
			server := fake.ServeNothing(t)

			got := runWith(t, envOf(server), slices.Concat(tc.argv, []string{tc.flag, tc.value, tc.flag, tc.value})...)

			assert.Equal(t, faultDocument{code: "bad_usage"}, requireFault(t, got))
			assert.Empty(t, server.Requests())
		})
	}
}
