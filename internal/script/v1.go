package script

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
)

func v1(e *engine) *goja.Object {
	api := e.vm.NewObject()
	services := map[string]map[string]function{
		"activities":   activities(),
		"articles":     articles(),
		"attachments":  attachments(),
		"comments":     comments(),
		"customFields": customFields(),
		"issues":       issues(e.host.Warn),
		"links":        links(),
		"projects":     projects(),
		"tags":         tags(),
		"users":        users(),
		"workItems":    workItems(),
	}
	for _, name := range slices.Sorted(maps.Keys(services)) {
		e.define(api, name, e.service(name, services[name]))
	}
	e.define(api, "fail", e.vm.ToValue(e.fail))
	e.define(api, "warn", e.vm.ToValue(e.warn))
	if err := api.DefineAccessorProperty("address", e.vm.ToValue(e.address), nil, goja.FLAG_FALSE, goja.FLAG_TRUE); err != nil {
		panic(err)
	}
	return api
}

func required(name string, kind paramKind) param {
	return param{name: name, kind: kind, required: true}
}

func optional(name string, kind paramKind) param {
	return param{name: name, kind: kind}
}

// The answer of a function must not change when ytrack changes the default fields of a command.
func fieldsParam() param {
	return param{name: "fields", kind: textParam, required: true, refuse: func(value any) string {
		expression := strings.TrimSpace(value.(string))
		if expression == "" || strings.HasPrefix(expression, "+") {
			return "names the default fields, and a function has no default: it takes the whole expression"
		}
		return ""
	}}
}

func paged(params ...param) []param {
	return append(params, fieldsParam(), optional("limit", wholeParam), optional("skip", wholeParam))
}

// The SDK reads a limit of 0 as its own default page.
func pageOf(opts options) (youtrack.Page, *diag.Fault) {
	if opts.given("limit") && opts.int("limit") < 1 {
		message := fmt.Sprintf("limit %d: a page holds at least one record", opts.int("limit"))
		return youtrack.Page{}, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
	}
	return youtrack.Page{Limit: opts.int("limit"), Skip: opts.int("skip")}, nil
}

type reading func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error)

func reply(f reading) binder {
	return func(opts options) (call, *diag.Fault) {
		return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return f(ctx, c, opts)
		}, nil
	}
}

type pageReading func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error)

func replyPage(f pageReading) binder {
	return func(opts options) (call, *diag.Fault) {
		page, fault := pageOf(opts)
		if fault != nil {
			return nil, fault
		}
		return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return f(ctx, c, opts, page)
		}, nil
	}
}

func written(opts options) *youtrack.WriteOptions {
	return &youtrack.WriteOptions{Fields: opts.string("fields")}
}

func projects() map[string]function {
	return map[string]function{
		"show": {
			params: []param{required("project", textParam), fieldsParam()},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Projects.Show(ctx, opts.string("project"), &youtrack.ShowProjectOptions{Fields: opts.string("fields")})
			}),
		},
		"list": {
			params: paged(),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				return c.Projects.List(ctx, &youtrack.ListProjectsOptions{Fields: opts.string("fields"), Page: page})
			}),
		},
	}
}

func users() map[string]function {
	return map[string]function{
		"show": {
			params: []param{required("login", textParam), fieldsParam()},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Users.Show(ctx, opts.string("login"), &youtrack.ShowUserOptions{Fields: opts.string("fields")})
			}),
		},
		"list": {
			params: paged(required("query", textParam)),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				return c.Users.List(ctx, opts.string("query"), &youtrack.ListUsersOptions{Fields: opts.string("fields"), Page: page})
			}),
		},
		"me": {
			bind: reply(func(ctx context.Context, c *youtrack.Client, _ options) (*youtrack.Node, error) {
				user, err := c.Users.Me(ctx)
				if err != nil {
					return nil, err
				}
				return userNode(*user), nil
			}),
		},
		"find": {
			params: []param{required("query", textParam), optional("limit", wholeParam)},
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				found, err := c.Users.Find(ctx, opts.string("query"), page.Limit)
				if err != nil {
					return nil, err
				}
				return list(found, userNode), nil
			}),
		},
	}
}

