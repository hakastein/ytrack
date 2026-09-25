package youtrack

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const (
	summaryKey       = "summary"
	descriptionKey   = "description"
	contentKey       = "content"
	textKey          = "text"
	projectKey       = "project"
	shortNameKey     = "shortName"
	parentArticleKey = "parentArticle"
	// The name --clear and --parent write the parent under, which is the word a caller says it with rather than
	// the name the schema gives the member.
	parentKey     = "parent"
	projectSchema = "Project"
	// The one kind of condition YouTrack has: a field of the project against the values of it that show another
	// field. Anything else under condition is read as a condition ytrack cannot evaluate.
	fieldBasedCondition = "FieldBasedCondition"
)

func CreateIssue(code, summary string, description *string, filled []string, expression *string) (Call, *diag.Fault) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	parts, fault := parseIssueCreate(summary, description, filled)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := issueFields(spec, expression, IssueShowFields, issueCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.createIssue(ctx, spec, code, parts, requested)
	}, nil
}

func UpdateIssue(id string, summary, description *string, filled, cleared []string, expression *string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	if summary == nil && description == nil && len(filled) == 0 && len(cleared) == 0 {
		return nil, &diag.Fault{Code: diag.BadUsage, Message: nothingToWrite}
	}
	parts, fault := parseIssueUpdate(summary, description, filled, cleared)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := issueFields(spec, expression, IssueShowFields, issueCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.updateIssue(ctx, spec, id, parts, requested)
	}, nil
}

// An update that writes nothing would be a request sent to leave the issue as it is.
const nothingToWrite = "the call writes nothing into the issue: an update is given --summary, --description, " +
	"--field \"Name=value\" or --clear Name, and a part it is given none of is left as the issue holds it"

const descriptionBothWays = "--description writes the prose of the issue and --clear description empties it, and the " +
	"call gives both"

func (c *Client) createIssue(ctx context.Context, spec *schemas, code string, parts issueInput, requested []requestedField) (*render.Node, *diag.Fault) {
	project, fault := c.readProjectMetadata(ctx, spec, code)
	if fault != nil {
		return nil, fault
	}
	// A new issue carries no field yet, so every class the body names comes from the table.
	filed, fault := parts.resolve(project, nil)
	if fault != nil {
		return nil, fault
	}
	if hidden := filed.hiddenFields(); len(hidden) > 0 {
		return nil, project.fault(diag.BadUsage, hiddenMessage, "invalid", invalidEntries(hidden))
	}
	if missing := filed.missing(); len(missing) > 0 {
		return nil, project.fault(diag.MissingRequired, missingMessage, "missing", names(missing))
	}
	if fault := c.resolveCustomFields(ctx, spec, requested); fault != nil {
		return nil, fault
	}
	asked := withFields(requested, filed.verifyFields()...)
	issueBlocks(spec, composedIssue(), asked)
	body := filed.createBody()
	return c.write(ctx, spec, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateIssue(ctx, body, fields)
	}, filed.verify, writeResultNode(requested))
}

func writeResultNode(requested []requestedField) func(decodedResponse) (*render.Node, *diag.Fault) {
	return func(a decodedResponse) (*render.Node, *diag.Fault) {
		return objectNode(a, requested, a.objects[0], nil)
	}
}

func (c *Client) deleteOwner(ctx context.Context, spec *schemas, kind ownerKind, schema string,
	read func(ctx context.Context, fields string) (*http.Response, error),
	destroy func(ctx context.Context, at readableID) (*http.Response, error),
) (*render.Node, *diag.Fault) {
	requested := []requestedField{{name: idReadableKey}}
	decoded, fault := c.request(ctx, spec, schema, requested, read)
	if fault != nil {
		return nil, fault
	}
	readable, fault := readableIDAt(decoded, decoded.objects[0], kind, "a deletion")
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return destroy(ctx, readable)
	}); fault != nil {
		return nil, fault
	}
	return objectNode(decoded, requested, decoded.objects[0], nil)
}

func (c *Client) updateIssue(ctx context.Context, spec *schemas, id string, parts issueInput, requested []requestedField) (*render.Node, *diag.Fault) {
	issue, fault := c.readIssueToWrite(ctx, spec, id)
	if fault != nil {
		return nil, fault
	}
	changed, fault := parts.resolve(issue.project, issue.kinds)
	if fault != nil {
		return nil, fault
	}
	if emptied := changed.requiredEmptied(); len(emptied) > 0 {
		return nil, issue.project.fault(diag.MissingRequired, emptiedMessage, "missing", names(emptied))
	}
	if fault := c.resolveCustomFields(ctx, spec, requested); fault != nil {
		return nil, fault
	}
	asked := withFields(requested, changed.verifyFields()...)
	issueBlocks(spec, composedIssue(), asked)
	body := changed.updateBody()
	return c.write(ctx, spec, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiUpdateIssue(ctx, issue.readable, body, fields)
	}, changed.verify, writeResultNode(requested))
}

type articleCreateInput struct {
	project  string
	summary  string
	content  *string
	parentID *string
}

type articleCreate struct {
	project string
	summary string
	content *string
	// The article this one is written under, as the read before the write found it: the body carries the
	// internal id that read gave rather than the argument. It is nil where the call names no parent.
	parent *articleRef
}

// parseArticleCreate is the whole of what a creation of an article writes, read off the flags it was given, with
// each part held to what YouTrack would keep of it.
//
// An article keeps a carriage return where the description of an issue loses one, so the text of it is refused
// for nothing but being empty; the title is one line here as it is there, and the runes YouTrack drops out of
// the title of an issue it keeps in the title of an article.
func parseArticleCreate(code, summary string, content, parent *string) (articleCreateInput, *diag.Fault) {
	if fault := rejectReplaced("--"+summaryKey, summary, summaryOfAnArticle, articleTitleRewrites()); fault != nil {
		return articleCreateInput{}, fault
	}
	if content != nil {
		if fault := rejectReplaced("--"+contentKey, *content, contentOfANewArticle, nil); fault != nil {
			return articleCreateInput{}, fault
		}
	}
	written := articleCreateInput{project: code, summary: summary, content: content}
	if parent != nil {
		id, fault := parseArticleID(*parent)
		if fault != nil {
			return articleCreateInput{}, fault
		}
		written.parentID = &id
	}
	return written, nil
}

// The title of an article is one line: YouTrack turns each line ending of it into a space and a CRLF into one
// space rather than into two, and keeps the NEL and the separators an issue loses.
func articleTitleRewrites() []charReplacement {
	return []charReplacement{
		{rune: '\n', into: "a space"},
		{rune: '\r', into: "a space"},
	}
}

const (
	summaryOfAnArticle   = "is empty, and YouTrack files no article without a title"
	contentOfANewArticle = "is empty, and YouTrack keeps empty content as none: leave the flag out to file the " +
		"article with no content at all"
	contentOfAnArticle = "is empty, and YouTrack keeps empty content as none: --clear content empties it " +
		"outright, and content the call does not write is left as the article holds it"
)

type articleUpdateInput struct {
	summary       *string
	content       *string
	clearsContent bool
	parentID      *string
	clearsParent  bool
}

type articleUpdate struct {
	summary       *string
	content       *string
	clearsContent bool
	// The article this one is moved under, as the read before the write found it: the body carries the internal
	// id that read gave rather than the argument. It is nil where the call names no parent or takes it away.
	parent       *articleRef
	clearsParent bool
}

// A part of an entity --clear empties, by the name the flag writes it under and by what it sets in the write.
// A second part is one more line in the table of that entity rather than a second reading of the flag.
type clearablePart[W any] struct {
	name  string
	empty func(*W)
}

func clearableArticleParts() []clearablePart[articleUpdateInput] {
	return []clearablePart[articleUpdateInput]{
		{name: contentKey, empty: func(w *articleUpdateInput) { w.clearsContent = true }},
		{name: parentKey, empty: func(w *articleUpdateInput) { w.clearsParent = true }},
	}
}

// parseArticleUpdate is the whole of what an update of an article writes, read off the flags it was given, with
// each part held to what YouTrack would keep of it.
func parseArticleUpdate(summary, content, parent *string, cleared []string) (articleUpdateInput, *diag.Fault) {
	written := articleUpdateInput{summary: summary, content: content}
	if fault := written.parseClear(cleared); fault != nil {
		return articleUpdateInput{}, fault
	}
	if written.clearsContent && content != nil {
		return articleUpdateInput{}, &diag.Fault{Code: diag.BadUsage, Message: contentBothWays}
	}
	if written.clearsParent && parent != nil {
		return articleUpdateInput{}, &diag.Fault{Code: diag.BadUsage, Message: parentBothWays}
	}
	if parent != nil {
		id, fault := parseArticleID(*parent)
		if fault != nil {
			return articleUpdateInput{}, fault
		}
		written.parentID = &id
	}
	if summary != nil {
		if fault := rejectReplaced("--"+summaryKey, *summary, summaryOfAnArticle, articleTitleRewrites()); fault != nil {
			return articleUpdateInput{}, fault
		}
	}
	if content != nil {
		if fault := rejectReplaced("--"+contentKey, *content, contentOfAnArticle, nil); fault != nil {
			return articleUpdateInput{}, fault
		}
	}
	return written, nil
}

// parseClear reads --clear: the parts of the article the call empties, matched without regard to letter case, the
// way every name a caller writes is matched.
func (w *articleUpdateInput) parseClear(cleared []string) *diag.Fault {
	parts := clearableArticleParts()
	for _, name := range cleared {
		at := clearablePartIndex(parts, name)
		if at < 0 {
			message := fmt.Sprintf("--clear %s names no part of an article a call may empty: it takes %s",
				render.Quote(name), partsOf(parts))
			return &diag.Fault{Code: diag.BadUsage, Message: message}
		}
		parts[at].empty(w)
	}
	return nil
}

