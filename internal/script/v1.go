package script

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dop251/goja"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
)

func v1(e *engine) *goja.Object {
	api := e.vm.NewObject()
	entities := map[string]map[string]function{
		"activity":   activityFunctions(),
		"article":    articleFunctions(),
		"attachment": attachmentFunctions(),
		"comment":    commentFunctions(),
		"field":      fieldFunctions(),
		"issue":      issueFunctions(e.host.Warn),
		"link":       linkFunctions(),
		"project":    projectFunctions(),
		"tag":        tagFunctions(),
		"time":       timeFunctions(),
		"user":       userFunctions(),
	}
	for _, name := range slices.Sorted(maps.Keys(entities)) {
		e.define(api, name, e.entity(name, entities[name]))
	}
	e.define(api, "fail", e.vm.ToValue(e.fail))
	e.define(api, "warn", e.vm.ToValue(e.warn))
	if err := api.DefineAccessorProperty("address", e.vm.ToValue(e.address), nil, goja.FLAG_FALSE, goja.FLAG_TRUE); err != nil {
		panic(err)
	}
	return api
}

// The answer of a function must not change when ytrack changes the default fields of a command.
func fieldsParam() param {
	return param{name: fieldsFlag, kind: StringFlag, refuse: func(value any) string {
		expression := strings.TrimSpace(value.(string))
		if expression == "" || strings.HasPrefix(expression, "+") {
			return "names the default fields, and a function has no default: it takes the whole expression"
		}
		return ""
	}}
}

func pageParams() []param {
	return []param{{name: limitFlag, kind: IntFlag}, {name: skipFlag, kind: IntFlag}}
}

func text(name string) param {
	return param{name: name, kind: StringFlag, optional: true}
}

func texts(name string) param {
	return param{name: name, kind: StringsFlag, optional: true}
}

func listParams(own ...param) []param {
	return append(append(own, fieldsParam()), pageParams()...)
}

type operation func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error)

func always(f operation) binder {
	return func(args []string, opts options) (call, *diag.Fault) {
		return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return f(ctx, c, args, opts)
		}, nil
	}
}

func paged(f operation) binder {
	return func(args []string, opts options) (call, *diag.Fault) {
		if fault := checkPage(opts); fault != nil {
			return nil, fault
		}
		return always(f)(args, opts)
	}
}

func writeOptions(opts options) *youtrack.WriteOptions {
	return &youtrack.WriteOptions{Fields: opts.string(fieldsFlag)}
}

func projectFunctions() map[string]function {
	return map[string]function{
		"show": {
			args:   []string{"code"},
			params: []param{fieldsParam()},
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Projects.Show(ctx, args[0], &youtrack.ShowProjectOptions{Fields: opts.string(fieldsFlag)})
			}),
		},
		"list": {
			params: listParams(),
			bind: paged(func(ctx context.Context, c *youtrack.Client, _ []string, opts options) (*youtrack.Node, error) {
				return c.Projects.List(ctx, &youtrack.ListProjectsOptions{Fields: opts.string(fieldsFlag), Page: pageOf(opts)})
			}),
		},
	}
}

func userFunctions() map[string]function {
	return map[string]function{
		"show": {
			args:   []string{"login"},
			params: []param{fieldsParam()},
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Users.Show(ctx, args[0], &youtrack.ShowUserOptions{Fields: opts.string(fieldsFlag)})
			}),
		},
		"list": {
			params: listParams(text(queryFlag)),
			bind: func(args []string, opts options) (call, *diag.Fault) {
				if fault := rejectNoQuery(opts, "the text to search for", "user"); fault != nil {
					return nil, fault
				}
				return paged(func(ctx context.Context, c *youtrack.Client, _ []string, opts options) (*youtrack.Node, error) {
					list := &youtrack.ListUsersOptions{Fields: opts.string(fieldsFlag), Page: pageOf(opts)}
					return c.Users.List(ctx, opts.string(queryFlag), list)
				})(args, opts)
			},
		},
	}
}

