package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/script"
)

func Run(ctx context.Context, argv, env []string, build *debug.BuildInfo, stdin *os.File, stdout, stderr io.Writer) int {
	renderer := render.YAML{}
	stream := diag.NewStream(stderr, renderer)
	// Given nil, cobra reads the process's own arguments.
	if argv == nil {
		argv = []string{}
	}
	root := newRoot(env, build, stdin, stdout, renderer, stream)
	catalog := loadScripts(root, env)
	addScripts(root, catalog, env, stdout, renderer, stream)
	err := execute(ctx, root, catalog, argv, stdout, stream)
	if err == nil {
		return 0
	}
	var fault *diag.Fault
	if !errors.As(err, &fault) {
		fault = &diag.Fault{Code: youtrack.CodeBadUsage, Message: cobraMessage(err)}
	}
	stream.Fail(fault)
	return fault.ExitCode()
}

// cobra's own __complete reads the process env and writes to its stderr and a debug file.
func execute(ctx context.Context, root *cobra.Command, catalog *script.Catalog, argv []string, stdout io.Writer,
	stream *diag.Stream,
) error {
	if len(argv) > 0 {
		switch argv[0] {
		case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			if fault := complete(root, stdout, argv[0], argv[1:]); fault != nil {
				return fault
			}
			return nil
		}
	}
	warnHidden(root, catalog, argv, stream)
	root.SetArgs(argv)
	return root.ExecuteContext(ctx)
}

func newRoot(env []string, build *debug.BuildInfo, stdin *os.File, stdout io.Writer, renderer render.Renderer,
	stream *diag.Stream,
) *cobra.Command {
	var version bool
	root := newCommand("ytrack", func(cmd *cobra.Command, args []string) *diag.Fault {
		if version {
			return printNode(stdout, renderer, versionNode(build))
		}
		return requireSubcommand(cmd, args)
	})
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentPreRunE = runE(refuseCobraCompletion)
	// cobra's help exits 0 on an unknown topic and lists a hidden "help"; a hidden long name still widens the list.
	help := newCommand("no-help", func(cmd *cobra.Command, _ []string) *diag.Fault {
		return unknownCommand(cmd.Root(), cmd.CalledAs())
	})
	help.Hidden = true
	root.SetHelpCommand(help)
	// SetHelpCommand takes effect only inside ExecuteC, which the completion protocol never reaches.
	root.AddCommand(help)
	root.SetOut(stdout)
	root.SetErr(io.Discard)
	root.SetFlagErrorFunc(runE(func(_ *cobra.Command, err error) *diag.Fault {
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: flagMessage(err)}
	}))
	// A flag of its own: cobra's Version field prints its own template past the renderer and takes -v.
	root.Flags().BoolVar(&version, "version", false, "print the version and the revision this binary was built from")
	root.AddCommand(newActivity(env, stdout, renderer), newArticle(env, stdout, renderer),
		newAttachment(env, stdout, renderer), newAuth(env, stdin, stdout, renderer),
		newComment(env, stdout, renderer), newCompletion(stdout), newField(env, stdout, renderer),
		newIssue(env, stdout, renderer, stream), newLink(env, stdout, renderer), newTag(env, stdout, renderer),
		newTime(env, stdout, renderer), newUser(env, stdout, renderer))
	return root
}

func refuseCobraCompletion(cmd *cobra.Command, _ []string) *diag.Fault {
	switch cmd.CalledAs() {
	case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return unknownCommand(cmd.Root(), cmd.CalledAs())
	}
	return nil
}

func newTag(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	tag := newCommand("tag", requireSubcommand)
	tag.Short = "Manage tags"
	tag.AddCommand(newTagList(env, stdout, renderer), newTagCreate(env, stdout, renderer),
		newTagDelete(env, stdout, renderer), newTagAdd(env, stdout, renderer),
		newTagRemove(env, stdout, renderer))
	return tag
}

func newTagRemove(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var name, ownedBy string
	remove := newCommand("remove <issue or article>", func(cmd *cobra.Command, args []string) *diag.Fault {
		if fault := rejectEmptyFlag(cmd, ownedByFlag, ownedBy, emptyOwnedBy); fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Tags.Remove(ctx, args[0], name, &youtrack.TagOptions{OwnedBy: ownedBy})
		})
	})
	remove.Args = cobra.ExactArgs(1)
	remove.Short = "Untag an issue or article"
	remove.Long = "Untag an issue or article. The tag stays."
	remove.Flags().StringVar(&name, nameFlag, "", "tag `name`")
	rejectRepeat(remove.Flags().Lookup(nameFlag))
	ownedByFlagOf(remove, &ownedBy)
	return remove
}

func newTagAdd(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var name, ownedBy string
	add := newCommand("add <issue or article>", func(cmd *cobra.Command, args []string) *diag.Fault {
		if fault := rejectEmptyFlag(cmd, ownedByFlag, ownedBy, emptyOwnedBy); fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Tags.Add(ctx, args[0], name, &youtrack.TagOptions{OwnedBy: ownedBy})
		})
	})
	add.Args = cobra.ExactArgs(1)
	add.Short = "Tag an issue or article"
	add.Long = "Tag an issue or article."
	add.Flags().StringVar(&name, nameFlag, "", "tag `name`")
	rejectRepeat(add.Flags().Lookup(nameFlag))
	ownedByFlagOf(add, &ownedBy)
	return add
}

func newTagCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, name string
	var shared youtrack.TagSharing
	create := newCommand("create", func(cmd *cobra.Command, _ []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Tags.Create(ctx, name, shared, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	create.Args = cobra.ExactArgs(0)
	create.Short = "Create a tag"
	create.Long = "Create a tag owned by you.\n\n" +
		"Without flags only the owner sees the tag. Only --taggable-by lets a group hang it."
	create.Flags().StringVar(&name, nameFlag, "", "tag `name`")
	rejectRepeat(create.Flags().Lookup(nameFlag))
	create.Flags().StringArrayVar((*[]string)(&shared.VisibleFor), visibleForFlag, nil,
		"`group` that sees the tag; repeatable")
	create.Flags().StringArrayVar((*[]string)(&shared.UpdatableBy), updateableByFlag, nil,
		"`group` that may edit the tag; repeatable")
	create.Flags().StringArrayVar((*[]string)(&shared.TaggableBy), taggableByFlag, nil,
		"`group` that may tag with it; repeatable")
	fieldsFlag(create, &fields, tagCreateFields)
	return create
}

func newTagDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var name, ownedBy string
	remove := newCommand("delete", func(cmd *cobra.Command, _ []string) *diag.Fault {
		if fault := rejectEmptyFlag(cmd, ownedByFlag, ownedBy, emptyOwnedBy); fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Tags.Delete(ctx, name, &youtrack.TagOptions{OwnedBy: ownedBy})
		})
	})
	remove.Args = cobra.ExactArgs(0)
	remove.Short = "Delete a tag"
	remove.Long = "Delete a tag everywhere.\n\n" +
		"An administrator's token deletes a tag of another user as well."
	remove.Flags().StringVar(&name, nameFlag, "", "tag `name`")
	rejectRepeat(remove.Flags().Lookup(nameFlag))
	ownedByFlagOf(remove, &ownedBy)
	return remove
}

func ownedByFlagOf(cmd *cobra.Command, login *string) {
	cmd.Flags().StringVar(login, ownedByFlag, "", "owner `login`, when names clash")
	rejectRepeat(cmd.Flags().Lookup(ownedByFlag))
}

func newTagList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var page youtrack.Page
	list := newCommand("list", func(cmd *cobra.Command, _ []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Tags.List(ctx, &youtrack.ListTagsOptions{Fields: fieldsFlagValue(cmd, &fields), Page: page})
		})
	})
	list.Args = cobra.ExactArgs(0)
	list.Short = "List tags"
	list.Long = "List tags you own or that are shared with you. Names may clash across owners."
	fieldsFlag(list, &fields, tagListFields)
	pageFlags(list, &page, "tags")
	return list
}

func newLink(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var listFields string
	list := newCommand("list <issue>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Links.List(ctx, args[0], &youtrack.ListLinksOptions{Fields: fieldsFlagValue(cmd, &listFields)})
		})
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List links of an issue"
	list.Long = "List links of an issue by phrase.\n\n" +
		"A phrase reads from this issue to the linked ones.\n\n" +
		"--fields applies to the linked issues."
	fieldsFlag(list, &listFields, linkListFields)

	var addFields string
	add := newCommand("add <id> <phrase> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Links.Add(ctx, args[0], args[1], args[2], &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &addFields)})
		})
	})
	add.Args = cobra.ExactArgs(3)
	add.Short = "Link two issues"
	add.Long = "Link two issues; prints the links of the first.\n\n" +
		"<phrase> reads from the first issue to the second, such as \"depends on\" or \"is required for\": " +
		"ytrack link add DEV-1 \"depends on\" DEV-2."
	fieldsFlag(add, &addFields, linkListFields)

	remove := newCommand("remove <id> <phrase> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Links.Remove(ctx, args[0], args[1], args[2])
		})
	})
	remove.Args = cobra.ExactArgs(3)
	remove.Short = "Unlink two issues"
	remove.Long = "Unlink two issues.\n\n" +
		"A link can be named from either end: DEV-1 \"depends on\" DEV-2 and DEV-2 \"is required for\" DEV-1 remove " +
		"the same link. A state a workflow set when the link was made stays."

	link := newCommand("link", requireSubcommand)
	link.Short = "Manage issue links"
	link.AddCommand(list, add, remove)
	return link
}

func newArticle(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	article := newCommand("article", requireSubcommand)
	article.Short = "Manage articles"
	article.AddCommand(newArticleShow(env, stdout, renderer), newArticleList(env, stdout, renderer),
		newArticleCreate(env, stdout, renderer), newArticleUpdate(env, stdout, renderer),
		newArticleDelete(env, stdout, renderer))
	return article
}

func newArticleCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, summary, content, parent string
	create := newCommand("create <project>", func(cmd *cobra.Command, args []string) *diag.Fault {
		if !cmd.Flags().Changed(summaryFlag) {
			message := "no --summary was given: it carries the title of the article, which YouTrack files none without"
			return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
		}
		if fault := rejectEmptyFlag(cmd, contentFlag, content, emptyContent); fault != nil {
			return fault
		}
		if fault := rejectEmptyFlag(cmd, parentFlag, parent, emptyParent); fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			in := &youtrack.ArticleInput{Summary: summary, Content: content, Parent: parent}
			return c.Articles.Create(ctx, args[0], in, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	create.Args = cobra.ExactArgs(1)
	create.Short = "Create an article"
	create.Long = "Create an article in a project."
	create.Flags().StringVar(&summary, summaryFlag, "", "`title`")
	rejectRepeat(create.Flags().Lookup(summaryFlag))
	create.Flags().StringVar(&content, contentFlag, "", "article `text`")
	rejectRepeat(create.Flags().Lookup(contentFlag))
	create.Flags().StringVar(&parent, parentFlag, "", "parent article `id`")
	rejectRepeat(create.Flags().Lookup(parentFlag))
	fieldsFlag(create, &fields, articleShowFields)
	return create
}

func newArticleUpdate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, summary, content, parent string
	var emptied []string
	update := newCommand("update <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		clearsContent, clearsParent, fault := articleClears(emptied)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			in := &youtrack.ArticleUpdate{Summary: flagValue(cmd, summaryFlag, &summary),
				Content: flagValue(cmd, contentFlag, &content), Parent: flagValue(cmd, parentFlag, &parent),
				ClearContent: clearsContent, ClearParent: clearsParent}
			return c.Articles.Update(ctx, args[0], in, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	update.Args = cobra.ExactArgs(1)
	update.Short = "Update an article"
	update.Long = "Update an article; unflagged parts stay."
	update.Flags().StringVar(&summary, summaryFlag, "", "`title`")
	rejectRepeat(update.Flags().Lookup(summaryFlag))
	update.Flags().StringVar(&content, contentFlag, "", "article `text`")
	rejectRepeat(update.Flags().Lookup(contentFlag))
	update.Flags().StringVar(&parent, parentFlag, "", "parent article `id`")
	rejectRepeat(update.Flags().Lookup(parentFlag))
	update.Flags().StringArrayVar(&emptied, clearFlag, nil, "empty a `part`: content or parent")
	fieldsFlag(update, &fields, articleShowFields)
	return update
}

func newArticleDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	del := newCommand("delete <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Articles.Delete(ctx, args[0])
		})
	})
	del.Args = cobra.ExactArgs(1)
	del.Short = "Delete an article with its children"
	del.Long = "Delete an article with its children."
	return del
}

func newArticleList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, query, parent string
	var page youtrack.Page
	list := newCommand("list", func(cmd *cobra.Command, _ []string) *diag.Fault {
		call, fault := articlesListed(cmd, query, parent, &youtrack.ListArticlesOptions{Fields: fieldsFlagValue(cmd, &fields), Page: page})
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(0)
	list.Short = "Search articles"
	list.Long = "Search articles.\n\n" +
		"--query takes the search attributes of articles: project or in, title, content, author, article id, tag, " +
		"created, updated, updater, has, sort by. An attribute of issues, such as summary, finds nothing, and a query " +
		"the server cannot parse finds every article, neither with an error.\n\n" +
		"--parent lists the children of an article instead, and takes no --query."
	list.Flags().StringVar(&query, "query", "", "YouTrack `search`")
	rejectRepeat(list.Flags().Lookup("query"))
	list.Flags().StringVar(&parent, parentFlag, "", "parent article `id`")
	rejectRepeat(list.Flags().Lookup(parentFlag))
	fieldsFlag(list, &fields, articleListFields)
	pageFlags(list, &page, "articles")
	return list
}

func articlesListed(cmd *cobra.Command, query, parent string, opts *youtrack.ListArticlesOptions) (call, *diag.Fault) {
	if !cmd.Flags().Changed(parentFlag) {
		if fault := rejectNoQuery(cmd, "the search to run", "article"); fault != nil {
			return nil, fault
		}
		return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Articles.List(ctx, query, opts)
		}, nil
	}
	if cmd.Flags().Changed("query") {
		message := "--parent and --query were both given: the children of an article are listed with no search"
		return nil, &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
	}
	return func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
		return c.Articles.Children(ctx, parent, opts)
	}, nil
}

func newArticleShow(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	comments := commentsValue{text: everyComment, comments: youtrack.AllComments()}
	show := newCommand("show <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Articles.Show(ctx, args[0], &youtrack.ShowArticleOptions{Fields: fieldsFlagValue(cmd, &fields), Comments: comments.comments})
		})
	})
	show.Args = cobra.ExactArgs(1)
	show.Short = "Show an article"
	show.Long = "Show an article with comments, oldest first."
	fieldsFlag(show, &fields, articleShowFields)
	commentsFlag(show, &comments)
	return show
}

func newAttachment(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	attachment := newCommand("attachment", requireSubcommand)
	attachment.Short = "Manage attachments"
	attachment.AddCommand(newAttachmentCreate(env, stdout, renderer), newAttachmentDelete(env, stdout, renderer),
		newAttachmentList(env, stdout, renderer))
	return attachment
}

func newAttachmentDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	remove := newCommand("delete <owner> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Attachments.Delete(ctx, args[0], args[1])
		})
	})
	remove.Args = cobra.ExactArgs(2)
	remove.Short = "Delete an attachment"
	remove.Long = "Delete an attachment.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1. <id> is the attachment id such as 12-1 that ytrack " +
		"attachment list prints, not a file name. A file attached to a comment belongs to the issue."
	return remove
}

func newAttachmentCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	create := newCommand("create <owner> <path>", func(cmd *cobra.Command, args []string) *diag.Fault {
		file, fault := openLocalFile(args[1])
		if fault != nil {
			return fault
		}
		defer file.Close()
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			sent := youtrack.File{Name: filepath.Base(args[1]), Content: file}
			return c.Attachments.Create(ctx, args[0], sent, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	create.Args = cobra.ExactArgs(2)
	create.Annotations = map[string]string{completesPathAt: "2"}
	create.Short = "Attach a file"
	create.Long = "Attach a local file.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1."
	fieldsFlag(create, &fields, attachmentListFields)
	return create
}

func newAttachmentList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var page youtrack.Page
	list := newCommand("list <owner>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Attachments.List(ctx, args[0], &youtrack.ListAttachmentsOptions{Fields: fieldsFlagValue(cmd, &fields), Page: page})
		})
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List attachments"
	list.Long = "List attachments, including those of comments; " +
		"--fields +comment(id) says which comment.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1.\n\n" +
		"url downloads the file without a token for up to three days: keep it as secret as a token."
	fieldsFlag(list, &fields, attachmentListFields)
	pageFlags(list, &page, "attachments")
	return list
}

func newComment(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	comment := newCommand("comment", requireSubcommand)
	comment.Short = "Manage comments"
	comment.AddCommand(newCommentList(env, stdout, renderer), newCommentCreate(env, stdout, renderer),
		newCommentUpdate(env, stdout, renderer), newCommentDelete(env, stdout, renderer))
	return comment
}

func newCommentList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var page youtrack.Page
	list := newCommand("list <owner>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Comments.List(ctx, args[0], &youtrack.ListCommentsOptions{Fields: fieldsFlagValue(cmd, &fields), Page: page})
		})
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List comments"
	list.Long = "List comments, oldest first.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1.\n\n" +
		"Comments of an article have no deleted. --limit keeps the oldest; ytrack issue show --comments 5 prints " +
		"the latest five."
	fieldsFlag(list, &fields, commentListFields)
	pageFlags(list, &page, "comments")
	return list
}

const commentOwner = "--fields +issue(...) on a comment of an article, or +article(...) on one of an issue, is " +
	"refused only after the comment is written."

const noCommentText = "no --text was given: it carries the text of the comment, which is the whole of what a " +
	"comment is"

func newCommentCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, text string
	create := newCommand("create <owner>", func(cmd *cobra.Command, args []string) *diag.Fault {
		if !cmd.Flags().Changed(textFlag) {
			return &diag.Fault{Code: youtrack.CodeBadUsage, Message: noCommentText}
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Comments.Create(ctx, args[0], text, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	create.Args = cobra.ExactArgs(1)
	create.Short = "Add a comment"
	create.Long = "Add a comment.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1.\n\n" +
		commentOwner
	create.Flags().StringVar(&text, textFlag, "", "comment `text`")
	rejectRepeat(create.Flags().Lookup(textFlag))
	fieldsFlag(create, &fields, commentFields)
	return create
}

func newCommentUpdate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, text string
	update := newCommand("update <owner> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		if !cmd.Flags().Changed(textFlag) {
			return &diag.Fault{Code: youtrack.CodeBadUsage, Message: noCommentText}
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Comments.Update(ctx, args[0], args[1], text, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	update.Args = cobra.ExactArgs(2)
	update.Short = "Replace a comment's text"
	update.Long = "Replace a comment's text.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1. <id> is the comment id such as 7-1 that ytrack comment " +
		"list prints.\n\n" +
		commentOwner
	update.Flags().StringVar(&text, textFlag, "", "comment `text`")
	rejectRepeat(update.Flags().Lookup(textFlag))
	fieldsFlag(update, &fields, commentFields)
	return update
}

func newCommentDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	del := newCommand("delete <owner> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Comments.Delete(ctx, args[0], args[1])
		})
	})
	del.Args = cobra.ExactArgs(2)
	del.Short = "Delete a comment"
	del.Long = "Delete a comment.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1. <id> is the comment id such as 7-1 that ytrack comment " +
		"list prints."
	return del
}

func newIssue(env []string, stdout io.Writer, renderer render.Renderer, stream *diag.Stream) *cobra.Command {
	issue := newCommand("issue", requireSubcommand)
	issue.Short = "Manage issues"
	issue.AddCommand(newIssueShow(env, stdout, renderer), newIssueList(env, stdout, renderer, stream),
		newIssueCreate(env, stdout, renderer), newIssueUpdate(env, stdout, renderer),
		newIssueDelete(env, stdout, renderer))
	return issue
}

func newIssueShow(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	comments := commentsValue{text: everyComment, comments: youtrack.AllComments()}
	show := newCommand("show <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Issues.Show(ctx, args[0], &youtrack.ShowIssueOptions{Fields: fieldsFlagValue(cmd, &fields), Comments: comments.comments})
		})
	})
	show.Args = cobra.ExactArgs(1)
	show.Short = "Show an issue"
	show.Long = "Show an issue with comments, oldest first.\n\n" +
		"Custom fields are named inside customFields by name or localized name, in any letter case, quoted where " +
		"they hold a space: --fields '+customFields(Priority,\"Due Date\")'. A bare customFields prints every field " +
		"that holds something; a named field is printed even when empty, and one the issue does not have is left out.\n\n" +
		"Comments print id, author(login), created and text whatever --fields says, and deleted ones are left out."
	fieldsFlag(show, &fields, issueShowFields)
	commentsFlag(show, &comments)
	return show
}

