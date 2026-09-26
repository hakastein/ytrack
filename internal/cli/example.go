package cli

import (
	"github.com/hakastein/go-youtrack"

	"encoding/json"
	"strconv"
	"strings"

	"github.com/hakastein/ytrack/internal/render"
)

func example(document *youtrack.Node) string {
	var out strings.Builder
	if err := (render.YAML{}).Render(&out, document); err != nil {
		panic("the example of a help does not render: " + err.Error())
	}
	return "  " + strings.ReplaceAll(strings.TrimSuffix(out.String(), "\n"), "\n", "\n  ")
}

const uncounted = -1

func listed(total int, truncated bool, plural string, records ...*youtrack.Node) *youtrack.Node {
	counted := youtrack.NewNull()
	if total != uncounted {
		counted = youtrack.NewNumber(json.Number(strconv.Itoa(total)))
	}
	return youtrack.NewMap(
		youtrack.Pair{Key: "total", Value: counted},
		youtrack.Pair{Key: "returned", Value: youtrack.NewNumber(json.Number(strconv.Itoa(len(records))))},
		youtrack.Pair{Key: "truncated", Value: youtrack.NewBool(truncated)},
		youtrack.Pair{Key: plural, Value: youtrack.NewList(records...)},
	)
}

func moment() *youtrack.Node {
	return youtrack.NewString("2026-01-01T00:00:00Z")
}

func byLogin() *youtrack.Node {
	return youtrack.NewMap(youtrack.Pair{Key: "login", Value: youtrack.NewString("user")})
}

func named(name string) *youtrack.Node {
	return youtrack.NewMap(youtrack.Pair{Key: "name", Value: youtrack.NewString(name)})
}

func sharedWith(groups ...*youtrack.Node) *youtrack.Node {
	return youtrack.NewMap(youtrack.Pair{Key: "permittedGroups", Value: youtrack.NewList(groups...)}, youtrack.Pair{Key: "permittedUsers", Value: youtrack.NewList()})
}

func linksOf(phrases int) *youtrack.Node {
	linked := func(id string) *youtrack.Node {
		return youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString(id)}, youtrack.Pair{Key: "summary", Value: youtrack.NewString("Summary")}))
	}
	links := []youtrack.Pair{youtrack.DataPair("depends on", linked("DEV-2"))}
	if phrases > 1 {
		links = append(links, youtrack.DataPair("subtask of", linked("DEV-3")))
	}
	return youtrack.NewMap(
		youtrack.Pair{Key: "total", Value: youtrack.NewNumber(json.Number(strconv.Itoa(len(links))))},
		youtrack.Pair{Key: "returned", Value: youtrack.NewNumber(json.Number(strconv.Itoa(len(links))))},
		youtrack.Pair{Key: "truncated", Value: youtrack.NewBool(false)},
		youtrack.Pair{Key: "links", Value: youtrack.NewMap(links...)},
	)
}

func articleExample(withComments bool) *youtrack.Node {
	pairs := []youtrack.Pair{
		youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-A-2")},
		youtrack.Pair{Key: "summary", Value: youtrack.NewString("Summary")},
		youtrack.Pair{Key: "reporter", Value: byLogin()},
		youtrack.Pair{Key: "created", Value: moment()},
		youtrack.Pair{Key: "updated", Value: moment()},
		youtrack.Pair{Key: "tags", Value: youtrack.NewList(named("Tag"))},
		youtrack.Pair{Key: "parentArticle", Value: youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-A-1")}, youtrack.Pair{Key: "summary", Value: youtrack.NewString("Summary")})},
		youtrack.Pair{Key: "childArticles", Value: youtrack.NewList()},
		youtrack.Pair{Key: "content", Value: youtrack.NewText("Text")},
	}
	if withComments {
		pairs = append(pairs, youtrack.Pair{Key: "comments", Value: youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString("8-1")},
			youtrack.Pair{Key: "author", Value: byLogin()}, youtrack.Pair{Key: "created", Value: moment()}, youtrack.Pair{Key: "text", Value: youtrack.NewText("Text")}))})
	}
	return youtrack.NewMap(pairs...)
}

