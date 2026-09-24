package youtrack

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const (
	commentsKey = "comments"
	deletedKey  = "deleted"
	// The key the id of a comment stands under in a refusal about it, which is what the caller calls the thing
	// the command wrote.
	commentKey = "comment"
	// Where that refusal sends the caller to read the id off: both entities that carry a readable id keep
	// comments of their own.
	commentHangsFrom = "the issue or the article"
)

// What a comment holds where the caller writes no expression of their own: which comment it is, who wrote it,
// when it was written, whether it has been changed since, and the text of it. Every key of it arrives under a
// 200 for a member's token as well as an admin's, so no key of the default costs a reader their document.
//
// updated is part of the state a write leaves behind — null on a comment nobody has changed, a moment on one
// that has been — which is why it stands here and in no expression --comments sends.
const CommentFields = "id,author(login),created,updated,text"

// The value --comments takes for every comment there is.
const everyComment = "all"

// Comments is how many comments of an issue or an article are printed: all of them, or the last of them by the
// moment they were written. The zero value asks for none.
type Comments struct {
	last int
	all  bool
}

// AllComments is what a show prints where the caller writes no --comments of their own.
func AllComments() Comments {
	return Comments{all: true}
}

// ParseComments reads the value of --comments.
func ParseComments(text string) (Comments, error) {
	if text == everyComment {
		return AllComments(), nil
	}
	last, err := strconv.Atoi(text)
	switch {
	case errors.Is(err, strconv.ErrRange):
		return Comments{}, errors.New("it is a larger number than there could ever be comments")
	case err != nil:
		return Comments{}, fmt.Errorf("it is neither %s nor a whole number of comments", everyComment)
	case last < 0:
		return Comments{}, errors.New("a number of comments is not negative")
	}
	return Comments{last: last}, nil
}

func (c Comments) String() string {
	if c.all {
		return everyComment
	}
	return strconv.Itoa(c.last)
}

// None asked for is not the same as none there, so at zero neither the key nor anything about comments in the
// request is there at all, while an entity that has none prints the key empty.
func (c Comments) asked() bool {
	return c.all || c.last > 0
}

type commented struct {
	schema  string
	comment string
	owner   ownerKind
	api     commentAPI
}

// takenBack: у задачи забранный автором комментарий остаётся в списке без текста, поэтому его надо прочитать
// и пропустить; у статьи такие удаляются сразу, поле nil.
type commentAPI struct {
	list      func(c *Client, ctx context.Context, at owner, fields string, w window) (*http.Response, error)
	create    func(c *Client, ctx context.Context, at owner, body []byte, fields string) (*http.Response, error)
	takenBack func(c *Client, ctx context.Context, at owner, comment childID, fields string) (*http.Response, error)
	rewrite   func(c *Client, ctx context.Context, at owner, comment childID, body []byte, fields string) (*http.Response, error)
	remove    func(c *Client, ctx context.Context, at owner, comment childID) (*http.Response, error)
}

func issueComments() commented {
	return commented{schema: issueSchema, comment: "IssueComment", owner: issueOwner, api: commentAPI{
		list:      (*Client).getIssueComments,
		create:    (*Client).createIssueComment,
		takenBack: (*Client).getIssueComment,
		rewrite:   (*Client).updateIssueComment,
		remove:    (*Client).deleteIssueComment,
	}}
}

func articleComments() commented {
	return commented{schema: articleSchema, comment: "ArticleComment", owner: articleOwner, api: commentAPI{
		list:    (*Client).getArticleComments,
		create:  (*Client).createArticleComment,
		rewrite: (*Client).updateArticleComment,
		remove:  (*Client).deleteArticleComment,
	}}
}

func (h commented) keepsTakenBack() bool {
	return h.api.takenBack != nil
}

// commentsOf is the machinery of the kind of owner a command was given, which is the one place the two kinds
// are told apart: everything below it — the schema the answer stands at, the word a refusal uses, the API the
// request goes to — follows from it rather than from a second reading of the id.
func commentsOf(kind ownerKind) commented {
	if kind == articleOwner {
		return articleComments()
	}
	return issueComments()
}

