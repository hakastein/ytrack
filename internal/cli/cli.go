// Package cli is every command of ytrack behind one entry point, Run; the process
// itself is touched only by cmd/ytrack.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
	"github.com/hakastein/ytrack/internal/youtrack"
)

// Run takes argv without the program name and env as KEY=value pairs. build is the stamp go build left in the
// binary, read by cmd/ytrack and handed over here like the environment: the image of the process is the
// process's, and nothing below this seam reads it (ADR-0006). A caller with no stamp to hand — a binary the Go
// toolchain did not build — hands nil. stdin is a file rather than a reader because the one thing done with it
// is asking whether it is a terminal, and only auth login asks. The commands are built anew on every call, so
// calls may run in parallel.
func Run(ctx context.Context, argv, env []string, build *debug.BuildInfo, stdin *os.File, stdout, stderr io.Writer) int {
	renderer := render.YAML{}
	stream := diag.NewStream(stderr, renderer)
	// Given nil, cobra reads the process's own arguments.
	if argv == nil {
		argv = []string{}
	}
	root := newRoot(env, build, stdin, stdout, renderer, stream)
	err := execute(ctx, root, argv, stdout)
	if err == nil {
		return 0
	}
	// Every function ytrack hands cobra returns a *diag.Fault, so any other error is
	// cobra or pflag refusing the call.
	var fault *diag.Fault
	if !errors.As(err, &fault) {
		fault = &diag.Fault{Code: diag.BadUsage, Message: cobraMessage(err)}
	}
	stream.Fail(fault)
	return fault.ExitCode()
}

// execute is the tree answering argv, which is cobra's work but for the completion protocol. A shell asks for
// it by the name of a hidden command cobra builds on its own, and cobra's answer to it reads the process's
// environment and writes to the process's stderr and to a file named by it, so ytrack answers first. The
// command is not put in the tree instead: cobra adds its own under that name whatever else stands there.
func execute(ctx context.Context, root *cobra.Command, argv []string, stdout io.Writer) error {
	if len(argv) > 0 {
		switch argv[0] {
		case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			if fault := complete(root, stdout, argv[0], argv[1:]); fault != nil {
				return fault
			}
			return nil
		}
	}
	root.SetArgs(argv)
	return root.ExecuteContext(ctx)
}

// The tree gets no stderr, so nothing in it writes there; the stream reaches the one group
// holding a command that warns, and every other command is built without it.
func newRoot(env []string, build *debug.BuildInfo, stdin *os.File, stdout io.Writer, renderer render.Renderer,
	stream *diag.Stream,
) *cobra.Command {
	var version bool
	root := newCommand("ytrack", func(cmd *cobra.Command, args []string) *diag.Fault {
		if version {
			return printNode(stdout, renderer, versionNode(build))
		}
		return rejectGroup(cmd, args)
	})
	root.SilenceErrors = true
	root.SilenceUsage = true
	// cobra builds a "completion" of its own on the spot when argv names it, subcommands and all;
	// ytrack's own stands in the tree instead.
	root.CompletionOptions.DisableDefaultCmd = true
	// That leaves the hidden completion protocol, which argv reaches even behind a flag,
	// where Run does not answer it and cobra would; no shell writes such a call.
	root.PersistentPreRunE = runE(func(cmd *cobra.Command, _ []string) *diag.Fault {
		switch cmd.CalledAs() {
		case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return unknownCommand(cmd.Root(), cmd.CalledAs())
		}
		return nil
	})
	// cobra's own help command, added once there are subcommands, answers an unknown topic
	// with exit code 0 and exits the process when it cannot print. The stand-in is not called
	// help: the template lists a command of that name even when it is hidden, so only a name of
	// its own keeps it out of the list, and ytrack help then meets the refusal of any unknown
	// command. The name is kept short because cobra pads the whole column by the longest name
	// among the commands, the hidden ones with them.
	help := newCommand("no-help", func(cmd *cobra.Command, _ []string) *diag.Fault {
		return unknownCommand(cmd.Root(), cmd.CalledAs())
	})
	help.Hidden = true
	root.SetHelpCommand(help)
	// SetHelpCommand only remembers it; the tree gets it inside ExecuteC, which the completion protocol never
	// reaches. It is put there at once instead, so that the tree the protocol answers from is the tree that
	// runs the commands, and the one command hidden in it is hidden from the shell as it is from the help.
	root.AddCommand(help)
	root.SetOut(stdout)
	// With errors silenced and the completion protocol refused, cobra has nothing left
	// to write to its error writer; the error itself reaches Run as a refusal.
	root.SetErr(io.Discard)
	root.SetFlagErrorFunc(runE(func(_ *cobra.Command, err error) *diag.Fault {
		return &diag.Fault{Code: diag.BadUsage, Message: flagMessage(err)}
	}))
	// Local and not persistent, so the flag stands on ytrack alone. cobra's own Version field is left
	// unset: it prints a template of its own past the renderer, and it takes -v while -v is free.
	root.Flags().BoolVar(&version, "version", false, "print the version and the revision this binary was built from")
	// The tree gets no reader either: asked for one it was never given, cobra answers with the process's own
	// standard input, which nothing here may touch, so the file Run was handed goes to the one command that reads.
	root.AddCommand(newActivity(env, stdout, renderer), newArticle(env, stdout, renderer),
		newAttachment(env, stdout, renderer), newAuth(env, stdin, stdout, renderer),
		newComment(env, stdout, renderer), newCompletion(stdout), newField(env, stdout, renderer),
		newIssue(env, stdout, renderer, stream), newLink(env, stdout, renderer),
		newProject(env, stdout, renderer), newTag(env, stdout, renderer),
		newTime(env, stdout, renderer), newUser(env, stdout, renderer))
	return root
}

func newTag(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	tag := newCommand("tag", rejectGroup)
	tag.Short = "Manage tags"
	tag.AddCommand(newTagList(env, stdout, renderer), newTagCreate(env, stdout, renderer),
		newTagDelete(env, stdout, renderer), newTagAdd(env, stdout, renderer),
		newTagRemove(env, stdout, renderer))
	return tag
}