func fieldFunctions() map[string]function {
	return map[string]function{
		"list": {
			args:   []string{"project"},
			params: []param{fieldsParam()},
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Fields.List(ctx, args[0], &youtrack.ListFieldsOptions{Fields: opts.string(fieldsFlag)})
			}),
		},
		"show": {
			args:   []string{"project", "field"},
			params: []param{fieldsParam()},
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Fields.Show(ctx, args[0], args[1], &youtrack.ShowFieldOptions{Fields: opts.string(fieldsFlag)})
			}),
		},
	}
}

func tagFunctions() map[string]function {
	byName := []param{text(nameFlag), text(ownedByFlag)}
	return map[string]function{
		"list": {
			params: listParams(),
			bind: paged(func(ctx context.Context, c *youtrack.Client, _ []string, opts options) (*youtrack.Node, error) {
				return c.Tags.List(ctx, &youtrack.ListTagsOptions{Fields: opts.string(fieldsFlag), Page: pageOf(opts)})
			}),
		},
		"create": {
			params: []param{text(nameFlag), texts(visibleForFlag), texts(updateableByFlag), texts(taggableByFlag), fieldsParam()},
			writes: true,
			bind: always(func(ctx context.Context, c *youtrack.Client, _ []string, opts options) (*youtrack.Node, error) {
				shared := youtrack.TagSharing{VisibleFor: opts.strings(visibleForFlag),
					UpdatableBy: opts.strings(updateableByFlag), TaggableBy: opts.strings(taggableByFlag)}
				return c.Tags.Create(ctx, opts.string(nameFlag), shared, writeOptions(opts))
			}),
		},
		"delete": {
			params: byName,
			writes: true,
			bind: ownedBy(func(ctx context.Context, c *youtrack.Client, _ []string, opts options) (*youtrack.Node, error) {
				return c.Tags.Delete(ctx, opts.string(nameFlag), tagOptions(opts))
			}),
		},
		"add": {
			args:   []string{"owner"},
			params: byName,
			writes: true,
			bind: ownedBy(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Tags.Add(ctx, args[0], opts.string(nameFlag), tagOptions(opts))
			}),
		},
		"remove": {
			args:   []string{"owner"},
			params: byName,
			writes: true,
			bind: ownedBy(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Tags.Remove(ctx, args[0], opts.string(nameFlag), tagOptions(opts))
			}),
		},
	}
}

func ownedBy(f operation) binder {
	return func(args []string, opts options) (call, *diag.Fault) {
		if fault := rejectEmpty(opts, ownedByFlag, emptyOwnedBy); fault != nil {
			return nil, fault
		}
		return always(f)(args, opts)
	}
}

func tagOptions(opts options) *youtrack.TagOptions {
	return &youtrack.TagOptions{OwnedBy: opts.string(ownedByFlag)}
}

func linkFunctions() map[string]function {
	return map[string]function{
		"list": {
			args:   []string{"issue"},
			params: []param{fieldsParam()},
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Links.List(ctx, args[0], &youtrack.ListLinksOptions{Fields: opts.string(fieldsFlag)})
			}),
		},
		"add": {
			args:   []string{"issue", "phrase", "target"},
			params: []param{fieldsParam()},
			writes: true,
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Links.Add(ctx, args[0], args[1], args[2], writeOptions(opts))
			}),
		},
		"remove": {
			args:   []string{"issue", "phrase", "target"},
			writes: true,
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, _ options) (*youtrack.Node, error) {
				return c.Links.Remove(ctx, args[0], args[1], args[2])
			}),
		},
	}
}

const noCommentText = "no --text was given: it carries the text of the comment, which is the whole of what a " +
	"comment is"