// Where the name a caller wrote stands in the table of the entity, matched without regard to letter case, the
// way every name a caller writes is matched.
func clearablePartIndex[W any](parts []clearablePart[W], name string) int {
	return slices.IndexFunc(parts, func(p clearablePart[W]) bool { return strings.EqualFold(name, p.name) })
}

func partsOf[W any](parts []clearablePart[W]) string {
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		names = append(names, part.name)
	}
	return strings.Join(names, " or ")
}

const nothingToWriteIntoAnArticle = "the call writes nothing into the article: an update is given --summary, " +
	"--content, --parent, --clear content or --clear parent, and a part it is given none of is left as the " +
	"article holds it"

const contentBothWays = "--content writes the text of the article and --clear content empties it, and the call " +
	"gives both"

const parentBothWays = "--parent writes the article this one hangs from and --clear parent takes it away, and " +
	"the call gives both"

// The body of an update of an article: the parts the call writes and not one key more. The article is addressed
// by the path, and a part the body says nothing about is a part the article keeps as it stands.
type updateArticleBody struct {
	Summary *string `json:"summary,omitempty"`
	// Raw JSON rather than a string: the text, an explicit null and no key at all are three things, and a
	// pointer tells only two of them apart.
	Content json.RawMessage `json:"content,omitempty"`
	// Raw JSON for the same reason: an object moves the article, a null takes it to the root of the knowledge
	// base, and no key at all leaves it hanging where it hangs.
	ParentArticle json.RawMessage `json:"parentArticle,omitempty"`
}

// Marshalling strings and structs of them cannot fail.
func (w articleUpdate) body() []byte {
	body, _ := json.Marshal(updateArticleBody{Summary: w.summary, Content: w.text(), ParentArticle: w.parentJSON()})
	return body
}

// parentJSON is the parent of the body: the internal id the read before the write gave, an explicit null where
// the call takes the parent away, and nothing at all where it says neither.
func (w articleUpdate) parentJSON() json.RawMessage {
	switch {
	case w.clearsParent:
		return json.RawMessage("null")
	case w.parent == nil:
		return nil
	}
	encoded, _ := json.Marshal(articleIDBody{ID: w.parent.id})
	return encoded
}

// text is the content of the body: the text where the call writes one, an explicit null where it empties one,
// and nothing at all where it says neither.
func (w articleUpdate) text() json.RawMessage {
	switch {
	case w.clearsContent:
		return json.RawMessage("null")
	case w.content == nil:
		return nil
	}
	encoded, _ := json.Marshal(*w.content)
	return encoded
}

// verifyFields is what the answer to the write is read for beside what the caller asked to print: every part that
// went out, so the check has it to compare, and the readable id, so a refusal can name the article whatever the
// caller asked for.
func (w articleUpdate) verifyFields() []requestedField {
	own := []requestedField{{name: idReadableKey}}
	if w.summary != nil {
		own = append(own, requestedField{name: summaryKey})
	}
	if w.content != nil || w.clearsContent {
		own = append(own, requestedField{name: contentKey})
	}
	if w.parent != nil || w.clearsParent {
		own = append(own, requestedField{name: parentArticleKey, children: []requestedField{{name: idReadableKey}}})
	}
	return own
}

// verify holds the answer against what the write sent: a 200 says the server took the body, not that what
// it kept is what went out. A part the call never named is never held against anything — the article holds
// what it held, and the answer is the only word there is on that.
func (w articleUpdate) verify(a decodedResponse) *diag.Fault {
	article := a.objects[0]
	var wrong []mismatch
	if w.summary != nil {
		wrong = textMismatch(wrong, summaryKey, *w.summary, article[summaryKey])
	}
	switch {
	case w.content != nil:
		wrong = textMismatch(wrong, contentKey, *w.content, article[contentKey])
	case w.clearsContent:
		wrong = emptyMismatch(wrong, contentKey, article[contentKey])
	}
	switch {
	case w.parent != nil:
		wrong = parentMismatch(wrong, w.parent.readable.String(), article[parentArticleKey])
	case w.clearsParent:
		wrong = noParentMismatch(wrong, article[parentArticleKey])
	}
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, knownAs(articleOwner.String(), responseID(a, idReadableKey)), wrong)
}

// The body of a creation of an article. The project is addressed by the code the caller typed: unlike a
// creation of an issue, nothing of the project is read first, since YouTrack answers a code it has none of
// with a 404 of its own. No $type stands here — the server takes the article without one.
type createArticleBody struct {
	Project articleProject `json:"project"`
	Summary string         `json:"summary"`
	Content *string        `json:"content,omitempty"`
	// The parent is addressed by the internal id the read before the write gave: this member takes no other
	// form, and a readable id under it is answered 400 Invalid structure of entity id.
	ParentArticle *articleIDBody `json:"parentArticle,omitempty"`
}

type articleProject struct {
	ShortName string `json:"shortName"`
}

type articleIDBody struct {
	ID string `json:"id"`
}

// Marshalling strings and structs of them cannot fail.
func (w articleCreate) body() []byte {
	filed := createArticleBody{
		Project: articleProject{ShortName: w.project},
		Summary: w.summary,
		Content: w.content,
	}
	if w.parent != nil {
		filed.ParentArticle = &articleIDBody{ID: w.parent.id}
	}
	body, _ := json.Marshal(filed)
	return body
}

// verifyFields is what the answer to the write is read for beside what the caller asked to print: every part that
// went out, so the check has it to compare, and the readable id, so a refusal can name the article that by then
// exists whatever the caller asked for.
func (w articleCreate) verifyFields() []requestedField {
	own := []requestedField{
		{name: idReadableKey},
		{name: summaryKey},
		{name: contentKey},
		{name: projectKey, children: []requestedField{{name: shortNameKey}}},
	}
	if w.parent != nil {
		own = append(own, requestedField{name: parentArticleKey, children: []requestedField{{name: idReadableKey}}})
	}
	return own
}

// verify holds the answer against what the write sent: a 200 says the server took the body, not that what
// it kept is what went out, and printing the answer unchecked would hand a rewritten value back as the
// caller's own.
func (w articleCreate) verify(a decodedResponse) *diag.Fault {
	article := a.objects[0]
	wrong := textMismatch(nil, summaryKey, w.summary, article[summaryKey])
	if w.content != nil {
		wrong = textMismatch(wrong, contentKey, *w.content, article[contentKey])
	}
	wrong = projectMismatch(wrong, w.project, article[projectKey])
	if w.parent != nil {
		wrong = parentMismatch(wrong, w.parent.readable.String(), article[parentArticleKey])
	}
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, knownAs(articleOwner.String(), responseID(a, idReadableKey)), wrong)
}

// The project is the one part held without regard to letter case: the server reads dev for DEV and answers
// with the code as it keeps it, so anything but the same project under another case is a disagreement.
func projectMismatch(wrong []mismatch, code string, value any) []mismatch {
	received := memberOf(value, shortNameKey)
	if kept, isText := received.(string); isText && strings.EqualFold(kept, code) {
		return wrong
	}
	return append(wrong, mismatch{field: projectKey, expected: render.NewString(code), actual: rawValueNode(received)})
}

// The parent is held by the readable id the read before the write gave it, which is what stands between the
// read and the write: a parent deleted in that moment leaves the article at the root of the tree under a 200,
// and the answer is the only word there is on it.
func parentMismatch(wrong []mismatch, readable string, value any) []mismatch {
	received := memberOf(value, idReadableKey)
	if kept, isText := received.(string); isText && kept == readable {
		return wrong
	}
	return append(wrong, mismatch{field: parentArticleKey, expected: render.NewString(readable), actual: rawValueNode(received)})
}

// A parent the call took away is an article the answer hangs from nothing at all. One still standing there is
// named by the readable id it came back under, the way a parent that was written is.
func noParentMismatch(wrong []mismatch, value any) []mismatch {
	if value == nil {
		return wrong
	}
	received := rawValueNode(memberOf(value, idReadableKey))
	return append(wrong, mismatch{field: parentArticleKey, expected: render.NewNull(), actual: received})
}

// The one member of a nested object a check reads, and nothing where the answer carried no object there at all.
func memberOf(value any, name string) any {
	object, isObject := value.(map[string]any)
	if !isObject {
		return nil
	}
	return object[name]
}

// The text of a comment as the call wrote it, and the whole of what a write of one carries: the body, what the
// answer is read for and the check of it are built from it and from nothing else. Nothing is read before a
// comment is written, so what the call wrote is already what the write goes out with.
type commentCreate struct {
	text string
}

// parseCommentText is the whole of what a comment writes, read off the flag it was given, with the text held to
// what YouTrack would keep of it — which is all of it. The server was measured to store thirty-two kinds of
// text at both kinds of owner byte for byte, a lone carriage return and three hundred kilobytes among them, so
// nothing is refused for being rewritten the way the title of an issue is.
func parseCommentText(text string) (commentCreate, *diag.Fault) {
	if fault := rejectReplaced("--"+textKey, text, textOfAComment, nil); fault != nil {
		return commentCreate{}, fault
	}
	return commentCreate{text: text}, nil
}

// An empty text is refused at both kinds of owner although only one of them refuses it: a caller names an owner
// rather than an API, and what an empty comment does should not turn on whether the id they typed was an issue
// or an article.
const textOfAComment = "is empty, and a comment is the text of it: YouTrack answers an empty one on an issue " +
	"with a refusal of its own and keeps an empty comment on an article, so ytrack writes neither"

