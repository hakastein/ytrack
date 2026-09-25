package youtrack_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

const (
	activityLinkTypesPath = "/api/issueLinkTypes"
	activityListPath      = "/api/issues/DEV-1/activities"
)

const (
	activityEarly = 1000
	activityLate  = 2000
)

const activityEveryCategory = "AttachmentsCategory,CommentTextCategory,CommentsCategory,CustomFieldCategory," +
	"DescriptionCategory,IssueCreatedCategory,IssueResolvedCategory,LinksCategory,SummaryCategory," +
	"TagsCategory,VcsChangeCategory,WorkItemCategory"

// "Goes before" is the phrase of two link types, so a link written by it resolves to neither.
const activityLinkTypes = `[` +
	`{"$type":"IssueLinkType","sourceToTarget":"leads to","targetToSource":"follows",` +
	`"localizedSourceToTarget":"Goes before","localizedTargetToSource":"Comes after"},` +
	`{"$type":"IssueLinkType","sourceToTarget":"relates to","targetToSource":"",` +
	`"localizedSourceToTarget":null,"localizedTargetToSource":""},` +
	`{"$type":"IssueLinkType","sourceToTarget":"mirrors","targetToSource":"mirrors",` +
	`"localizedSourceToTarget":"Reflects","localizedTargetToSource":"Reflects"},` +
	`{"$type":"IssueLinkType","sourceToTarget":"precedes","targetToSource":"succeeds",` +
	`"localizedSourceToTarget":"Goes before","localizedTargetToSource":"Comes later"}` +
	`]`

func activityServer(t *testing.T, activities string) *fake.Server {
	t.Helper()
	return activityServerOf(t, fake.JSON(http.StatusOK, activityLinkTypes), fake.JSON(http.StatusOK, activities))
}

func activityServerOf(t *testing.T, linkTypes, activities http.HandlerFunc) *fake.Server {
	t.Helper()
	routes := http.NewServeMux()
	routes.Handle("GET "+activityLinkTypesPath, linkTypes)
	routes.Handle("GET "+activityListPath, activities)
	return fake.Serve(t, routes.ServeHTTP)
}

func activityJSON(kind, category string, at int, members string) string {
	return fmt.Sprintf(`{"$type":%q,"timestamp":%d,"category":{"$type":"ActivityCategory","id":%q},%s}`,
		kind, at, category, members)
}

func activityOfField(valueType, added string) string {
	field := fmt.Sprintf(`{"$type":"CustomFilterField","name":"Label","customField":{"$type":"CustomField",`+
		`"name":"Named","fieldType":{"$type":"FieldType","valueType":%q}}}`, valueType)
	return activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
		`"field":`+field+`,"added":`+added+`,"removed":[]`)
}

func activityOfLink(label string) string {
	return activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
		`"field":{"$type":"LinkTypeFilterField","name":`+strconv.Quote(label)+`},"added":[],"removed":[]`)
}

func activityArray(records ...string) string {
	return "[" + strings.Join(records, ",") + "]"
}

func activityList(t *testing.T, server *fake.Server, expression *string, categories ...string) (*render.Node, *diag.Fault) {
	t.Helper()
	call, fault := youtrack.ListActivities("DEV-1", expression, youtrack.Page{Limit: 50}, categories)
	require.Nil(t, fault)
	return call(t.Context(), client(t, server))
}

func activitiesListed(records ...*render.Node) *render.Node {
	count := render.NewNumber(json.Number(strconv.Itoa(len(records))))
	return render.NewMap(
		render.Pair{Key: "total", Value: count},
		render.Pair{Key: "returned", Value: count},
		render.Pair{Key: "truncated", Value: render.NewBool(false)},
		render.Pair{Key: "activities", Value: render.NewList(append([]*render.Node{}, records...)...)})
}

func activityNothing() *render.Node {
	return render.NewList([]*render.Node{}...)
}

func activityNoNames() *render.Node {
	return render.NewMap([]render.Pair{}...)
}