func newTagRemove(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var name, ownedBy string
	remove := newCommand("remove <issue or article>", func(cmd *cobra.Command, args []string) *diag.Fault {
		if fault := rejectNoName(cmd); fault != nil {
			return fault
		}
		call, fault := youtrack.RemoveTag(args[0], name, flagValue(cmd, ownedByFlag, &ownedBy))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	remove.Args = cobra.ExactArgs(1)
	remove.Short = "Untag an issue or article"
	remove.Long = "Untag an issue or article. The tag stays.\n\n" +
		example(render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
			render.Pair{Key: "removed", Value: render.NewMap(render.Pair{Key: "name", Value: render.NewString("Tag")}, render.Pair{Key: "owner", Value: byLogin()})}))
	remove.Flags().StringVar(&name, nameFlag, "", "tag `name`")
	rejectRepeat(remove.Flags().Lookup(nameFlag))
	ownedByFlagOf(remove, &ownedBy)
	return remove
}

func newTagAdd(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var name, ownedBy string
	add := newCommand("add <issue or article>", func(cmd *cobra.Command, args []string) *diag.Fault {
		if fault := rejectNoName(cmd); fault != nil {
			return fault
		}
		call, fault := youtrack.AddTag(args[0], name, flagValue(cmd, ownedByFlag, &ownedBy))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	add.Args = cobra.ExactArgs(1)
	add.Short = "Tag an issue or article"
	add.Long = "Tag an issue or article.\n\n" +
		example(render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
			render.Pair{Key: "added", Value: render.NewMap(render.Pair{Key: "name", Value: render.NewString("Tag")}, render.Pair{Key: "owner", Value: byLogin()})}))
	add.Flags().StringVar(&name, nameFlag, "", "tag `name`")
	rejectRepeat(add.Flags().Lookup(nameFlag))
	ownedByFlagOf(add, &ownedBy)
	return add
}

func newTagCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, name string
	var shared youtrack.TagSharing
	create := newCommand("create", func(cmd *cobra.Command, _ []string) *diag.Fault {
		if fault := rejectNoName(cmd); fault != nil {
			return fault
		}
		call, fault := youtrack.CreateTag(name, shared, fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	create.Args = cobra.ExactArgs(0)
	create.Short = "Create a tag"
	create.Long = "Create a tag owned by you.\n\n" +
		example(render.NewMap(render.Pair{Key: "name", Value: render.NewString("Tag")}, render.Pair{Key: "owner", Value: byLogin()},
			render.Pair{Key: "readSharingSettings", Value: sharedWith(named("Group"))},
			render.Pair{Key: "updateSharingSettings", Value: sharedWith()},
			render.Pair{Key: "tagSharingSettings", Value: sharedWith()})) + "\n\n" +
		"Without flags only the owner sees the tag. Only --taggable-by lets a group hang it."
	create.Flags().StringVar(&name, nameFlag, "", "tag `name`")
	rejectRepeat(create.Flags().Lookup(nameFlag))
	create.Flags().StringArrayVar((*[]string)(&shared.VisibleFor), visibleForFlag, nil,
		"`group` that sees the tag; repeatable")
	create.Flags().StringArrayVar((*[]string)(&shared.UpdatableBy), updateableByFlag, nil,
		"`group` that may edit the tag; repeatable")
	create.Flags().StringArrayVar((*[]string)(&shared.TaggableBy), taggableByFlag, nil,
		"`group` that may tag with it; repeatable")
	fieldsFlag(create, &fields, youtrack.TagCreateFields)
	return create
}

func newTagDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var name, ownedBy string
	remove := newCommand("delete", func(cmd *cobra.Command, _ []string) *diag.Fault {
		if fault := rejectNoName(cmd); fault != nil {
			return fault
		}
		call, fault := youtrack.DeleteTag(name, flagValue(cmd, ownedByFlag, &ownedBy))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	remove.Args = cobra.ExactArgs(0)
	remove.Short = "Delete a tag"
	remove.Long = "Delete a tag everywhere.\n\n" +
		example(render.NewMap(render.Pair{Key: "name", Value: render.NewString("Tag")}, render.Pair{Key: "owner", Value: byLogin()})) + "\n\n" +
		"An administrator's token deletes a tag of another user as well."
	remove.Flags().StringVar(&name, nameFlag, "", "tag `name`")
	rejectRepeat(remove.Flags().Lookup(nameFlag))
	ownedByFlagOf(remove, &ownedBy)
	return remove
}

// The value of the flag reaches the call by written, like the value of every other flag that may go unwritten:
// an empty login names a user YouTrack keeps none of and is refused, while no flag at all is the caller naming
// the tag by its name alone.
func ownedByFlagOf(cmd *cobra.Command, login *string) {
	cmd.Flags().StringVar(login, ownedByFlag, "", "owner `login`, when names clash")
	rejectRepeat(cmd.Flags().Lookup(ownedByFlag))
}

func rejectNoName(cmd *cobra.Command) *diag.Fault {
	if cmd.Flags().Changed(nameFlag) {
		return nil
	}
	message := "no --name was given: it carries the name of the tag, which is what a tag is addressed by"
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func newTagList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var page youtrack.Page
	list := newCommand("list", func(cmd *cobra.Command, _ []string) *diag.Fault {
		call, fault := youtrack.ListTags(fieldsFlagValue(cmd, &fields), page)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(0)
	list.Short = "List tags"
	list.Long = "List tags you own or that are shared with you. Names may clash across owners.\n\n" +
		example(listed(1, false, "tags", render.NewMap(render.Pair{Key: "name", Value: render.NewString("Tag")}, render.Pair{Key: "owner", Value: byLogin()},
			render.Pair{Key: "readSharingSettings", Value: sharedWith(named("Group"))})))
	fieldsFlag(list, &fields, youtrack.TagListFields)
	pageFlags(list, &page, "tags")
	return list
}

func newLink(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var listFields string
	list := newCommand("list <issue>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ListLinks(args[0], fieldsFlagValue(cmd, &listFields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List links of an issue"
	list.Long = "List links of an issue by phrase.\n\n" +
		"A phrase reads from this issue to the linked ones.\n\n" +
		example(linksOf(2)) + "\n\n" +
		"--fields applies to the linked issues."
	fieldsFlag(list, &listFields, youtrack.LinkListFields)

	var addFields string
	add := newCommand("add <id> <phrase> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.AddLink(args[0], args[1], args[2], fieldsFlagValue(cmd, &addFields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	add.Args = cobra.ExactArgs(3)
	add.Short = "Link two issues"
	add.Long = "Link two issues; prints the links of the first.\n\n" +
		"<phrase> reads from the first issue to the second, such as \"depends on\" or \"is required for\": " +
		"ytrack link add DEV-1 \"depends on\" DEV-2.\n\n" +
		example(linksOf(1))
	fieldsFlag(add, &addFields, youtrack.LinkListFields)

	remove := newCommand("remove <id> <phrase> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.RemoveLink(args[0], args[1], args[2])
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	remove.Args = cobra.ExactArgs(3)
	remove.Short = "Unlink two issues"
	remove.Long = "Unlink two issues.\n\n" +
		"A link can be named from either end: DEV-1 \"depends on\" DEV-2 and DEV-2 \"is required for\" DEV-1 remove " +
		"the same link. A state a workflow set when the link was made stays.\n\n" +
		example(render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")},
			render.Pair{Key: "removed", Value: render.NewMap(render.FromData("depends on",
				render.NewList(render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-2")}))))}))

	link := newCommand("link", rejectGroup)
	link.Short = "Manage issue links"
	link.AddCommand(list, add, remove)
	return link
}

func newArticle(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	article := newCommand("article", rejectGroup)
	article.Short = "Manage articles"
	article.AddCommand(newArticleShow(env, stdout, renderer), newArticleList(env, stdout, renderer),
		newArticleCreate(env, stdout, renderer), newArticleUpdate(env, stdout, renderer),
		newArticleDelete(env, stdout, renderer))
	return article
}

func newArticleCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, summary, content, parent string
	create := newCommand("create <project>", func(cmd *cobra.Command, args []string) *diag.Fault {
		// Пустой --summary и отсутствие флага — разные ошибки, различает только Changed.
		if !cmd.Flags().Changed(summaryFlag) {
			message := "no --summary was given: it carries the title of the article, which YouTrack files none without"
			return &diag.Fault{Code: diag.BadUsage, Message: message}
		}
		call, fault := youtrack.CreateArticle(args[0], summary, flagValue(cmd, contentFlag, &content),
			flagValue(cmd, parentFlag, &parent), fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	create.Args = cobra.ExactArgs(1)
	create.Short = "Create an article"
	create.Long = "Create an article in a project.\n\n" +
		example(articleExample(false))
	create.Flags().StringVar(&summary, summaryFlag, "", "`title`")
	rejectRepeat(create.Flags().Lookup(summaryFlag))
	create.Flags().StringVar(&content, contentFlag, "", "article `text`")
	rejectRepeat(create.Flags().Lookup(contentFlag))
	create.Flags().StringVar(&parent, parentFlag, "", "parent article `id`")
	rejectRepeat(create.Flags().Lookup(parentFlag))
	fieldsFlag(create, &fields, youtrack.ArticleShowFields)
	return create
}

func newArticleUpdate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, summary, content, parent string
	var emptied []string
	update := newCommand("update <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.UpdateArticle(args[0], flagValue(cmd, summaryFlag, &summary),
			flagValue(cmd, contentFlag, &content), flagValue(cmd, parentFlag, &parent), emptied,
			fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	update.Args = cobra.ExactArgs(1)
	update.Short = "Update an article"
	update.Long = "Update an article; unflagged parts stay.\n\n" +
		example(articleExample(false))
	update.Flags().StringVar(&summary, summaryFlag, "", "`title`")
	rejectRepeat(update.Flags().Lookup(summaryFlag))
	update.Flags().StringVar(&content, contentFlag, "", "article `text`")
	rejectRepeat(update.Flags().Lookup(contentFlag))
	update.Flags().StringVar(&parent, parentFlag, "", "parent article `id`")
	rejectRepeat(update.Flags().Lookup(parentFlag))
	update.Flags().StringArrayVar(&emptied, clearFlag, nil, "empty a `part`: content or parent")
	fieldsFlag(update, &fields, youtrack.ArticleShowFields)
	return update
}

func newArticleDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	del := newCommand("delete <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.DeleteArticle(args[0])
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	del.Args = cobra.ExactArgs(1)
	del.Short = "Delete an article with its children"
	del.Long = "Delete an article with its children.\n\n" +
		example(render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-A-1")}))
	return del
}

func newArticleList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, query, parent string
	var page youtrack.Page
	list := newCommand("list", func(cmd *cobra.Command, _ []string) *diag.Fault {
		call, fault := articlesListed(cmd, query, parent, fieldsFlagValue(cmd, &fields), page)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(0)
	list.Short = "Search articles"
	list.Long = "Search articles.\n\n" +
		example(listed(1, false, "articles", render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-A-1")},
			render.Pair{Key: "summary", Value: render.NewString("Summary")}))) + "\n\n" +
		"--query takes the search attributes of articles: project or in, title, content, author, article id, tag, " +
		"created, updated, updater, has, sort by. An attribute of issues, such as summary, finds nothing, and a query " +
		"the server cannot parse finds every article, neither with an error.\n\n" +
		"--parent lists the children of an article instead, and takes no --query."
	list.Flags().StringVar(&query, "query", "", "YouTrack `search`")
	rejectRepeat(list.Flags().Lookup("query"))
	list.Flags().StringVar(&parent, parentFlag, "", "parent article `id`")
	rejectRepeat(list.Flags().Lookup(parentFlag))
	fieldsFlag(list, &fields, youtrack.ArticleListFields)
	pageFlags(list, &page, "articles")
	return list
}

func articlesListed(cmd *cobra.Command, query, parent string, expression *string, page youtrack.Page) (youtrack.Call, *diag.Fault) {
	if !cmd.Flags().Changed(parentFlag) {
		if fault := rejectNoQuery(cmd, "the search to run", "article"); fault != nil {
			return nil, fault
		}
		return youtrack.ListArticles(query, expression, page)
	}
	if cmd.Flags().Changed("query") {
		message := "--parent and --query were both given: the children of an article are listed with no search"
		return nil, &diag.Fault{Code: diag.BadUsage, Message: message}
	}
	return youtrack.ListChildArticles(parent, expression, page)
}

func newArticleShow(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	comments := commentsValue{comments: youtrack.AllComments()}
	show := newCommand("show <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ShowArticle(args[0], fieldsFlagValue(cmd, &fields), comments.comments)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	show.Args = cobra.ExactArgs(1)
	show.Short = "Show an article"
	show.Long = "Show an article with comments, oldest first.\n\n" +
		example(articleExample(true))
	fieldsFlag(show, &fields, youtrack.ArticleShowFields)
	commentsFlag(show, &comments)
	return show
}

func newAttachment(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	attachment := newCommand("attachment", rejectGroup)
	attachment.Short = "Manage attachments"
	attachment.AddCommand(newAttachmentCreate(env, stdout, renderer), newAttachmentDelete(env, stdout, renderer),
		newAttachmentList(env, stdout, renderer))
	return attachment
}

func newAttachmentDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	remove := newCommand("delete <owner> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.DeleteAttachment(args[0], args[1])
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	remove.Args = cobra.ExactArgs(2)
	remove.Short = "Delete an attachment"
	remove.Long = "Delete an attachment.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1. <id> is the attachment id such as 12-1 that ytrack " +
		"attachment list prints, not a file name. A file attached to a comment belongs to the issue.\n\n" +
		example(render.NewMap(render.Pair{Key: "id", Value: render.NewString("12-1")}, render.Pair{Key: "name", Value: render.NewString("file.txt")},
			render.Pair{Key: "issue", Value: render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")})}))
	return remove
}

func newAttachmentCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	create := newCommand("create <owner> <path>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.CreateAttachment(args[0], func() (youtrack.AttachedFile, *diag.Fault) {
			return openLocalFile(args[1])
		}, fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	create.Args = cobra.ExactArgs(2)
	// The one command of ytrack an argument of which is a file, and it is the second argument: the shell
	// completes names where the path stands, and nowhere else.
	create.Annotations = map[string]string{completesPathAt: "2"}
	create.Short = "Attach a file"
	create.Long = "Attach a local file.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1.\n\n" +
		example(attachmentExample())
	fieldsFlag(create, &fields, youtrack.AttachmentListFields)
	return create
}

func newAttachmentList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var page youtrack.Page
	list := newCommand("list <owner>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ListAttachments(args[0], fieldsFlagValue(cmd, &fields), page)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List attachments"
	list.Long = "List attachments, including those of comments; " +
		"--fields +comment(id) says which comment.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1.\n\n" +
		example(listed(1, false, "attachments", attachmentExample())) + "\n\n" +
		"url downloads the file without a token for up to three days: keep it as secret as a token."
	fieldsFlag(list, &fields, youtrack.AttachmentListFields)
	pageFlags(list, &page, "attachments")
	return list
}

func newComment(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	comment := newCommand("comment", rejectGroup)
	comment.Short = "Manage comments"
	comment.AddCommand(newCommentList(env, stdout, renderer), newCommentCreate(env, stdout, renderer),
		newCommentUpdate(env, stdout, renderer), newCommentDelete(env, stdout, renderer))
	return comment
}

func newCommentList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var page youtrack.Page
	list := newCommand("list <owner>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ListComments(args[0], fieldsFlagValue(cmd, &fields), page)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List comments"
	list.Long = "List comments, oldest first.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1.\n\n" +
		example(listed(2, false, "comments",
			render.NewMap(render.Pair{Key: "id", Value: render.NewString("7-1")}, render.Pair{Key: "author", Value: byLogin()}, render.Pair{Key: "created", Value: moment()},
				render.Pair{Key: "text", Value: render.NewString("Text")}, render.Pair{Key: "deleted", Value: render.NewBool(false)}),
			render.NewMap(render.Pair{Key: "id", Value: render.NewString("7-2")}, render.Pair{Key: "author", Value: byLogin()}, render.Pair{Key: "created", Value: moment()},
				render.Pair{Key: "text", Value: render.NewNull()}, render.Pair{Key: "deleted", Value: render.NewBool(true)}))) + "\n\n" +
		"Comments of an article have no deleted. --limit keeps the oldest; ytrack issue show --comments 5 prints " +
		"the latest five."
	fieldsFlag(list, &fields, youtrack.CommentListFields())
	pageFlags(list, &page, "comments")
	return list
}

// A write of a comment does not print its owner, and the key the owner goes by depends on its kind.
const commentOwner = "--fields +issue(...) on a comment of an article, or +article(...) on one of an issue, is " +
	"refused only after the comment is written."

// The refusal a write of a comment gives where the flag carrying its text was not written at all, which is a
// different mistake from writing it empty and is told from it by the flag alone.
const noCommentText = "no --text was given: it carries the text of the comment, which is the whole of what a " +
	"comment is"

func newCommentCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, text string
	create := newCommand("create <owner>", func(cmd *cobra.Command, args []string) *diag.Fault {
		// Read before the owner, so that a call giving neither is answered about the text: an empty text and no
		// text at all are different mistakes, and only the flag tells them apart.
		if !cmd.Flags().Changed(textFlag) {
			return &diag.Fault{Code: diag.BadUsage, Message: noCommentText}
		}
		call, fault := youtrack.CreateComment(args[0], text, fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	create.Args = cobra.ExactArgs(1)
	create.Short = "Add a comment"
	create.Long = "Add a comment.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1.\n\n" +
		example(commentExample(render.NewNull())) + "\n\n" + commentOwner
	create.Flags().StringVar(&text, textFlag, "", "comment `text`")
	rejectRepeat(create.Flags().Lookup(textFlag))
	fieldsFlag(create, &fields, youtrack.CommentFields)
	return create
}

func newCommentUpdate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, text string
	update := newCommand("update <owner> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		if !cmd.Flags().Changed(textFlag) {
			return &diag.Fault{Code: diag.BadUsage, Message: noCommentText}
		}
		call, fault := youtrack.UpdateComment(args[0], args[1], text, fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	update.Args = cobra.ExactArgs(2)
	update.Short = "Replace a comment's text"
	update.Long = "Replace a comment's text.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1. <id> is the comment id such as 7-1 that ytrack comment " +
		"list prints.\n\n" +
		example(commentExample(moment())) + "\n\n" + commentOwner
	update.Flags().StringVar(&text, textFlag, "", "comment `text`")
	rejectRepeat(update.Flags().Lookup(textFlag))
	fieldsFlag(update, &fields, youtrack.CommentFields)
	return update
}

func newCommentDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	del := newCommand("delete <owner> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.DeleteComment(args[0], args[1])
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	del.Args = cobra.ExactArgs(2)
	del.Short = "Delete a comment"
	del.Long = "Delete a comment.\n\n" +
		"<owner> is a readable id such as DEV-1 or DEV-A-1. <id> is the comment id such as 7-1 that ytrack comment " +
		"list prints.\n\n" +
		example(render.NewMap(render.Pair{Key: "id", Value: render.NewString("7-1")}))
	return del
}

func newIssue(env []string, stdout io.Writer, renderer render.Renderer, stream *diag.Stream) *cobra.Command {
	issue := newCommand("issue", rejectGroup)
	issue.Short = "Manage issues"
	issue.AddCommand(newIssueShow(env, stdout, renderer), newIssueList(env, stdout, renderer, stream),
		newIssueCreate(env, stdout, renderer), newIssueUpdate(env, stdout, renderer),
		newIssueDelete(env, stdout, renderer))
	return issue
}

func newIssueShow(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	comments := commentsValue{comments: youtrack.AllComments()}
	show := newCommand("show <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ShowIssue(args[0], fieldsFlagValue(cmd, &fields), comments.comments)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	show.Args = cobra.ExactArgs(1)
	show.Short = "Show an issue"
	show.Long = "Show an issue with comments, oldest first.\n\n" +
		example(issueExample(true)) + "\n\n" +
		"Custom fields are named inside customFields by name or localized name, in any letter case, quoted where " +
		"they hold a space: --fields '+customFields(Priority,\"Due Date\")'. A bare customFields prints every field " +
		"that holds something; a named field is printed even when empty, and one the issue does not have is left out.\n\n" +
		"Comments print these keys whatever --fields says, and deleted ones are left out."
	fieldsFlag(show, &fields, youtrack.IssueShowFields)
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
		call, fault := youtrack.ListIssues(query, fieldsFlagValue(cmd, &fields), page, stream.Warn)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(0)
	list.Short = "Search issues"
	list.Long = "Search issues.\n\n" +
		example(listed(1, false, "issues", render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")}, render.Pair{Key: "summary", Value: render.NewString("Summary")},
			render.Pair{Key: "customFields", Value: render.NewMap(render.FromData("State", render.NewString("Open")), render.FromData("Type", render.NewString("Task")))},
			render.Pair{Key: "created", Value: moment()}))) + "\n\n" +
		"Custom fields are named inside customFields by name or localized name, in any letter case, quoted where " +
		"they hold a space: --fields '+customFields(Priority,\"Due Date\")'. A bare customFields prints every field " +
		"that holds something; a named field is printed even when empty, and one the issue does not have is left out. " +
		"Links print under their phrase: --fields '+links(issues(idReadable))'."
	list.Flags().StringVar(&query, "query", "", "YouTrack `search`")
	rejectRepeat(list.Flags().Lookup("query"))
	fieldsFlag(list, &fields, youtrack.IssueListFields)
	pageFlags(list, &page, "issues")
	return list
}

func newIssueCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, summary, description string
	var filled []string
	create := newCommand("create <project>", func(cmd *cobra.Command, args []string) *diag.Fault {
		// An empty title and no title at all are different mistakes, and only the flag itself tells them apart.
		if !cmd.Flags().Changed(summaryFlag) {
			message := "no --summary was given: it carries the title of the issue, which YouTrack files none without"
			return &diag.Fault{Code: diag.BadUsage, Message: message}
		}
		call, fault := youtrack.CreateIssue(args[0], summary, flagValue(cmd, descriptionFlag, &description), filled,
			fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	create.Args = cobra.ExactArgs(1)
	create.Short = "Create an issue"
	create.Long = "Create an issue in a project.\n\n" +
		example(issueExample(false))
	create.Flags().StringVar(&summary, summaryFlag, "", "`title`")
	rejectRepeat(create.Flags().Lookup(summaryFlag))
	create.Flags().StringVar(&description, descriptionFlag, "", "`text` of the description")
	rejectRepeat(create.Flags().Lookup(descriptionFlag))
	create.Flags().StringArrayVar(&filled, fieldFlag, nil,
		"custom field `Name=value`; repeatable")
	fieldsFlag(create, &fields, youtrack.IssueShowFields)
	return create
}

func newIssueUpdate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, summary, description string
	var filled, emptied []string
	update := newCommand("update <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.UpdateIssue(args[0], flagValue(cmd, summaryFlag, &summary),
			flagValue(cmd, descriptionFlag, &description), filled, emptied, fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	update.Args = cobra.ExactArgs(1)
	update.Short = "Update an issue"
	update.Long = "Update an issue; unflagged parts stay.\n\n" +
		"--field on a field of several values leaves it holding exactly the values given, not the old ones plus " +
		"them.\n\n" +
		example(issueExample(false))
	update.Flags().StringVar(&summary, summaryFlag, "", "`title`")
	rejectRepeat(update.Flags().Lookup(summaryFlag))
	update.Flags().StringVar(&description, descriptionFlag, "", "`text` of the description")
	rejectRepeat(update.Flags().Lookup(descriptionFlag))
	update.Flags().StringArrayVar(&filled, fieldFlag, nil,
		"custom field `Name=value`; repeatable")
	update.Flags().StringArrayVar(&emptied, clearFlag, nil,
		"empty a custom field or description by `name`")
	fieldsFlag(update, &fields, youtrack.IssueShowFields)
	return update
}

func newIssueDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	del := newCommand("delete <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.DeleteIssue(args[0])
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	del.Args = cobra.ExactArgs(1)
	del.Short = "Delete an issue"
	del.Long = "Delete an issue.\n\n" +
		example(render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")}))
	return del
}

func newActivity(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var categories []string
	var page youtrack.Page
	list := newCommand("list <issue>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ListActivities(args[0], fieldsFlagValue(cmd, &fields), page, categories)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List the journal of an issue"
	list.Long = "List the journal of an issue, newest first.\n\n" +
		example(listed(uncounted, true, "activities",
			render.NewMap(render.Pair{Key: "timestamp", Value: moment()}, render.Pair{Key: "author", Value: byLogin()}, render.Pair{Key: "category", Value: render.NewString("CustomFieldCategory")},
				render.Pair{Key: "field", Value: render.NewString("State")},
				render.Pair{Key: "added", Value: render.NewList(render.NewMap(render.Pair{Key: "id", Value: render.NewString("150-2")}, render.Pair{Key: "name", Value: render.NewString("Value")}))},
				render.Pair{Key: "removed", Value: render.NewList(render.NewMap(render.Pair{Key: "id", Value: render.NewString("150-1")}, render.Pair{Key: "name", Value: render.NewString("Value")}))}),
			render.NewMap(render.Pair{Key: "timestamp", Value: moment()}, render.Pair{Key: "author", Value: byLogin()}, render.Pair{Key: "category", Value: render.NewString("DescriptionCategory")},
				render.Pair{Key: "field", Value: render.NewNull()},
				render.Pair{Key: "added", Value: render.NewList(render.NewString("New text"))}, render.Pair{Key: "removed", Value: render.NewList(render.NewString("Old text"))}))) +
		"\n\n" +
		"Changes of a description, a summary or a comment carry the whole text before and after. Narrow with " +
		"--category, or leave added and removed out of --fields.\n\n" +
		"--category takes " + strings.Join(youtrack.ActivityCategories(), ", ") + "."
	// A comma is part of the name: `LinksCategory,CommentsCategory` is a misspelling to report, not two
	// categories.
	list.Flags().StringArrayVar(&categories, "category", nil,
		"`category` to print; repeatable; default all")
	closedSet(list.Flags().Lookup("category"), youtrack.ActivityCategories())
	fieldsFlag(list, &fields, youtrack.ActivityListFields)
	pageFlags(list, &page, "activities")

	activity := newCommand("activity", rejectGroup)
	activity.Short = "Read issue journals"
	activity.AddCommand(list)
	return activity
}

// flagValue is the value of a flag the caller wrote, and nothing where they wrote no such flag: an empty value
// and no flag at all are two different things to say, and pflag holds the same empty string for both.
func flagValue(cmd *cobra.Command, flag string, value *string) *string {
	if !cmd.Flags().Changed(flag) {
		return nil
	}
	return value
}

// The free text of an issue goes by one name wherever it is written, so a caller who knows how to file an
// issue knows how to change one.
const (
	summaryFlag     = "summary"
	descriptionFlag = "description"
	contentFlag     = "content"
	textFlag        = "text"
	fieldFlag       = "field"
	attributeFlag   = "attribute"
	clearFlag       = "clear"
	parentFlag      = "parent"
	// The name of a tag is free text: a tag called --help or -x is a tag like any other, and text a caller wrote
	// reaches ytrack whole only as the value of a flag.
	nameFlag = "name"
	// The three sets of sharing a creation writes, each named after what a group in it may do rather than after
	// the member it goes into: being shown a tag, renaming it and hanging it are three rights YouTrack keeps
	// apart, and --taggable-by is the only one of the three that grants the last.
	visibleForFlag   = "visible-for"
	updateableByFlag = "updateable-by"
	taggableByFlag   = "taggable-by"
	// The other half of what tells one tag from another: the login of the user the tag belongs to, which is
	// what a name two tags carry is settled by.
	ownedByFlag  = "owned-by"
	dateFlag     = "date"
	typeFlag     = "type"
	durationFlag = "duration"
)

func newUser(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var showFields string
	show := newCommand("show <login>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ShowUser(args[0], showFields)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	show.Args = cobra.ExactArgs(1)
	show.Short = "Show a user"
	show.Long = "Show a user.\n\n" +
		"<login> is a login, not a full name; ytrack user list --query finds one by name.\n\n" +
		example(render.NewMap(render.Pair{Key: "login", Value: render.NewString("user")}, render.Pair{Key: "fullName", Value: render.NewString("User")},
			render.Pair{Key: "email", Value: render.NewString("user@example.com")}, render.Pair{Key: "banned", Value: render.NewBool(false)}))
	fieldsFlag(show, &showFields, youtrack.UserShowFields)

	var listFields, search string
	var page youtrack.Page
	list := newCommand("list", func(cmd *cobra.Command, _ []string) *diag.Fault {
		if fault := rejectNoQuery(cmd, "the text to search for", "user"); fault != nil {
			return fault
		}
		call, fault := youtrack.ListUsers(search, listFields, page)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(0)
	list.Short = "Search users"
	list.Long = "Search users by login or name prefix. " +
		"An email address matches nothing.\n\n" +
		example(listed(1, false, "users", render.NewMap(render.Pair{Key: "login", Value: render.NewString("user")}, render.Pair{Key: "fullName", Value: render.NewString("User")},
			render.Pair{Key: "banned", Value: render.NewBool(false)})))
	list.Flags().StringVar(&search, "query", "", "login or name prefix")
	rejectRepeat(list.Flags().Lookup("query"))
	fieldsFlag(list, &listFields, youtrack.UserListFields)
	pageFlags(list, &page, "users")

	user := newCommand("user", rejectGroup)
	user.Short = "Find users"
	user.AddCommand(show, list)
	return user
}

func newField(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var listFields string
	list := newCommand("list <project>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ListFields(args[0], listFields)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List custom fields of a project"
	list.Long = "List custom fields of a project.\n\n" +
		example(listed(1, false, "fields", fieldOf()))
	fieldsFlag(list, &listFields, youtrack.FieldListFields)

	var showFields string
	show := newCommand("show <project> <field>", func(cmd *cobra.Command, args []string) *diag.Fault {
		// What field show prints by default is a function of what the field holds, so no string stands in for
		// it here.
		call, fault := youtrack.ShowField(args[0], args[1], fieldsFlagValue(cmd, &showFields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	show.Args = cobra.ExactArgs(2)
	show.Short = "Show a custom field"
	show.Long = "Show a custom field with its allowed values.\n\n" +
		example(fieldOf(render.Pair{Key: "bundle", Value: render.NewMap(render.Pair{Key: "values", Value: render.NewList(render.NewMap(render.Pair{Key: "name", Value: render.NewString("Open")}, render.Pair{Key: "archived", Value: render.NewBool(false)}))})})) + "\n\n" +
		"A field of users prints bundle.aggregatedUsers(login) instead, where an empty list means anyone."
	fieldsFlag(show, &showFields, "")

	field := newCommand("field", rejectGroup)
	field.Short = "Read custom fields"
	field.AddCommand(list, show)
	return field
}

func newProject(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var showFields string
	show := newCommand("show <code>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ShowProject(args[0], showFields)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	show.Args = cobra.ExactArgs(1)
	show.Short = "Show a project"
	show.Long = "Show a project.\n\n" +
		"workItemTypes are what ytrack time create --type takes.\n\n" +
		example(render.NewMap(render.Pair{Key: "shortName", Value: render.NewString("DEV")}, render.Pair{Key: "name", Value: render.NewString("Project")},
			render.Pair{Key: "plugins", Value: render.NewMap(render.Pair{Key: "timeTrackingSettings", Value: render.NewMap(render.Pair{Key: "enabled", Value: render.NewBool(true)},
				render.Pair{Key: "workItemTypes", Value: render.NewList(named("Type"))})})}))
	fieldsFlag(show, &showFields, youtrack.ProjectShowFields)

	var listFields string
	var page youtrack.Page
	list := newCommand("list", func(cmd *cobra.Command, _ []string) *diag.Fault {
		call, fault := youtrack.ListProjects(listFields, page)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(0)
	list.Short = "List projects"
	list.Long = "List projects.\n\n" +
		example(listed(1, false, "projects", render.NewMap(render.Pair{Key: "shortName", Value: render.NewString("DEV")}, render.Pair{Key: "name", Value: render.NewString("Project")})))
	fieldsFlag(list, &listFields, youtrack.ProjectListFields)
	pageFlags(list, &page, "projects")

	project := newCommand("project", rejectGroup)
	project.Short = "Read projects"
	project.AddCommand(show, list)
	return project
}

func newTime(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	tracking := newCommand("time", rejectGroup)
	tracking.Short = "Manage logged time"
	tracking.AddCommand(newTimeList(env, stdout, renderer), newTimeCreate(env, stdout, renderer),
		newTimeUpdate(env, stdout, renderer), newTimeDelete(env, stdout, renderer))
	return tracking
}

func newTimeList(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields string
	var page youtrack.Page
	list := newCommand("list <issue>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.ListWorkItems(args[0], fieldsFlagValue(cmd, &fields), page)
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	list.Args = cobra.ExactArgs(1)
	list.Short = "List work items"
	list.Long = "List work items, oldest first.\n\n" +
		example(listed(1, false, "workItems", render.NewMap(render.Pair{Key: "id", Value: render.NewString("150-1")}, render.Pair{Key: "duration", Value: render.NewString("PT1H30M")},
			render.Pair{Key: "type", Value: named("Type")}, render.Pair{Key: "attributes", Value: render.NewMap(render.FromData("Attribute", render.NewString("Value")))},
			render.Pair{Key: "author", Value: byLogin()}, render.Pair{Key: "date", Value: moment()}, render.Pair{Key: "text", Value: render.NewString("Text")}))) +
		"\n\n" +
		"date is a day, printed as its midnight UTC."
	fieldsFlag(list, &fields, youtrack.WorkItemListFields)
	pageFlags(list, &page, "work items")
	return list
}

func newTimeCreate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, date, text, workType string
	var attributes []string
	create := newCommand("create <issue> <duration>", func(cmd *cobra.Command, args []string) *diag.Fault {
		// YouTrack itself defaults an omitted day to today, the type to none and the text to empty.
		call, fault := youtrack.CreateWorkItem(args[0], args[1], flagValue(cmd, dateFlag, &date),
			flagValue(cmd, textFlag, &text), flagValue(cmd, typeFlag, &workType), attributes, fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	create.Args = cobra.ExactArgs(2)
	create.Short = "Log time"
	create.Long = "Log time as you.\n\n" +
		"<duration> is an ISO 8601 period of hours and minutes, such as PT1H30M. --date is today if left out. " +
		"--type is one of the workItemTypes ytrack project show prints, --attribute one of its attributes.\n\n" +
		example(workItemExample())
	create.Flags().StringVar(&date, dateFlag, "", "`day`, as in 2026-09-01")
	rejectRepeat(create.Flags().Lookup(dateFlag))
	create.Flags().StringVar(&workType, typeFlag, "", "work item type `name`")
	rejectRepeat(create.Flags().Lookup(typeFlag))
	create.Flags().StringVar(&text, textFlag, "", "work item `text`")
	rejectRepeat(create.Flags().Lookup(textFlag))
	create.Flags().StringArrayVar(&attributes, attributeFlag, nil, "work item attribute `Name=value`; repeatable")
	fieldsFlag(create, &fields, youtrack.WorkItemWriteFields)
	return create
}

func newTimeUpdate(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	var fields, spent, date, text, workType string
	var attributes, emptied []string
	update := newCommand("update <issue> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.UpdateWorkItem(args[0], args[1], flagValue(cmd, durationFlag, &spent),
			flagValue(cmd, dateFlag, &date), flagValue(cmd, textFlag, &text),
			flagValue(cmd, typeFlag, &workType), attributes, emptied, fieldsFlagValue(cmd, &fields))
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	update.Args = cobra.ExactArgs(2)
	update.Short = "Update a work item"
	update.Long = "Update a work item; unflagged parts stay.\n\n" +
		"<id> is the work item id such as 150-1 that ytrack time list prints.\n\n" +
		example(workItemExample())
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
	fieldsFlag(update, &fields, youtrack.WorkItemWriteFields)
	return update
}

func newTimeDelete(env []string, stdout io.Writer, renderer render.Renderer) *cobra.Command {
	del := newCommand("delete <issue> <id>", func(cmd *cobra.Command, args []string) *diag.Fault {
		call, fault := youtrack.DeleteWorkItem(args[0], args[1])
		if fault != nil {
			return fault
		}
		return runCall(cmd.Context(), env, stdout, renderer, call)
	})
	del.Args = cobra.ExactArgs(2)
	del.Short = "Delete a work item"
	del.Long = "Delete a work item.\n\n" +
		"<id> is the work item id such as 150-1 that ytrack time list prints.\n\n" +
		example(render.NewMap(render.Pair{Key: "id", Value: render.NewString("150-1")}, render.Pair{Key: "issue", Value: render.NewMap(render.Pair{Key: "idReadable", Value: render.NewString("DEV-1")})}))
	return del
}

type commentsValue struct {
	comments youtrack.Comments
}

func (v *commentsValue) String() string {
	return v.comments.String()
}

func (v *commentsValue) Set(text string) error {
	comments, err := youtrack.ParseComments(text)
	if err != nil {
		return err
	}
	v.comments = comments
	return nil
}

func (v *commentsValue) Type() string {
	return "count"
}

// How many records a list prints unasked. Every list of the tool takes the same number, so it is written once.
const listLimit = 50

func pageFlags(cmd *cobra.Command, page *youtrack.Page, plural string) {
	cmd.Flags().IntVar(&page.Limit, "limit", listLimit, "max "+plural)
	rejectRepeat(cmd.Flags().Lookup("limit"))
	cmd.Flags().IntVar(&page.Skip, "skip", 0, plural+" to pass over before the first")
	rejectRepeat(cmd.Flags().Lookup("skip"))
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

// fieldsFlagValue is the expression the caller wrote, and nil where they wrote none: what pflag holds then is the
// default of the command, which stands for no expression at all.
func fieldsFlagValue(cmd *cobra.Command, expression *string) *string {
	if !cmd.Flags().Changed("fields") {
		return nil
	}
	return expression
}

// An empty --query is the caller asking for every record there is, so only the flag itself tells a search from
// none. carries is what the flag holds, and thing what one record of the command is.
func rejectNoQuery(cmd *cobra.Command, carries, thing string) *diag.Fault {
	if cmd.Flags().Changed("query") {
		return nil
	}
	message := fmt.Sprintf(`no --query was given: it carries %s, and --query "" finds every %s`, carries, thing)
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

// runCall is a command that prints what the server answered and nothing besides.
func runCall(ctx context.Context, env []string, stdout io.Writer, renderer render.Renderer, call youtrack.Call) *diag.Fault {
	_, node, fault := connectAndCall(ctx, env, call)
	if fault != nil {
		return fault
	}
	return printNode(stdout, renderer, node)
}

// connectAndCall is the one place a command reaches YouTrack from, and the connection comes back out for the command whose
// document says more than the answer did. Only a call that assembled reads the address and the token, so a call
// that did not is bad_usage whatever env holds.
func connectAndCall(ctx context.Context, env []string, call youtrack.Call) (connection, *render.Node, *diag.Fault) {
	c, fault := connect(env)
	if fault != nil {
		return connection{}, nil, fault
	}
	node, fault := call(ctx, c.client)
	if fault != nil {
		return connection{}, nil, c.withOrigin(fault)
	}
	return c, node, nil
}

// A failure ytrack has no code for goes under upstream_failed (ADR-0005).
func printNode(stdout io.Writer, renderer render.Renderer, node *render.Node) *diag.Fault {
	if err := renderer.Render(stdout, node); err != nil {
		return &diag.Fault{Code: diag.UpstreamFailed, Message: err.Error()}
	}
	return nil
}

// pflag keeps the last of the values a flag is given, so a flag that takes one refuses the second.
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

// Without RunE, cobra answers a call that names none of a group's commands as it answers
// --help, with exit code 0.
func rejectGroup(cmd *cobra.Command, args []string) *diag.Fault {
	if len(args) == 0 {
		return &diag.Fault{Code: diag.BadUsage, Message: "no command given"}
	}
	return unknownCommand(cmd, args[0])
}

// flagMessage is the error of pflag with what it quotes in backticks rather than in the %q it writes, and with
// a number that did not parse told by why alone: strconv names the function and quotes the text a second time.
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

// cobra refuses a word under the root before a flag is parsed, and quotes it with %q in an error of no type of its
// own; it is written again the way unknownCommand writes it under every group, whatever cobra adds after it kept.
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

// The words cobra's legacyArgs refuses an unknown command with, once there are
// subcommands and Args is nil.
func unknownCommand(parent *cobra.Command, word string) *diag.Fault {
	message := fmt.Sprintf("unknown command %s for %s", render.Quote(word), render.Quote(parent.CommandPath()))
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func newCommand(use string, run func(cmd *cobra.Command, args []string) *diag.Fault) *cobra.Command {
	return &cobra.Command{Use: use, RunE: runE(run)}
}

// Every function ytrack hands cobra is built here, so it fails with a *diag.Fault or not
// at all; a nil *diag.Fault handed on as an error would not be nil.
func runE[T any](f func(cmd *cobra.Command, arg T) *diag.Fault) func(*cobra.Command, T) error {
	return func(cmd *cobra.Command, arg T) error {
		if fault := f(cmd, arg); fault != nil {
			return fault
		}
		return nil
	}
}