func newIssueList(env []string, stdout io.Writer, renderer render.Renderer, stream *diag.Stream) *cobra.Command {
	var fields, query string
	var page youtrack.Page
	list := newCommand("list", func(cmd *cobra.Command, _ []string) *diag.Fault {
		if fault := rejectNoQuery(cmd, "the search to run", "issue"); fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Issues.List(ctx, query, &youtrack.ListIssuesOptions{Fields: fieldsFlagValue(cmd, &fields), Page: page, Warn: stream.Warn})
		})
	})
	list.Args = cobra.ExactArgs(0)
	list.Short = "Search issues"
	list.Long = "Search issues.\n\n" +
		"Custom fields are named inside customFields by name or localized name, in any letter case, quoted where " +
		"they hold a space: --fields '+customFields(Priority,\"Due Date\")'. A bare customFields prints every field " +
		"that holds something; a named field is printed even when empty, and one the issue does not have is left out. " +
		"Links print under their phrase: --fields '+links(issues(idReadable))'."
	list.Flags().StringVar(&query, "query", "", "YouTrack `search`")
	rejectRepeat(list.Flags().Lookup("query"))
	fieldsFlag(list, &fields, issueListFields)
	pageFlags(list, &page, "issues")
	return list
}

func newIssueCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, summary, description string
	var filled []string
	create := newCommand("create <project>", func(cmd *cobra.Command, args []string) *diag.Fault {
		if !cmd.Flags().Changed(summaryFlag) {
			message := "no --summary was given: it carries the title of the issue, which YouTrack files none without"
			return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
		}
		if fault := rejectEmptyFlag(cmd, descriptionFlag, description, emptyDescription); fault != nil {
			return fault
		}
		writes, _, fault := issueFieldWrites(filled, nil)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			in := &youtrack.IssueInput{Summary: summary, Description: description, Fields: writes}
			return c.Issues.Create(ctx, args[0], in, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	create.Args = cobra.ExactArgs(1)
	create.Short = "Create an issue"
	create.Long = "Create an issue in a project."
	create.Flags().StringVar(&summary, summaryFlag, "", "`title`")
	rejectRepeat(create.Flags().Lookup(summaryFlag))
	create.Flags().StringVar(&description, descriptionFlag, "", "`text` of the description")
	rejectRepeat(create.Flags().Lookup(descriptionFlag))
	create.Flags().StringArrayVar(&filled, fieldFlag, nil,
		"custom field `Name=value`; repeatable")
	fieldsFlag(create, &fields, issueShowFields)
	return create
}

func newIssueUpdate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, summary, description string
	var filled, emptied []string
	update := newCommand("update <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		writes, clearsDescription, fault := issueFieldWrites(filled, emptied)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			in := &youtrack.IssueUpdate{Summary: flagValue(cmd, summaryFlag, &summary),
				Description: flagValue(cmd, descriptionFlag, &description), ClearDescription: clearsDescription,
				Fields: writes}
			return c.Issues.Update(ctx, args[0], in, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	update.Args = cobra.ExactArgs(1)
	update.Short = "Update an issue"
	update.Long = "Update an issue; unflagged parts stay.\n\n" +
		"--field on a field of several values leaves it holding exactly the values given, not the old ones plus " +
		"them."
	update.Flags().StringVar(&summary, summaryFlag, "", "`title`")
	rejectRepeat(update.Flags().Lookup(summaryFlag))
	update.Flags().StringVar(&description, descriptionFlag, "", "`text` of the description")
	rejectRepeat(update.Flags().Lookup(descriptionFlag))
	update.Flags().StringArrayVar(&filled, fieldFlag, nil,
		"custom field `Name=value`; repeatable")
	update.Flags().StringArrayVar(&emptied, clearFlag, nil,
		"empty a custom field or description by `name`")
	fieldsFlag(update, &fields, issueShowFields)
	return update
}

func newIssueDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	del := newCommand("delete <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Issues.Delete(ctx, args[0])
		})
	})
	del.Args = cobra.ExactArgs(1)
	del.Short = "Delete an issue"
	del.Long = "Delete an issue."
	return del
}

func newActivity(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var categories []string
	var page youtrack.Page
	list := newCommand("list <issue>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Activities.List(ctx, args[0], &youtrack.ListActivitiesOptions{Fields: fieldsFlagValue(cmd, &fields), Page: page, Categories: categories})
		})
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List activities of an issue"
	list.Long = "List activities of an issue, newest first." +
		"\n\n" +
		"Changes of a description, a summary or a comment carry the whole text before and after. Narrow with " +
		"--category, or leave added and removed out of --fields.\n\n" +
		"--category takes " + strings.Join(youtrack.ActivityCategories(), ", ") + "."
	list.Flags().StringArrayVar(&categories, "category", nil,
		"`category` to print; repeatable; default all")
	closedSet(list.Flags().Lookup("category"), youtrack.ActivityCategories())
	fieldsFlag(list, &fields, activityListFields)
	pageFlags(list, &page, "activities")

	activity := newCommand("activity", requireSubcommand)
	activity.Short = "Read issue activities"
	activity.AddCommand(list)
	return activity
}

func flagValue(cmd *cobra.Command, flag string, value *string) *string {
	if !cmd.Flags().Changed(flag) {
		return nil
	}
	return value
}

const (
	summaryFlag      = "summary"
	descriptionFlag  = "description"
	contentFlag      = "content"
	textFlag         = "text"
	fieldFlag        = "field"
	attributeFlag    = "attribute"
	clearFlag        = "clear"
	parentFlag       = "parent"
	nameFlag         = "name"
	visibleForFlag   = "visible-for"
	updateableByFlag = "updateable-by"
	taggableByFlag   = "taggable-by"
	ownedByFlag      = "owned-by"
	dateFlag         = "date"
	typeFlag         = "type"
	durationFlag     = "duration"
)