// The body of a comment: the text and nothing else. No $type stands here — the server takes the comment
// without one — and no id either, since the owner is the path and the id is the server's to give.
type commentBody struct {
	Text string `json:"text"`
}

// Marshalling a string and a struct of one cannot fail.
func (w commentCreate) body() []byte {
	body, _ := json.Marshal(commentBody{Text: w.text})
	return body
}

// verifyFields is what the answer to the write is read for beside what the caller asked to print: the text that went
// out, so the check has it to compare, and the id, so a refusal can name the comment that by then exists
// whatever the caller asked for.
func (w commentCreate) verifyFields() []requestedField {
	return []requestedField{{name: idKey}, {name: textKey}}
}

// 200 говорит лишь, что сервер принял тело, а не что сохранил тот же текст.
func (w commentCreate) verifyText(a decodedResponse, named *render.Node) *diag.Fault {
	wrong := textMismatch(nil, textKey, w.text, a.objects[0][textKey])
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, knownAs(commentKey, named), wrong)
}

func (w commentCreate) verify(a decodedResponse) *diag.Fault {
	return w.verifyText(a, responseID(a, idKey))
}

type commentUpdate struct {
	commentCreate
	at childID
}

// verifyFields is what the answer to the write is read for beside what the caller asked to print: the text that went
// out, and nothing else. The id is not asked for the way a creation asks for it — the comment was addressed by
// an id the caller wrote, and that is what a refusal names it by.
func (w commentUpdate) verifyFields() []requestedField {
	return []requestedField{{name: textKey}}
}

// Комментарий, удалённый между чтением и записью, приходит без текста — это тоже расхождение.
func (w commentUpdate) verify(a decodedResponse) *diag.Fault {
	return w.verifyText(a, render.NewString(w.at.String()))
}

// What a creation of a work item carries as the call wrote it, and the whole of what the write goes out with:
// nothing of the issue is read first. A nil day or text is the caller who wrote no flag for it, and the
// key is then absent from the body — YouTrack writes such a work item against today of its own and leaves the
// text empty.
type workItemCreateInput struct {
	spent      parsedDuration
	day        *workDate
	text       *string
	attributes []namedValue
}

// How long a work item is, as the caller wrote it, which is what a refusal shows them, beside the minutes the
// body carries: the ISO period and the minutes are one length said two ways.
type parsedDuration struct {
	text    string
	minutes int64
}

// The day a work item is written against: what the caller wrote and the millisecond the body carries for it.
type workDate struct {
	text string
	noon int64
}

type workItemCreate struct {
	input workItemCreateInput
	// The type of work, as the read before the write resolved it, and nil where the call names none: the body
	// then carries no type at all, and YouTrack writes the work item against none.
	workType   *resolvedWorkType
	attributes []resolvedAttribute
}

// The type of work a work item is written against: the id the body carries, since YouTrack answers a type given
// by name with укажите ее ID, and the name the caller wrote, which is what a refusal about it shows them.
type resolvedWorkType struct {
	id   string
	name string
}

// The type of work, read off --type. An empty name answers to no type of any project, and finding that out
// would cost the read before the write.
func rejectWorkItemType(named *string) *diag.Fault {
	if named == nil || *named != "" {
		return nil
	}
	return invalidValueFault("--"+typeKey, *named, "names no type of work: the types an issue may be written "+
		"against are the settings of its project, printed by ytrack project show <code> under plugins")
}

// YouTrack keeps an empty text of a work item as an empty text, so the one thing refused of --text
// here is what the encoder would rewrite: a byte that is no UTF-8 reaches the server as U+FFFD, and the check
// of the write would then report ytrack's own rewriting as the server's, over a work item that exists.
func rejectWorkItemText(text *string) *diag.Fault {
	if text == nil {
		return nil
	}
	return rejectNoUTF8("--"+textKey, *text)
}

// parseWorkItemCreate is the whole of what a creation writes, read off the argument and the flags it was given. The
// name of the type is read here as well, although what goes out for it is the id the read before the write
// resolves: --type is read in one place, and an empty name costs no request to find out about.
func parseWorkItemCreate(spent string, day, text, named *string, attributes []string) (workItemCreateInput, *diag.Fault) {
	length, fault := parseDuration(spent)
	if fault != nil {
		return workItemCreateInput{}, fault
	}
	if fault := rejectWorkItemText(text); fault != nil {
		return workItemCreateInput{}, fault
	}
	written := workItemCreateInput{spent: length, text: text}
	if day != nil {
		against, fault := parseWorkDate(*day)
		if fault != nil {
			return workItemCreateInput{}, fault
		}
		written.day = &against
	}
	if fault := rejectWorkItemType(named); fault != nil {
		return workItemCreateInput{}, fault
	}
	if written.attributes, fault = attributeValues(attributes); fault != nil {
		return workItemCreateInput{}, fault
	}
	return written, nil
}

// How long the work item is, written as ytrack prints one. A day and a week are refused rather than converted:
// P1D of YouTrack is the working day of the instance — eight hours on this one — so an ISO day would mean one
// thing here and another anywhere else.
func parseDuration(text string) (parsedDuration, *diag.Fault) {
	minutes, read := periodMinutes(text)
	switch {
	case !read:
		return parsedDuration{}, invalidValueFault("duration", text, "is no ISO 8601 period of hours and minutes, as "+
			"in PT1H30M, PT90M or PT0M: ytrack writes a work item as the minutes it comes to, and neither a day "+
			"nor a week is a fixed count of them — YouTrack reads P1D as the working day of the instance — while "+
			"a second and a fraction are no part of what a work item holds")
	case minutes > math.MaxInt32:
		return parsedDuration{}, invalidValueFault("duration", text,
			fmt.Sprintf("is longer than the %d minutes YouTrack keeps a work item for", math.MaxInt32))
	}
	return parsedDuration{text: text, minutes: minutes}, nil
}

// The day, read off --date: the calendar day, or midnight UTC of one, which is how ytrack prints the day of a
// work item, so what time list printed goes back in as it came out. Any other moment is refused before the
// write: YouTrack would file it under the calendar day of the time zone of whoever's token wrote it.
func parseWorkDate(text string) (workDate, *diag.Fault) {
	if day, err := time.Parse(time.DateOnly, text); err == nil {
		return workDate{text: text, noon: noonUTC(day)}, nil
	}
	moment, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return workDate{}, invalidValueFault(dateKey, text, "is neither a calendar day, as in 2026-09-01, nor "+
			"midnight UTC of one, as in 2026-09-01T00:00:00Z: a work item is written against a day, and "+
			"YouTrack keeps no moment of it")
	}
	utc := moment.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	if !utc.Equal(midnight) {
		return workDate{}, invalidValueFault(dateKey, text, "names a time of day, and a work item is written "+
			"against a day: YouTrack would file the moment under the calendar day of the time zone of whoever "+
			"wrote it, which is not the caller's to know")
	}
	if _, offset := moment.Zone(); offset != 0 {
		return workDate{}, invalidValueFault(dateKey, text, "is midnight UTC written in an offset of its own, "+
			"and a day goes in as the day it is written in: a calendar day, as in 2026-09-01, or midnight UTC "+
			"of one with Z or +00:00 on it, as ytrack prints it")
	}
	return workDate{text: text, noon: noonUTC(midnight)}, nil
}

// A value of a positional argument or of a flag, refused before anything is sent, with the value quoted: a byte
// that cannot be printed stands escaped, as it does wherever a caller's string reaches a document.
func invalidValueFault(named, value, because string) *diag.Fault {
	return &diag.Fault{Code: diag.BadUsage, Message: fmt.Sprintf("%s %s %s", named, render.Quote(value), because)}
}

// The body of a work item: how long it is, and the type, the day and the text where the call names them. No
// $type stands here — the server takes the work item without one — and no author either: YouTrack writes it
// down as whoever the token belongs to, and naming anyone else would be ytrack deciding for the caller.
type createWorkItemBody struct {
	Duration   minutesBody     `json:"duration"`
	Type       *workItemIDBody `json:"type,omitempty"`
	Date       *int64          `json:"date,omitempty"`
	Text       *string         `json:"text,omitempty"`
	Attributes []attributeBody `json:"attributes,omitempty"`
}

type workItemIDBody struct {
	ID string `json:"id"`
}

func (w workItemCreate) body() []byte {
	written := createWorkItemBody{Duration: minutesBody{Minutes: w.input.spent.minutes}, Text: w.input.text,
		Attributes: attributeBodies(w.attributes)}
	if w.workType != nil {
		written.Type = &workItemIDBody{ID: w.workType.id}
	}
	if w.input.day != nil {
		written.Date = &w.input.day.noon
	}
	// Marshalling numbers, strings and a struct of them cannot fail.
	body, _ := json.Marshal(written)
	return body
}

// verifyFields is what the answer to the write is read for beside what the caller asked to print: every part that
// went out, so the check has them to compare, and the pair a work item is addressed by, so a refusal names the
// work item that by then exists whatever the caller asked for.
func (w workItemCreateInput) verifyFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: durationKey},
		{name: dateKey},
		{name: textKey},
		{name: issueOwner.String(), children: []requestedField{{name: idReadableKey}}},
	}
}

// The type and the attributes stand here and nowhere above it: the parts a write is held to are the parts that
// went out, and these went out only because the read before the write resolved them. The id is what a type went
// out as and the name what a disagreement is shown in, so a creation and an update ask the same of it; the
// attributes are a block ytrack composes itself, which asks the same two of each.
func verifyFieldsWithSettings(own []requestedField, workType *resolvedWorkType, attributes []resolvedAttribute) []requestedField {
	if workType != nil {
		own = append(own, requestedField{name: typeKey, children: []requestedField{{name: idKey}, {name: nameKey}}})
	}
	if len(attributes) > 0 {
		own = append(own, requestedField{name: attributesKey})
	}
	return own
}