func withNearest(key, written string, nearest ...string) *render.Node {
	return render.NewMap(
		render.Pair{Key: key, Value: render.NewString(written)},
		render.Pair{Key: "nearest", Value: texts(nearest...)})
}

func activityUnknown(pairs ...render.Pair) diag.Fault {
	return diag.Fault{Code: diag.UnknownName, Details: pairs}
}

func TestListActivitiesRefusesACallItCannotSend(t *testing.T) {
	t.Parallel()
	every := strings.Split(activityEveryCategory, ",")
	tests := []struct {
		name       string
		expression *string
		categories []string
		want       diag.Fault
	}{
		{name: "a name under the category", expression: new("category(id)"), want: diag.Fault{Code: diag.BadUsage}},
		{
			name:       "a name under the category added to the default",
			expression: new("+category(id)"),
			want:       diag.Fault{Code: diag.BadUsage},
		},
		{name: "a name under the field", expression: new("field(name)"), want: diag.Fault{Code: diag.BadUsage}},
		{
			name:       "a name under the field added to the default",
			expression: new("+field(customField(name))"),
			want:       diag.Fault{Code: diag.BadUsage},
		},
		{name: "a category of nothing at all", categories: []string{""}, want: diag.Fault{Code: diag.BadUsage}},
		{
			name:       "a name no value of an activity declares",
			expression: new("added(logn)"),
			want: activityUnknown(
				render.Pair{Key: "fields", Value: render.NewString("added(logn)")},
				render.Pair{Key: "unknown", Value: render.NewList(withNearest("field", "added(logn)", "login"))}),
		},
		{
			name:       "a name no value declares added to the default",
			expression: new("+removed(idReadabel)"),
			want: activityUnknown(
				render.Pair{Key: "fields", Value: render.NewString("+removed(idReadabel)")},
				render.Pair{Key: "unknown", Value: render.NewList(withNearest("field", "removed(idReadabel)", "idReadable"))}),
		},
		{
			name:       "a name no value declares at either end",
			expression: new("added(verson),removed(urlz)"),
			want: activityUnknown(
				render.Pair{Key: "fields", Value: render.NewString("added(verson),removed(urlz)")},
				render.Pair{Key: "unknown", Value: render.NewList(
					withNearest("field", "added(verson)", "version"),
					withNearest("field", "removed(urlz)", "url", "urls"))}),
		},
		{
			name:       "a category a letter short",
			categories: []string{"LinksCategry"},
			want: activityUnknown(render.Pair{Key: "unknown", Value: render.NewList(
				withNearest("category", "LinksCategry", "LinksCategory"))}),
		},
		{
			name:       "a category holding a letter of another alphabet",
			categories: []string{"Links\u0421ategory"},
			want: activityUnknown(render.Pair{Key: "unknown", Value: render.NewList(
				withNearest("category", "Links\u0421ategory", "LinksCategory"))}),
		},
		{
			name:       "two categories written as one name",
			categories: []string{"LinksCategory,CommentsCategory"},
			want: activityUnknown(render.Pair{Key: "unknown", Value: render.NewList(
				withNearest("category", "LinksCategory,CommentsCategory", every...))}),
		},
		{
			name:       "one misspelling written in two letter cases",
			categories: []string{"Bogus", "BOGUS"},
			want: activityUnknown(render.Pair{Key: "unknown", Value: render.NewList(
				withNearest("category", "Bogus", every...))}),
		},
		{
			name:       "two misspellings beside a category that resolves",
			categories: []string{"Bogus", "LinksCategory", "Nope"},
			want: activityUnknown(render.Pair{Key: "unknown", Value: render.NewList(
				withNearest("category", "Bogus", every...),
				withNearest("category", "Nope", every...))}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, fault := youtrack.ListActivities("DEV-1", tc.expression, youtrack.Page{Limit: 50}, tc.categories)

			assert.Equal(t, tc.want, faultOf(t, fault))
		})
	}
}

