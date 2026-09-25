package cli_test

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/fake"
)

const linkTypesPath = "/api/issueLinkTypes"

func sentLinkType(source, target, translatedSourceJSON, translatedTargetJSON string) string {
	return sentLinkTypeWith(strconv.Quote(source), strconv.Quote(target), translatedSourceJSON, translatedTargetJSON)
}

func sentLinkTypeWith(sourceJSON, targetJSON, translatedSourceJSON, translatedTargetJSON string) string {
	return `{"$type":"IssueLinkType","name":"Type","sourceToTarget":` + sourceJSON +
		`,"targetToSource":` + targetJSON +
		`,"localizedSourceToTarget":` + translatedSourceJSON +
		`,"localizedTargetToSource":` + translatedTargetJSON + `}`
}

func devInstanceLinkTypes() string {
	return `[` + strings.Join([]string{
		sentLinkType("relates to", "", `"связана с"`, `""`),
		sentLinkType("is required for", "depends on", `"обязательна для"`, `"зависит от"`),
		sentLinkType("is duplicated by", "duplicates", `"дублирована"`, `"дублирует"`),
		sentLinkType("parent for", "subtask of", `"родитель для"`, `"подзадача для"`),
		sentLinkType("Скопирована в", "Копия", `null`, `""`),
	}, ",") + `]`
}

func linkTypesOf(answering, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == linkTypesPath {
			answering(w, r)
			return
		}
		handler(w, r)
	}
}

func linksKnown(rest http.HandlerFunc) http.HandlerFunc {
	return linkTypesOf(fake.JSON(http.StatusOK, devInstanceLinkTypes()), rest)
}

func linkingTypes(t *testing.T, types, handler http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, linkTypesOf(types, handler))
}

func sentLinkRecord(label string) string {
	return sentActivity{
		kind: "LinksActivityItem", category: "LinksCategory", timestamp: middle,
		field: `{"$type":"LinkTypeFilterField","name":` + strconv.Quote(label) + `}`,
		added: sentLinkedIssue,
	}.sent()
}

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
			server := activityServer(t, fake.JSON(http.StatusOK, `[`+sentLinkRecord(tc.label)+`]`))

			got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", "field")

			assert.Equal(t, outcome{stdout: oneRecord(`field: "` + tc.want + `"`)}, got)
			assert.Equal(t, []string{linkTypesPath, activitiesPath}, server.Paths())
		})
	}
}

func TestActivityRefusesALinkThePhrasesOfTheInstanceDoNotResolve(t *testing.T) {
	t.Parallel()
	ambiguous := `[` + sentLinkType("relates to", "", `"связана с"`, `""`) + `,` +
		sentLinkType("refers to", "", `"связана с"`, `""`) + `,` +
		sentLinkType("is required for", "depends on", `"обязательна для"`, `"зависит от"`) + `]`
	t.Run("a link of a phrase no type of the instance goes by", func(t *testing.T) {
		t.Parallel()
		server := activityServer(t, fake.JSON(http.StatusOK, `[`+sentLinkRecord("Блокирует")+`]`))

		got := runWith(t, server.Env(), "activity", "list", activityIssue)

		found := requireFault(t, got)
		assert.Equal(t, "upstream_invalid", found.code)
		assert.Empty(t, got.stdout)
	})
	t.Run("a link of a phrase two types of the instance go by", func(t *testing.T) {
		t.Parallel()
		server := linkingTypes(t, fake.JSON(http.StatusOK, ambiguous), fake.JSON(http.StatusOK, `[`+sentLinkRecord("Связана с")+`]`))

		got := runWith(t, server.Env(), "activity", "list", activityIssue)

		found := requireFault(t, got)
		assert.Equal(t, "upstream_invalid", found.code)
	})
	t.Run("two types written alike and no record of either", func(t *testing.T) {
		t.Parallel()
		server := linkingTypes(t, fake.JSON(http.StatusOK, ambiguous), fake.JSON(http.StatusOK, `[`+sentLinkRecord("Зависит от")+`]`))

		got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", "field")

		assert.Equal(t, outcome{stdout: oneRecord(`field: "depends on"`)}, got)
	})
}

func TestActivityPrintsALinkOfATypeWrittenAlikeAtBothEnds(t *testing.T) {
	t.Parallel()
	symmetric := `[` + sentLinkType("relates to", "relates to", `"связана с"`, `"связана с"`) + `,` +
		sentLinkType("is required for", "depends on", `"обязательна для"`, `"зависит от"`) + `]`
	server := linkingTypes(t, fake.JSON(http.StatusOK, symmetric), fake.JSON(http.StatusOK, `[`+sentLinkRecord("Связана с")+`]`))

	got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", "field")

	assert.Equal(t, outcome{stdout: oneRecord(`field: "relates to"`)}, got)
}