// Why comments stand nowhere in the expression of a show: the flag of its own fills them.
const commentsOfAShow = "is filled by --comments, which settles how many comments are printed and what each of " +
	"them holds"

// Why comments stand nowhere in the expression of a selection: a record is one line, which a list of comments
// does not fit on.
func (h commented) commentsOfAList() string {
	return fmt.Sprintf("holds the comments of an %s, which are printed a record at a time by ytrack comment "+
		"list, and with the %s itself by ytrack %s show --comments", h.owner, h.owner, h.owner)
}

// Why comments stand nowhere in the expression of a write: a write never touches them, and an entity with
// dozens of them would be paid for on every call.
func (h commented) commentsOfAWrite() string {
	return fmt.Sprintf("holds the comments of an %s, which no write changes; they are printed by ytrack %s "+
		"show --comments", h.owner, h.owner)
}

// Comments are asked for one way, and it is never the expression, wherever the entity stands in it.
func (h commented) refuse(spec *schemas, expression string, requested []requestedField, because string) *diag.Fault {
	path, written := nameAt(spec, h.schema, h.schema, commentsKey, requested, nil)
	if !written {
		return nil
	}
	message := fmt.Sprintf("fields %s: %s %s", render.Quote(expression), path, because)
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

// What a comment holds is the tool's: --comments names comments, --fields names the fields of the entity.
func printedComment() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: "author", children: []requestedField{{name: "login"}}},
		{name: "created"},
		{name: textKey},
	}
}

// CreateComment is the call that writes text as a new comment of the issue or of the article of that readable
// id, and prints the comment as the server kept it, with the fields of expression, or with them added to
// CommentFields when it starts with +; nil is the caller leaning on the default whole.
func CreateComment(id, text string, expression *string) (Call, *diag.Fault) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	written, fault := commentText(text)
	if fault != nil {
		return nil, fault
	}
	requested, fault := commentFields(expression)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.commentCreated(ctx, spec, at, written, requested)
	}, nil
}

// One POST is the whole command. The owner is not read first: an issue or an article the instance has none of,
// and one the token may not see, are both answered 404 by the server itself, and nothing is written either way;
// the answer carries the comment that was added, so nothing is read back afterwards.
func (c *Client) commentCreated(ctx context.Context, spec *schemas, at owner, written commentWritten, requested []requestedField) (*render.Node, *diag.Fault) {
	held := commentsOf(at.kind)
	body := written.body()
	return c.write(ctx, spec, held.comment, asking(requested, written.checked()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return held.api.create(c, ctx, at, body, fields)
	}, written.confirmedBy, writtenNode(requested))
}

// UpdateComment is the call that writes text into the comment of that id on the issue or the article of that
// readable id, and prints the comment as the server kept it, with the fields of expression, or with them added
// to CommentFields when it starts with +; nil is the caller leaning on the default whole.
func UpdateComment(id, comment, text string, expression *string) (Call, *diag.Fault) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	which, fault := parseChildID(commentKey, commentHangsFrom, comment)
	if fault != nil {
		return nil, fault
	}
	written, fault := commentText(text)
	if fault != nil {
		return nil, fault
	}
	requested, fault := commentFields(expression)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	rewritten := commentRewritten{commentWritten: written, at: which}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.commentUpdated(ctx, spec, at, rewritten, requested)
	}, nil
}

// An issue keeps a comment its author took back, and lets a write into one through with a 200 that changes the
// text where nothing prints it and answers text: null. So the kind that has such comments is read before it is
// written and the kind that has none is written outright, and which kind it is comes from commentsOf,
// like everything else the two are told apart by.
func (c *Client) commentUpdated(ctx context.Context, spec *schemas, at owner, written commentRewritten, requested []requestedField) (*render.Node, *diag.Fault) {
	held := commentsOf(at.kind)
	if held.keepsTakenBack() {
		if fault := c.refuseACommentTakenBack(ctx, spec, held, at, written.at); fault != nil {
			return nil, fault
		}
	}
	body := written.body()
	return c.write(ctx, spec, held.comment, asking(requested, written.checked()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return held.api.rewrite(c, ctx, at, written.at, body, fields)
	}, written.confirmedBy, writtenNode(requested))
}