func commentFunctions() map[string]function {
	return map[string]function{
		"list": {
			args:   []string{"owner"},
			params: listParams(),
			bind: paged(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Comments.List(ctx, args[0], &youtrack.ListCommentsOptions{Fields: opts.string(fieldsFlag), Page: pageOf(opts)})
			}),
		},
		"create": {
			args:   []string{"owner"},
			params: []param{text(textFlag), fieldsParam()},
			writes: true,
			bind: withText(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Comments.Create(ctx, args[0], opts.string(textFlag), writeOptions(opts))
			}),
		},
		"update": {
			args:   []string{"owner", "id"},
			params: []param{text(textFlag), fieldsParam()},
			writes: true,
			bind: withText(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Comments.Update(ctx, args[0], args[1], opts.string(textFlag), writeOptions(opts))
			}),
		},
		"delete": {
			args:   []string{"owner", "id"},
			writes: true,
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, _ options) (*youtrack.Node, error) {
				return c.Comments.Delete(ctx, args[0], args[1])
			}),
		},
	}
}

func withText(f operation) binder {
	return func(args []string, opts options) (call, *diag.Fault) {
		if fault := requireFlag(opts, textFlag, noCommentText); fault != nil {
			return nil, fault
		}
		return always(f)(args, opts)
	}
}

func attachmentFunctions() map[string]function {
	return map[string]function{
		"list": {
			args:   []string{"owner"},
			params: listParams(),
			bind: paged(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Attachments.List(ctx, args[0], &youtrack.ListAttachmentsOptions{Fields: opts.string(fieldsFlag), Page: pageOf(opts)})
			}),
		},
		"create": {
			args:   []string{"owner", "path"},
			params: []param{fieldsParam()},
			writes: true,
			bind: func(args []string, opts options) (call, *diag.Fault) {
				// Checked before the login is looked up, and opened again for the upload.
				checked, fault := openLocalFile(args[1])
				if fault != nil {
					return nil, fault
				}
				_ = checked.Close()
				return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
					file, fault := openLocalFile(args[1])
					if fault != nil {
						return nil, fault
					}
					defer file.Close()
					sent := youtrack.File{Name: filepath.Base(args[1]), Content: file}
					return c.Attachments.Create(ctx, args[0], sent, writeOptions(opts))
				}, nil
			},
		},
		"delete": {
			args:   []string{"owner", "id"},
			writes: true,
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, _ options) (*youtrack.Node, error) {
				return c.Attachments.Delete(ctx, args[0], args[1])
			}),
		},
	}
}

func timeFunctions() map[string]function {
	return map[string]function{
		"list": {
			args:   []string{"issue"},
			params: listParams(),
			bind: paged(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.WorkItems.List(ctx, args[0], &youtrack.ListWorkItemsOptions{Fields: opts.string(fieldsFlag), Page: pageOf(opts)})
			}),
		},
		"create": {
			args:   []string{"issue", "duration"},
			params: []param{text(dateFlag), text(typeFlag), text(textFlag), texts(attributeFlag), fieldsParam()},
			writes: true,
			bind:   bindWorkItemCreate,
		},
		"update": {
			args: []string{"issue", "id"},
			params: []param{text(durationFlag), text(dateFlag), text(typeFlag), text(textFlag), texts(attributeFlag),
				texts(clearFlag), fieldsParam()},
			writes: true,
			bind:   bindWorkItemUpdate,
		},
		"delete": {
			args:   []string{"issue", "id"},
			writes: true,
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, _ options) (*youtrack.Node, error) {
				return c.WorkItems.Delete(ctx, args[0], args[1])
			}),
		},
	}
}

func bindWorkItemCreate(args []string, opts options) (call, *diag.Fault) {
	spent, fault := parseDuration(args[1])
	if fault != nil {
		return nil, fault
	}
	if fault := rejectEmpty(opts, dateFlag, emptyWorkDate); fault != nil {
		return nil, fault
	}
	if fault := rejectEmpty(opts, typeFlag, emptyWorkType); fault != nil {
		return nil, fault
	}
	written, fault := workItemAttributes(opts.strings(attributeFlag))
	if fault != nil {
		return nil, fault
	}
	in := &youtrack.WorkItemInput{Duration: spent, Date: opts.string(dateFlag), Text: opts.string(textFlag),
		Type: opts.string(typeFlag), Attributes: written}
	return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
		return c.WorkItems.Create(ctx, args[0], in, writeOptions(opts))
	}, nil
}

