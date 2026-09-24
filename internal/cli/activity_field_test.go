package cli_test

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Where the phrases of the links of the instance are read, which is a request of its own before the journal.
const linkTypesPath = "/api/issueLinkTypes"

// A link type as the server sends one: both phrases and both translations, the last two written as JSON, since
// a translation the instance keeps none of arrives as "" from one type and as null from the next.
func sentLinkType(source, target, translatedSource, translatedTarget string) string {
	return sentLinkTypeHolding(strconv.Quote(source), strconv.Quote(target), translatedSource, translatedTarget)
}

// sentLinkTypeHolding is sentLinkType with all four phrases written as JSON, for a scenario that sends one of a
// shape no phrase can be read out of.
func sentLinkTypeHolding(source, target, translatedSource, translatedTarget string) string {
	return `{"$type":"IssueLinkType","name":"Type","sourceToTarget":` + source +
		`,"targetToSource":` + target +
		`,"localizedSourceToTarget":` + translatedSource +
		`,"localizedTargetToSource":` + translatedTarget + `}`
}

// The five link types of the polygon, as it answers them: the translations are written in lower case while a
// record of the journal carries the same phrase capitalized, Copy carries none at either end, and the
// undirected Relates has an empty phrase at the end no issue stands at.
func polygonLinkTypes() string {
	return `[` + strings.Join([]string{
		sentLinkType("relates to", "", `"связана с"`, `""`),
		sentLinkType("is required for", "depends on", `"обязательна для"`, `"зависит от"`),
		sentLinkType("is duplicated by", "duplicates", `"дублирована"`, `"дублирует"`),
		sentLinkType("parent for", "subtask of", `"родитель для"`, `"подзадача для"`),
		sentLinkType("Скопирована в", "Копия", `null`, `""`),
	}, ",") + `]`
}

// linkTypesOf answers a request for the link types with answering and leaves every other request to handler.
func linkTypesOf(answering, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == linkTypesPath {
			answering(w, r)
			return
		}
		handler(w, r)
	}
}

// linksKnown is the requests of a journal with the link types of the polygon answered before them, for a
// scenario that stands its own server up and says nothing of the types.
func linksKnown(rest http.HandlerFunc) http.HandlerFunc {
	return linkTypesOf(answer(http.StatusOK, polygonLinkTypes()), rest)
}

// linkingTypes is journal with link types of the scenario's own in place of the five the polygon keeps.
func linkingTypes(t *testing.T, types, handler http.HandlerFunc) *upstream {
	t.Helper()
	return serve(t, linkTypesOf(types, handler))
}

// A record of a link as the server sends one: the end of the link type is named by the phrase alone, translated
// and capitalized, and the issue at the other end stands under added.
func sentLinkRecord(label string) string {
	return sentActivity{
		kind: "LinksActivityItem", category: "LinksCategory", timestamp: middle,
		field: `{"$type":"LinkTypeFilterField","name":` + strconv.Quote(label) + `}`,
		added: sentLinkedIssue,
	}.sent()
}

// A record of a link names the end of its link type in the language of the instance, and the phrase ytrack
// prints is the untranslated one the link type keeps: the two are held together by the label alone, letter case
// aside, and a type with no translation is written under the untranslated phrase itself.
func TestActivityPrintsALinkByThePhraseOfItsType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		label string
		want  string
	}{
		{label: "Связана с", want: "relates to"},
		{label: "Зависит от", want: "depends on"},
		{label: "Обязательна для", want: "is required for"},
		{label: "Подзадача для", want: "subtask of"},
		{label: "Родитель для", want: "parent for"},
		{label: "Дублирует", want: "duplicates"},
		{label: "Дублирована", want: "is duplicated by"},
		{label: "Копия", want: "Копия"},
		{label: "Скопирована в", want: "Скопирована в"},
		{label: "связана с", want: "relates to"},
	}
	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			server := journal(t, answer(http.StatusOK, `[`+sentLinkRecord(tc.label)+`]`))

			got := runWith(t, server.env(), "activity", "list", journalIssue, "--fields", "field")

			assert.Equal(t, outcome{stdout: oneRecord(`field: "` + tc.want + `"`)}, got)
			assert.Equal(t, []string{linkTypesPath, activitiesPath}, server.sentPaths())
		})
	}
}

