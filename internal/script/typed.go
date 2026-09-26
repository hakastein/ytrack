package script

import (
	"github.com/hakastein/go-youtrack"
)

func pairs(keys []string, values ...*youtrack.Node) *youtrack.Node {
	held := make([]youtrack.Pair, 0, len(keys))
	for i, key := range keys {
		held = append(held, youtrack.Pair{Key: key, Value: values[i]})
	}
	return youtrack.NewMap(held...)
}

func str(value string) *youtrack.Node {
	return youtrack.NewString(value)
}

func list[T any](items []T, node func(T) *youtrack.Node) *youtrack.Node {
	nodes := make([]*youtrack.Node, 0, len(items))
	for _, item := range items {
		nodes = append(nodes, node(item))
	}
	return youtrack.NewList(nodes...)
}

func issueNode(issue *youtrack.Issue) *youtrack.Node {
	return pairs([]string{"id", "idReadable", "summary", "description", "project", "fields", "links"},
		str(issue.ID), str(issue.IDReadable), str(issue.Summary), str(issue.Description), projectNode(issue.Project),
		list(issue.Fields, fieldNode), list(issue.Links, linkNode))
}

func projectNode(project youtrack.Project) *youtrack.Node {
	return pairs([]string{"id", "shortName", "name"}, str(project.ID), str(project.ShortName), str(project.Name))
}

func fieldNode(field youtrack.Field) *youtrack.Node {
	return pairs([]string{"name", "localizedName", "type", "values"},
		str(field.Name), str(field.LocalizedName), fieldTypeNode(field.Type), list(field.Values, valueNode))
}

func fieldTypeNode(kind youtrack.FieldType) *youtrack.Node {
	return pairs([]string{"valueType", "isMultiValue"}, str(string(kind.ValueType)), youtrack.NewBool(kind.Multi))
}

func valueNode(value youtrack.Value) *youtrack.Node {
	return pairs([]string{"id", "text", "localizedName"}, str(value.ID), str(value.Text), str(value.LocalizedName))
}

func linkNode(link youtrack.Link) *youtrack.Node {
	linkType := pairs([]string{"name", "sourceToTarget", "targetToSource"},
		str(link.Type.Name), str(link.Type.SourceToTarget), str(link.Type.TargetToSource))
	return pairs([]string{"direction", "type", "issues"}, str(string(link.Direction)), linkType,
		list(link.Issues, func(ref youtrack.IssueRef) *youtrack.Node {
			return pairs([]string{"id", "idReadable"}, str(ref.ID), str(ref.IDReadable))
		}))
}

func metadataNode(metadata *youtrack.Metadata) *youtrack.Node {
	return pairs([]string{"fields", "fromCache"}, list(metadata.Fields, projectFieldNode),
		youtrack.NewBool(metadata.FromCache))
}

func projectFieldNode(field youtrack.ProjectField) *youtrack.Node {
	return pairs([]string{"id", "name", "localizedName", "type", "canBeEmpty"}, str(field.ID), str(field.Name),
		str(field.LocalizedName), fieldTypeNode(field.Type), youtrack.NewBool(field.CanBeEmpty))
}

func bundleNode(bundle *youtrack.Bundle) *youtrack.Node {
	return pairs([]string{"field", "values"}, projectFieldNode(bundle.Field),
		list(bundle.Values, func(value youtrack.BundleValue) *youtrack.Node {
			return pairs([]string{"id", "name", "archived"}, str(value.ID), str(value.Name),
				youtrack.NewBool(value.Archived))
		}))
}

func userNode(user youtrack.User) *youtrack.Node {
	return pairs([]string{"id", "login", "fullName", "email", "banned"}, str(user.ID), str(user.Login),
		str(user.FullName), str(user.Email), youtrack.NewBool(user.Banned))
}