func bindWorkItemUpdate(args []string, opts options) (call, *diag.Fault) {
	in := &youtrack.WorkItemUpdate{Date: opts.optional(dateFlag), Text: opts.optional(textFlag),
		Type: opts.optional(typeFlag)}
	if opts.given(durationFlag) {
		length, fault := parseDuration(opts.string(durationFlag))
		if fault != nil {
			return nil, fault
		}
		in.Duration = &length
	}
	clears, fault := workItemClearsOf(opts.strings(clearFlag))
	if fault != nil {
		return nil, fault
	}
	in.ClearText, in.ClearType = clears.text, clears.workType
	if in.Attributes, fault = workItemAttributes(opts.strings(attributeFlag)); fault != nil {
		return nil, fault
	}
	in.Attributes = append(in.Attributes, clears.attributes...)
	return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
		return c.WorkItems.Update(ctx, args[0], args[1], in, writeOptions(opts))
	}, nil
}

func activityFunctions() map[string]function {
	return map[string]function{
		"list": {
			args:   []string{"issue"},
			params: listParams(texts(categoryFlag)),
			bind: paged(func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				list := &youtrack.ListActivitiesOptions{Fields: opts.string(fieldsFlag), Page: pageOf(opts),
					Categories: opts.strings(categoryFlag)}
				return c.Activities.List(ctx, args[0], list)
			}),
		},
	}
}

func articleFunctions() map[string]function {
	return map[string]function{
		"show": {
			args:   []string{"id"},
			params: []param{fieldsParam(), {name: commentsFlag, kind: StringFlag}},
			bind: func(args []string, opts options) (call, *diag.Fault) {
				comments, fault := commentsOf(opts)
				if fault != nil {
					return nil, fault
				}
				return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
					show := &youtrack.ShowArticleOptions{Fields: opts.string(fieldsFlag), Comments: comments}
					return c.Articles.Show(ctx, args[0], show)
				}, nil
			},
		},
		"list": {
			params: listParams(text(queryFlag), text(parentFlag)),
			bind:   bindArticleList,
		},
		"create": {
			args:   []string{"project"},
			params: []param{text(summaryFlag), text(contentFlag), text(parentFlag), fieldsParam()},
			writes: true,
			bind:   bindArticleCreate,
		},
		"update": {
			args:   []string{"id"},
			params: []param{text(summaryFlag), text(contentFlag), text(parentFlag), texts(clearFlag), fieldsParam()},
			writes: true,
			bind: func(args []string, opts options) (call, *diag.Fault) {
				clearsContent, clearsParent, fault := articleClears(opts.strings(clearFlag))
				if fault != nil {
					return nil, fault
				}
				in := &youtrack.ArticleUpdate{Summary: opts.optional(summaryFlag), Content: opts.optional(contentFlag),
					Parent: opts.optional(parentFlag), ClearContent: clearsContent, ClearParent: clearsParent}
				return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
					return c.Articles.Update(ctx, args[0], in, writeOptions(opts))
				}, nil
			},
		},
		"delete": {
			args:   []string{"id"},
			writes: true,
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, _ options) (*youtrack.Node, error) {
				return c.Articles.Delete(ctx, args[0])
			}),
		},
	}
}

func bindArticleList(args []string, opts options) (call, *diag.Fault) {
	if fault := checkPage(opts); fault != nil {
		return nil, fault
	}
	list := &youtrack.ListArticlesOptions{Fields: opts.string(fieldsFlag), Page: pageOf(opts)}
	if !opts.given(parentFlag) {
		if fault := rejectNoQuery(opts, "the search to run", "article"); fault != nil {
			return nil, fault
		}
		return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Articles.List(ctx, opts.string(queryFlag), list)
		}, nil
	}
	if opts.given(queryFlag) {
		message := "--parent and --query were both given: the children of an article are listed with no search"
		return nil, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
	}
	return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
		return c.Articles.Children(ctx, opts.string(parentFlag), list)
	}, nil
}

