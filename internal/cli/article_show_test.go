package cli_test

import ()

const articleShowFields = "idReadable,summary,reporter(login),created,updated,tags(name)," +
	"parentArticle(idReadable,summary),childArticles(idReadable,summary),content"

func articleWithAChild() string {
	return `{"summary":"Title","$type":"Article","id":"177-1",` +
		`"childArticles":[{"$type":"Article","idReadable":"DEV-A-2","summary":"Child"}],` +
		`"content":"First line\nsecond line","updated":1787942509046,"comments":[],` +
		`"tags":[{"name":"Tag","$type":"Tag"}],"reporter":{"$type":"User","login":"author"},` +
		`"created":1789035410875,"idReadable":"DEV-A-1","parentArticle":null}`
}

const printedArticleWithAChild = `idReadable: "DEV-A-1"
summary: "Title"
reporter:
  login: "author"
created: "2026-09-10T10:16:50.875Z"
updated: "2026-08-28T18:41:49.046Z"
tags:
  - {name: "Tag"}
parentArticle: null
childArticles:
  - {idReadable: "DEV-A-2", summary: "Child"}
content: |-
  First line
  second line
comments: []
`