// The check both writes of a work item end in: the parts that went out held against what came back, the type
// among them where one was resolved, and, where anything disagrees, a refusal naming the work item by identity —
// the pair a creation reads off the answer and an update holds from before it was sent.
func verifyWorkItem(a decodedResponse, wrong []mismatch, workType *resolvedWorkType, attributes []resolvedAttribute, identity []render.Pair) *diag.Fault {
	if workType != nil {
		wrong = workType.verify(wrong, a.objects[0][typeKey])
	}
	wrong = attributeMismatches(wrong, attributes, a.objects[0][attributesKey])
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, identity, wrong)
}

func (w workItemCreate) verifyFields() []requestedField {
	return verifyFieldsWithSettings(w.input.verifyFields(), w.workType, w.attributes)
}

// diff holds the answer against what the write sent: a 200 says the server took the body, not that what
// it kept is what went out. The day is held to the calendar day of UTC rather than to the millisecond,
// since the body carries noon and the server keeps midnight of the same day. What the call named nothing for is
// held to nothing: the day YouTrack chose itself and the empty text it left are its answer, not a disagreement.
func (w workItemCreateInput) diff(item map[string]any) []mismatch {
	wrong := w.spent.verify(nil, item[durationKey])
	if w.day != nil {
		wrong = w.day.verify(wrong, item[dateKey])
	}
	if w.text != nil {
		wrong = textMismatch(wrong, textKey, *w.text, item[textKey])
	}
	return wrong
}

func (w workItemCreate) verify(a decodedResponse) *diag.Fault {
	return verifyWorkItem(a, w.input.diff(a.objects[0]), w.workType, w.attributes, []render.Pair{
		{Key: issueOwner.String(), Value: owningIssue(a)},
		{Key: idKey, Value: responseID(a, idKey)},
	})
}

// The type is held by the id that went out, which is the whole of what a type of work is written by; the names
// are what the disagreement is shown in, since a caller who wrote a name has no id of theirs to read.
func (t resolvedWorkType) verify(wrong []mismatch, value any) []mismatch {
	if kept, isText := memberOf(value, idKey).(string); isText && kept == t.id {
		return wrong
	}
	return append(wrong, mismatch{
		field:    typeKey,
		expected: render.NewString(t.name),
		actual:   rawValueNode(memberOf(value, nameKey)),
	})
}

// A duration that arrived without the minutes it holds is as much a disagreement as one of another length: the
// minutes are the whole of what says how long a work item is.
func (d parsedDuration) verify(wrong []mismatch, value any) []mismatch {
	held, isObject := value.(map[string]any)
	if isObject {
		if minutes, isWhole := parseInt64(held[minutesKey]); isWhole {
			if minutes == d.minutes {
				return wrong
			}
			return append(wrong, mismatch{
				field:    durationKey,
				expected: render.NewString(d.text),
				actual:   render.NewString(duration(minutes)),
			})
		}
	}
	return append(wrong, mismatch{field: durationKey, expected: render.NewString(d.text), actual: render.NewNull()})
}

func (d workDate) verify(wrong []mismatch, value any) []mismatch {
	at, isInstant := parseInt64(value)
	if isInstant && sameDayUTC(at, d.noon) {
		return wrong
	}
	received := render.NewNull()
	if isInstant {
		received = render.NewString(formatDateTime(at))
	}
	return append(wrong, mismatch{field: dateKey, expected: render.NewString(d.text), actual: received})
}

func sameDayUTC(a, b int64) bool {
	return time.UnixMilli(a).UTC().Format(time.DateOnly) == time.UnixMilli(b).UTC().Format(time.DateOnly)
}

// The issue a work item hangs from, as the answer names it: a work item carries no readable id, so a refusal
// about one names the pair it is addressed by.
func owningIssue(a decodedResponse) *render.Node {
	issue, isObject := a.objects[0][issueOwner.String()].(map[string]any)
	if !isObject {
		return render.NewNull()
	}
	readable, isText := issue[idReadableKey].(string)
	if !isText {
		return render.NewNull()
	}
	return render.NewString(readable)
}

// What an update of a work item carries as the call wrote it: only the parts it named, so a part it named none
// of keeps its key out of the body and is left as the work item holds it. A part under --clear goes out as
// an explicit null instead, and so does an attribute --clear names.
type workItemUpdateInput struct {
	spent            *parsedDuration
	day              *workDate
	text             *string
	attributes       []namedValue
	clearsType       bool
	clearsText       bool
	clearsAttributes []string
}

type workItemUpdate struct {
	input workItemUpdateInput
	issue string
	at    childID
	// The type of work, as the read before the write resolved it, and nil where the call names none.
	workType   *resolvedWorkType
	attributes []resolvedAttribute
}

// The parts of a work item --clear empties. Neither duration nor date is among them: YouTrack answers a null
// under either with Field <name> cannot be null, so a call that asks is refused before anything is sent.
func clearableWorkItemParts() []clearablePart[workItemUpdateInput] {
	return []clearablePart[workItemUpdateInput]{
		{name: typeKey, empty: func(w *workItemUpdateInput) { w.clearsType = true }},
		{name: textKey, empty: func(w *workItemUpdateInput) { w.clearsText = true }},
	}
}

func keptWorkItemParts() []string {
	return []string{durationKey, dateKey}
}

// parseWorkItemUpdate is the whole of what an update writes, read off the flags it was given, and it is where a
// call that writes nothing is refused: every part is read in one place, so what the flags come to is settled
// here and nowhere above.
func parseWorkItemUpdate(spent, day, text, named *string, attributes, cleared []string) (workItemUpdateInput, *diag.Fault) {
	if spent == nil && day == nil && text == nil && named == nil && len(attributes) == 0 && len(cleared) == 0 {
		return workItemUpdateInput{}, &diag.Fault{Code: diag.BadUsage, Message: nothingToWriteIntoAWorkItem}
	}
	var written workItemUpdateInput
	if fault := written.parseClear(cleared); fault != nil {
		return workItemUpdateInput{}, fault
	}
	if written.clearsType && named != nil {
		return workItemUpdateInput{}, &diag.Fault{Code: diag.BadUsage, Message: typeBothWays}
	}
	if written.clearsText && text != nil {
		return workItemUpdateInput{}, &diag.Fault{Code: diag.BadUsage, Message: textBothWays}
	}
	if fault := rejectWorkItemText(text); fault != nil {
		return workItemUpdateInput{}, fault
	}
	written.text = text
	if spent != nil {
		length, fault := parseDuration(*spent)
		if fault != nil {
			return workItemUpdateInput{}, fault
		}
		written.spent = &length
	}
	if day != nil {
		against, fault := parseWorkDate(*day)
		if fault != nil {
			return workItemUpdateInput{}, fault
		}
		written.day = &against
	}
	if fault := rejectWorkItemType(named); fault != nil {
		return workItemUpdateInput{}, fault
	}
	var fault *diag.Fault
	if written.attributes, fault = attributeValues(attributes); fault != nil {
		return workItemUpdateInput{}, fault
	}
	for _, set := range written.attributes {
		if slices.ContainsFunc(written.clearsAttributes, func(name string) bool { return strings.EqualFold(name, set.name) }) {
			return workItemUpdateInput{}, attributeBothWays(set.name)
		}
	}
	return written, nil
}

func (w *workItemUpdateInput) parseClear(cleared []string) *diag.Fault {
	parts := clearableWorkItemParts()
	for _, name := range cleared {
		if at := clearablePartIndex(parts, name); at >= 0 {
			parts[at].empty(w)
			continue
		}
		if at := slices.IndexFunc(keptWorkItemParts(), func(kept string) bool {
			return strings.EqualFold(name, kept)
		}); at >= 0 {
			held := keptWorkItemParts()[at]
			message := fmt.Sprintf("--clear %s names a part every work item holds: YouTrack answers a %s of null "+
				"with Field %s cannot be null, so there is no way to empty one; --%s writes it afresh",
				render.Quote(name), held, held, held)
			return &diag.Fault{Code: diag.BadUsage, Message: message}
		}
		if name == "" {
			message := fmt.Sprintf(`--clear "" names nothing to empty: it takes %s or the name of an attribute`, partsOf(parts))
			return &diag.Fault{Code: diag.BadUsage, Message: message}
		}
		// Any other name is an attribute, which only the project can say it has.
		w.clearsAttributes = append(w.clearsAttributes, name)
	}
	return nil
}

const nothingToWriteIntoAWorkItem = "the call writes nothing into the work item: an update is given --duration, " +
	"--type, --date, --text, --attribute or --clear, and a part it is given none of is left as the work item " +
	"holds it"

const typeBothWays = "--type writes the type of work of the work item and --clear type takes it away, and the " +
	"call gives both"

const textBothWays = "--text writes the text of the work item and --clear text empties it, and the call gives both"

// The body of an update of a work item: the parts the call writes and not one key more. The work item is
// addressed by the path, and a part the body says nothing about is a part it keeps as it stands.
type updateWorkItemBody struct {
	Duration *minutesBody `json:"duration,omitempty"`
	// Raw JSON rather than an object: a type, an explicit null and no key at all are three things, and a pointer
	// tells only two of them apart.
	Type json.RawMessage `json:"type,omitempty"`
	Date *int64          `json:"date,omitempty"`
	// Raw JSON for the same reason, and the reason is sharper here: YouTrack keeps an empty text as an empty
	// text, so only a null empties one.
	Text       json.RawMessage `json:"text,omitempty"`
	Attributes []attributeBody `json:"attributes,omitempty"`
}

