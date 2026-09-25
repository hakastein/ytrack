package cli_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const capturedCommits = `[` +
	`{"removed":[],"added":[{"urls":["https://gitlab.example.com/example/app/-/commit/14091a9f2461267ee7e02525b4f1f2923f1c9849"],"version":"14091a9f2461267ee7e02525b4f1f2923f1c9849","text":"Merge branch 'DEV-451' into 'master'\n\nDEV-451: Отмена заказа OrderCancel\n\nSee merge request example/app!41","date":1761877899000,"id":"229-185","$type":"VcsChange"}],"id":"229-185.0-0","author":{"login":"Петров.Пётр","$type":"User"},"field":null,"timestamp":1761877899000,"category":{"id":"VcsChangeCategory","$type":"ActivityCategory"},"$type":"VcsChangeActivityItem"}` + `,` +
	`{"removed":[],"added":[{"urls":["https://gitlab.example.com/example/app/-/commit/352f7829a2384b001cc12b0c2613c756454a1f6a"],"version":"352f7829a2384b001cc12b0c2613c756454a1f6a","text":"DEV-451: отмена заказа, событие отправляется после сохранения заказа\n","date":1761829642000,"id":"229-163","$type":"VcsChange"}],"id":"229-163.0-0","author":{"login":"system_user@","$type":"VcsUnresolvedUser"},"field":null,"timestamp":1761829642000,"category":{"id":"VcsChangeCategory","$type":"ActivityCategory"},"$type":"VcsChangeActivityItem"}` + `,` +
	`{"removed":[],"added":[{"urls":["https://gitlab.example.com/example/app/-/commit/e0996a37c13d44c3b06074939d43fa3759bd32c1"],"version":"e0996a37c13d44c3b06074939d43fa3759bd32c1","text":"DEV-451: отмена заказа, событие отправляется после сохранения заказа\n","date":1761829190000,"id":"229-162","$type":"VcsChange"}],"id":"229-162.0-0","author":{"login":"system_user@","$type":"VcsUnresolvedUser"},"field":null,"timestamp":1761829190000,"category":{"id":"VcsChangeCategory","$type":"ActivityCategory"},"$type":"VcsChangeActivityItem"}` +
	`]`

func TestActivityPrintsTheCommitsOfAnIssueByTheirLinks(t *testing.T) {
	t.Parallel()
	server := activityServer(t, respondWith(http.StatusOK, capturedCommits))

	got := runWith(t, server.env(), "activity", "list", activityIssue, "--category", "vcschangecategory")

	want := "total: 3\nreturned: 3\ntruncated: false\nactivities:\n" +
		`  - {timestamp: "2025-10-31T02:31:39Z", author: {login: "Петров.Пётр"}, category: "VcsChangeCategory", ` +
		`field: null, added: [{id: "229-185", urls: ["https://gitlab.example.com/example/app/-/commit/` +
		`14091a9f2461267ee7e02525b4f1f2923f1c9849"]}], removed: []}` + "\n" +
		`  - {timestamp: "2025-10-30T13:07:22Z", author: {login: "system_user@"}, category: "VcsChangeCategory", ` +
		`field: null, added: [{id: "229-163", urls: ["https://gitlab.example.com/example/app/-/commit/` +
		`352f7829a2384b001cc12b0c2613c756454a1f6a"]}], removed: []}` + "\n" +
		`  - {timestamp: "2025-10-30T12:59:50Z", author: {login: "system_user@"}, category: "VcsChangeCategory", ` +
		`field: null, added: [{id: "229-162", urls: ["https://gitlab.example.com/example/app/-/commit/` +
		`e0996a37c13d44c3b06074939d43fa3759bd32c1"]}], removed: []}` + "\n"
	assert.Equal(t, outcome{stdout: want}, got)
	sent := activitySent(t, server)
	assert.Equal(t, []string{"VcsChangeCategory"}, sent["categories"])
	assert.Equal(t, []string{sentActivityFields}, sent["fields"])
	assert.Equal(t, 0, sentTo(server, linkTypesPath))
}

func TestActivityAsksForTheCommitsWithEveryOtherCategory(t *testing.T) {
	t.Parallel()
	server := activityServer(t, respondWith(http.StatusOK, capturedCommits))

	got := runWith(t, server.env(), "activity", "list", activityIssue)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Contains(t, strings.Split(activitySent(t, server).Get("categories"), ","), "VcsChangeCategory")
	assert.Equal(t, []string{activityCategories}, activitySent(t, server)["categories"])
	assert.Equal(t, 3, strings.Count(got.stdout, `category: "VcsChangeCategory"`))
}

func TestActivityPrintsTheMessageAndTheHashOfACommitAskedFor(t *testing.T) {
	t.Parallel()
	server := activityServer(t, respondWith(http.StatusOK, capturedCommits))

	got := runWith(t, server.env(), "activity", "list", activityIssue, "--category", "VcsChangeCategory",
		"--limit", "2", "--fields", "+added(text,version,date)")

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.True(t, strings.HasPrefix(got.stdout, "total: null\nreturned: 2\ntruncated: true\nactivities:\n"), got.stdout)
	merged :=
		`  - {timestamp: "2025-10-31T02:31:39Z", author: {login: "Петров.Пётр"}, category: "VcsChangeCategory", ` +
			`field: null, added: [{id: "229-185", urls: ["https://gitlab.example.com/example/app/-/commit/` +
			`14091a9f2461267ee7e02525b4f1f2923f1c9849"], text: "Merge branch 'DEV-451' into 'master'\n\nDEV-451: ` +
			`Отмена заказа OrderCancel\n\nSee merge request example/app!41", ` +
			`version: "14091a9f2461267ee7e02525b4f1f2923f1c9849", date: "2025-10-31T02:31:39Z"}], removed: []}` + "\n"
	assert.Contains(t, got.stdout, merged)
	assert.Equal(t, "timestamp,author(login),category(id),field(name,customField(name,fieldType(valueType))),"+
		"added(id,idReadable,login,name,urls,text,version,date,minutes),removed(id,idReadable,login,name,urls,minutes)",
		activitySent(t, server).Get("fields"))
}