func TestListActivitiesAsksForTheCategoriesItResolves(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		categories []string
		sent       string
	}{
		{name: "no category, which asks for every one and the commits among them", sent: activityEveryCategory},
		{
			name:       "one category in two letter cases beside another",
			categories: []string{"linkscategory", "LINKSCATEGORY", "CommentsCategory"},
			sent:       "CommentsCategory,LinksCategory",
		},
		{name: "the commits alone", categories: []string{"vcschangecategory"}, sent: "VcsChangeCategory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, `[]`)

			_, fault := activityList(t, server, new("category"), tc.categories...)

			require.Nil(t, fault)
			assert.Equal(t, []string{tc.sent}, server.Last(t).URL.Query()["categories"])
		})
	}
}

func TestListActivitiesAsksForWhatItReadsBesideWhatItPrints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		sent       string
	}{
		{name: "the category alone, beside which the moment is checked", expression: "category", sent: "category(id),timestamp"},
		{name: "the moment alone, beside which the category is read", expression: "timestamp", sent: "timestamp,category(id)"},
		{
			name:       "the field, which a custom field prints by its name in the project",
			expression: "field",
			sent:       "field(name,customField(name)),timestamp,category(id)",
		},
		{
			name:       "the values, which a custom field reads by its type and a duration by its minutes",
			expression: "added",
			sent:       "added(minutes),timestamp,category(id),field(customField(fieldType(valueType)))",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, `[]`)

			_, fault := activityList(t, server, &tc.expression, "CommentsCategory")

			require.Nil(t, fault)
			assert.Equal(t, []string{tc.sent}, server.Fields())
		})
	}
}

func TestListActivitiesReadsTheLinkTypesOnlyToPrintTheFieldOfALink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression *string
		categories []string
		sent       []string
	}{
		{name: "the default, which prints the field of every category", sent: []string{activityLinkTypesPath, activityListPath}},
		{name: "activities that print no field", expression: new("timestamp,added"), sent: []string{activityListPath}},
		{name: "activities of no link", categories: []string{"CommentsCategory"}, sent: []string{activityListPath}},
		{name: "activities of links alone", categories: []string{"LinksCategory"}, sent: []string{activityLinkTypesPath, activityListPath}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, `[]`)

			_, fault := activityList(t, server, tc.expression, tc.categories...)

			require.Nil(t, fault)
			assert.Equal(t, tc.sent, server.Paths())
		})
	}
}

func TestListActivitiesSendsNoActivitiesWhereTheLinkTypesFail(t *testing.T) {
	t.Parallel()
	server := activityServerOf(t, fake.JSON(http.StatusInternalServerError, `{}`), fake.JSON(http.StatusOK, `[]`))

	_, fault := activityList(t, server, nil)

	want := diag.Fault{Code: diag.UpstreamFailed, Details: []render.Pair{
		lastRequest(t, server),
		{Key: "upstream_status", Value: render.NewNumber("500")},
	}}
	assert.Equal(t, want, faultOf(t, fault))
	assert.Equal(t, []string{activityLinkTypesPath}, server.Paths())
}

func activityLinkTypesAsManyAsAskedFor() string {
	linkTypes := make([]string, 0, 1000)
	for at := range 1000 {
		linkTypes = append(linkTypes, fmt.Sprintf(`{"$type":"IssueLinkType","sourceToTarget":"goes with %d",`+
			`"targetToSource":"","localizedSourceToTarget":null,"localizedTargetToSource":null}`, at))
	}
	return activityArray(linkTypes...)
}

func TestListActivitiesRefusesLinkTypesItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		linkTypes string
	}{
		{
			name: "a phrase that is a number",
			linkTypes: `[{"$type":"IssueLinkType","sourceToTarget":7,"targetToSource":"",` +
				`"localizedSourceToTarget":null,"localizedTargetToSource":null}]`,
		},
		{
			name: "a translation that is a list",
			linkTypes: `[{"$type":"IssueLinkType","sourceToTarget":"leads to","targetToSource":"",` +
				`"localizedSourceToTarget":["Goes before"],"localizedTargetToSource":null}]`,
		},
		{name: "as many link types as were asked for", linkTypes: activityLinkTypesAsManyAsAskedFor()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServerOf(t, fake.JSON(http.StatusOK, tc.linkTypes), fake.JSON(http.StatusOK, `[]`))

			_, fault := activityList(t, server, nil)

			assert.Equal(t, unreadable(lastRequest(t, server), tc.linkTypes), faultOf(t, fault))
			assert.Equal(t, []string{activityLinkTypesPath}, server.Paths())
		})
	}
}

func TestListActivitiesRefusesAnActivityItCannotRead(t *testing.T) {
	t.Parallel()
	const created = `"field":null,"added":[],"removed":[]`
	tests := []struct {
		name       string
		activities string
	}{
		{
			name: "an activity newer than the one before it",
			activities: activityArray(
				activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly, created),
				activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityLate, created)),
		},
		{
			name: "a moment that is the text of one",
			activities: `[{"$type":"IssueCreatedActivityItem","timestamp":"1970-01-01T00:00:01Z",` +
				`"category":{"$type":"ActivityCategory","id":"IssueCreatedCategory"},` + created + `}]`,
		},
		{
			name:       "a category nobody asked for",
			activities: activityArray(activityJSON("VotersActivityItem", "VotersCategory", activityEarly, created)),
		},
		{
			name: "a category that is a list of one",
			activities: `[{"$type":"IssueCreatedActivityItem","timestamp":1000,` +
				`"category":[{"$type":"ActivityCategory","id":"IssueCreatedCategory"}],` + created + `}]`,
		},
		{
			name: "a category whose identifier is a number",
			activities: `[{"$type":"IssueCreatedActivityItem","timestamp":1000,` +
				`"category":{"$type":"ActivityCategory","id":7},` + created + `}]`,
		},
		{
			name:       "an object where the type of the field holds a bare value",
			activities: activityArray(activityOfField("period", `[{"$type":"DurationValue","id":"90","minutes":90}]`)),
		},
		{
			name:       "a bare value where the type of the field holds values with names of their own",
			activities: activityArray(activityOfField("state", `"Named"`)),
		},
		{
			name:       "a value of a type outside the ones ytrack models",
			activities: activityArray(activityOfField("unmodelled", `"Named"`)),
		},
		{name: "a fraction where the minutes of a period stand", activities: activityArray(activityOfField("period", `1.5`))},
		{name: "a text where the milliseconds of a moment stand", activities: activityArray(activityOfField("date and time", `"1970-01-01"`))},
		{
			name: "a change of a custom field standing for a filter that is no custom field",
			activities: activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
				`"field":{"$type":"PredefinedFilterField","name":"Label"},"added":[],"removed":[]`)),
		},
		{
			name: "a change of a custom field standing for no filter at all",
			activities: activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
				`"field":"Label","added":[],"removed":[]`)),
		},
		{
			name: "a custom field with no type of value",
			activities: activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
				`"field":{"$type":"CustomFilterField","name":"Label","customField":{"$type":"CustomField","name":"Named"}},`+
					`"added":[],"removed":[]`)),
		},
		{
			name: "a custom field with no name in the project",
			activities: activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
				`"field":{"$type":"CustomFilterField","name":"Label","customField":{"$type":"CustomField",`+
					`"fieldType":{"$type":"FieldType","valueType":"state"}}},"added":[],"removed":[]`)),
		},
		{
			name: "a link standing for its phrase rather than for a filter",
			activities: activityArray(activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
				`"field":"Comes after","added":[],"removed":[]`)),
		},
		{
			name: "a link standing for a filter with no phrase",
			activities: activityArray(activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
				`"field":{"$type":"LinkTypeFilterField"},"added":[],"removed":[]`)),
		},
		{name: "a link of a phrase no link type goes by", activities: activityArray(activityOfLink("Blocks"))},
		{name: "a link of a phrase two link types go by", activities: activityArray(activityOfLink("Goes before"))},
		{name: "a link of no phrase at all", activities: activityArray(activityOfLink(""))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, tc.activities)

			_, fault := activityList(t, server, new("field,added,removed"))

			assert.Equal(t, unreadable(lastRequest(t, server), tc.activities), faultOf(t, fault))
		})
	}
}