func customFields() map[string]function {
	metadata := func(read func(c *youtrack.Client) func(context.Context, string) (*youtrack.Metadata, error)) function {
		return function{
			params: []param{required("project", textParam)},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				held, err := read(c)(ctx, opts.string("project"))
				if err != nil {
					return nil, err
				}
				return metadataNode(held), nil
			}),
		}
	}
	return map[string]function{
		"list": {
			params: []param{required("project", textParam), fieldsParam()},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Fields.List(ctx, opts.string("project"), &youtrack.ListFieldsOptions{Fields: opts.string("fields")})
			}),
		},
		"show": {
			params: []param{required("project", textParam), required("name", textParam), fieldsParam()},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				show := &youtrack.ShowFieldOptions{Fields: opts.string("fields")}
				return c.Fields.Show(ctx, opts.string("project"), opts.string("name"), show)
			}),
		},
		"metadata": metadata(func(c *youtrack.Client) func(context.Context, string) (*youtrack.Metadata, error) {
			return c.Fields.Metadata
		}),
		"readMetadata": metadata(func(c *youtrack.Client) func(context.Context, string) (*youtrack.Metadata, error) {
			return c.Fields.ReadMetadata
		}),
		"bundle": {
			params: []param{required("project", textParam), required("name", textParam)},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				bundle, err := c.Fields.Bundle(ctx, opts.string("project"), opts.string("name"))
				if err != nil {
					return nil, err
				}
				return bundleNode(bundle), nil
			}),
		},
	}
}

func issues(warn func(*youtrack.Warning)) map[string]function {
	return map[string]function{
		"show": {
			params: []param{required("id", textParam), fieldsParam(), optional("comments", commentsParam)},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				show := &youtrack.ShowIssueOptions{Fields: opts.string("fields"), Comments: opts.comments("comments")}
				return c.Issues.Show(ctx, opts.string("id"), show)
			}),
		},
		"list": {
			params: paged(required("query", textParam)),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				list := &youtrack.ListIssuesOptions{Fields: opts.string("fields"), Page: page, Warn: warn}
				return c.Issues.List(ctx, opts.string("query"), list)
			}),
		},
		"create": {
			params: []param{required("project", textParam), required("summary", textParam),
				optional("description", textParam), optional("customFields", fieldValuesParam), fieldsParam()},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				in := &youtrack.IssueInput{Summary: opts.string("summary"), Description: opts.string("description"),
					Fields: opts.fieldWrites("customFields")}
				return c.Issues.Create(ctx, opts.string("project"), in, written(opts))
			}),
		},
		"update": {
			params: []param{required("id", textParam), optional("summary", textParam),
				optional("description", clearableParam), optional("customFields", fieldValuesParam), fieldsParam()},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				in := &youtrack.IssueUpdate{Summary: opts.optional("summary"), Description: opts.optional("description"),
					ClearDescription: opts.cleared("description"), Fields: opts.fieldWrites("customFields")}
				return c.Issues.Update(ctx, opts.string("id"), in, written(opts))
			}),
		},
		"delete": {
			params: []param{required("id", textParam)},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Issues.Delete(ctx, opts.string("id"))
			}),
		},
		"get": {
			params: []param{required("id", textParam)},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				issue, err := c.Issues.Get(ctx, opts.string("id"))
				if err != nil {
					return nil, err
				}
				return issueNode(issue), nil
			}),
		},
		"writeFields": {
			params: []param{required("id", textParam), required("customFields", fieldValuesParam)},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				issue, err := c.Issues.WriteFields(ctx, opts.string("id"), opts.fieldWrites("customFields"))
				if err != nil {
					return nil, err
				}
				return issueNode(issue), nil
			}),
		},
	}
}

func articles() map[string]function {
	return map[string]function{
		"show": {
			params: []param{required("id", textParam), fieldsParam(), optional("comments", commentsParam)},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				show := &youtrack.ShowArticleOptions{Fields: opts.string("fields"), Comments: opts.comments("comments")}
				return c.Articles.Show(ctx, opts.string("id"), show)
			}),
		},
		"list": {
			params: paged(required("query", textParam)),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				return c.Articles.List(ctx, opts.string("query"), &youtrack.ListArticlesOptions{Fields: opts.string("fields"), Page: page})
			}),
		},
		"children": {
			params: paged(required("parent", textParam)),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				return c.Articles.Children(ctx, opts.string("parent"), &youtrack.ListArticlesOptions{Fields: opts.string("fields"), Page: page})
			}),
		},
		"create": {
			params: []param{required("project", textParam), required("summary", textParam),
				optional("content", textParam), optional("parent", textParam), fieldsParam()},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				in := &youtrack.ArticleInput{Summary: opts.string("summary"), Content: opts.string("content"),
					Parent: opts.string("parent")}
				return c.Articles.Create(ctx, opts.string("project"), in, written(opts))
			}),
		},
		"update": {
			params: []param{required("id", textParam), optional("summary", textParam),
				optional("content", clearableParam), optional("parent", clearableParam), fieldsParam()},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				in := &youtrack.ArticleUpdate{Summary: opts.optional("summary"), Content: opts.optional("content"),
					Parent: opts.optional("parent"), ClearContent: opts.cleared("content"),
					ClearParent: opts.cleared("parent")}
				return c.Articles.Update(ctx, opts.string("id"), in, written(opts))
			}),
		},
		"delete": {
			params: []param{required("id", textParam)},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Articles.Delete(ctx, opts.string("id"))
			}),
		},
	}
}

