package cli_test

import (
	"encoding/json"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every phrase the polygon has, written and read back on two issues of the contract test's own. The table
// is the grammar of a link seen from outside: a phrase, the phrase the same link is read by from its other end,
// and the suffix the server addresses that end's slot by. Nothing here is read off the code — the reverse
// phrase is read off the partner and the suffix off the request that went out.
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
			partner := aContractIssue(t, dev, "the one at the other end")

			read := len(dev.requests())
			got := runWith(t, dev.env(), "link", "add", source, tc.phrase, partner)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Empty(t, got.stderr)
			assert.Equal(t, []string{partner}, partnersUnder(t, got.stdout, tc.phrase))

			// The write is addressed by the slot the read before it sent, and the suffix of that id is the end
			// the phrase names: an id composed out of the type and a guess at the suffix would reach the other
			// end of the same type under a 200.
			slots := everySlotID(t, dev.answers()[read])
			slot := path.Base(strings.TrimSuffix(dev.sentPaths()[read+2], "/issues"))
			assert.Regexp(t, `^[0-9]+-[0-9]+`+tc.suffix+`$`, slot)
			assert.Equal(t, slotOfThePhrase(t, dev.answers()[read], endOfTheSuffix(tc.suffix), tc.phrase), slot)

			// The link stands on the partner under the phrase of the partner's own end, and a directed type is
			// read by one phrase at each end, so the phrase the call was made with is nowhere on it.
			other := runWith(t, dev.env(), "link", "list", partner)

			require.Equal(t, 0, other.code, "stderr: %s", other.stderr)
			assert.Equal(t, []string{source}, partnersUnder(t, other.stdout, tc.reverse))
			if tc.reverse != tc.phrase {
				assert.NotContains(t, keysOf(nodeAt(t, requireMapping(t, "stdout", other.stdout), "links")),
					tc.phrase)
			}

			// The two documents the write and the read printed carry no id the server addresses a slot by.
			requireNoSlotID(t, got.stdout, slots)
			requireNoSlotID(t, other.stdout, slots)

			taken := len(dev.requests())
			away := runWith(t, dev.env(), "link", "remove", source, tc.phrase, partner)

			require.Equal(t, 0, away.code, "stderr: %s", away.stderr)
			assert.Equal(t, []detail{
				{"idReadable", source},
				{"removed", []detail{{tc.phrase, []any{[]detail{{"idReadable", partner}}}}}},
			}, requireDocument(t, away.stdout))
			assert.Equal(t, slot, path.Base(path.Dir(path.Dir(dev.sentPaths()[taken+2]))))
			requireNoSlotID(t, away.stdout, slots)

			// One link is one link from either end: taking it away from the end the phrase names leaves neither
			// issue holding half of it.
			for _, issue := range []string{source, partner} {
				held := runWith(t, dev.env(), "link", "list", issue)

				require.Equal(t, 0, held.code, "stderr: %s", held.stderr)
				assert.Equal(t, []detail{{"total", 0}, {"returned", 0}, {"truncated", false}},
					requireDocument(t, held.stdout)[:3], issue)
				assert.Empty(t, keysOf(nodeAt(t, requireMapping(t, "stdout", held.stdout), "links")), issue)
			}
		})
	}
}

// A refusal about a write names what went out word for word, and the address of a write carries the id of
// a slot and the internal id of the partner. Both stay there: the keys that name what the caller fixes are the
// readable ids and the phrase, which mean the same on any instance.
func TestLinkRemoveKeepsTheSlotIDToTheEvidenceOfWhatWentOut(t *testing.T) {
	t.Parallel()
	dev := devInstance(t)
	source := aContractIssue(t, dev, "the one the phrase is written on")
	partner := aContractIssue(t, dev, "the one at the other end")

	read := len(dev.requests())
	got := runWith(t, dev.env(), "link", "remove", source, "depends on", partner)

	found := requireRefusal(t, got)
	assert.Equal(t, "not_found", found.code)
	assert.Equal(t, []detail{{"issue", source}, {"phrase", "depends on"}, {"partner", partner}}, found.details[1:4])

	slot := slotOfThePhrase(t, dev.answers()[read], "INWARD", "depends on")
	internal := internalIDOf(t, dev.answers()[read+1])
	sent, isText := detailNamed(t, found, "request").(string)
	require.True(t, isText, "stderr: %q", got.stderr)
	assert.Contains(t, sent, "/links/"+slot+"/issues/"+internal)
	assert.Contains(t, detailNamed(t, found, "upstream_message"), internal)

	// Every other key of the refusal is the caller's own words back: nothing that names something names it by an
	// identifier of this instance.
	for _, printed := range found.details {
		if printed.key == "request" || strings.HasPrefix(printed.key, "upstream_") {
			continue
		}
		text, isText := printed.value.(string)
		require.True(t, isText, "%s: %v", printed.key, printed.value)
		assert.NotContains(t, text, slot, printed.key)
		assert.NotContains(t, text, internal, printed.key)
	}
}

// requireNoSlotID holds a document to the rule the slot exists behind: the phrase stands where the id of a slot
// would, and an id of this instance in stdout would be an address a caller could not carry anywhere.
func requireNoSlotID(t *testing.T, stdout string, slots []string) {
	t.Helper()
	for _, slot := range slots {
		assert.NotContains(t, stdout, slot)
	}
}

// everySlotID is the id the answer to a read addresses each slot of the issue by.
func everySlotID(t *testing.T, body []byte) []string {
	t.Helper()
	var read struct {
		Links []struct {
			ID string `json:"id"`
		} `json:"links"`
	}
	require.NoError(t, json.Unmarshal(body, &read), "the answer read: %s", body)
	ids := make([]string, 0, len(read.Links))
	for _, slot := range read.Links {
		ids = append(ids, slot.ID)
	}
	require.NotEmpty(t, ids, "the answer holds no slot: %s", body)
	return ids
}

// internalIDOf is the id of the instance's own the answer gave the issue, which is what a link addresses the
// partner by and what a readable id is printed instead of.
func internalIDOf(t *testing.T, body []byte) string {
	t.Helper()
	var read struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(body, &read), "the answer read: %s", body)
	require.Regexp(t, `^[0-9]+-[0-9]+$`, read.ID)
	return read.ID
}

// The suffix of a slot id stands for the end of the link the issue is at: none for a link read the same from
// either end, an s where the issue is the source and a t where it is the target.
func endOfTheSuffix(suffix string) string {
	switch suffix {
	case "s":
		return "OUTWARD"
	case "t":
		return "INWARD"
	}
	return "BOTH"
}