func attachmentExample() *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "id", Value: youtrack.NewString("12-1")},
		youtrack.Pair{Key: "name", Value: youtrack.NewString("file.txt")},
		youtrack.Pair{Key: "size", Value: youtrack.NewNumber(json.Number(strconv.Itoa(4)))},
		youtrack.Pair{Key: "mimeType", Value: youtrack.NewString("text/plain")},
		youtrack.Pair{Key: "url", Value: youtrack.NewString("https://youtrack.example.com/api/files/12-1?sign=SIGNATURE")},
	)
}

func commentExample(updated *youtrack.Node) *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "id", Value: youtrack.NewString("7-1")},
		youtrack.Pair{Key: "author", Value: byLogin()},
		youtrack.Pair{Key: "created", Value: moment()},
		youtrack.Pair{Key: "updated", Value: updated},
		youtrack.Pair{Key: "text", Value: youtrack.NewText("Text")},
	)
}

func issueExample(withComments bool) *youtrack.Node {
	pairs := []youtrack.Pair{
		youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")},
		youtrack.Pair{Key: "summary", Value: youtrack.NewString("Summary")},
		youtrack.Pair{Key: "reporter", Value: byLogin()},
		youtrack.Pair{Key: "created", Value: moment()},
		youtrack.Pair{Key: "updated", Value: moment()},
		youtrack.Pair{Key: "resolved", Value: youtrack.NewNull()},
		youtrack.Pair{Key: "tags", Value: youtrack.NewList(named("Tag"))},
		youtrack.Pair{Key: "customFields", Value: youtrack.NewMap(
			youtrack.DataPair("State", youtrack.NewString("Open")),
			youtrack.DataPair("Assignee", youtrack.NewString("user")),
			youtrack.DataPair("Fix versions", youtrack.NewList(youtrack.NewString("1.0"))),
		)},
		youtrack.Pair{Key: "links", Value: youtrack.NewMap(youtrack.DataPair("depends on", youtrack.NewList(
			youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-2")}, youtrack.Pair{Key: "summary", Value: youtrack.NewString("Summary")}))))},
		youtrack.Pair{Key: "description", Value: youtrack.NewText("Text")},
	}
	if withComments {
		pairs = append(pairs, youtrack.Pair{Key: "comments", Value: youtrack.NewList(youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString("7-1")},
			youtrack.Pair{Key: "author", Value: byLogin()}, youtrack.Pair{Key: "created", Value: moment()}, youtrack.Pair{Key: "text", Value: youtrack.NewText("Text")}))})
	}
	return youtrack.NewMap(pairs...)
}

func fieldOf(after ...youtrack.Pair) *youtrack.Node {
	return youtrack.NewMap(append([]youtrack.Pair{
		youtrack.Pair{Key: "field", Value: youtrack.NewMap(
			youtrack.Pair{Key: "name", Value: youtrack.NewString("State")},
			youtrack.Pair{Key: "localizedName", Value: youtrack.NewNull()},
			youtrack.Pair{Key: "fieldType", Value: youtrack.NewMap(youtrack.Pair{Key: "valueType", Value: youtrack.NewString("state")}, youtrack.Pair{Key: "isMultiValue", Value: youtrack.NewBool(false)})},
		)},
		youtrack.Pair{Key: "canBeEmpty", Value: youtrack.NewBool(false)},
	}, after...)...)
}

func workItemExample() *youtrack.Node {
	return youtrack.NewMap(
		youtrack.Pair{Key: "id", Value: youtrack.NewString("150-1")},
		youtrack.Pair{Key: "duration", Value: youtrack.NewString("PT1H30M")},
		youtrack.Pair{Key: "type", Value: named("Type")},
		youtrack.Pair{Key: "attributes", Value: youtrack.NewMap(youtrack.DataPair("Attribute", youtrack.NewString("Value")))},
		youtrack.Pair{Key: "author", Value: byLogin()},
		youtrack.Pair{Key: "date", Value: moment()},
		youtrack.Pair{Key: "issue", Value: youtrack.NewMap(
			youtrack.Pair{Key: "idReadable", Value: youtrack.NewString("DEV-1")},
			youtrack.Pair{Key: "customFields", Value: youtrack.NewMap(youtrack.DataPair("Spent time", youtrack.NewString("PT1H30M")))},
		)},
		youtrack.Pair{Key: "text", Value: youtrack.NewText("Text")},
	)
}
