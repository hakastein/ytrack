package cli_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The three commits of DEV-451, as the journal of that issue answered them on a live instance with nothing
// taken out but the target nobody asks for any more: a merge commit whose author YouTrack matched to a user,
// and two commits it matched to nobody. The polygon has none to hold them to — it has no VCS integration, and
// no API files a commit without one — so these are what VcsChangeCategory is held to.
const capturedCommits = `[` +
	`{"removed":[],"added":[{"urls":["https://gitlab.example.com/example/app/-/commit/14091a9f2461267ee7e02525b4f1f2923f1c9849"],"version":"14091a9f2461267ee7e02525b4f1f2923f1c9849","text":"Merge branch 'DEV-451' into 'master'\n\nDEV-451: Отмена заказа OrderCancel\n\nSee merge request example/app!41","date":1761877899000,"id":"229-185","$type":"VcsChange"}],"id":"229-185.0-0","author":{"login":"Петров.Пётр","$type":"User"},"field":null,"timestamp":1761877899000,"category":{"id":"VcsChangeCategory","$type":"ActivityCategory"},"$type":"VcsChangeActivityItem"}` + `,` +
	`{"removed":[],"added":[{"urls":["https://gitlab.example.com/example/app/-/commit/352f7829a2384b001cc12b0c2613c756454a1f6a"],"version":"352f7829a2384b001cc12b0c2613c756454a1f6a","text":"DEV-451: отмена заказа, событие отправляется после сохранения заказа\n","date":1761829642000,"id":"229-163","$type":"VcsChange"}],"id":"229-163.0-0","author":{"login":"system_user@","$type":"VcsUnresolvedUser"},"field":null,"timestamp":1761829642000,"category":{"id":"VcsChangeCategory","$type":"ActivityCategory"},"$type":"VcsChangeActivityItem"}` + `,` +
	`{"removed":[],"added":[{"urls":["https://gitlab.example.com/example/app/-/commit/e0996a37c13d44c3b06074939d43fa3759bd32c1"],"version":"e0996a37c13d44c3b06074939d43fa3759bd32c1","text":"DEV-451: отмена заказа, событие отправляется после сохранения заказа\n","date":1761829190000,"id":"229-162","$type":"VcsChange"}],"id":"229-162.0-0","author":{"login":"system_user@","$type":"VcsUnresolvedUser"},"field":null,"timestamp":1761829190000,"category":{"id":"VcsChangeCategory","$type":"ActivityCategory"},"$type":"VcsChangeActivityItem"}` +
	`]`

// A commit is printed by the link to it, which is what the default asks of every value and what a commit has
// among those names; its author is the user YouTrack matched the committer to, or system_user@ where it
// matched nobody.
func TestActivityPrintsTheCommitsOfAnIssueByTheirLinks(t *testing.T) {
	t.Parallel()
	server := journal(t, answer(http.StatusOK, capturedCommits))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--category", "vcschangecategory")

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
	// A commit stands for no one field of the issue, so no link types are read for it.
	assert.Equal(t, 0, sentTo(server, linkTypesPath))
}

// The commits stand in the journal of every category, since the default asks for all of them: the chronology of
// an issue is whole without a flag.
func TestActivityAsksForTheCommitsWithEveryOtherCategory(t *testing.T) {
	t.Parallel()
	server := journal(t, answer(http.StatusOK, capturedCommits))

	got := runWith(t, server.env(), "activity", "list", journalIssue)

	require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
	assert.Contains(t, strings.Split(activitySent(t, server).Get("categories"), ","), "VcsChangeCategory")
	assert.Equal(t, []string{activityCategories}, activitySent(t, server)["categories"])
	assert.Equal(t, 3, strings.Count(got.stdout, `category: "VcsChangeCategory"`))
}

// The message and the hash of a commit are asked for by name: a message runs to several lines, and a merge
// commit, which is what a merged merge request stands as, says so in its message alone.
func TestActivityPrintsTheMessageAndTheHashOfACommitAskedFor(t *testing.T) {
	t.Parallel()
	server := journal(t, answer(http.StatusOK, capturedCommits))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--category", "VcsChangeCategory",
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