func newUser(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var showFields string
	show := newCommand("show <login>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Users.Show(ctx, args[0], &youtrack.ShowUserOptions{Fields: fieldsFlagValue(cmd, &showFields)})
		})
	})
	show.Args = cobra.ExactArgs(1)
	show.Short = "Show a user"
	show.Long = "Show a user.\n\n" +
		"<login> is a login, not a full name; ytrack user list --query finds one by name."
	fieldsFlag(show, &showFields, userShowFields)

	var listFields, search string
	var page youtrack.Page
	list := newCommand("list", func(cmd *cobra.Command, _ []string) *diag.Fault {
		if fault := rejectNoQuery(cmd, "the text to search for", "user"); fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Users.List(ctx, search, &youtrack.ListUsersOptions{Fields: listFields, Page: page})
		})
	})
	list.Args = cobra.ExactArgs(0)
	list.Short = "Search users"
	list.Long = "Search users by login or name prefix. " +
		"An email address matches nothing."
	list.Flags().StringVar(&search, "query", "", "login or name prefix")
	rejectRepeat(list.Flags().Lookup("query"))
	fieldsFlag(list, &listFields, userListFields)
	pageFlags(list, &page, "users")

	user := newCommand("user", requireSubcommand)
	user.Short = "Find users"
	user.AddCommand(show, list)
	return user
}

func newField(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var listFields string
	list := newCommand("list <project>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Fields.List(ctx, args[0], &youtrack.ListFieldsOptions{Fields: listFields})
		})
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List custom fields of a project"
	list.Long = "List custom fields of a project."
	fieldsFlag(list, &listFields, fieldListFields)

	var showFields string
	show := newCommand("show <project> <field>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.Fields.Show(ctx, args[0], args[1], &youtrack.ShowFieldOptions{Fields: fieldsFlagValue(cmd, &showFields)})
		})
	})
	show.Args = cobra.ExactArgs(2)
	show.Short = "Show a custom field"
	show.Long = "Show a custom field with its allowed values.\n\n" +
		"A field of users prints the people and groups of its bundle under bundle.values and every user they hold " +
		"under bundle.aggregatedUsers, where an empty list means anyone."
	fieldsFlag(show, &showFields, fieldShowFields)

	field := newCommand("field", requireSubcommand)
	field.Short = "Read custom fields"
	field.AddCommand(list, show)
	return field
}

func newTime(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	tracking := newCommand("time", requireSubcommand)
	tracking.Short = "Manage logged time"
	tracking.AddCommand(newTimeList(env, stdout, renderer), newTimeCreate(env, stdout, renderer),
		newTimeUpdate(env, stdout, renderer), newTimeDelete(env, stdout, renderer))
	return tracking
}

func newTimeList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var page youtrack.Page
	list := newCommand("list <issue>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.WorkItems.List(ctx, args[0], &youtrack.ListWorkItemsOptions{Fields: fieldsFlagValue(cmd, &fields), Page: page})
		})
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List work items"
	list.Long = "List work items, oldest first." +
		"\n\n" +
		"date is a day, printed as its midnight UTC."
	fieldsFlag(list, &fields, workItemListFields)
	pageFlags(list, &page, "work items")
	return list
}

func newTimeCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, date, text, workType string
	var attributes []string
	create := newCommand("create <issue> <duration>", func(cmd *cobra.Command, args []string) *diag.Fault {
		spent, fault := parseDuration(args[1])
		if fault != nil {
			return fault
		}
		if fault := rejectEmptyFlag(cmd, dateFlag, date, emptyWorkDate); fault != nil {
			return fault
		}
		if fault := rejectEmptyFlag(cmd, typeFlag, workType, emptyWorkType); fault != nil {
			return fault
		}
		written, fault := workItemAttributes(attributes)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			in := &youtrack.WorkItemInput{Duration: spent, Date: date, Text: text, Type: workType, Attributes: written}
			return c.WorkItems.Create(ctx, args[0], in, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	create.Args = cobra.ExactArgs(2)
	create.Short = "Log time"
	create.Long = "Log time as you.\n\n" +
		"<duration> is an ISO 8601 period of hours and minutes, such as PT1H30M. --date is today if left out. " +
		"--type is one of the workItemTypes ytrack project show prints, --attribute one of its attributes."
	create.Flags().StringVar(&date, dateFlag, "", "`day`, as in 2026-09-01")
	rejectRepeat(create.Flags().Lookup(dateFlag))
	create.Flags().StringVar(&workType, typeFlag, "", "work item type `name`")
	rejectRepeat(create.Flags().Lookup(typeFlag))
	create.Flags().StringVar(&text, textFlag, "", "work item `text`")
	rejectRepeat(create.Flags().Lookup(textFlag))
	create.Flags().StringArrayVar(&attributes, attributeFlag, nil, "work item attribute `Name=value`; repeatable")
	fieldsFlag(create, &fields, workItemWriteFields)
	return create
}

func newTimeUpdate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, spent, date, text, workType string
	var attributes, emptied []string
	update := newCommand("update <issue> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		in := &youtrack.WorkItemUpdate{Date: flagValue(cmd, dateFlag, &date), Text: flagValue(cmd, textFlag, &text),
			Type: flagValue(cmd, typeFlag, &workType)}
		if cmd.Flags().Changed(durationFlag) {
			length, fault := parseDuration(spent)
			if fault != nil {
				return fault
			}
			in.Duration = &length
		}
		clears, fault := workItemClearsOf(emptied)
		if fault != nil {
			return fault
		}
		in.ClearText, in.ClearType = clears.text, clears.workType
		if in.Attributes, fault = workItemAttributes(attributes); fault != nil {
			return fault
		}
		in.Attributes = append(in.Attributes, clears.attributes...)
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.WorkItems.Update(ctx, args[0], args[1], in, &youtrack.WriteOptions{Fields: fieldsFlagValue(cmd, &fields)})
		})
	})
	update.Args = cobra.ExactArgs(2)
	update.Short = "Update a work item"
	update.Long = "Update a work item; unflagged parts stay.\n\n" +
		"<id> is the work item id such as 150-1 that ytrack time list prints."
	update.Flags().StringVar(&spent, durationFlag, "", "`duration`, as in PT1H30M")
	rejectRepeat(update.Flags().Lookup(durationFlag))
	update.Flags().StringVar(&date, dateFlag, "", "`day`, as in 2026-09-01")
	rejectRepeat(update.Flags().Lookup(dateFlag))
	update.Flags().StringVar(&workType, typeFlag, "", "work item type `name`")
	rejectRepeat(update.Flags().Lookup(typeFlag))
	update.Flags().StringVar(&text, textFlag, "", "work item `text`")
	rejectRepeat(update.Flags().Lookup(textFlag))
	update.Flags().StringArrayVar(&attributes, attributeFlag, nil, "work item attribute `Name=value`; repeatable")
	update.Flags().StringArrayVar(&emptied, clearFlag, nil, "empty a `part`: type, text or an attribute name")
	fieldsFlag(update, &fields, workItemWriteFields)
	return update
}

func newTimeDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	del := newCommand("delete <issue> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		return runCall(cmd.Context(), env, stdout, renderer, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return c.WorkItems.Delete(ctx, args[0], args[1])
		})
	})
	del.Args = cobra.ExactArgs(2)
	del.Short = "Delete a work item"
	del.Long = "Delete a work item.\n\n" +
		"<id> is the work item id such as 150-1 that ytrack time list prints."
	return del
}

type commentsValue struct {
	text     string
	comments youtrack.Comments
}

const everyComment = "all"

func (v *commentsValue) String() string {
	return v.text
}

func (v *commentsValue) Set(text string) error {
	if text == everyComment {
		v.text, v.comments = text, youtrack.AllComments()
		return nil
	}
	last, err := strconv.Atoi(text)
	switch {
	case errors.Is(err, strconv.ErrRange):
		return errors.New("it is a larger number than there could ever be comments")
	case err != nil:
		return fmt.Errorf("it is neither %s nor a whole number of comments", everyComment)
	case last < 0:
		return errors.New("a number of comments is not negative")
	}
	v.text, v.comments = text, youtrack.LastComments(last)
	return nil
}

func (v *commentsValue) Type() string {
	return "count"
}

const listLimit = 50

func pageFlags(cmd *cobra.Command, page *youtrack.Page, plural string) {
	page.Limit = listLimit
	cmd.Flags().Var((*limitValue)(&page.Limit), "limit", "max "+plural)
	rejectRepeat(cmd.Flags().Lookup("limit"))
	cmd.Flags().IntVar(&page.Skip, "skip", 0, plural+" to pass over before the first")
	rejectRepeat(cmd.Flags().Lookup("skip"))
}

// The SDK has no default fields: a command holds its own.
const (
	activityListFields   = "timestamp,author(login),category,field,added(id,idReadable,login,name,urls),removed(id,idReadable,login,name,urls)"
	articleListFields    = "idReadable,summary"
	articleShowFields    = "idReadable,summary,reporter(login),created,updated,tags(name),parentArticle(idReadable,summary),childArticles(idReadable,summary),content"
	attachmentListFields = "id,name,size,mimeType,url"
	commentFields        = "id,author(login),created,updated,text"
	commentListFields    = "id,author(login),created,text,deleted"
	fieldListFields      = "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty"
	fieldShowFields      = fieldListFields + ",bundle(values(name,archived),aggregatedUsers(login))"
	issueListFields      = "idReadable,summary,resolved,created"
	issueShowFields      = "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields,links(issues(idReadable,summary)),description"
	linkListFields       = "idReadable,summary"
	tagListFields        = "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login))"
	tagCreateFields      = "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login)),updateSharingSettings(permittedGroups(name),permittedUsers(login)),tagSharingSettings(permittedGroups(name),permittedUsers(login))"
	userListFields       = "login,fullName,banned"
	userShowFields       = "login,fullName,email,banned"
	workItemListFields   = "id,duration,type(name),attributes,author(login),date,text"
	workItemWriteFields  = "id,duration,type(name),attributes,author(login),date,issue(idReadable,customFields),text"
)

// The module reads a limit of 0 as its default page.
type limitValue int

func (v *limitValue) String() string {
	return strconv.Itoa(int(*v))
}

func (v *limitValue) Set(text string) error {
	limit, err := strconv.Atoi(text)
	switch {
	case err != nil:
		return err
	case limit < 1:
		return errors.New("a page holds at least one record")
	}
	*v = limitValue(limit)
	return nil
}