// Marshalling numbers, strings and a struct of them cannot fail.
func (w workItemUpdate) body() []byte {
	changed := updateWorkItemBody{Type: w.typeJSON(), Text: w.input.textJSON(), Attributes: attributeBodies(w.attributes)}
	if w.input.spent != nil {
		changed.Duration = &minutesBody{Minutes: w.input.spent.minutes}
	}
	if w.input.day != nil {
		changed.Date = &w.input.day.noon
	}
	body, _ := json.Marshal(changed)
	return body
}

// The type of the body: the id the read before the write resolved, an explicit null where the call takes the
// type away, and nothing at all where it says neither.
func (w workItemUpdate) typeJSON() json.RawMessage {
	switch {
	case w.input.clearsType:
		return json.RawMessage("null")
	case w.workType == nil:
		return nil
	}
	encoded, _ := json.Marshal(workItemIDBody{ID: w.workType.id})
	return encoded
}

func (w workItemUpdateInput) textJSON() json.RawMessage {
	switch {
	case w.clearsText:
		return json.RawMessage("null")
	case w.text == nil:
		return nil
	}
	encoded, _ := json.Marshal(*w.text)
	return encoded
}

// verifyFields is what the answer to the write is read for beside what the caller asked to print: the parts that
// went out, and nothing else. Neither the id nor the issue is asked for the way a creation asks for them — the
// work item was addressed by a pair the caller wrote, and that is what a refusal names it by.
func (w workItemUpdateInput) verifyFields() []requestedField {
	var own []requestedField
	if w.spent != nil {
		own = append(own, requestedField{name: durationKey})
	}
	if w.day != nil {
		own = append(own, requestedField{name: dateKey})
	}
	if w.text != nil || w.clearsText {
		own = append(own, requestedField{name: textKey})
	}
	if w.clearsType {
		own = append(own, requestedField{name: typeKey, children: []requestedField{{name: nameKey}}})
	}
	return own
}

func (w workItemUpdate) verifyFields() []requestedField {
	return verifyFieldsWithSettings(w.input.verifyFields(), w.workType, w.attributes)
}

func (w workItemUpdate) verify(a decodedResponse) *diag.Fault {
	return verifyWorkItem(a, w.input.diff(a.objects[0]), w.workType, w.attributes, []render.Pair{
		{Key: issueOwner.String(), Value: render.NewString(w.issue)},
		{Key: idKey, Value: render.NewString(w.at.String())},
	})
}

// diff holds the answer against the parts the update sent and against those alone: what the call named
// nothing for is the work item as it stood, and a workflow that moved it is the server's word, not a
// disagreement with a write that said nothing about it.
func (w workItemUpdateInput) diff(item map[string]any) []mismatch {
	var wrong []mismatch
	if w.spent != nil {
		wrong = w.spent.verify(wrong, item[durationKey])
	}
	if w.day != nil {
		wrong = w.day.verify(wrong, item[dateKey])
	}
	switch {
	case w.text != nil:
		wrong = textMismatch(wrong, textKey, *w.text, item[textKey])
	case w.clearsText:
		wrong = emptyMismatch(wrong, textKey, item[textKey])
	}
	if w.clearsType {
		// By the name it is printed under: a caller who emptied the type has no id of theirs to be shown.
		wrong = emptyMismatch(wrong, typeKey, memberOf(item[typeKey], nameKey))
	}
	return wrong
}

type issueInput struct {
	summary           *string
	description       *string
	named             []namedValue
	clearsDescription bool
	cleared           []string
}

// parseIssueCreate is the whole of what a creation writes, read off the flags it was given: a new issue is filed
// with a title and keeps nothing a call does not put there, so there is nothing for it to empty.
func parseIssueCreate(summary string, description *string, filled []string) (issueInput, *diag.Fault) {
	if fault := rejectReplacedText(&summary, description, descriptionOfANewIssue); fault != nil {
		return issueInput{}, fault
	}
	named, fault := parseFieldValues(filled)
	if fault != nil {
		return issueInput{}, fault
	}
	return issueInput{summary: &summary, description: description, named: named}, nil
}

// parseIssueUpdate is the whole of what an update writes, read off the flags it was given.
func parseIssueUpdate(summary, description *string, filled, cleared []string) (issueInput, *diag.Fault) {
	emptied, clearsDescription, fault := clearedFields(cleared)
	if fault != nil {
		return issueInput{}, fault
	}
	if clearsDescription && description != nil {
		return issueInput{}, &diag.Fault{Code: diag.BadUsage, Message: descriptionBothWays}
	}
	if fault := rejectReplacedText(summary, description, descriptionOfAnIssue); fault != nil {
		return issueInput{}, fault
	}
	named, fault := parseFieldValues(filled)
	if fault != nil {
		return issueInput{}, fault
	}
	return issueInput{summary: summary, description: description, named: named, clearsDescription: clearsDescription, cleared: emptied}, nil
}

// A custom field as the caller wrote it: the name they addressed it by and the value they gave it.
type namedValue struct {
	name  string
	value string
}

// parseFieldValues reads --field. The split is at the first = of the flag, so a value carrying one of its own goes
// out whole, and the name is taken as it was typed: a project may well call a field something with a space at
// the end of it.
func parseFieldValues(filled []string) ([]namedValue, *diag.Fault) {
	named := make([]namedValue, 0, len(filled))
	for _, flag := range filled {
		name, value, split := strings.Cut(flag, "=")
		switch {
		case !split:
			message := fmt.Sprintf("--field %s holds no =: a custom field is filled by writing its name, an = "+
				"and the value, as in --field Type=Task", render.Quote(flag))
			return nil, &diag.Fault{Code: diag.BadUsage, Message: message}
		case name == "":
			message := fmt.Sprintf("--field %s names no custom field: the name stands before the =", render.Quote(flag))
			return nil, &diag.Fault{Code: diag.BadUsage, Message: message}
		}
		if fault := rejectOwnName(flag, name); fault != nil {
			return nil, fault
		}
		named = append(named, namedValue{name: name, value: value})
	}
	return named, nil
}

func clearedFields(cleared []string) ([]string, bool, *diag.Fault) {
	names := make([]string, 0, len(cleared))
	clearsDescription := false
	for _, name := range cleared {
		switch {
		case name == "":
			message := `--clear "" names no custom field: it takes the name of the field to empty, as in --clear Assignee`
			return nil, false, &diag.Fault{Code: diag.BadUsage, Message: message}
		case strings.EqualFold(name, summaryKey):
			message := fmt.Sprintf("--clear %s names the summary of the issue, which YouTrack files none "+
				"without: a title is written with --summary and cannot be taken away", render.Quote(name))
			return nil, false, &diag.Fault{Code: diag.BadUsage, Message: message}
		case strings.EqualFold(name, descriptionKey):
			clearsDescription = true
		default:
			names = append(names, name)
		}
	}
	return names, clearsDescription, nil
}

func rejectOwnName(flag, name string) *diag.Fault {
	for _, own := range []string{summaryKey, descriptionKey} {
		if !strings.EqualFold(name, own) {
			continue
		}
		message := fmt.Sprintf("--field %s names the %s of the issue, which is no custom field of it: it is "+
			"written with --%s", render.Quote(flag), own, own)
		return &diag.Fault{Code: diag.BadUsage, Message: message}
	}
	return nil
}

func rejectReplacedText(summary, description *string, emptyDescription string) *diag.Fault {
	if summary != nil {
		if fault := rejectReplaced("--summary", *summary, summaryEmpty, summaryRewrites()); fault != nil {
			return fault
		}
	}
	if description != nil {
		if fault := rejectReplaced("--description", *description, emptyDescription, descriptionRewrites()); fault != nil {
			return fault
		}
	}
	return nil
}

// A rune YouTrack does not store as it arrived, against what it stores instead.
type charReplacement struct {
	rune rune
	into string
}

// A title is one line: YouTrack turns each line ending of it into a space, a CRLF into two, and drops the NEL
// outright.
func summaryRewrites() []charReplacement {
	return []charReplacement{
		{rune: '\n', into: "a space"},
		{rune: '\r', into: "a space"},
		{rune: 0x85, into: "nothing at all"},
		{rune: 0x2028, into: "a space"},
		{rune: 0x2029, into: "a space"},
	}
}

func descriptionRewrites() []charReplacement {
	return []charReplacement{{rune: '\r', into: "nothing at all"}}
}

const (
	summaryEmpty           = "is empty, and YouTrack files no issue without a title"
	descriptionOfANewIssue = "is empty, and YouTrack keeps an empty description as none: leave the flag out to " +
		"file the issue with no description at all"
	descriptionOfAnIssue = "is empty, and YouTrack keeps an empty description as none: --clear description " +
		"empties it outright, and a description the call does not write is left as the issue holds it"
)

func rejectReplaced(flag, text, empty string, replacements []charReplacement) *diag.Fault {
	if text == "" {
		return &diag.Fault{Code: diag.BadUsage, Message: flag + " " + empty}
	}
	if fault := rejectNoUTF8(flag, text); fault != nil {
		return fault
	}
	for _, rewritten := range replacements {
		if strings.ContainsRune(text, rewritten.rune) {
			return &diag.Fault{Code: diag.BadUsage, Message: rewrittenAs(flag, rewritten)}
		}
	}
	return nil
}

func rejectNoUTF8(flag, text string) *diag.Fault {
	if utf8.ValidString(text) {
		return nil
	}
	return &diag.Fault{Code: diag.BadUsage, Message: noUTF8(flag)}
}