func comments() map[string]function {
	return map[string]function{
		"list": {
			params: paged(required("owner", textParam)),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				return c.Comments.List(ctx, opts.string("owner"), &youtrack.ListCommentsOptions{Fields: opts.string("fields"), Page: page})
			}),
		},
		"create": {
			params: []param{required("owner", textParam), required("text", textParam), fieldsParam()},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Comments.Create(ctx, opts.string("owner"), opts.string("text"), written(opts))
			}),
		},
		"update": {
			params: []param{required("owner", textParam), required("id", textParam), required("text", textParam),
				fieldsParam()},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Comments.Update(ctx, opts.string("owner"), opts.string("id"), opts.string("text"), written(opts))
			}),
		},
		"delete": {
			params: []param{required("owner", textParam), required("id", textParam)},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Comments.Delete(ctx, opts.string("owner"), opts.string("id"))
			}),
		},
	}
}

func attachments() map[string]function {
	return map[string]function{
		"list": {
			params: paged(required("owner", textParam)),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				return c.Attachments.List(ctx, opts.string("owner"), &youtrack.ListAttachmentsOptions{Fields: opts.string("fields"), Page: page})
			}),
		},
		"create": {
			params: []param{required("owner", textParam), optional("path", textParam), optional("name", textParam),
				optional("content", bytesParam), fieldsParam()},
			writes: true,
			refuse: func(opts options) string {
				switch {
				case opts.given("path") == opts.given("content"):
					return "takes either path or content"
				case opts.given("content") != opts.given("name"):
					return "takes name with content and only with it"
				}
				return ""
			},
			bind: func(opts options) (call, *diag.Fault) {
				if opts.given("content") {
					return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
						sent := youtrack.File{Name: opts.string("name"), Content: bytes.NewReader(opts.bytes("content"))}
						return c.Attachments.Create(ctx, opts.string("owner"), sent, written(opts))
					}, nil
				}
				path := opts.string("path")
				// Checked before the login is looked up, and opened again for the upload.
				checked, fault := openLocalFile(path)
				if fault != nil {
					return nil, fault
				}
				_ = checked.Close()
				return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
					file, fault := openLocalFile(path)
					if fault != nil {
						return nil, fault
					}
					defer file.Close()
					return c.Attachments.Create(ctx, opts.string("owner"), youtrack.File{Name: filepath.Base(path), Content: file}, written(opts))
				}, nil
			},
		},
		"delete": {
			params: []param{required("owner", textParam), required("id", textParam)},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Attachments.Delete(ctx, opts.string("owner"), opts.string("id"))
			}),
		},
	}
}

func links() map[string]function {
	ends := []param{required("issue", textParam), required("phrase", textParam), required("target", textParam)}
	return map[string]function{
		"list": {
			params: []param{required("issue", textParam), fieldsParam()},
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Links.List(ctx, opts.string("issue"), &youtrack.ListLinksOptions{Fields: opts.string("fields")})
			}),
		},
		"add": {
			params: append(slices.Clone(ends), fieldsParam()),
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Links.Add(ctx, opts.string("issue"), opts.string("phrase"), opts.string("target"), written(opts))
			}),
		},
		"remove": {
			params: ends,
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Links.Remove(ctx, opts.string("issue"), opts.string("phrase"), opts.string("target"))
			}),
		},
	}
}

