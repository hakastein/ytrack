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

func emptyIssueLinks() []receivedLink {
	return []receivedLink{
		{direction: "BOTH", sourceToTarget: "relates to"},
		{direction: "OUTWARD", sourceToTarget: "is required for", targetToSource: "depends on"},
		{direction: "INWARD", sourceToTarget: "is required for", targetToSource: "depends on"},
		{direction: "OUTWARD", sourceToTarget: "is duplicated by", targetToSource: "duplicates"},
		{direction: "INWARD", sourceToTarget: "is duplicated by", targetToSource: "duplicates"},
		{direction: "OUTWARD", sourceToTarget: "parent for", targetToSource: "subtask of"},
		{direction: "INWARD", sourceToTarget: "parent for", targetToSource: "subtask of"},
		{direction: "OUTWARD", sourceToTarget: "Скопирована в", targetToSource: "Копия"},
		{direction: "INWARD", sourceToTarget: "Скопирована в", targetToSource: "Копия"},
	}
}

func keysOf(node *yaml.Node) []string {
	keys := []string{}
	for pair := range slices.Chunk(node.Content, 2) {
		keys = append(keys, pair[0].Value)
	}
	return keys
}