func TestListActivitiesChecksTheOrderOfTheMomentsWhateverItPrints(t *testing.T) {
	t.Parallel()
	const created = `"field":null`
	outOfOrder := activityArray(
		activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly, created),
		activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityLate, created))
	for _, expression := range []string{"timestamp", "category"} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, outOfOrder)

			_, fault := activityList(t, server, &expression)

			assert.Equal(t, unreadable(lastRequest(t, server), outOfOrder), faultOf(t, fault))
		})
	}
}

func TestListActivitiesChecksTheFilterOfAChangedFieldWhateverItPrints(t *testing.T) {
	t.Parallel()
	predefined := activityArray(activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityEarly,
		`"field":{"$type":"PredefinedFilterField","name":"Label","customField":{"$type":"CustomField",`+
			`"name":"Named","fieldType":{"$type":"FieldType","valueType":"state"}}},"added":[],"removed":[]`))
	for _, expression := range []string{"field", "field,added", "added"} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, predefined)

			_, fault := activityList(t, server, &expression)

			assert.Equal(t, unreadable(lastRequest(t, server), predefined), faultOf(t, fault))
		})
	}
}

func TestListActivitiesPrintsTheActivitiesAsTheyArrive(t *testing.T) {
	t.Parallel()
	const created = `"field":null`
	early := activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly, created)
	late := activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityLate, created)
	tests := []struct {
		name       string
		activities string
		want       *render.Node
	}{
		{name: "none at all", activities: `[]`, want: activitiesListed()},
		{
			name:       "the newest first",
			activities: activityArray(late, early),
			want: activitiesListed(
				render.NewMap(render.Pair{Key: "timestamp", Value: render.NewString("1970-01-01T00:00:02Z")}),
				render.NewMap(render.Pair{Key: "timestamp", Value: render.NewString("1970-01-01T00:00:01Z")})),
		},
		{
			name:       "two of one moment",
			activities: activityArray(early, early),
			want: activitiesListed(
				render.NewMap(render.Pair{Key: "timestamp", Value: render.NewString("1970-01-01T00:00:01Z")}),
				render.NewMap(render.Pair{Key: "timestamp", Value: render.NewString("1970-01-01T00:00:01Z")})),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, tc.activities)

			got, fault := activityList(t, server, new("timestamp"))

			require.Nil(t, fault)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestListActivitiesPrintsTheFieldOfAChangeByItsCategory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		activity string
		want     *render.Node
	}{
		{
			name:     "a custom field, by its name in the project and not by the label of the change",
			activity: activityOfField("state", `[]`),
			want:     render.NewString("Named"),
		},
		{
			name: "a filing, which changes no one field",
			activity: activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly,
				`"field":{"$type":"PredefinedFilterField","name":"Label"}`),
			want: render.NewNull(),
		},
		{name: "a link, by the phrase its label translates", activity: activityOfLink("Comes after"), want: render.NewString("follows")},
		{name: "a link, whatever the letter case of its label", activity: activityOfLink("comes after"), want: render.NewString("follows")},
		{name: "a link of a phrase with no translation", activity: activityOfLink("Relates to"), want: render.NewString("relates to")},
		{name: "a link of a type written alike at both ends", activity: activityOfLink("Reflects"), want: render.NewString("mirrors")},
		{
			name:     "a link of a type whose other end shares its phrase",
			activity: activityOfLink("Comes later"),
			want:     render.NewString("succeeds"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, activityArray(tc.activity))

			got, fault := activityList(t, server, new("field"))

			require.Nil(t, fault)
			assert.Equal(t, activitiesListed(render.NewMap(render.Pair{Key: "field", Value: tc.want})), got)
		})
	}
}