func tags() map[string]function {
	byName := func(own ...param) []param {
		return append(own, required("name", textParam), optional("ownedBy", textParam))
	}
	tagOptions := func(opts options) *youtrack.TagOptions {
		return &youtrack.TagOptions{OwnedBy: opts.string("ownedBy")}
	}
	return map[string]function{
		"list": {
			params: paged(),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				return c.Tags.List(ctx, &youtrack.ListTagsOptions{Fields: opts.string("fields"), Page: page})
			}),
		},
		"create": {
			params: []param{required("name", textParam), optional("visibleFor", textsParam),
				optional("updatableBy", textsParam), optional("taggableBy", textsParam), fieldsParam()},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				shared := youtrack.TagSharing{VisibleFor: opts.strings("visibleFor"),
					UpdatableBy: opts.strings("updatableBy"), TaggableBy: opts.strings("taggableBy")}
				return c.Tags.Create(ctx, opts.string("name"), shared, written(opts))
			}),
		},
		"delete": {
			params: byName(),
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Tags.Delete(ctx, opts.string("name"), tagOptions(opts))
			}),
		},
		"add": {
			params: byName(required("id", textParam)),
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Tags.Add(ctx, opts.string("id"), opts.string("name"), tagOptions(opts))
			}),
		},
		"remove": {
			params: byName(required("id", textParam)),
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.Tags.Remove(ctx, opts.string("id"), opts.string("name"), tagOptions(opts))
			}),
		},
	}
}

func workItems() map[string]function {
	return map[string]function{
		"list": {
			params: paged(required("issue", textParam)),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				return c.WorkItems.List(ctx, opts.string("issue"), &youtrack.ListWorkItemsOptions{Fields: opts.string("fields"), Page: page})
			}),
		},
		"create": {
			params: []param{required("issue", textParam), required("minutes", wholeParam), optional("date", textParam),
				optional("type", textParam), optional("text", textParam), optional("attributes", attributesParam),
				fieldsParam()},
			writes: true,
			bind: func(opts options) (call, *diag.Fault) {
				spent, fault := minutesOf(opts.int("minutes"))
				if fault != nil {
					return nil, fault
				}
				in := &youtrack.WorkItemInput{Duration: spent, Date: opts.string("date"), Text: opts.string("text"),
					Type: opts.string("type"), Attributes: opts.attributeWrites("attributes")}
				return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
					return c.WorkItems.Create(ctx, opts.string("issue"), in, written(opts))
				}, nil
			},
		},
		"update": {
			params: []param{required("issue", textParam), required("id", textParam), optional("minutes", wholeParam),
				optional("date", textParam), optional("type", clearableParam), optional("text", clearableParam),
				optional("attributes", attributesParam), fieldsParam()},
			writes: true,
			bind: func(opts options) (call, *diag.Fault) {
				in := &youtrack.WorkItemUpdate{Date: opts.optional("date"), Text: opts.optional("text"),
					Type: opts.optional("type"), ClearText: opts.cleared("text"), ClearType: opts.cleared("type"),
					Attributes: opts.attributeWrites("attributes")}
				if opts.given("minutes") {
					spent, fault := minutesOf(opts.int("minutes"))
					if fault != nil {
						return nil, fault
					}
					in.Duration = &spent
				}
				return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
					return c.WorkItems.Update(ctx, opts.string("issue"), opts.string("id"), in, written(opts))
				}, nil
			},
		},
		"delete": {
			params: []param{required("issue", textParam), required("id", textParam)},
			writes: true,
			bind: reply(func(ctx context.Context, c *youtrack.Client, opts options) (*youtrack.Node, error) {
				return c.WorkItems.Delete(ctx, opts.string("issue"), opts.string("id"))
			}),
		},
	}
}

func minutesOf(minutes int) (time.Duration, *diag.Fault) {
	if minutes < 0 || int64(minutes) > longestWorkItem {
		message := fmt.Sprintf("minutes %d: a work item is written for 0 to %d minutes", minutes, longestWorkItem)
		return 0, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
	}
	return time.Duration(minutes) * time.Minute, nil
}

func activities() map[string]function {
	return map[string]function{
		"list": {
			params: paged(required("issue", textParam), optional("categories", textsParam)),
			bind: replyPage(func(ctx context.Context, c *youtrack.Client, opts options, page youtrack.Page) (*youtrack.Node, error) {
				list := &youtrack.ListActivitiesOptions{Fields: opts.string("fields"), Page: page,
					Categories: opts.strings("categories")}
				return c.Activities.List(ctx, opts.string("issue"), list)
			}),
		},
	}
}
