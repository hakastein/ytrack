package cli_test

import (
	"encoding/json"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinkOfTheDevInstanceWritesEveryPhraseAtTheEndItNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		phrase  string
		reverse string
		suffix  string
	}{
		{name: "relates to", phrase: "relates to", reverse: "relates to", suffix: ""},
		{name: "is required for", phrase: "is required for", reverse: "depends on", suffix: "s"},
		{name: "depends on", phrase: "depends on", reverse: "is required for", suffix: "t"},
		{name: "is duplicated by", phrase: "is duplicated by", reverse: "duplicates", suffix: "s"},
		{name: "duplicates", phrase: "duplicates", reverse: "is duplicated by", suffix: "t"},
		{name: "parent for", phrase: "parent for", reverse: "subtask of", suffix: "s"},
		{name: "subtask of", phrase: "subtask of", reverse: "parent for", suffix: "t"},
		{name: "copied to", phrase: "Скопирована в", reverse: "Копия", suffix: "s"},
		{name: "copy of", phrase: "Копия", reverse: "Скопирована в", suffix: "t"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := devInstance(t)
			source := aContractIssue(t, dev, "the one the phrase is written on")
			target := aContractIssue(t, dev, "the one at the other end")

			read := len(dev.requests())
			got := runWith(t, dev.env(), "link", "add", source, tc.phrase, target)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []string{target}, targetsUnder(t, got.stdout, tc.phrase))

			links := everyIssueLinkID(t, dev.answers()[read])
			link := path.Base(strings.TrimSuffix(dev.sentPaths()[read+2], "/issues"))
			assert.Regexp(t, `^[0-9]+-[0-9]+`+tc.suffix+`$`, link)
			assert.Equal(t, issueLinkOfThePhrase(t, dev.answers()[read], endOfTheSuffix(tc.suffix), tc.phrase), link)

			other := runWith(t, dev.env(), "link", "list", target)

			require.Equal(t, 0, other.code, "stderr: %s", other.stderr)
			assert.Equal(t, []string{source}, targetsUnder(t, other.stdout, tc.reverse))
			if tc.reverse != tc.phrase {
				assert.NotContains(t, keysOf(nodeAt(t, requireMapping(t, "stdout", other.stdout), "links")),
					tc.phrase)
			}

			requireNoIssueLinkID(t, got.stdout, links)
			requireNoIssueLinkID(t, other.stdout, links)

			taken := len(dev.requests())
			away := runWith(t, dev.env(), "link", "remove", source, tc.phrase, target)

			require.Equal(t, 0, away.code, "stderr: %s", away.stderr)
			assert.Equal(t, []detail{
				{"idReadable", source},
				{"removed", []detail{{tc.phrase, []any{[]detail{{"idReadable", target}}}}}},
			}, requireDocument(t, away.stdout))
			assert.Equal(t, link, path.Base(path.Dir(path.Dir(dev.sentPaths()[taken+2]))))
			requireNoIssueLinkID(t, away.stdout, links)

			for _, issue := range []string{source, target} {
				held := runWith(t, dev.env(), "link", "list", issue)

				require.Equal(t, 0, held.code, "stderr: %s", held.stderr)
				assert.Equal(t, []detail{{"total", 0}, {"returned", 0}, {"truncated", false}},
					requireDocument(t, held.stdout)[:3], issue)
				assert.Empty(t, keysOf(nodeAt(t, requireMapping(t, "stdout", held.stdout), "links")), issue)
			}
		})
	}
}

func TestLinkRemoveKeepsTheSlotIDToTheEvidenceOfWhatWentOut(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	source := aContractIssue(t, dev, "the one the phrase is written on")
	target := aContractIssue(t, dev, "the one at the other end")

	read := len(dev.requests())
	got := runWith(t, dev.env(), "link", "remove", source, "depends on", target)

	found := requireFault(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, []detail{{"issue", source}, {"phrase", "depends on"}, {"target", target}}, found.details[1:4])

	link := issueLinkOfThePhrase(t, dev.answers()[read], "INWARD", "depends on")
	internal := internalIDOf(t, dev.answers()[read+1])
	sent, isText := detailNamed(t, found, "request").(string)
	require.True(t, isText, "stderr: %q", got.stderr)
	assert.Contains(t, sent, "/links/"+link+"/issues/"+internal)
	assert.Contains(t, detailNamed(t, found, "upstream_message"), internal)

	for _, printed := range found.details {
		if printed.key == "request" || strings.HasPrefix(printed.key, "upstream_") {
			continue
		}
		text, isText := printed.value.(string)
		require.True(t, isText, "%s: %v", printed.key, printed.value)
		assert.NotContains(t, text, link, printed.key)
		assert.NotContains(t, text, internal, printed.key)
	}
}

func requireNoIssueLinkID(t *testing.T, stdout string, links []string) {
	t.Helper()
	for _, link := range links {
		assert.NotContains(t, stdout, link)
	}
}

func everyIssueLinkID(t *testing.T, body []byte) []string {
	t.Helper()
	var read struct {
		Links []struct {
			ID string `json:"id"`
		} `json:"links"`
	}
	require.NoError(t, json.Unmarshal(body, &read), "the answer read: %s", body)
	ids := make([]string, 0, len(read.Links))
	for _, link := range read.Links {
		ids = append(ids, link.ID)
	}
	require.NotEmpty(t, ids, "the answer holds no slot: %s", body)
	return ids
}

func internalIDOf(t *testing.T, body []byte) string {
	t.Helper()
	var read struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(body, &read), "the answer read: %s", body)
	require.Regexp(t, `^[0-9]+-[0-9]+$`, read.ID)
	return read.ID
}

func endOfTheSuffix(suffix string) string {
	switch suffix {
	case "s":
		return "OUTWARD"
	case "t":
		return "INWARD"
	}
	return "BOTH"
}