func TestListActivitiesPrintsTheValuesOfAChangeAsAList(t *testing.T) {
	t.Parallel()
	issue := render.NewMap(
		render.Pair{Key: "id", Value: render.NewString("1-2")},
		render.Pair{Key: "idReadable", Value: render.NewString("DEV-2")})
	const issueJSON = `{"$type":"Issue","id":"1-2","idReadable":"DEV-2"}`
	tests := []struct {
		name     string
		activity string
		added    *render.Node
		removed  *render.Node
	}{
		{
			name: "a moment put there and null taken away",
			activity: activityJSON("IssueResolvedActivityItem", "IssueResolvedCategory", activityEarly,
				`"field":null,"added":2000,"removed":null`),
			added:   texts("1970-01-01T00:00:02Z"),
			removed: activityNothing(),
		},
		{
			name: "one text at either end",
			activity: activityJSON("SimpleValueActivityItem", "SummaryCategory", activityEarly,
				`"field":null,"added":"Late","removed":"Early"`),
			added:   texts("Late"),
			removed: texts("Early"),
		},
		{
			name: "a text of lines, which stays on the line of the record",
			activity: activityJSON("TextMarkupActivityItem", "DescriptionCategory", activityEarly,
				`"field":null,"added":"First\nSecond","removed":null`),
			added:   texts("First\nSecond"),
			removed: activityNothing(),
		},
		{
			name: "a list of one issue and an empty list",
			activity: activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
				`"field":null,"added":[`+issueJSON+`],"removed":[]`),
			added:   render.NewList(issue),
			removed: activityNothing(),
		},
		{
			name: "an issue on its own rather than in a list",
			activity: activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly,
				`"field":null,"added":`+issueJSON+`,"removed":null`),
			added:   render.NewList(issue),
			removed: activityNothing(),
		},
		{
			name: "the duration of a work item, which is a period of its minutes",
			activity: activityJSON("WorkItemDurationActivityItem", "WorkItemCategory", activityEarly,
				`"field":null,"added":{"$type":"DurationValue","id":"120","minutes":120},`+
					`"removed":{"$type":"DurationValue","id":"90","minutes":90}`),
			added:   texts("PT2H"),
			removed: texts("PT1H30M"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, activityArray(tc.activity))

			got, fault := activityList(t, server, new("added(id,idReadable),removed(id,idReadable)"))

			require.Nil(t, fault)
			want := render.NewMap(render.Pair{Key: "added", Value: tc.added}, render.Pair{Key: "removed", Value: tc.removed})
			assert.Equal(t, activitiesListed(want), got)
		})
	}
}