func bindArticleCreate(args []string, opts options) (call, *diag.Fault) {
	noSummary := "no --summary was given: it carries the title of the article, which YouTrack files none without"
	if fault := requireFlag(opts, summaryFlag, noSummary); fault != nil {
		return nil, fault
	}
	if fault := rejectEmpty(opts, contentFlag, emptyContent); fault != nil {
		return nil, fault
	}
	if fault := rejectEmpty(opts, parentFlag, emptyParent); fault != nil {
		return nil, fault
	}
	in := &youtrack.ArticleInput{Summary: opts.string(summaryFlag), Content: opts.string(contentFlag),
		Parent: opts.string(parentFlag)}
	return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
		return c.Articles.Create(ctx, args[0], in, writeOptions(opts))
	}, nil
}

func issueFunctions(warn func(*youtrack.Warning)) map[string]function {
	return map[string]function{
		"show": {
			args:   []string{"id"},
			params: []param{fieldsParam(), {name: commentsFlag, kind: StringFlag}},
			bind: func(args []string, opts options) (call, *diag.Fault) {
				comments, fault := commentsOf(opts)
				if fault != nil {
					return nil, fault
				}
				return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
					show := &youtrack.ShowIssueOptions{Fields: opts.string(fieldsFlag), Comments: comments}
					return c.Issues.Show(ctx, args[0], show)
				}, nil
			},
		},
		"list": {
			params: listParams(text(queryFlag)),
			bind: func(args []string, opts options) (call, *diag.Fault) {
				if fault := rejectNoQuery(opts, "the search to run", "issue"); fault != nil {
					return nil, fault
				}
				return paged(func(ctx context.Context, c *youtrack.Client, _ []string, opts options) (*youtrack.Node, error) {
					list := &youtrack.ListIssuesOptions{Fields: opts.string(fieldsFlag), Page: pageOf(opts), Warn: warn}
					return c.Issues.List(ctx, opts.string(queryFlag), list)
				})(args, opts)
			},
		},
		"create": {
			args:   []string{"project"},
			params: []param{text(summaryFlag), text(descriptionFlag), texts(fieldFlag), fieldsParam()},
			writes: true,
			bind:   bindIssueCreate,
		},
		"update": {
			args:   []string{"id"},
			params: []param{text(summaryFlag), text(descriptionFlag), texts(fieldFlag), texts(clearFlag), fieldsParam()},
			writes: true,
			bind: func(args []string, opts options) (call, *diag.Fault) {
				writes, clearsDescription, fault := issueFieldWrites(opts.strings(fieldFlag), opts.strings(clearFlag))
				if fault != nil {
					return nil, fault
				}
				in := &youtrack.IssueUpdate{Summary: opts.optional(summaryFlag), Description: opts.optional(descriptionFlag),
					ClearDescription: clearsDescription, Fields: writes}
				return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
					return c.Issues.Update(ctx, args[0], in, writeOptions(opts))
				}, nil
			},
		},
		"delete": {
			args:   []string{"id"},
			writes: true,
			bind: always(func(ctx context.Context, c *youtrack.Client, args []string, _ options) (*youtrack.Node, error) {
				return c.Issues.Delete(ctx, args[0])
			}),
		},
	}
}

func bindIssueCreate(args []string, opts options) (call, *diag.Fault) {
	noSummary := "no --summary was given: it carries the title of the issue, which YouTrack files none without"
	if fault := requireFlag(opts, summaryFlag, noSummary); fault != nil {
		return nil, fault
	}
	if fault := rejectEmpty(opts, descriptionFlag, emptyDescription); fault != nil {
		return nil, fault
	}
	writes, _, fault := issueFieldWrites(opts.strings(fieldFlag), nil)
	if fault != nil {
		return nil, fault
	}
	in := &youtrack.IssueInput{Summary: opts.string(summaryFlag), Description: opts.string(descriptionFlag),
		Fields: writes}
	return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
		return c.Issues.Create(ctx, args[0], in, writeOptions(opts))
	}, nil
}