// The encoder writes a byte that is no UTF-8 as U+FFFD, so the rewriting here would be ytrack's own and the
// check of the write would report it over an issue that by then exists.
func noUTF8(what string) string {
	return fmt.Sprintf("%s is no valid UTF-8, and every byte of it that is none would reach YouTrack as %s",
		what, render.Quote(string(utf8.RuneError)))
}

func rewrittenAs(what string, rewritten charReplacement) string {
	return fmt.Sprintf("%s holds U+%04X, which YouTrack stores as %s", what, rewritten.rune, rewritten.into)
}

// The body of a creation. The project is addressed by the id the metadata gave rather than by the code the
// caller typed: the code is how a human writes a project and the id is what the write is tied to.
type createIssueBody struct {
	Project      projectIDBody     `json:"project"`
	Summary      string            `json:"summary"`
	Description  *string           `json:"description,omitempty"`
	CustomFields []customFieldBody `json:"customFields,omitempty"`
}

type projectIDBody struct {
	ID string `json:"id"`
}

// The body of an update: the parts the call writes and not one key more. The issue is addressed by the path,
// and a part the body says nothing about is a part the issue keeps as it stands.
type updateIssueBody struct {
	Summary      *string           `json:"summary,omitempty"`
	Description  json.RawMessage   `json:"description,omitempty"`
	CustomFields []customFieldBody `json:"customFields,omitempty"`
}

// One custom field of the body. The field is addressed by the name it goes by rather than by the id of its
// binding: a localized name or one of another letter case answers 500 (ADR-0002). The $type is the one thing
// the server takes no field without, although the specification marks all three read-only.
type customFieldBody struct {
	Type  string `json:"$type"`
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type issueWrite struct {
	text    issueInput
	fields  []resolvedField
	project projectMetadata
}

// A custom field a write fills: the field as the project has it, the values as the caller wrote them, the same
// values as the body carries them, and the identities the answer is held against.
type resolvedField struct {
	field    projectField
	kind     fieldType
	values   []string
	sent     []any
	sentKeys []string
	// Whether --clear named the field, which is the call writing an empty value into it rather than any value
	// at all: values and checked are then empty, and the answer is held to holding nothing.
	cleared bool
}

// Marshalling strings, maps of them and structs of both cannot fail.
func (w issueWrite) createBody() []byte {
	body, _ := json.Marshal(createIssueBody{
		Project:      projectIDBody{ID: w.project.id},
		Summary:      *w.text.summary,
		Description:  w.text.description,
		CustomFields: w.bodies(),
	})
	return body
}

func (w issueWrite) updateBody() []byte {
	body, _ := json.Marshal(updateIssueBody{
		Summary:      w.text.summary,
		Description:  w.text.descriptionJSON(),
		CustomFields: w.bodies(),
	})
	return body
}

// descriptionJSON is the description of the body: the text where the call writes one, an explicit null where it clears
// one, and nothing at all where it says neither.
func (w issueInput) descriptionJSON() json.RawMessage {
	switch {
	case w.clearsDescription:
		return json.RawMessage("null")
	case w.description == nil:
		return nil
	}
	encoded, _ := json.Marshal(*w.description)
	return encoded
}

func (w issueWrite) bodies() []customFieldBody {
	fields := make([]customFieldBody, 0, len(w.fields))
	for _, field := range w.fields {
		fields = append(fields, field.body())
	}
	return fields
}

func (f resolvedField) body() customFieldBody {
	var value any
	switch {
	case f.kind.isMultiValue:
		value = f.sent
	case len(f.sent) > 0:
		value = f.sent[0]
	}
	return customFieldBody{Type: f.kind.sent, Name: f.field.info.name, Value: value}
}

// verifyFields is what the answer to the write is read for beside what the caller asked to print: every value that
// went out, so the check has it to compare, and the readable id, so a refusal can name the issue that by then
// exists whatever the caller asked for.
func (w issueWrite) verifyFields() []requestedField {
	own := []requestedField{{name: idReadableKey}}
	if w.text.summary != nil {
		own = append(own, requestedField{name: summaryKey})
	}
	if w.text.description != nil || w.text.clearsDescription {
		own = append(own, requestedField{name: descriptionKey})
	}
	if len(w.fields) > 0 {
		own = append(own, requestedField{name: customFieldsKey})
	}
	return own
}

// verify holds the answer against what the write sent: a 200 says the server took the body, not that what
// it kept is what went out, and printing the answer unchecked would hand a rewritten value back as the
// caller's own.
func (w issueWrite) verify(a decodedResponse) *diag.Fault {
	issue := a.objects[0]
	var wrong []mismatch
	if w.text.summary != nil {
		wrong = textMismatch(wrong, summaryKey, *w.text.summary, issue[summaryKey])
	}
	switch {
	case w.text.description != nil:
		wrong = textMismatch(wrong, descriptionKey, *w.text.description, issue[descriptionKey])
	case w.text.clearsDescription:
		wrong = emptyMismatch(wrong, descriptionKey, issue[descriptionKey])
	}
	wrong, fault := w.verifyCustomFields(a, wrong)
	switch {
	case fault != nil:
		return fault
	case len(wrong) == 0:
		return nil
	}
	return mismatchFault(a, knownAs(issueOwner.String(), responseID(a, idReadableKey)), wrong)
}

// The custom fields come back as a block of their own, so they are read the way the document reads them and
// held against what went out value by value. A field the answer does not carry at all is a field the write
// did not reach, which is the same disagreement as a value that came back another.
func (w issueWrite) verifyCustomFields(a decodedResponse, wrong []mismatch) ([]mismatch, *diag.Fault) {
	if len(w.fields) == 0 {
		return wrong, nil
	}
	n := converter{response: a}
	received, fault := n.readCustomFields(a.objects[0][customFieldsKey])
	if fault != nil {
		return nil, fault
	}
	held := make(map[string]issueCustomField, len(received))
	for _, field := range received {
		held[field.name] = field
	}
	for _, written := range w.fields {
		name := written.field.info.name
		field, onTheIssue := held[name]
		if !onTheIssue {
			// A field the answer carries nowhere is a field the issue holds nothing in, which is what a call
			// that emptied it asked for and what a call that filled it did not get.
			if written.cleared {
				continue
			}
			wrong = append(wrong, mismatch{field: name, expected: written.node(), actual: render.NewNull()})
			continue
		}
		texts, fault := n.valueKeys(field)
		if fault != nil {
			return nil, fault
		}
		if sameValues(written.kind, written.sentKeys, texts) {
			continue
		}
		wrong = append(wrong, mismatch{field: name, expected: written.node(), actual: valueNode(texts, written.kind)})
	}
	return wrong, nil
}

// Two sets of values name the same thing where each names every one of the other: the server answers in the
// order of the bundle rather than in the order the values were written and keeps one value of a value written
// twice, so neither the order nor the count of them is held to anything.
func sameValues(kind fieldType, sent, received []string) bool {
	return covers(kind, sent, received) && covers(kind, received, sent)
}

func covers(kind fieldType, all, some []string) bool {
	for _, value := range some {
		if !slices.ContainsFunc(all, func(held string) bool { return kind.sameValue(held, value) }) {
			return false
		}
	}
	return true
}

func (f resolvedField) node() *render.Node {
	return valueNode(f.values, f.kind)
}

// valueNode is values as the document prints what a field holds: the one value of a field that holds one, the
// list of a field that holds several, and null where a field that holds one holds nothing.
func valueNode(values []string, kind fieldType) *render.Node {
	items := make([]*render.Node, 0, len(values))
	for _, value := range values {
		items = append(items, render.NewString(value))
	}
	switch {
	case kind.isMultiValue:
		return render.NewList(items...)
	case len(items) == 0:
		return render.NewNull()
	}
	return items[0]
}

// A value the write sent against the value the answer brought back for it, each written as issue show prints it.
type mismatch struct {
	field    string
	expected *render.Node
	actual   *render.Node
}

// A name the answer carries as something other than text — a null, a number, or nothing at all — is a value
// that is not what was written, whatever else it is.
func textMismatch(wrong []mismatch, field, sent string, value any) []mismatch {
	if received, isText := value.(string); isText && received == sent {
		return wrong
	}
	return append(wrong, mismatch{field: field, expected: render.NewString(sent), actual: rawValueNode(value)})
}

// A part the call emptied is a part the answer holds nothing in. Anything still standing there is the write
// disagreeing with itself as much as a value that came back another.
func emptyMismatch(wrong []mismatch, field string, value any) []mismatch {
	if value == nil {
		return wrong
	}
	return append(wrong, mismatch{field: field, expected: render.NewNull(), actual: rawValueNode(value)})
}

// Размер считается по потоку, поэтому файл, изменившийся при чтении, ошибкой не считается.
func sizeMismatch(wrong []mismatch, sent int64, value any) []mismatch {
	if received, isNumber := parseInt64(value); isNumber && received == sent {
		return wrong
	}
	written := render.NewNumber(json.Number(strconv.FormatInt(sent, 10)))
	return append(wrong, mismatch{field: sizeKey, expected: written, actual: rawValueNode(value)})
}

// What the write left behind is the server's word by now, so the refusal names the entity it wrote and each
// value both ways rather than sending anything else to find out. identity is how the entity is addressed:
// what the answer brought back where the write is what brought it into being, what the caller wrote where it
// stood there already, and both an owner and a child where one alone names nothing.
func mismatchFault(a decodedResponse, identity []render.Pair, wrong []mismatch) *diag.Fault {
	entries := make([]*render.Node, 0, len(wrong))
	for _, m := range wrong {
		entries = append(entries, render.NewMap(
			render.Pair{Key: "field", Value: render.NewString(m.field)},
			render.Pair{Key: "expected", Value: m.expected},
			render.Pair{Key: "actual", Value: m.actual}))
	}
	details := append([]render.Pair{requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted())},
		append(identity, render.Pair{Key: "mismatch", Value: render.NewList(entries...)})...)
	message := "the write went through and the values under mismatch came back as something other than what was written"
	return &diag.Fault{Code: diag.UpstreamInvalid, Message: message, Details: details}
}