func TestListActivitiesPrintsTheValuesOfACustomFieldByTheTypeOfTheField(t *testing.T) {
	t.Parallel()
	named := func(id, name string) *render.Node {
		return render.NewMap(
			render.Pair{Key: "id", Value: render.NewString(id)},
			render.Pair{Key: "name", Value: render.NewString(name)})
	}
	tests := []struct {
		name      string
		valueType string
		added     string
		want      *render.Node
	}{
		{
			name: "a state", valueType: "state",
			added: `[{"$type":"StateBundleElement","id":"1-1","name":"First"}]`,
			want:  render.NewList(named("1-1", "First")),
		},
		{
			name: "an enum of two values", valueType: "enum",
			added: `[{"$type":"EnumBundleElement","id":"1-1","name":"First"},{"$type":"EnumBundleElement","id":"1-2","name":"Second"}]`,
			want:  render.NewList(named("1-1", "First"), named("1-2", "Second")),
		},
		{
			name: "a group", valueType: "group",
			added: `[{"$type":"UserGroup","id":"1-1","name":"First"}]`,
			want:  render.NewList(named("1-1", "First")),
		},
		{
			name: "a user", valueType: "user",
			added: `[{"$type":"User","id":"1-1","login":"first","name":"First"}]`,
			want: render.NewList(render.NewMap(
				render.Pair{Key: "id", Value: render.NewString("1-1")},
				render.Pair{Key: "login", Value: render.NewString("first")},
				render.Pair{Key: "name", Value: render.NewString("First")})),
		},
		{name: "a period", valueType: "period", added: `6755`, want: texts("PT112H35M")},
		{name: "a period of no minutes", valueType: "period", added: `0`, want: texts("PT0M")},
		{name: "a date", valueType: "date", added: `129600000`, want: texts("1970-01-02")},
		{name: "a moment", valueType: "date and time", added: `0`, want: texts("1970-01-01T00:00:00Z")},
		{name: "a whole number", valueType: "integer", added: `10`, want: render.NewList(render.NewNumber("10"))},
		{name: "a fraction", valueType: "float", added: `1.5`, want: render.NewList(render.NewNumber("1.5"))},
		{name: "a line of text", valueType: "string", added: `"Line"`, want: texts("Line")},
		{name: "a text of lines", valueType: "text", added: `"First\nSecond"`, want: texts("First\nSecond")},
		{name: "a field emptied outright", valueType: "state", added: `null`, want: activityNothing()},
		{name: "a field nothing was put into", valueType: "state", added: `[]`, want: activityNothing()},
		{name: "a field of bare values emptied outright", valueType: "period", added: `null`, want: activityNothing()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, activityArray(activityOfField(tc.valueType, tc.added)))

			got, fault := activityList(t, server, new("added(id,login,name)"))

			require.Nil(t, fault)
			assert.Equal(t, activitiesListed(render.NewMap(render.Pair{Key: "added", Value: tc.want})), got)
		})
	}
}