func (v *limitValue) Type() string {
	return "int"
}

func fieldsFlag(cmd *cobra.Command, expression *string, defaults string) {
	cmd.Flags().StringVar(expression, "fields", defaults, "YouTrack fields `expression`; +expr adds to the default")
	rejectRepeat(cmd.Flags().Lookup("fields"))
}

func commentsFlag(cmd *cobra.Command, comments *commentsValue) {
	cmd.Flags().Var(comments, "comments",
		"latest comments to print, 0 for none")
	rejectRepeat(cmd.Flags().Lookup("comments"))
}

func fieldsFlagValue(cmd *cobra.Command, expression *string) string {
	return script.Fields(cmd.Flags().Lookup("fields").DefValue, *expression)
}

func rejectNoQuery(cmd *cobra.Command, carries, thing string) *diag.Fault {
	if cmd.Flags().Changed("query") {
		return nil
	}
	message := fmt.Sprintf(`no --query was given: it carries %s, and --query "" finds every %s`, carries, thing)
	return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
}

type call func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error)

func runCall(ctx context.Context, env []string, stdout io.Writer, renderer render.Renderer, call call) *diag.Fault {
	_, node, fault := connectAndCall(ctx, env, call)
	if fault != nil {
		return fault
	}
	return printNode(stdout, renderer, node)
}

func connectAndCall(ctx context.Context, env []string, call call) (connection, *youtrack.Node, *diag.Fault) {
	c, fault := connect(env)
	if fault != nil {
		return connection{}, nil, fault
	}
	node, fault := c.call(ctx, call)
	if fault != nil {
		return connection{}, nil, fault
	}
	return c, node, nil
}

func (c connection) call(ctx context.Context, call call) (*youtrack.Node, *diag.Fault) {
	node, err := call(ctx, c.client)
	if err != nil {
		var fault *diag.Fault
		if !errors.As(err, &fault) {
			fault = diag.FromError(err)
		}
		return nil, c.withLoginSource(fault)
	}
	return node, nil
}

func printNode(stdout io.Writer, renderer render.Renderer, node *youtrack.Node) *diag.Fault {
	if err := renderer.Render(stdout, node); err != nil {
		return &diag.Fault{Code: youtrack.CodeUpstreamFailed, Message: err.Error()}
	}
	return nil
}

func rejectRepeat(flag *pflag.Flag) {
	flag.Value = &onceValue{Value: flag.Value}
}

type onceValue struct {
	pflag.Value
	given bool
}

func (v *onceValue) Set(value string) error {
	if v.given {
		return errors.New("the flag is given more than once")
	}
	v.given = true
	return v.Value.Set(value)
}

func requireSubcommand(cmd *cobra.Command, args []string) *diag.Fault {
	if len(args) == 0 {
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: "no command given"}
	}
	return unknownCommand(cmd, args[0])
}

func flagMessage(err error) string {
	var invalid *pflag.InvalidValueError
	var required *pflag.ValueRequiredError
	var unknown *pflag.NotExistError
	switch {
	case errors.As(err, &invalid):
		flag := "--" + invalid.GetFlag().Name
		if invalid.GetFlag().Shorthand != "" {
			flag = "-" + invalid.GetFlag().Shorthand + ", " + flag
		}
		cause := errors.Unwrap(invalid)
		var number *strconv.NumError
		if errors.As(cause, &number) {
			cause = number.Err
		}
		return fmt.Sprintf("invalid argument %s for %s flag: %v", render.Quote(invalid.GetValue()), render.Quote(flag), cause)
	case errors.As(err, &required) && required.GetSpecifiedShortnames() != "":
		return "flag needs an argument: " + render.Quote(required.GetSpecifiedName()) + " in -" +
			required.GetSpecifiedShortnames()
	case errors.As(err, &unknown) && unknown.GetSpecifiedShortnames() != "":
		return "unknown shorthand flag: " + render.Quote(unknown.GetSpecifiedName()) + " in -" +
			unknown.GetSpecifiedShortnames()
	}
	return err.Error()
}

// cobra reports an unknown word under the root in an untyped error that quotes it with %q.
var cobraUnknownCommand = regexp.MustCompile(`^unknown command ("(?:[^"\\]|\\.)*") for ("(?:[^"\\]|\\.)*")`)

func cobraMessage(err error) string {
	message := err.Error()
	found := cobraUnknownCommand.FindStringSubmatch(message)
	if found == nil {
		return message
	}
	word, wordErr := strconv.Unquote(found[1])
	path, pathErr := strconv.Unquote(found[2])
	if wordErr != nil || pathErr != nil {
		return message
	}
	return "unknown command " + render.Quote(word) + " for " + render.Quote(path) + message[len(found[0]):]
}

func unknownCommand(parent *cobra.Command, word string) *diag.Fault {
	message := fmt.Sprintf("unknown command %s for %s", render.Quote(word), render.Quote(parent.CommandPath()))
	return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
}

func newCommand(use string, run func(cmd *cobra.Command, args []string) *diag.Fault) *cobra.Command {
	return &cobra.Command{Use: use, RunE: runE(run)}
}

func runE[T any](f func(cmd *cobra.Command, arg T) *diag.Fault) func(*cobra.Command, T) error {
	return func(cmd *cobra.Command, arg T) error {
		if fault := f(cmd, arg); fault != nil {
			return fault
		}
		return nil
	}
}