// The label is the whole of what a record says about the link it stands for, so a label the types of the
// instance do not settle leaves nothing to print: the phrase would have to be guessed either way.
func TestActivityRefusesALinkThePhrasesOfTheInstanceDoNotSettle(t *testing.T) {
	t.Parallel()
	ambiguous := `[` + sentLinkType("relates to", "", `"связана с"`, `""`) + `,` +
		sentLinkType("refers to", "", `"связана с"`, `""`) + `,` +
		sentLinkType("is required for", "depends on", `"обязательна для"`, `"зависит от"`) + `]`
	t.Run("a link of a phrase no type of the instance goes by", func(t *testing.T) {
		t.Parallel()
		server := journal(t, answer(http.StatusOK, `[`+sentLinkRecord("Блокирует")+`]`))

		got := runWith(t, server.env(), "activity", "list", journalIssue)

		found := requireRefusal(t, got)
		assert.Equal(t, "upstream_lied", found.code)
		assert.Empty(t, got.stdout)
	})
	t.Run("a link of a phrase two types of the instance go by", func(t *testing.T) {
		t.Parallel()
		server := linkingTypes(t, answer(http.StatusOK, ambiguous), answer(http.StatusOK, `[`+sentLinkRecord("Связана с")+`]`))

		got := runWith(t, server.env(), "activity", "list", journalIssue)

		found := requireRefusal(t, got)
		assert.Equal(t, "upstream_lied", found.code)
	})
	// Two types written alike are the instance's business until a record stands for one of them: what is judged
	// is the journal that arrived and not the catalogue it was read against.
	t.Run("two types written alike and no record of either", func(t *testing.T) {
		t.Parallel()
		server := linkingTypes(t, answer(http.StatusOK, ambiguous), answer(http.StatusOK, `[`+sentLinkRecord("Зависит от")+`]`))

		got := runWith(t, server.env(), "activity", "list", journalIssue, "--fields", "field")

		assert.Equal(t, outcome{stdout: oneRecord(`field: "depends on"`)}, got)
	})
}

// A type whose two ends are written alike is one phrase under one label, not two of them: without that, every
// record of such a link would be refused as a phrase two types go by, and the whole journal of the links of the
// instance would be unreadable. YouTrack lets a symmetric type be filed this way, and the polygon carries none.
func TestActivityPrintsALinkOfATypeWrittenAlikeAtBothEnds(t *testing.T) {
	t.Parallel()
	symmetric := `[` + sentLinkType("relates to", "relates to", `"связана с"`, `"связана с"`) + `,` +
		sentLinkType("is required for", "depends on", `"обязательна для"`, `"зависит от"`) + `]`
	server := linkingTypes(t, answer(http.StatusOK, symmetric), answer(http.StatusOK, `[`+sentLinkRecord("Связана с")+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--fields", "field")

	assert.Equal(t, outcome{stdout: oneRecord(`field: "relates to"`)}, got)
}

// An end a type keeps no phrase for is no end rather than an end whose phrase is nothing at all: a record that
// names its link by no phrase stands for a link of no type of the instance, and the phrase would have to be
// guessed. The undirected type of the polygon is the one that has such an end.
func TestActivityRefusesALinkNamedByNoPhraseAtAll(t *testing.T) {
	t.Parallel()
	server := journal(t, answer(http.StatusOK, `[`+sentLinkRecord("")+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--fields", "field")

	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_lied", found.code)
	assert.Empty(t, got.stdout)
}

// The catalogue of phrases is held to its own shape before any record is read by it: a phrase that is neither
// text nor absent leaves the instance saying something other than the link types it was asked for, and a journal
// printed past that would name links by labels nobody asked about.
func TestActivityRefusesACatalogueOfLinkTypesOfAShapeItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind string
	}{
		{
			name: "a phrase that arrived as a number",
			kind: sentLinkTypeHolding(`7`, `""`, `""`, `""`),
		},
		{
			name: "a translation that arrived as a list",
			kind: sentLinkTypeHolding(`"relates to"`, `""`, `["связана с"]`, `""`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linkingTypes(t, answer(http.StatusOK, `[`+tc.kind+`]`),
				answer(http.StatusOK, `[`+sentLinkRecord("Зависит от")+`]`))

			got := runWith(t, server.env(), "activity", "list", journalIssue)

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Equal(t, 0, sentTo(server, activitiesPath))
		})
	}
}

// The link types are read where a record of a link may be printed by them and nowhere else: how many requests a
// journal makes follows what was asked of it, and a caller who asks for neither the field nor the links of the
// journal pays for neither.
func TestActivityReadsTheLinkTypesOnlyForAJournalThatPrintsALink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
		sent  int
	}{
		{name: "the default, which prints the field of every category", sent: 1},
		{name: "a journal that prints no field", flags: []string{"--fields", "timestamp,added"}, sent: 0},
		{
			name:  "a journal of a category that is no link",
			flags: []string{"--category", "CommentsCategory"},
			sent:  0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			comment := sentActivity{
				kind: "CommentActivityItem", category: "CommentsCategory", timestamp: middle,
			}.sent()
			server := journal(t, answer(http.StatusOK, `[`+comment+`]`))

			got := runWith(t, server.env(), slices.Concat([]string{"activity", "list", journalIssue}, tc.flags)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, tc.sent, sentTo(server, linkTypesPath))
			assert.Equal(t, 1, sentTo(server, activitiesPath))
		})
	}
}