func TestListActivitiesPrintsOfAValueTheNamesItsTypeDeclares(t *testing.T) {
	t.Parallel()
	comment := activityJSON("CommentActivityItem", "CommentsCategory", activityEarly,
		`"field":null,"added":[{"$type":"IssueComment","id":"1-1","text":"First","author":{"$type":"User","login":"first"}}],"removed":[]`)
	tests := []struct {
		name       string
		expression string
		activities string
		want       []*render.Node
	}{
		{
			name:       "names of the caller's own",
			expression: "added(text,author(login))",
			activities: activityArray(comment),
			want: []*render.Node{render.NewMap(render.Pair{Key: "added", Value: render.NewList(render.NewMap(
				render.Pair{Key: "text", Value: render.NewString("First")},
				render.Pair{Key: "author", Value: render.NewMap(render.Pair{Key: "login", Value: render.NewString("first")})}))})},
		},
		{
			name:       "the value with no names under it",
			expression: "added",
			activities: activityArray(comment),
			want:       []*render.Node{render.NewMap(render.Pair{Key: "added", Value: render.NewList(activityNoNames())})},
		},
		{
			name:       "a name only another type declares",
			expression: "category,added(login,idReadable)",
			activities: activityArray(
				activityJSON("CustomFieldActivityItem", "CustomFieldCategory", activityLate,
					`"field":{"$type":"CustomFilterField","name":"Label","customField":{"$type":"CustomField",`+
						`"name":"Named","fieldType":{"$type":"FieldType","valueType":"user"}}},`+
						`"added":[{"$type":"User","login":"first"}],"removed":[]`),
				activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
					`"field":null,"added":[{"$type":"Issue","idReadable":"DEV-2"}],"removed":[]`),
				comment),
			want: []*render.Node{
				render.NewMap(
					render.Pair{Key: "category", Value: render.NewString("CustomFieldCategory")},
					render.Pair{Key: "added", Value: render.NewList(render.NewMap(render.Pair{Key: "login", Value: render.NewString("first")}))}),
				render.NewMap(
					render.Pair{Key: "category", Value: render.NewString("LinksCategory")},
					render.Pair{Key: "added", Value: render.NewList(render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-2")}))}),
				render.NewMap(
					render.Pair{Key: "category", Value: render.NewString("CommentsCategory")},
					render.Pair{Key: "added", Value: render.NewList(activityNoNames())}),
			},
		},
		{
			name:       "names declared by no value that arrived",
			expression: "added(idReadable,localizedName,version),removed(text)",
			activities: activityArray(activityJSON("LinksActivityItem", "LinksCategory", activityEarly,
				`"field":null,"added":[{"$type":"Issue","idReadable":"DEV-2"}],"removed":[]`)),
			want: []*render.Node{render.NewMap(
				render.Pair{Key: "added", Value: render.NewList(render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-2")}))},
				render.Pair{Key: "removed", Value: activityNothing()})},
		},
		{
			name:       "a commit by its link, its hash, its message and its moment",
			expression: "added(urls,version,text,date)",
			activities: activityArray(activityJSON("VcsChangeActivityItem", "VcsChangeCategory", activityEarly,
				`"field":null,"added":[{"$type":"VcsChange","urls":["https://vcs.example/commit/1"],"version":"1",`+
					`"text":"First\nSecond","date":1000}],"removed":[]`)),
			want: []*render.Node{render.NewMap(render.Pair{Key: "added", Value: render.NewList(render.NewMap(
				render.Pair{Key: "urls", Value: texts("https://vcs.example/commit/1")},
				render.Pair{Key: "version", Value: render.NewString("1")},
				render.Pair{Key: "text", Value: render.NewString("First\nSecond")},
				render.Pair{Key: "date", Value: render.NewString("1970-01-01T00:00:01Z")}))})},
		},
		{
			name:       "a field below the record, which is no field of a change",
			expression: "author(savedQueries(issues(customFields(projectCustomField(field(name))))))",
			activities: activityArray(activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly,
				`"field":null,"author":{"$type":"User","savedQueries":[{"$type":"SavedQuery","issues":[{"$type":"Issue",`+
					`"customFields":[{"$type":"IssueCustomField","projectCustomField":{"$type":"ProjectCustomField",`+
					`"field":{"$type":"CustomField","name":"Named"}}}]}]}]}`)),
			want: []*render.Node{render.NewMap(render.Pair{Key: "author", Value: render.NewMap(
				render.Pair{Key: "savedQueries", Value: render.NewList(render.NewMap(
					render.Pair{Key: "issues", Value: render.NewList(render.NewMap(
						render.Pair{Key: "customFields", Value: render.NewList(render.NewMap(
							render.Pair{Key: "projectCustomField", Value: render.NewMap(
								render.Pair{Key: "field", Value: render.NewMap(
									render.Pair{Key: "name", Value: render.NewString("Named")})})}))}))}))})})},
		},
		{
			name:       "the removed of an attachment below the record, which is no value of a change",
			expression: "author(savedQueries(issues(attachments(removed))))",
			activities: activityArray(activityJSON("IssueCreatedActivityItem", "IssueCreatedCategory", activityEarly,
				`"field":null,"author":{"$type":"User","savedQueries":[{"$type":"SavedQuery","issues":[{"$type":"Issue",`+
					`"attachments":[{"$type":"IssueAttachment","removed":false}]}]}]}`)),
			want: []*render.Node{render.NewMap(render.Pair{Key: "author", Value: render.NewMap(
				render.Pair{Key: "savedQueries", Value: render.NewList(render.NewMap(
					render.Pair{Key: "issues", Value: render.NewList(render.NewMap(
						render.Pair{Key: "attachments", Value: render.NewList(render.NewMap(
							render.Pair{Key: "removed", Value: render.NewBool(false)}))}))}))})})},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := activityServer(t, tc.activities)

			got, fault := activityList(t, server, &tc.expression)

			require.Nil(t, fault)
			assert.Equal(t, activitiesListed(tc.want...), got)
		})
	}
}