// The read before the write, which is a read and not a write: a comment the owner has none of is answered 404
// here and the write never goes out, and one that was taken back is refused with nothing written either. The
// race — taken back between this read and the write — is what the check of the answer is for.
func (c *Client) refuseACommentTakenBack(ctx context.Context, spec *schemas, held commented, at owner, comment childID) *diag.Fault {
	a, fault := c.request(ctx, spec, held.comment, []requestedField{{name: deletedKey}}, func(ctx context.Context, fields string) (*http.Response, error) {
		return held.api.takenBack(c, ctx, at, comment, fields)
	})
	if fault != nil {
		return fault
	}
	gone, fault := held.deleted(a, a.objects[0])
	if fault != nil {
		return fault
	}
	if !gone {
		return nil
	}
	details := []render.Pair{
		requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted()),
		{Key: commentKey, Value: render.NewString(comment.String())},
	}
	return &diag.Fault{Code: diag.BadUsage, Message: commentTakenBack, Details: details}
}

const commentTakenBack = "the comment was taken back by whoever wrote it, and YouTrack takes a write into such " +
	"a comment without a word: the text would be changed where nothing prints it and the answer would carry " +
	"none. A comment taken back is removed for good by ytrack comment delete and changed by nothing at all"

// DeleteComment is the call that takes the comment of that id away from the issue or the article of that
// readable id for good, and prints the id it was known by.
func DeleteComment(id, comment string) (Call, *diag.Fault) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	which, fault := parseChildID(commentKey, commentHangsFrom, comment)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.commentRemoved(ctx, at, which)
	}, nil
}

// Nothing is read before the deletion, unlike the deletion of an owner: the server matches the id of a
// comment exactly — 7-02 names no comment where 7-2 stands — so a 200 says the caller's own id is the one the
// comment went by, and a comment that is not there, or hangs from another owner, is answered 404 with nothing
// destroyed. A comment its author took back is not read for either: removing it for good is what this is.
func (c *Client) commentRemoved(ctx context.Context, at owner, comment childID) (*render.Node, *diag.Fault) {
	held := commentsOf(at.kind)
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return held.api.remove(c, ctx, at, comment)
	}); fault != nil {
		return nil, fault
	}
	return render.NewMap(render.Pair{Key: idKey, Value: render.NewString(comment.String())}), nil
}

// commentFields is an expression of a command that prints one comment, held to what a comment may be asked
// for. A nil expression is the caller leaning on the default whole, and then nothing in the tree is theirs to
// answer for.
func commentFields(expression *string) ([]requestedField, *diag.Fault) {
	if expression == nil {
		return theDefault(CommentFields, false)
	}
	return parseFields(*expression, CommentFields)
}

// listed is what a comment is asked for by the owner's kind: what a show prints of it and, where the kind keeps
// comments taken back, whether this one was. An article keeps none, and the server sends no deleted for its
// comments at all, so the name is asked for there by nobody.
func (h commented) listed() []requestedField {
	held := printedComment()
	if h.keepsTakenBack() {
		held = append(held, requestedField{name: deletedKey})
	}
	return held
}

// CommentListFields is what comment list asks of a comment of an issue where the caller writes no expression
// of their own, which is the default --help names. A show asks the same and prints all but deleted, since it
// leaves a comment taken back out; a list prints that comment among the rest, so it has to say which it is.
func CommentListFields() string {
	return walk(issueComments().listed())
}

