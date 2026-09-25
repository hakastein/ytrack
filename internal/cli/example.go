package cli

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/hakastein/ytrack/internal/render"
)

func example(document *render.Node) string {
	var out strings.Builder
	if err := (render.YAML{}).Render(&out, document); err != nil {
		panic("the example of a help does not render: " + err.Error())
	}
	return "  " + strings.ReplaceAll(strings.TrimSuffix(out.String(), "\n"), "\n", "\n  ")
}

const uncounted = -1

func listed(total int, truncated bool, plural string, records ...*render.Node) *render.Node {
	counted := render.NewNull()
	if total != uncounted {
		counted = render.NewNumber(json.Number(strconv.Itoa(total)))
	}
	return render.NewMap(
		render.Pair{Key: "total", Value: counted},
		render.Pair{Key: "returned", Value: render.NewNumber(json.Number(strconv.Itoa(len(records))))},
		render.Pair{Key: "truncated", Value: render.NewBool(truncated)},
		render.Pair{Key: plural, Value: render.NewList(records...)},
	)
}

func moment() *render.Node {
	return render.NewString("2026-01-01T00:00:00Z")
}

func byLogin() *render.Node {
	return render.NewMap(render.Pair{Key: "login", Value: render.NewString("user")})
}

func named(name string) *render.Node {
	return render.NewMap(render.Pair{Key: "name", Value: render.NewString(name)})
}

func sharedWith(groups ...*render.Node) *render.Node {
	return render.NewMap(render.Pair{Key: "permittedGroups", Value: render.NewList(groups...)}, render.Pair{Key: "permittedUsers", Value: render.NewList()})
}

func linksOf(phrases int) *render.Node {
	linked := func(id string) *render.Node {
		return render.NewList(render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString(id)}, render.Pair{Key: "summary", Value: render.NewString("Summary")}))
	}
	links := []render.Pair{render.FromData("depends on", linked("DEV-2"))}
	if phrases > 1 {
		links = append(links, render.FromData("subtask of", linked("DEV-3")))
	}
	return render.NewMap(
		render.Pair{Key: "total", Value: render.NewNumber(json.Number(strconv.Itoa(len(links))))},
		render.Pair{Key: "returned", Value: render.NewNumber(json.Number(strconv.Itoa(len(links))))},
		render.Pair{Key: "truncated", Value: render.NewBool(false)},
		render.Pair{Key: "links", Value: render.NewMap(links...)},
	)
}

func articleExample(withComments bool) *render.Node {
	pairs := []render.Pair{
		render.Pair{Key: "idReadable", Value: render.NewString("DEV-A-2")},
		render.Pair{Key: "summary", Value: render.NewString("Summary")},
		render.Pair{Key: "reporter", Value: byLogin()},
		render.Pair{Key: "created", Value: moment()},
		render.Pair{Key: "updated", Value: moment()},
		render.Pair{Key: "tags", Value: render.NewList(named("Tag"))},
		render.Pair{Key: "parentArticle", Value: render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-A-1")}, render.Pair{Key: "summary", Value: render.NewString("Summary")})},
		render.Pair{Key: "childArticles", Value: render.NewList()},
		render.Pair{Key: "content", Value: render.NewText("Text")},
	}
	if withComments {
		pairs = append(pairs, render.Pair{Key: "comments", Value: render.NewList(render.NewMap(render.Pair{Key: "id", Value: render.NewString("8-1")},
			render.Pair{Key: "author", Value: byLogin()}, render.Pair{Key: "created", Value: moment()}, render.Pair{Key: "text", Value: render.NewText("Text")}))})
	}
	return render.NewMap(pairs...)
}

func attachmentExample() *render.Node {
	return render.NewMap(
		render.Pair{Key: "id", Value: render.NewString("12-1")},
		render.Pair{Key: "name", Value: render.NewString("file.txt")},
		render.Pair{Key: "size", Value: render.NewNumber(json.Number(strconv.Itoa(4)))},
		render.Pair{Key: "mimeType", Value: render.NewString("text/plain")},
		render.Pair{Key: "url", Value: render.NewString("https://youtrack.example.com/api/files/12-1?sign=SIGNATURE")},
	)
}

func commentExample(updated *render.Node) *render.Node {
	return render.NewMap(
		render.Pair{Key: "id", Value: render.NewString("7-1")},
		render.Pair{Key: "author", Value: byLogin()},
		render.Pair{Key: "created", Value: moment()},
		render.Pair{Key: "updated", Value: updated},
		render.Pair{Key: "text", Value: render.NewText("Text")},
	)
}

func issueExample(withComments bool) *render.Node {
	pairs := []render.Pair{
		render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
		render.Pair{Key: "summary", Value: render.NewString("Summary")},
		render.Pair{Key: "reporter", Value: byLogin()},
		render.Pair{Key: "created", Value: moment()},
		render.Pair{Key: "updated", Value: moment()},
		render.Pair{Key: "resolved", Value: render.NewNull()},
		render.Pair{Key: "tags", Value: render.NewList(named("Tag"))},
		render.Pair{Key: "customFields", Value: render.NewMap(
			render.FromData("State", render.NewString("Open")),
			render.FromData("Assignee", render.NewString("user")),
			render.FromData("Fix versions", render.NewList(render.NewString("1.0"))),
		)},
		render.Pair{Key: "links", Value: render.NewMap(render.FromData("depends on", render.NewList(
			render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-2")}, render.Pair{Key: "summary", Value: render.NewString("Summary")}))))},
		render.Pair{Key: "description", Value: render.NewText("Text")},
	}
	if withComments {
		pairs = append(pairs, render.Pair{Key: "comments", Value: render.NewList(render.NewMap(render.Pair{Key: "id", Value: render.NewString("7-1")},
			render.Pair{Key: "author", Value: byLogin()}, render.Pair{Key: "created", Value: moment()}, render.Pair{Key: "text", Value: render.NewText("Text")}))})
	}
	return render.NewMap(pairs...)
}

func fieldOf(after ...render.Pair) *render.Node {
	return render.NewMap(append([]render.Pair{
		render.Pair{Key: "field", Value: render.NewMap(
			render.Pair{Key: "name", Value: render.NewString("State")},
			render.Pair{Key: "localizedName", Value: render.NewNull()},
			render.Pair{Key: "fieldType", Value: render.NewMap(render.Pair{Key: "valueType", Value: render.NewString("state")}, render.Pair{Key: "isMultiValue", Value: render.NewBool(false)})},
		)},
		render.Pair{Key: "canBeEmpty", Value: render.NewBool(false)},
	}, after...)...)
}

func workItemExample() *render.Node {
	return render.NewMap(
		render.Pair{Key: "id", Value: render.NewString("150-1")},
		render.Pair{Key: "duration", Value: render.NewString("PT1H30M")},
		render.Pair{Key: "type", Value: named("Type")},
		render.Pair{Key: "attributes", Value: render.NewMap(render.FromData("Attribute", render.NewString("Value")))},
		render.Pair{Key: "author", Value: byLogin()},
		render.Pair{Key: "date", Value: moment()},
		render.Pair{Key: "issue", Value: render.NewMap(
			render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
			render.Pair{Key: "customFields", Value: render.NewMap(render.FromData("Spent time", render.NewString("PT1H30M")))},
		)},
		render.Pair{Key: "text", Value: render.NewText("Text")},
	)
}