// knownAs is the one key an entity with an address of its own is named by.
func knownAs(named string, id *render.Node) []render.Pair {
	return []render.Pair{{Key: named, Value: id}}
}

func responseID(a decodedResponse, name string) *render.Node {
	id, isText := a.objects[0][name].(string)
	if !isText {
		return render.NewNull()
	}
	return render.NewString(id)
}

// The metadata a write reads off a project: the id the body addresses it by, the name the server keeps it
// under, every custom field with what settles whether a write has to name it, and the answer it all arrived
// in, which a refusal before the write names as the request that was sent.
type projectMetadata struct {
	id       string
	code     string
	fields   []projectField
	response decodedResponse
}

// A custom field of a project as a write reads it: the naming every command resolves by, and beside it whether
// the project lets the field stand empty, what it puts there unasked and the condition that hides it.
type projectField struct {
	customField
	canBeEmpty bool
	defaults   []string
	condition  fieldCondition
}

// A condition keeps a custom field off an issue until the field it watches holds one of the values it names.
type fieldCondition struct {
	kind             string
	controls         string
	values           []string
	showForNullValue bool
	given            bool
}

// What a write reads of a project before it sends anything, read afresh every time and never off the cache:
// a $type, a requirement or a condition out of date would turn into a refusal ytrack invented or a field the
// server threw away silently, and both would be ytrack's word about a server it had not asked.
func writeMetadataFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: "shortName"},
		{name: customFieldsKey, children: []requestedField{
			{name: idKey},
			{name: "canBeEmpty"},
			{name: "defaultValues", children: []requestedField{{name: nameKey}}},
			{name: "condition", children: []requestedField{
				{name: "$type"},
				{name: "showForNullValue"},
				{name: "field", children: []requestedField{{name: idKey}}},
				{name: "values", children: []requestedField{{name: nameKey}}},
			}},
			fieldInfoFields(),
		}},
	}
}

func (c *Client) readProjectMetadata(ctx context.Context, spec *schemas, code string) (projectMetadata, *diag.Fault) {
	a, fault := c.request(ctx, spec, projectSchema, writeMetadataFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProject(ctx, code, fields)
	})
	if fault != nil {
		return projectMetadata{}, fault
	}
	return readWriteMetadata(a, a.objects[0])
}

// The issue an update writes, as the one read before it sees it: the id the write is addressed by, the project
// its names are resolved against, and the class the server names each field the issue already holds by.
type issueForUpdate struct {
	readable readableID
	project  projectMetadata
	// The class of every custom field the issue carries, keyed by the binding that puts it there: the binding
	// is the project's field, which is what an element of the body is written for.
	kinds map[string]string
}

// What the read before an update asks for: the readable id, the project whole, and of each field on the issue
// the class the server named it by beside the binding that field stands for. The project is read here rather
// than off /admin/projects, so an update costs one request where a creation costs one as well.
func issueToWriteFields() []requestedField {
	return []requestedField{
		{name: idReadableKey},
		{name: customFieldsKey, children: []requestedField{
			{name: "$type"},
			{name: nameKey},
			{name: "projectCustomField", children: []requestedField{{name: idKey}}},
		}},
		{name: "project", children: writeMetadataFields()},
	}
}

func (c *Client) readIssueToWrite(ctx context.Context, spec *schemas, id string) (issueForUpdate, *diag.Fault) {
	a, fault := c.request(ctx, spec, issueSchema, issueToWriteFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return issueForUpdate{}, fault
	}
	readable, fault := readableIDAt(a, a.objects[0], issueOwner, "an update")
	if fault != nil {
		return issueForUpdate{}, fault
	}
	held, isObject := a.objects[0]["project"].(map[string]any)
	if !isObject {
		return issueForUpdate{}, shapeFailure(a.httpResponse, a.body, "the project of the issue is not a JSON object")
	}
	project, fault := readWriteMetadata(a, held)
	if fault != nil {
		return issueForUpdate{}, fault
	}
	kinds, fault := readIssueKinds(a)
	if fault != nil {
		return issueForUpdate{}, fault
	}
	return issueForUpdate{readable: readable, project: project, kinds: kinds}, nil
}

func readIssueKinds(a decodedResponse) (map[string]string, *diag.Fault) {
	items, isList := a.objects[0][customFieldsKey].([]any)
	if !isList {
		return nil, shapeFailure(a.httpResponse, a.body, "the custom fields of the issue are not a JSON array")
	}
	kinds := make(map[string]string, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.httpResponse, a.body, "a custom field of the issue is not a JSON object")
		}
		name, isNamed := object[nameKey].(string)
		kind, isText := object["$type"].(string)
		if !isNamed || !isText {
			return nil, shapeFailure(a.httpResponse, a.body, brokenIssueField)
		}
		place, isObject := object["projectCustomField"].(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.httpResponse, a.body, brokenBinding(name))
		}
		binding, isText := place[idKey].(string)
		if !isText {
			return nil, shapeFailure(a.httpResponse, a.body, brokenBinding(name))
		}
		kinds[binding] = kind
	}
	return kinds, nil
}

const brokenIssueField = "the name or the class of a custom field of the issue is not text"

const brokenProject = "the id or the short name of the project is not text"

func readWriteMetadata(a decodedResponse, project map[string]any) (projectMetadata, *diag.Fault) {
	id, isText := project[idKey].(string)
	code, isName := project["shortName"].(string)
	if !isText || !isName {
		return projectMetadata{}, shapeFailure(a.httpResponse, a.body, brokenProject)
	}
	items, isList := project[customFieldsKey].([]any)
	if !isList {
		return projectMetadata{}, shapeFailure(a.httpResponse, a.body, "the custom fields of the project are not a JSON array")
	}
	fields := make([]projectField, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return projectMetadata{}, shapeFailure(a.httpResponse, a.body, brokenField)
		}
		field, ok := readProjectField(object)
		if !ok {
			return projectMetadata{}, shapeFailure(a.httpResponse, a.body, brokenFieldInfo)
		}
		fields = append(fields, field)
	}
	return projectMetadata{id: id, code: code, fields: fields, response: a}, nil
}

func readProjectField(object map[string]any) (projectField, bool) {
	id, isText := object[idKey].(string)
	named, isNamed := readFieldInfo(object)
	canBeEmpty, isFlag := object["canBeEmpty"].(bool)
	if !isText || !isNamed || !isFlag {
		return projectField{}, false
	}
	// Only the types that keep their values in a bundle declare defaultValues at all, so a field without the
	// key is a field the project fills with nothing rather than an answer of the wrong shape.
	defaults, ok := readValueNames(object["defaultValues"])
	if !ok {
		return projectField{}, false
	}
	shown, ok := readCondition(object["condition"])
	if !ok {
		return projectField{}, false
	}
	field := customField{id: id, info: named}
	return projectField{customField: field, canBeEmpty: canBeEmpty, defaults: defaults, condition: shown}, true
}