// ListComments is the call for one page of the comments of the issue or the article of that readable id, with the
// fields of expression, or with them added to the default of that kind of owner when it starts with +; nil is
// the caller leaning on the default whole.
func ListComments(id string, expression *string, page Page) (Call, *diag.Fault) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	held := commentsOf(at.kind)
	defaults := walk(held.listed())
	requested, fault := theDefault(defaults, false)
	if expression != nil {
		requested, fault = parseFields(*expression, defaults)
	}
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listComments(ctx, spec, held, at, requested, page)
	}, nil
}

// The comments of an owner are a subresource of their own, so the machinery of every other list of the tool
// holds here unchanged. The count is the pass over ids rather than commentsCount of the owner: the counter leaves
// a comment taken back out, and the page carries it.
func (c *Client) listComments(ctx context.Context, spec *schemas, held commented, at owner, requested []requestedField, page Page) (*render.Node, *diag.Fault) {
	return c.selection(ctx, spec, commentsKey, "[]"+held.comment, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return held.api.list(c, ctx, at, fields, w)
	})
}

// merged is the expression that goes out: the caller's with the comments added where any were asked for, and
// theirs untouched where none were.
func (c Comments) merged(h commented, asked []requestedField) []requestedField {
	if !c.asked() {
		return asked
	}
	return asking(asked, requestedField{name: commentsKey, children: h.listed()})
}

// pair is what a show prints after the fields asked of it, and nothing at all where no comments were asked for.
func (c Comments) pair(h commented, a answer, holder map[string]any) ([]render.Pair, *diag.Fault) {
	if !c.asked() {
		return nil, nil
	}
	printed, fault := c.of(h, a, holder)
	if fault != nil {
		return nil, fault
	}
	return []render.Pair{{Key: commentsKey, Value: printed}}, nil
}

// A comment beside the moment it was written, which is what the list is put in order by.
type writtenComment struct {
	written int64
	comment map[string]any
}

// of is the comments as they are printed: the deleted left out, the rest oldest first, and of those the last
// the caller asked for. The order is ytrack's own, so an answer sent out of order is nothing to guard against.
func (c Comments) of(h commented, a answer, holder map[string]any) (*render.Node, *diag.Fault) {
	arrived, isList := holder[commentsKey].([]any)
	if !isList {
		return nil, shapeFailure(a.response, a.body, fmt.Sprintf("the comments of the %s arrived as something other than an array", h.owner))
	}
	kept := make([]writtenComment, 0, len(arrived))
	for _, item := range arrived {
		comment, isObject := item.(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.response, a.body, fmt.Sprintf("a comment of the %s arrived as something other than an object", h.owner))
		}
		gone, fault := h.deleted(a, comment)
		if fault != nil {
			return nil, fault
		}
		if gone {
			continue
		}
		written, isInstant := wholeNumber(comment["created"])
		if !isInstant {
			return nil, shapeFailure(a.response, a.body, notAnInstant("created"))
		}
		kept = append(kept, writtenComment{written: written, comment: comment})
	}
	slices.SortStableFunc(kept, func(a, b writtenComment) int { return cmp.Compare(a.written, b.written) })
	if !c.all {
		kept = kept[max(len(kept)-c.last, 0):]
	}
	objects := make([]map[string]any, 0, len(kept))
	for _, written := range kept {
		objects = append(objects, written.comment)
	}
	printed, fault := printing(a, onLinesOfItsOwn).objectsAt(h.comment, printedComment(), objects)
	if fault != nil {
		return nil, fault
	}
	return render.NewList(printed...), nil
}

// deleted is whether the author took the comment back, and always false where the entity keeps none: nothing
// about deletion was asked for there, so nothing about it is read.
func (h commented) deleted(a answer, comment map[string]any) (bool, *diag.Fault) {
	if !h.keepsTakenBack() {
		return false, nil
	}
	gone, isFlag := comment[deletedKey].(bool)
	if !isFlag {
		return false, shapeFailure(a.response, a.body, "whether a comment is deleted arrived as neither true nor false")
	}
	return gone, nil
}