// The phrases are read before the journal, so a catalogue that does not arrive stops the call where it is: a
// journal printed without them would name every link by the label of an instance nobody asked about.
func TestActivitySendsNoJournalWhereTheLinkTypesFail(t *testing.T) {
	t.Parallel()
	server := linkingTypes(t, answer(http.StatusInternalServerError, `{"error":"Internal Server Error"}`),
		answer(http.StatusOK, `[`+sentLinkRecord("Зависит от")+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue)

	assert.Equal(t, "upstream_failed", requireRefusal(t, got).code)
	assert.Empty(t, got.stdout)
	assert.Equal(t, 0, sentTo(server, activitiesPath))
}

// The catalogue is read with a count of its own, and a catalogue that fills it is one nothing says the end of:
// a phrase past the thousandth would be missing and every record of it refused as a phrase of no type.
func TestActivityRefusesACatalogueOfLinkTypesAsLongAsItAskedFor(t *testing.T) {
	t.Parallel()
	types := make([]string, 0, 1000)
	for at := range 1000 {
		types = append(types, sentLinkType("goes with "+strconv.Itoa(at), "", `""`, `""`))
	}
	server := linkingTypes(t, answer(http.StatusOK, `[`+strings.Join(types, ",")+`]`),
		answer(http.StatusOK, `[`+sentLinkRecord("Зависит от")+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue)

	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_lied", found.code)
	assert.Equal(t, 0, sentTo(server, activitiesPath))
}

// What a change of a custom field was of is the field the project keeps, named as the project named it: the
// label on the record is that name translated, and a category that stands for no one field of the issue prints
// nothing at all however the server fills its filter in.
func TestActivityPrintsTheFieldOfEveryOtherCategoryByItsCategory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		activity string
		want     string
	}{
		{
			name: "a custom field, by the name the project gave it and not by the label on the record",
			activity: sentActivity{
				kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: middle,
				field: sentStateField, added: sentStateValue, removed: sentStateBefore,
			}.sent(),
			want: `"State"`,
		},
		{
			name:     "a filing, which is a change of no one field of the issue",
			activity: sentActivity{kind: "IssueCreatedActivityItem", category: "IssueCreatedCategory", timestamp: middle}.sent(),
			want:     "null",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := journal(t, answer(http.StatusOK, `[`+tc.activity+`]`))

			got := runWith(t, server.env(), "activity", "list", journalIssue, "--fields", "field")

			assert.Equal(t, outcome{stdout: oneRecord("field: " + tc.want)}, got)
			assert.NotContains(t, got.stdout, "Состояние")
		})
	}
}

// A change of a custom field that names no field is the server saying something other than the journal it was
// asked for: the name the field prints is kept by the project and nowhere in the record itself.
func TestActivityRefusesAChangeOfACustomFieldThatNamesNoField(t *testing.T) {
	t.Parallel()
	activity := sentActivity{
		kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: middle,
		field: `{"$type":"CustomFilterField","name":"Состояние"}`,
	}.sent()
	server := journal(t, answer(http.StatusOK, `[`+activity+`]`))

	got := runWith(t, server.env(), "activity", "list", journalIssue, "--fields", "field")

	found := requireRefusal(t, got)
	assert.Equal(t, "upstream_lied", found.code)
}

// What a record stands for is the row's assertion like everything else about it: a record of a link that names
// no filter of its own, or one whose filter carries no phrase, is the server answering something other than the
// journal it was asked for. Neither is caught by the judgment of names — the names ytrack asks below the field
// are judged by the row that prints them.
func TestActivityRefusesAFieldOfARecordOfALinkTheRowDoesNotStandFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field string
	}{
		{
			name:  "a link whose field arrived as the phrase itself rather than as a filter",
			field: `"Зависит от"`,
		},
		{
			name:  "a filter of a link that carries no phrase",
			field: `{"$type":"LinkTypeFilterField"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			activity := sentActivity{
				kind: "LinksActivityItem", category: "LinksCategory", timestamp: middle,
				field: tc.field, added: sentLinkedIssue,
			}.sent()
			server := journal(t, answer(http.StatusOK, `[`+activity+`]`))

			got := runWith(t, server.env(), "activity", "list", journalIssue, "--fields", "field")

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_lied", found.code)
			assert.Empty(t, got.stdout)
		})
	}
}

// One lie of the server is one diagnosis: what a change of a custom field stands for is judged wherever such a
// record is read, so a filter of the wrong subtype is named as that whether the journal prints the field, the
// values or both. Read on the way to the values alone, it would blame the project for a field with no name.
func TestActivityNamesAFilterOfTheWrongSubtypeWhateverTheJournalPrints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields string
	}{
		{name: "a journal that prints the field alone", fields: "timestamp,field"},
		{name: "a journal that prints the values beside it", fields: "timestamp,field,added"},
		{name: "a journal that prints the values alone", fields: "timestamp,added"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			activity := sentActivity{
				kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: middle,
				field: `{"$type":"PredefinedFilterField","name":"Состояние","customField":{"$type":"CustomField",` +
					`"name":"State","fieldType":{"$type":"FieldType","valueType":"state"}}}`,
				added: sentStateValue,
			}.sent()
			server := journal(t, answer(http.StatusOK, `[`+activity+`]`))

			got := runWith(t, server.env(), "activity", "list", journalIssue, "--fields", tc.fields)

			found := requireRefusal(t, got)
			assert.Equal(t, "upstream_lied", found.code)
		})
	}
}