// readValueNames is the names of a list of bundle values, and nothing for a key the answer left out or sent
// null: a field with no values named is a field with no values, not a broken answer.
func readValueNames(value any) ([]string, bool) {
	if value == nil {
		return nil, true
	}
	items, isList := value.([]any)
	if !isList {
		return nil, false
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, false
		}
		name, isText := object[nameKey].(string)
		if !isText {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

// A condition of a kind ytrack cannot evaluate is read as far as its kind and no further: the members below
// belong to FieldBasedCondition, and the specification declares no other subtype that has them.
func readCondition(value any) (fieldCondition, bool) {
	if value == nil {
		return fieldCondition{}, true
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		return fieldCondition{}, false
	}
	kind, isText := object["$type"].(string)
	if !isText {
		return fieldCondition{}, false
	}
	if kind != fieldBasedCondition {
		return fieldCondition{kind: kind, given: true}, true
	}
	shown, isFlag := object["showForNullValue"].(bool)
	values, isList := readValueNames(object["values"])
	if !isFlag || !isList {
		return fieldCondition{}, false
	}
	controls := ""
	// The specification allows the field a condition watches to be null, and a condition watching nothing
	// hides nothing.
	if watched, isObject := object["field"].(map[string]any); isObject {
		if controls, isText = watched[idKey].(string); !isText {
			return fieldCondition{}, false
		}
	}
	return fieldCondition{kind: kind, controls: controls, values: values, showForNullValue: shown, given: true}, true
}

// resolve is the write with every name of it resolved against the project the issue is filed in, which is the
// whole of what a name is held against: nothing of a name ever reaches the server, since YouTrack answers an
// unknown field name with a 500 (ADR-0002). named is the class the server itself gave each field the issue
// already holds, by the id of its binding, and nothing at all where the issue does not exist yet.
func (w issueInput) resolve(project projectMetadata, named map[string]string) (issueWrite, *diag.Fault) {
	fields, fault := project.resolveFields(w.named, w.cleared, named)
	if fault != nil {
		return issueWrite{}, fault
	}
	return issueWrite{text: w, fields: fields, project: project}, nil
}

// resolveFields is the custom fields the call writes, in the order the project puts its fields in, so that a body
// says nothing about the order the flags were written in; the values of one field keep that order, since the
// server keeps what it is given.
//
// Every name that resolves to no field of the project is refused at once, and so is every value the call
// cannot send: a caller fixing one flag per attempt would read the project as many times over.
func (p projectMetadata) resolveFields(named []namedValue, cleared []string, kinds map[string]string) ([]resolvedField, *diag.Fault) {
	catalogue := make([]fieldInfo, 0, len(p.fields))
	for _, field := range p.fields {
		catalogue = append(catalogue, field.info)
	}
	given := make([][]string, len(p.fields))
	emptied := make([]bool, len(p.fields))
	var unknown, ambiguous []*render.Node
	// A name that resolves to nothing stands in the refusal once, however many flags wrote it: a field that
	// holds several values takes --field once per value, so a misspelling there would be listed once per value.
	listed := make(map[string]bool, len(named)+len(cleared))
	place := func(name string) (int, bool) {
		places := findMatches(name, catalogue)
		switch {
		case len(places) == 1:
			return places[0], true
		case listed[name]:
		case len(places) == 0:
			unknown = append(unknown, unknownEntry(name, nearestNamed(name, catalogue)))
		default:
			ambiguous = append(ambiguous, ambiguousEntry(name, canonical(pick(catalogue, places))))
		}
		listed[name] = true
		return 0, false
	}
	for _, addressed := range named {
		if at, found := place(addressed.name); found {
			given[at] = append(given[at], addressed.value)
		}
	}
	for _, name := range cleared {
		if at, found := place(name); found {
			emptied[at] = true
		}
	}
	switch {
	case len(unknown) > 0:
		return nil, p.fault(diag.UnknownName, unknownMessage, "unknown", unknown)
	case len(ambiguous) > 0:
		return nil, p.fault(diag.UnknownName, ambiguousMessage, "ambiguous", ambiguous)
	}
	return p.encodeValues(given, emptied, kinds)
}

func (p projectMetadata) encodeValues(given [][]string, emptied []bool, kinds map[string]string) ([]resolvedField, *diag.Fault) {
	fields := make([]resolvedField, 0, len(p.fields))
	var invalid []*render.Node
	for at, values := range given {
		if len(values) == 0 && !emptied[at] {
			continue
		}
		field := p.fields[at]
		kind, modelled := typeOf(field.info)
		if !modelled {
			return nil, unmodelledType(field.info, p.response)
		}
		// The class of a field the issue already carries is copied off the server word for word: it knows of a
		// StateMachineIssueCustomField, which no table of ytrack's can, and where it has said which class a
		// field is of, a class of ytrack's own would be a guess made beside an answer.
		if sent, onTheIssue := kinds[field.id]; onTheIssue {
			kind.sent = sent
		}
		written := resolvedField{field: field, kind: kind, values: values, cleared: emptied[at]}
		switch {
		case emptied[at] && len(values) > 0:
			invalid = append(invalid, invalidEntry(field.info.name, values[0], setAndClearedMessage))
			continue
		case emptied[at]:
			// An empty list rather than the nil one, which marshals as the null a multi-valued field is not
			// given; a field that holds one value carries no list at all.
			written.sent = []any{}
			fields = append(fields, written)
			continue
		case !kind.isMultiValue && len(values) > 1:
			reason := fmt.Sprintf("the custom field holds one value by its type, and the call gives it %d", len(values))
			invalid = append(invalid, invalidEntry(field.info.name, values[1], reason))
			continue
		}
		for _, value := range values {
			sent, reason := kind.encodeValue(value)
			if reason != "" {
				invalid = append(invalid, invalidEntry(field.info.name, value, reason))
				continue
			}
			written.sent = append(written.sent, sent.body)
			written.sentKeys = append(written.sentKeys, sent.valueKey)
		}
		fields = append(fields, written)
	}
	if len(invalid) > 0 {
		return nil, p.fault(diag.BadUsage, invalidMessage, "invalid", invalid)
	}
	return fields, nil
}

func invalidEntry(field, value, reason string) *render.Node {
	return render.NewMap(
		render.Pair{Key: "field", Value: render.NewString(field)},
		render.Pair{Key: "value", Value: render.NewString(value)},
		render.Pair{Key: "reason", Value: render.NewString(reason)})
}

// missing is every custom field the project requires of a new issue that the write does not fill, in the order
// the project sends its fields in and all of them at once: the server names one field per attempt, so a caller
// told by the server alone would learn the next one only by filing again.
func (w issueWrite) missing() []string {
	var missing []string
	for _, field := range w.project.fields {
		if field.canBeEmpty || len(field.defaults) > 0 || w.sets(field) {
			continue
		}
		if _, hidden := w.hiddenReason(field); hidden {
			continue
		}
		missing = append(missing, field.info.name)
	}
	return missing
}

// requiredEmptied is every custom field the call empties that the project lets no issue stand without, in the
// order the project puts its fields in and all of them at once, so that a caller is not answered one field per
// attempt.
func (w issueWrite) requiredEmptied() []string {
	var required []string
	for _, written := range w.fields {
		if written.cleared && !written.field.canBeEmpty {
			required = append(required, written.field.info.name)
		}
	}
	return required
}

func (w issueWrite) sets(field projectField) bool {
	return slices.ContainsFunc(w.fields, func(written resolvedField) bool { return written.field.id == field.id })
}

const (
	missingMessage   = "the custom fields under missing are required by the project and the call fills none of them"
	emptiedMessage   = "the custom fields under missing are required by the project and the call empties them"
	unknownMessage   = "the names under unknown are not custom fields of the project"
	ambiguousMessage = "the names under ambiguous are the names of more than one custom field of the project each"
	invalidMessage   = "the values under invalid are not values the fields they name can be given, and nothing was sent"
	hiddenMessage    = "the custom fields under invalid do not stand on the issue the call would file, and " +
		"nothing was sent"
	setAndClearedMessage = "the call writes a value into the custom field and empties it both, and one write " +
		"leaves it one way"
)

// A refusal the metadata of the project settled names that read as the request that was sent, since it is the
// only one that went out, and the project the names were held against.
func (p projectMetadata) fault(code diag.Code, message, key string, entries []*render.Node) *diag.Fault {
	a := p.response
	details := []render.Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: "project", Value: render.NewString(p.code)},
		{Key: key, Value: render.NewList(entries...)},
	}
	return &diag.Fault{Code: code, Message: message, Details: details}
}

func names(fields []string) []*render.Node {
	nodes := make([]*render.Node, 0, len(fields))
	for _, name := range fields {
		nodes = append(nodes, render.NewString(name))
	}
	return nodes
}

func invalidEntries(hidden []hiddenField) []*render.Node {
	entries := make([]*render.Node, 0, len(hidden))
	for _, field := range hidden {
		entries = append(entries, invalidEntry(field.name, field.value, field.reason))
	}
	return entries
}

func (w issueWrite) hiddenReason(field projectField) (string, bool) {
	c := field.condition
	if !c.given || c.kind != fieldBasedCondition || c.controls == "" {
		return "", false
	}
	watched, found := fieldByID(w.project.fields, c.controls)
	if !found || watched.info.isMultiValue {
		return "", false
	}
	held, filled := w.effectiveValue(watched)
	switch {
	case !filled:
		if c.showForNullValue {
			return "", false
		}
	case slices.ContainsFunc(c.values, func(name string) bool { return strings.EqualFold(name, held) }):
		return "", false
	}
	return hiddenBy(watched.info.name, c, held, filled), true
}

// hiddenFields is every custom field the call fills that a condition keeps off the issue the body files, in the
// order the project puts its fields in and all of them at once.
func (w issueWrite) hiddenFields() []hiddenField {
	var hidden []hiddenField
	for _, written := range w.fields {
		reason, kept := w.hiddenReason(written.field)
		if !kept || len(written.values) == 0 {
			continue
		}
		hidden = append(hidden, hiddenField{name: written.field.info.name, value: written.values[0], reason: reason})
	}
	return hidden
}

// A custom field the call fills that the issue would not hold, as the refusal names it.
type hiddenField struct {
	name   string
	value  string
	reason string
}

func (w issueWrite) effectiveValue(field projectField) (string, bool) {
	for _, written := range w.fields {
		if written.field.id == field.id && len(written.values) > 0 {
			return written.values[0], true
		}
	}
	return field.defaultValue()
}

// What a field holds on an issue the write does not name it on: the value the project fills it with unasked,
// and nothing where the project fills it with none.
func (f projectField) defaultValue() (string, bool) {
	if len(f.defaults) == 0 {
		return "", false
	}
	return f.defaults[0], true
}

func hiddenBy(controls string, c fieldCondition, held string, filled bool) string {
	return fmt.Sprintf("the project shows the custom field on an issue whose %s holds %s, the issue this call "+
		"files holds %s in it, and YouTrack would file the issue without the value under a 200",
		controls, c.visibleForValues(), describeValue(held, filled))
}

// visibleForValues is the values a condition shows its field at, read as a caller reads them: a condition with no value
// to show its field at and no null to show it for hides it from every issue there is.
func (c fieldCondition) visibleForValues() string {
	shown := make([]string, 0, len(c.values)+1)
	for _, name := range c.values {
		shown = append(shown, render.Quote(name))
	}
	if c.showForNullValue {
		shown = append(shown, emptyValueText)
	}
	if len(shown) == 0 {
		return "no value at all"
	}
	return strings.Join(shown, " or ")
}

func describeValue(held string, filled bool) string {
	if !filled {
		return emptyValueText
	}
	return render.Quote(held)
}

const emptyValueText = "nothing at all"

func fieldByID(fields []projectField, id string) (projectField, bool) {
	for _, field := range fields {
		if field.id == id {
			return field, true
		}
	}
	return projectField{}, false
}