func TestActivityRefusesALinkNamedByNoPhraseAtAll(t *testing.T) {
	t.Parallel()
	server := activityServer(t, fake.JSON(http.StatusOK, `[`+sentLinkRecord("")+`]`))

	got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", "field")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Empty(t, got.stdout)
}

func TestActivityRefusesACatalogueOfLinkTypesOfAShapeItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind string
	}{
		{
			name: "a phrase that arrived as a number",
			kind: sentLinkTypeWith(`7`, `""`, `""`, `""`),
		},
		{
			name: "a translation that arrived as a list",
			kind: sentLinkTypeWith(`"relates to"`, `""`, `["связана с"]`, `""`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := linkingTypes(t, fake.JSON(http.StatusOK, `[`+tc.kind+`]`),
				fake.JSON(http.StatusOK, `[`+sentLinkRecord("Зависит от")+`]`))

			got := runWith(t, server.Env(), "activity", "list", activityIssue)

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Equal(t, 0, sentTo(server, activitiesPath))
		})
	}
}

func TestActivityReadsTheLinkTypesOnlyForActivitiesThatPrintALink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
		sent  int
	}{
		{name: "the default, which prints the field of every category", sent: 1},
		{name: "activities that print no field", flags: []string{"--fields", "timestamp,added"}, sent: 0},
		{
			name:  "activities of a category that is no link",
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
			server := activityServer(t, fake.JSON(http.StatusOK, `[`+comment+`]`))

			got := runWith(t, server.Env(), slices.Concat([]string{"activity", "list", activityIssue}, tc.flags)...)

			require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
			assert.Equal(t, tc.sent, sentTo(server, linkTypesPath))
			assert.Equal(t, 1, sentTo(server, activitiesPath))
		})
	}
}

func TestActivitySendsNoActivitiesWhereTheLinkTypesFail(t *testing.T) {
	t.Parallel()
	server := linkingTypes(t, fake.JSON(http.StatusInternalServerError, `{"error":"Internal Server Error"}`),
		fake.JSON(http.StatusOK, `[`+sentLinkRecord("Зависит от")+`]`))

	got := runWith(t, server.Env(), "activity", "list", activityIssue)

	assert.Equal(t, "upstream_failed", requireFault(t, got).code)
	assert.Empty(t, got.stdout)
	assert.Equal(t, 0, sentTo(server, activitiesPath))
}

func TestActivityRefusesACatalogueOfLinkTypesAsLongAsItAskedFor(t *testing.T) {
	t.Parallel()
	const linkTypesAskedFor = 1000
	types := make([]string, 0, linkTypesAskedFor)
	for at := range linkTypesAskedFor {
		types = append(types, sentLinkType("goes with "+strconv.Itoa(at), "", `""`, `""`))
	}
	server := linkingTypes(t, fake.JSON(http.StatusOK, `[`+strings.Join(types, ",")+`]`),
		fake.JSON(http.StatusOK, `[`+sentLinkRecord("Зависит от")+`]`))

	got := runWith(t, server.Env(), "activity", "list", activityIssue)

	found := requireFault(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
	assert.Equal(t, 0, sentTo(server, activitiesPath))
}

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
			server := activityServer(t, fake.JSON(http.StatusOK, `[`+tc.activity+`]`))

			got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", "field")

			assert.Equal(t, outcome{stdout: oneRecord("field: " + tc.want)}, got)
			assert.NotContains(t, got.stdout, "Состояние")
		})
	}
}

func TestActivityRefusesAChangeOfACustomFieldThatNamesNoField(t *testing.T) {
	t.Parallel()
	activity := sentActivity{
		kind: "CustomFieldActivityItem", category: "CustomFieldCategory", timestamp: middle,
		field: `{"$type":"CustomFilterField","name":"Состояние"}`,
	}.sent()
	server := activityServer(t, fake.JSON(http.StatusOK, `[`+activity+`]`))

	got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", "field")

	found := requireFault(t, got)
	assert.Equal(t, "upstream_invalid", found.code)
}

func TestActivityRefusesAFieldOfARecordOfALinkTheRowDoesNotReferTo(t *testing.T) {
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
			server := activityServer(t, fake.JSON(http.StatusOK, `[`+activity+`]`))

			got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", "field")

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
			assert.Empty(t, got.stdout)
		})
	}
}

func TestActivityNamesAFilterOfTheWrongSubtypeWhateverTheActivitiesPrint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields string
	}{
		{name: "activities that print the field alone", fields: "timestamp,field"},
		{name: "activities that print the values beside it", fields: "timestamp,field,added"},
		{name: "activities that print the values alone", fields: "timestamp,added"},
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
			server := activityServer(t, fake.JSON(http.StatusOK, `[`+activity+`]`))

			got := runWith(t, server.Env(), "activity", "list", activityIssue, "--fields", tc.fields)

			found := requireFault(t, got)
			assert.Equal(t, "upstream_invalid", found.code)
		})
	}
}
