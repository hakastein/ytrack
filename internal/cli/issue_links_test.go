package cli_test

import (
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const linkParts = "direction,linkType(sourceToTarget,targetToSource)"

type receivedLink struct {
	direction      string
	sourceToTarget string
	targetToSource string
	issues         []string
}

func (l receivedLink) sent(id string) string {
	return `{"$type":"IssueLink","id":` + strconv.Quote(id) +
		`,"direction":` + strconv.Quote(l.direction) +
		`,"linkType":{"$type":"IssueLinkType","name":"Type","sourceToTarget":` + strconv.Quote(l.sourceToTarget) +
		`,"targetToSource":` + strconv.Quote(l.targetToSource) +
		`},"issues":[` + strings.Join(l.issues, ",") + `]}`
}

func receivedLinks(links ...receivedLink) string {
	sent := make([]string, 0, len(links))
	for i, link := range links {
		sent = append(sent, link.sent("163-"+strconv.Itoa(i)))
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func targetIssue(id, summary string) string {
	return `{"$type":"Issue","idReadable":` + strconv.Quote(id) + `,"summary":` + strconv.Quote(summary) + `}`
}

func keysOf(node *yaml.Node) []string {
	keys := []string{}
	for pair := range slices.Chunk(node.Content, 2) {
		keys = append(keys, pair[0].Value)
	}
	return keys
}
