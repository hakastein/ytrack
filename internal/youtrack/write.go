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

// CreateIssue is the call that files an issue in the project of that code, titled summary and, where
// description is given, carrying that prose. Each of filled is one custom field of the new issue, written
// Name=value. It prints the issue as it stands after the write, with the fields of expression, or with them
// added to IssueShowFields when it starts with +; nil is the caller leaning on the default whole.
func CreateIssue(code, summary string, description *string, filled []string, expression *string) (Call, *diag.Fault) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	parts, fault := filedIssue(summary, description, filled)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := issueFields(spec, expression, IssueShowFields, issueComments().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.issueCreated(ctx, spec, code, parts, requested)
	}, nil
}

// UpdateIssue is the call that writes the parts given into the issue of that id: the title where summary is
// given, the prose where description is, one custom field per element of filled, written Name=value, and an
// empty value into each part cleared names. A part the call does not give is left as the issue holds it. It
// prints the issue as it stands after the write, with the fields of expression, or with them added to
// IssueShowFields when it starts with +; nil is the caller leaning on the default whole.
func UpdateIssue(id string, summary, description *string, filled, cleared []string, expression *string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	if summary == nil && description == nil && len(filled) == 0 && len(cleared) == 0 {
		return nil, &diag.Fault{Code: diag.BadUsage, Message: nothingToWrite}
	}
	parts, fault := rewrittenIssue(summary, description, filled, cleared)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := issueFields(spec, expression, IssueShowFields, issueComments().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.issueUpdated(ctx, spec, id, parts, requested)
	}, nil
}

// An update that writes nothing would be a request sent to leave the issue as it is.
const nothingToWrite = "the call writes nothing into the issue: an update is given --summary, --description, " +
	"--field \"Name=value\" or --clear Name, and a part it is given none of is left as the issue holds it"

const proseBothWays = "--description writes the prose of the issue and --clear description empties it, and the " +
	"call gives both"

func (c *Client) issueCreated(ctx context.Context, spec *schemas, code string, parts writtenIssue, requested []requestedField) (*render.Node, *diag.Fault) {
	project, fault := c.projectToWrite(ctx, spec, code)
	if fault != nil {
		return nil, fault
	}
	// A new issue carries no field yet, so every class the body names comes from the table.
	filed, fault := parts.filling(project, nil)
	if fault != nil {
		return nil, fault
	}
	if hidden := filed.hidden(); len(hidden) > 0 {
		return nil, project.refusing(diag.BadUsage, hiddenMessage, "invalid", invalidEntries(hidden))
	}
	if missing := filed.missing(); len(missing) > 0 {
		return nil, project.refusing(diag.MissingRequired, missingMessage, "missing", names(missing))
	}
	if fault := c.resolveCustomFields(ctx, spec, requested); fault != nil {
		return nil, fault
	}
	asked := asking(requested, filed.checked()...)
	issueBlocks(spec, composedIssue(), asked)
	body := filed.body()
	return c.writing(ctx, spec, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.createIssue(ctx, body, fields)
	}, filed.confirmedBy, writtenNode(requested))
}

// writtenNode is the document a write prints: the entity the answer carries, with the fields the caller asked
// for. It is handed to the passage rather than run after it, so a refusal it raises is a refusal after the
// write like any other.
func writtenNode(requested []requestedField) func(answer) (*render.Node, *diag.Fault) {
	return func(a answer) (*render.Node, *diag.Fault) {
		return objectNode(a, requested, a.objects[0], nil)
	}
}

// deletion is the whole of what a delete of an owner does, and it is one for both kinds: a deletion is answered
// with nothing, so what is printed is read before it goes. The server answers dev-7 and 3-26 for the same issue
// and dev-A-7 and 177-58 for the same article, so the argument printed back would be neither checked nor the id
// the entity goes by, and the read that settles it turns an entity that is not there into a not_found before
// anything is destroyed. read asks for the one field printed; destroy goes to the readable id that read gave.
func (c *Client) deletion(ctx context.Context, spec *schemas, kind ownerKind, schema string,
	read func(ctx context.Context, fields string) (*http.Response, error),
	destroy func(ctx context.Context, at addressed) (*http.Response, error),
) (*render.Node, *diag.Fault) {
	requested := []requestedField{{name: idReadableKey}}
	answer, fault := c.passing(ctx, spec, schema, requested, read)
	if fault != nil {
		return nil, fault
	}
	readable, fault := addressedIn(answer, answer.objects[0], kind, "a deletion")
	if fault != nil {
		return nil, fault
	}
	if fault := writingNothing(ctx, func(ctx context.Context) (*http.Response, error) {
		return destroy(ctx, readable)
	}); fault != nil {
		return nil, fault
	}
	return objectNode(answer, requested, answer.objects[0], nil)
}

// An update reads the issue before it writes it, and that one read carries the whole of what the write needs:
// the readable id it is addressed by, the project the names are resolved against and the class the server names
// each field the issue already holds by.
//
// What the project requires is held against the fields the call empties and against nothing else: an issue
// filed before its project required a field holds that field empty to this day — DEV-2 of the polygon is one —
// so requiring it of a caller who never mentioned it would be a refusal ytrack invented.
func (c *Client) issueUpdated(ctx context.Context, spec *schemas, id string, parts writtenIssue, requested []requestedField) (*render.Node, *diag.Fault) {
	issue, fault := c.readIssueToWrite(ctx, spec, id)
	if fault != nil {
		return nil, fault
	}
	changed, fault := parts.filling(issue.project, issue.kinds)
	if fault != nil {
		return nil, fault
	}
	if emptied := changed.requiredEmptied(); len(emptied) > 0 {
		return nil, issue.project.refusing(diag.MissingRequired, emptiedMessage, "missing", names(emptied))
	}
	if fault := c.resolveCustomFields(ctx, spec, requested); fault != nil {
		return nil, fault
	}
	asked := asking(requested, changed.checked()...)
	issueBlocks(spec, composedIssue(), asked)
	body := changed.changes()
	return c.writing(ctx, spec, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.updateIssue(ctx, issue.readable, body, fields)
	}, changed.confirmedBy, writtenNode(requested))
}

// What a creation of an article carries as the call wrote it: the project it is filed in, its title, the text
// of it where the call writes one, and the article it is written under as the caller addressed it. A nil
// content is the caller who wrote no flag for it, and the key is then absent from the body, which is a third
// thing beside text and an explicit empty (ADR-0001).
//
// Nothing here builds a body: the parent is a readable id the server has not been asked about yet, and a body
// carrying it would be a body no read stands behind. filedUnder is what turns these into the write that goes.
type writtenArticle struct {
	project  string
	summary  string
	content  *string
	parentID *string
}

// articleFiledUnder is a creation with its parent settled: every part of it is one the reads before the write
// allowed, so the body, what the answer is read for and the check of it are built from these and from nothing
// else. filedUnder is the one place that makes one, which is what holds the read before the write in place of
// an order kept by hand.
type articleFiledUnder struct {
	project string
	summary string
	content *string
	// The article this one is written under, as the read before the write found it: the body carries the
	// internal id that read gave rather than the argument. It is nil where the call names no parent.
	parent *articleToWrite
}

// filedArticle is the whole of what a creation of an article writes, read off the flags it was given, with
// each part held to what YouTrack would keep of it.
//
// An article keeps a carriage return where the description of an issue loses one, so the text of it is refused
// for nothing but being empty; the title is one line here as it is there, and the runes YouTrack drops out of
// the title of an issue it keeps in the title of an article.
func filedArticle(code, summary string, content, parent *string) (writtenArticle, *diag.Fault) {
	if fault := refuseRewritten("--"+summaryKey, summary, summaryOfAnArticle, articleTitleRewrites()); fault != nil {
		return writtenArticle{}, fault
	}
	if content != nil {
		if fault := refuseRewritten("--"+contentKey, *content, contentOfANewArticle, nil); fault != nil {
			return writtenArticle{}, fault
		}
	}
	written := writtenArticle{project: code, summary: summary, content: content}
	if parent != nil {
		id, fault := parseArticleID(*parent)
		if fault != nil {
			return writtenArticle{}, fault
		}
		written.parentID = &id
	}
	return written, nil
}

// The title of an article is one line: YouTrack turns each line ending of it into a space and a CRLF into one
// space rather than into two, and keeps the NEL and the separators an issue loses.
func articleTitleRewrites() []rewrite {
	return []rewrite{
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

// What an update of an article writes as the call wrote it: the parts the call names and not one more, the
// parent among them as the caller addressed it. A nil part is the caller who wrote no flag for it, which leaves
// its key out of the body; an emptied part is a third thing beside text and no key at all, so it is said
// outright (ADR-0001).
//
// Nothing here builds a body, for the reason writtenArticle carries: writtenUnder is what turns these into the
// write that goes.
type changedArticle struct {
	summary       *string
	content       *string
	clearsContent bool
	parentID      *string
	clearsParent  bool
}

// articleWrittenUnder is an update with its parent settled, and what articleFiledUnder is to a creation: the
// body, what the answer is read for and the check of it are built from parts the reads before the write
// allowed. writtenUnder is the one place that makes one.
type articleWrittenUnder struct {
	summary       *string
	content       *string
	clearsContent bool
	// The article this one is moved under, as the read before the write found it: the body carries the internal
	// id that read gave rather than the argument. It is nil where the call names no parent or takes it away.
	parent       *articleToWrite
	clearsParent bool
}

// A part of an entity --clear empties, by the name the flag writes it under and by what it sets in the write.
// A second part is one more line in the table of that entity rather than a second reading of the flag.
type clearablePart[W any] struct {
	name  string
	empty func(*W)
}

func clearableArticleParts() []clearablePart[changedArticle] {
	return []clearablePart[changedArticle]{
		{name: contentKey, empty: func(w *changedArticle) { w.clearsContent = true }},
		{name: parentKey, empty: func(w *changedArticle) { w.clearsParent = true }},
	}
}

// rewrittenArticle is the whole of what an update of an article writes, read off the flags it was given, with
// each part held to what YouTrack would keep of it.
func rewrittenArticle(summary, content, parent *string, cleared []string) (changedArticle, *diag.Fault) {
	written := changedArticle{summary: summary, content: content}
	if fault := written.emptying(cleared); fault != nil {
		return changedArticle{}, fault
	}
	if written.clearsContent && content != nil {
		return changedArticle{}, &diag.Fault{Code: diag.BadUsage, Message: contentBothWays}
	}
	if written.clearsParent && parent != nil {
		return changedArticle{}, &diag.Fault{Code: diag.BadUsage, Message: parentBothWays}
	}
	if parent != nil {
		id, fault := parseArticleID(*parent)
		if fault != nil {
			return changedArticle{}, fault
		}
		written.parentID = &id
	}
	if summary != nil {
		if fault := refuseRewritten("--"+summaryKey, *summary, summaryOfAnArticle, articleTitleRewrites()); fault != nil {
			return changedArticle{}, fault
		}
	}
	if content != nil {
		if fault := refuseRewritten("--"+contentKey, *content, contentOfAnArticle, nil); fault != nil {
			return changedArticle{}, fault
		}
	}
	return written, nil
}

// emptying reads --clear: the parts of the article the call empties, matched without regard to letter case, the
// way every name a caller writes is matched.
func (w *changedArticle) emptying(cleared []string) *diag.Fault {
	parts := clearableArticleParts()
	for _, name := range cleared {
		at := emptying(parts, name)
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
func emptying[W any](parts []clearablePart[W], name string) int {
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
type updatedArticle struct {
	Summary *string `json:"summary,omitempty"`
	// Raw JSON rather than a string: the text, an explicit null and no key at all are three things, and a
	// pointer tells only two of them apart.
	Content json.RawMessage `json:"content,omitempty"`
	// Raw JSON for the same reason: an object moves the article, a null takes it to the root of the knowledge
	// base, and no key at all leaves it hanging where it hangs.
	ParentArticle json.RawMessage `json:"parentArticle,omitempty"`
}

// Marshalling strings and structs of them cannot fail.
func (w articleWrittenUnder) changes() []byte {
	body, _ := json.Marshal(updatedArticle{Summary: w.summary, Content: w.text(), ParentArticle: w.hangsFrom()})
	return body
}

// hangsFrom is the parent of the body: the internal id the read before the write gave, an explicit null where
// the call takes the parent away, and nothing at all where it says neither.
func (w articleWrittenUnder) hangsFrom() json.RawMessage {
	switch {
	case w.clearsParent:
		return json.RawMessage("null")
	case w.parent == nil:
		return nil
	}
	encoded, _ := json.Marshal(addressedArticle{ID: w.parent.id})
	return encoded
}

// text is the content of the body: the text where the call writes one, an explicit null where it empties one,
// and nothing at all where it says neither.
func (w articleWrittenUnder) text() json.RawMessage {
	switch {
	case w.clearsContent:
		return json.RawMessage("null")
	case w.content == nil:
		return nil
	}
	encoded, _ := json.Marshal(*w.content)
	return encoded
}

// checked is what the answer to the write is read for beside what the caller asked to print: every part that
// went out, so the check has it to compare, and the readable id, so a refusal can name the article whatever the
// caller asked for.
func (w articleWrittenUnder) checked() []requestedField {
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

// confirmedBy holds the answer against what the write sent: a 200 says the server took the body, not that what
// it kept is what went out. A part the call never named is never held against anything — the article holds
// what it held, and the answer is the only word there is on that.
func (w articleWrittenUnder) confirmedBy(a answer) *diag.Fault {
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
	return rewrittenByTheServer(a, knownAs(articleOwner.String(), writtenID(a, idReadableKey)), wrong)
}

// The body of a creation of an article. The project is addressed by the code the caller typed: unlike a
// creation of an issue, nothing of the project is read first, since YouTrack answers a code it has none of
// with a 404 of its own. No $type stands here — the server takes the article without one.
type createdArticle struct {
	Project articleProject `json:"project"`
	Summary string         `json:"summary"`
	Content *string        `json:"content,omitempty"`
	// The parent is addressed by the internal id the read before the write gave: this member takes no other
	// form, and a readable id under it is answered 400 Invalid structure of entity id.
	ParentArticle *addressedArticle `json:"parentArticle,omitempty"`
}

type articleProject struct {
	ShortName string `json:"shortName"`
}

type addressedArticle struct {
	ID string `json:"id"`
}

// Marshalling strings and structs of them cannot fail.
func (w articleFiledUnder) body() []byte {
	filed := createdArticle{
		Project: articleProject{ShortName: w.project},
		Summary: w.summary,
		Content: w.content,
	}
	if w.parent != nil {
		filed.ParentArticle = &addressedArticle{ID: w.parent.id}
	}
	body, _ := json.Marshal(filed)
	return body
}

// checked is what the answer to the write is read for beside what the caller asked to print: every part that
// went out, so the check has it to compare, and the readable id, so a refusal can name the article that by then
// exists whatever the caller asked for.
func (w articleFiledUnder) checked() []requestedField {
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

// confirmedBy holds the answer against what the write sent: a 200 says the server took the body, not that what
// it kept is what went out, and printing the answer unchecked would hand a rewritten value back as the
// caller's own.
func (w articleFiledUnder) confirmedBy(a answer) *diag.Fault {
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
	return rewrittenByTheServer(a, knownAs(articleOwner.String(), writtenID(a, idReadableKey)), wrong)
}

// The project is the one part held without regard to letter case: the server reads dev for DEV and answers
// with the code as it keeps it, so anything but the same project under another case is a disagreement.
func projectMismatch(wrong []mismatch, code string, value any) []mismatch {
	arrived := memberOf(value, shortNameKey)
	if kept, isText := arrived.(string); isText && strings.EqualFold(kept, code) {
		return wrong
	}
	return append(wrong, mismatch{field: projectKey, written: render.NewString(code), arrived: asArrived(arrived)})
}

// The parent is held by the readable id the read before the write gave it, which is what stands between the
// read and the write: a parent deleted in that moment leaves the article at the root of the tree under a 200,
// and the answer is the only word there is on it.
func parentMismatch(wrong []mismatch, readable string, value any) []mismatch {
	arrived := memberOf(value, idReadableKey)
	if kept, isText := arrived.(string); isText && kept == readable {
		return wrong
	}
	return append(wrong, mismatch{field: parentArticleKey, written: render.NewString(readable), arrived: asArrived(arrived)})
}

// A parent the call took away is an article the answer hangs from nothing at all. One still standing there is
// named by the readable id it came back under, the way a parent that was written is.
func noParentMismatch(wrong []mismatch, value any) []mismatch {
	if value == nil {
		return wrong
	}
	arrived := asArrived(memberOf(value, idReadableKey))
	return append(wrong, mismatch{field: parentArticleKey, written: render.NewNull(), arrived: arrived})
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
type commentWritten struct {
	text string
}

// commentText is the whole of what a comment writes, read off the flag it was given, with the text held to
// what YouTrack would keep of it — which is all of it. The server was measured to store thirty-two kinds of
// text at both kinds of owner byte for byte, a lone carriage return and three hundred kilobytes among them, so
// nothing is refused for being rewritten the way the title of an issue is.
func commentText(text string) (commentWritten, *diag.Fault) {
	if fault := refuseRewritten("--"+textKey, text, textOfAComment, nil); fault != nil {
		return commentWritten{}, fault
	}
	return commentWritten{text: text}, nil
}

// An empty text is refused at both kinds of owner although only one of them refuses it: a caller names an owner
// rather than an API, and what an empty comment does should not turn on whether the id they typed was an issue
// or an article.
const textOfAComment = "is empty, and a comment is the text of it: YouTrack answers an empty one on an issue " +
	"with a refusal of its own and keeps an empty comment on an article, so ytrack writes neither"

// The body of a comment: the text and nothing else. No $type stands here — the server takes the comment
// without one — and no id either, since the owner is the path and the id is the server's to give.
type createdComment struct {
	Text string `json:"text"`
}

// Marshalling a string and a struct of one cannot fail.
func (w commentWritten) body() []byte {
	body, _ := json.Marshal(createdComment{Text: w.text})
	return body
}

// checked is what the answer to the write is read for beside what the caller asked to print: the text that went
// out, so the check has it to compare, and the id, so a refusal can name the comment that by then exists
// whatever the caller asked for.
func (w commentWritten) checked() []requestedField {
	return []requestedField{{name: idKey}, {name: textKey}}
}

// 200 говорит лишь, что сервер принял тело, а не что сохранил тот же текст.
func (w commentWritten) confirms(a answer, named *render.Node) *diag.Fault {
	wrong := textMismatch(nil, textKey, w.text, a.objects[0][textKey])
	if len(wrong) == 0 {
		return nil
	}
	return rewrittenByTheServer(a, knownAs(commentKey, named), wrong)
}

func (w commentWritten) confirmedBy(a answer) *diag.Fault {
	return w.confirms(a, writtenID(a, idKey))
}

// commentRewritten is what an update of a comment carries, and what commentWritten is to a creation: the text,
// and beside it the comment the write goes to. The address is the caller's own, held to a form before anything
// was sent, so it names the comment in a refusal whatever the answer turns out to hold.
type commentRewritten struct {
	commentWritten
	at childID
}

// checked is what the answer to the write is read for beside what the caller asked to print: the text that went
// out, and nothing else. The id is not asked for the way a creation asks for it — the comment was addressed by
// an id the caller wrote, and that is what a refusal names it by.
func (w commentRewritten) checked() []requestedField {
	return []requestedField{{name: textKey}}
}

// Комментарий, удалённый между чтением и записью, приходит без текста — это тоже расхождение.
func (w commentRewritten) confirmedBy(a answer) *diag.Fault {
	return w.confirms(a, render.NewString(w.at.String()))
}

// What a creation of a work item carries as the call wrote it, and the whole of what the write goes out with:
// nothing of the issue is read first. A nil day or text is the caller who wrote no flag for it, and the
// key is then absent from the body — YouTrack writes such a work item against today of its own and leaves the
// text empty.
type writtenWorkItem struct {
	spent      writtenDuration
	day        *writtenAgainst
	text       *string
	attributes []namedValue
}

// How long a work item is, as the caller wrote it, which is what a refusal shows them, beside the minutes the
// body carries: the ISO period and the minutes are one length said two ways.
type writtenDuration struct {
	written string
	minutes int64
}

// The day a work item is written against: what the caller wrote and the millisecond the body carries for it.
type writtenAgainst struct {
	written string
	noon    int64
}

// A creation with its type of work settled, and what articleFiledUnder is to an article: the body, what the
// answer is read for and the check of it are built from parts a read before the write allowed, and the type is
// the only part there is such a read for. workItemFiled is the one place that makes one.
type workItemFiledWithType struct {
	written writtenWorkItem
	// The type of work, as the read before the write resolved it, and nil where the call names none: the body
	// then carries no type at all, and YouTrack writes the work item against none.
	workType   *filedWorkItemType
	attributes []filedAttribute
}

// The type of work a work item is written against: the id the body carries, since YouTrack answers a type given
// by name with укажите ее ID, and the name the caller wrote, which is what a refusal about it shows them.
type filedWorkItemType struct {
	id      string
	written string
}

// The type of work, read off --type. An empty name answers to no type of any project, and finding that out
// would cost the read before the write.
func refuseWorkItemType(named *string) *diag.Fault {
	if named == nil || *named != "" {
		return nil
	}
	return refusedValue("--"+typeKey, *named, "names no type of work: the types an issue may be written "+
		"against are the settings of its project, printed by ytrack project show <code> under plugins")
}

// YouTrack keeps an empty text of a work item as an empty text, so the one thing refused of --text
// here is what the encoder would rewrite: a byte that is no UTF-8 reaches the server as U+FFFD, and the check
// of the write would then report ytrack's own rewriting as the server's, over a work item that exists.
func refuseWorkItemText(text *string) *diag.Fault {
	if text == nil {
		return nil
	}
	return refuseNoUTF8("--"+textKey, *text)
}

// filedWorkItem is the whole of what a creation writes, read off the argument and the flags it was given. The
// name of the type is read here as well, although what goes out for it is the id the read before the write
// resolves: --type is read in one place, and an empty name costs no request to find out about.
func filedWorkItem(spent string, day, text, named *string, attributes []string) (writtenWorkItem, *diag.Fault) {
	length, fault := minutesSpent(spent)
	if fault != nil {
		return writtenWorkItem{}, fault
	}
	if fault := refuseWorkItemText(text); fault != nil {
		return writtenWorkItem{}, fault
	}
	written := writtenWorkItem{spent: length, text: text}
	if day != nil {
		against, fault := dayWrittenAgainst(*day)
		if fault != nil {
			return writtenWorkItem{}, fault
		}
		written.day = &against
	}
	if fault := refuseWorkItemType(named); fault != nil {
		return writtenWorkItem{}, fault
	}
	if written.attributes, fault = attributeValues(attributes); fault != nil {
		return writtenWorkItem{}, fault
	}
	return written, nil
}

// How long the work item is, written as ytrack prints one. A day and a week are refused rather than converted:
// P1D of YouTrack is the working day of the instance — eight hours on this one — so an ISO day would mean one
// thing here and another anywhere else.
func minutesSpent(text string) (writtenDuration, *diag.Fault) {
	minutes, read := periodMinutes(text)
	switch {
	case !read:
		return writtenDuration{}, refusedValue("duration", text, "is no ISO 8601 period of hours and minutes, as "+
			"in PT1H30M, PT90M or PT0M: ytrack writes a work item as the minutes it comes to, and neither a day "+
			"nor a week is a fixed count of them — YouTrack reads P1D as the working day of the instance — while "+
			"a second and a fraction are no part of what a work item holds")
	case minutes > math.MaxInt32:
		return writtenDuration{}, refusedValue("duration", text,
			fmt.Sprintf("is longer than the %d minutes YouTrack keeps a work item for", math.MaxInt32))
	}
	return writtenDuration{written: text, minutes: minutes}, nil
}

// The day, read off --date: the calendar day, or midnight UTC of one, which is how ytrack prints the day of a
// work item, so what time list printed goes back in as it came out. Any other moment is refused before the
// write: YouTrack would file it under the calendar day of the time zone of whoever's token wrote it.
func dayWrittenAgainst(text string) (writtenAgainst, *diag.Fault) {
	if day, err := time.Parse(time.DateOnly, text); err == nil {
		return writtenAgainst{written: text, noon: noonUTC(day)}, nil
	}
	moment, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return writtenAgainst{}, refusedValue(dateKey, text, "is neither a calendar day, as in 2026-09-01, nor "+
			"midnight UTC of one, as in 2026-09-01T00:00:00Z: a work item is written against a day, and "+
			"YouTrack keeps no moment of it")
	}
	utc := moment.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	if !utc.Equal(midnight) {
		return writtenAgainst{}, refusedValue(dateKey, text, "names a time of day, and a work item is written "+
			"against a day: YouTrack would file the moment under the calendar day of the time zone of whoever "+
			"wrote it, which is not the caller's to know")
	}
	if _, offset := moment.Zone(); offset != 0 {
		return writtenAgainst{}, refusedValue(dateKey, text, "is midnight UTC written in an offset of its own, "+
			"and a day goes in as the day it is written in: a calendar day, as in 2026-09-01, or midnight UTC "+
			"of one with Z or +00:00 on it, as ytrack prints it")
	}
	return writtenAgainst{written: text, noon: noonUTC(midnight)}, nil
}

// A value of a positional argument or of a flag, refused before anything is sent, with the value quoted: a byte
// that cannot be printed stands escaped, as it does wherever a caller's string reaches a document.
func refusedValue(named, value, because string) *diag.Fault {
	return &diag.Fault{Code: diag.BadUsage, Message: fmt.Sprintf("%s %s %s", named, render.Quote(value), because)}
}

// The body of a work item: how long it is, and the type, the day and the text where the call names them. No
// $type stands here — the server takes the work item without one — and no author either: YouTrack writes it
// down as whoever the token belongs to, and naming anyone else would be ytrack deciding for the caller.
type createdWorkItem struct {
	Duration   writtenMinutes     `json:"duration"`
	Type       *addressedWorkItem `json:"type,omitempty"`
	Date       *int64             `json:"date,omitempty"`
	Text       *string            `json:"text,omitempty"`
	Attributes []attributeWritten `json:"attributes,omitempty"`
}

type addressedWorkItem struct {
	ID string `json:"id"`
}

func (w workItemFiledWithType) body() []byte {
	written := createdWorkItem{Duration: writtenMinutes{Minutes: w.written.spent.minutes}, Text: w.written.text,
		Attributes: attributesWritten(w.attributes)}
	if w.workType != nil {
		written.Type = &addressedWorkItem{ID: w.workType.id}
	}
	if w.written.day != nil {
		written.Date = &w.written.day.noon
	}
	// Marshalling numbers, strings and a struct of them cannot fail.
	body, _ := json.Marshal(written)
	return body
}

// checked is what the answer to the write is read for beside what the caller asked to print: every part that
// went out, so the check has them to compare, and the pair a work item is addressed by, so a refusal names the
// work item that by then exists whatever the caller asked for.
func (w writtenWorkItem) checked() []requestedField {
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
func checkedWithSettings(own []requestedField, workType *filedWorkItemType, attributes []filedAttribute) []requestedField {
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
func confirmedWorkItem(a answer, wrong []mismatch, workType *filedWorkItemType, attributes []filedAttribute, identity []render.Pair) *diag.Fault {
	if workType != nil {
		wrong = workType.confirmedBy(wrong, a.objects[0][typeKey])
	}
	wrong = attributeMismatches(wrong, attributes, a.objects[0][attributesKey])
	if len(wrong) == 0 {
		return nil
	}
	return rewrittenByTheServer(a, identity, wrong)
}

func (w workItemFiledWithType) checked() []requestedField {
	return checkedWithSettings(w.written.checked(), w.workType, w.attributes)
}

// mismatches holds the answer against what the write sent: a 200 says the server took the body, not that what
// it kept is what went out. The day is held to the calendar day of UTC rather than to the millisecond,
// since the body carries noon and the server keeps midnight of the same day. What the call named nothing for is
// held to nothing: the day YouTrack chose itself and the empty text it left are its answer, not a disagreement.
func (w writtenWorkItem) mismatches(item map[string]any) []mismatch {
	wrong := w.spent.confirmedBy(nil, item[durationKey])
	if w.day != nil {
		wrong = w.day.confirmedBy(wrong, item[dateKey])
	}
	if w.text != nil {
		wrong = textMismatch(wrong, textKey, *w.text, item[textKey])
	}
	return wrong
}

func (w workItemFiledWithType) confirmedBy(a answer) *diag.Fault {
	return confirmedWorkItem(a, w.written.mismatches(a.objects[0]), w.workType, w.attributes, []render.Pair{
		{Key: issueOwner.String(), Value: owningIssue(a)},
		{Key: idKey, Value: writtenID(a, idKey)},
	})
}

// The type is held by the id that went out, which is the whole of what a type of work is written by; the names
// are what the disagreement is shown in, since a caller who wrote a name has no id of theirs to read.
func (t filedWorkItemType) confirmedBy(wrong []mismatch, value any) []mismatch {
	if kept, isText := memberOf(value, idKey).(string); isText && kept == t.id {
		return wrong
	}
	return append(wrong, mismatch{
		field:   typeKey,
		written: render.NewString(t.written),
		arrived: asArrived(memberOf(value, nameKey)),
	})
}

// A duration that arrived without the minutes it holds is as much a disagreement as one of another length: the
// minutes are the whole of what says how long a work item is.
func (d writtenDuration) confirmedBy(wrong []mismatch, value any) []mismatch {
	held, isObject := value.(map[string]any)
	if isObject {
		if minutes, isWhole := wholeNumber(held[minutesKey]); isWhole {
			if minutes == d.minutes {
				return wrong
			}
			return append(wrong, mismatch{
				field:   durationKey,
				written: render.NewString(d.written),
				arrived: render.NewString(duration(minutes)),
			})
		}
	}
	return append(wrong, mismatch{field: durationKey, written: render.NewString(d.written), arrived: render.NewNull()})
}

func (d writtenAgainst) confirmedBy(wrong []mismatch, value any) []mismatch {
	at, isInstant := wholeNumber(value)
	if isInstant && sameDayUTC(at, d.noon) {
		return wrong
	}
	arrived := render.NewNull()
	if isInstant {
		arrived = render.NewString(momentText(at))
	}
	return append(wrong, mismatch{field: dateKey, written: render.NewString(d.written), arrived: arrived})
}

func sameDayUTC(a, b int64) bool {
	return time.UnixMilli(a).UTC().Format(time.DateOnly) == time.UnixMilli(b).UTC().Format(time.DateOnly)
}

// The issue a work item hangs from, as the answer names it: a work item carries no readable id, so a refusal
// about one names the pair it is addressed by.
func owningIssue(a answer) *render.Node {
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
type changedWorkItem struct {
	spent            *writtenDuration
	day              *writtenAgainst
	text             *string
	attributes       []namedValue
	clearsType       bool
	clearsText       bool
	clearsAttributes []string
}

// An update with its type of work settled, and what workItemFiledWithType is to a creation. The pair the work
// item is addressed by stands here as well: it is the caller's own, held to a form before anything was sent, so
// a refusal names the work item whatever the answer turns out to hold.
type workItemRewritten struct {
	written changedWorkItem
	issue   string
	at      childID
	// The type of work, as the read before the write resolved it, and nil where the call names none.
	workType   *filedWorkItemType
	attributes []filedAttribute
}

// The parts of a work item --clear empties. Neither duration nor date is among them: YouTrack answers a null
// under either with Field <name> cannot be null, so a call that asks is refused before anything is sent.
func clearableWorkItemParts() []clearablePart[changedWorkItem] {
	return []clearablePart[changedWorkItem]{
		{name: typeKey, empty: func(w *changedWorkItem) { w.clearsType = true }},
		{name: textKey, empty: func(w *changedWorkItem) { w.clearsText = true }},
	}
}

func keptWorkItemParts() []string {
	return []string{durationKey, dateKey}
}

// rewrittenWorkItem is the whole of what an update writes, read off the flags it was given, and it is where a
// call that writes nothing is refused: every part is read in one place, so what the flags come to is settled
// here and nowhere above.
func rewrittenWorkItem(spent, day, text, named *string, attributes, cleared []string) (changedWorkItem, *diag.Fault) {
	if spent == nil && day == nil && text == nil && named == nil && len(attributes) == 0 && len(cleared) == 0 {
		return changedWorkItem{}, &diag.Fault{Code: diag.BadUsage, Message: nothingToWriteIntoAWorkItem}
	}
	var written changedWorkItem
	if fault := written.emptying(cleared); fault != nil {
		return changedWorkItem{}, fault
	}
	if written.clearsType && named != nil {
		return changedWorkItem{}, &diag.Fault{Code: diag.BadUsage, Message: typeBothWays}
	}
	if written.clearsText && text != nil {
		return changedWorkItem{}, &diag.Fault{Code: diag.BadUsage, Message: textBothWays}
	}
	if fault := refuseWorkItemText(text); fault != nil {
		return changedWorkItem{}, fault
	}
	written.text = text
	if spent != nil {
		length, fault := minutesSpent(*spent)
		if fault != nil {
			return changedWorkItem{}, fault
		}
		written.spent = &length
	}
	if day != nil {
		against, fault := dayWrittenAgainst(*day)
		if fault != nil {
			return changedWorkItem{}, fault
		}
		written.day = &against
	}
	if fault := refuseWorkItemType(named); fault != nil {
		return changedWorkItem{}, fault
	}
	var fault *diag.Fault
	if written.attributes, fault = attributeValues(attributes); fault != nil {
		return changedWorkItem{}, fault
	}
	for _, set := range written.attributes {
		if slices.ContainsFunc(written.clearsAttributes, func(name string) bool { return strings.EqualFold(name, set.name) }) {
			return changedWorkItem{}, attributeBothWays(set.name)
		}
	}
	return written, nil
}

func (w *changedWorkItem) emptying(cleared []string) *diag.Fault {
	parts := clearableWorkItemParts()
	for _, name := range cleared {
		if at := emptying(parts, name); at >= 0 {
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
type updatedWorkItem struct {
	Duration *writtenMinutes `json:"duration,omitempty"`
	// Raw JSON rather than an object: a type, an explicit null and no key at all are three things, and a pointer
	// tells only two of them apart.
	Type json.RawMessage `json:"type,omitempty"`
	Date *int64          `json:"date,omitempty"`
	// Raw JSON for the same reason, and the reason is sharper here: YouTrack keeps an empty text as an empty
	// text, so only a null empties one.
	Text       json.RawMessage    `json:"text,omitempty"`
	Attributes []attributeWritten `json:"attributes,omitempty"`
}

// Marshalling numbers, strings and a struct of them cannot fail.
func (w workItemRewritten) body() []byte {
	changed := updatedWorkItem{Type: w.writtenType(), Text: w.written.writtenText(), Attributes: attributesWritten(w.attributes)}
	if w.written.spent != nil {
		changed.Duration = &writtenMinutes{Minutes: w.written.spent.minutes}
	}
	if w.written.day != nil {
		changed.Date = &w.written.day.noon
	}
	body, _ := json.Marshal(changed)
	return body
}

// The type of the body: the id the read before the write resolved, an explicit null where the call takes the
// type away, and nothing at all where it says neither.
func (w workItemRewritten) writtenType() json.RawMessage {
	switch {
	case w.written.clearsType:
		return json.RawMessage("null")
	case w.workType == nil:
		return nil
	}
	encoded, _ := json.Marshal(addressedWorkItem{ID: w.workType.id})
	return encoded
}

func (w changedWorkItem) writtenText() json.RawMessage {
	switch {
	case w.clearsText:
		return json.RawMessage("null")
	case w.text == nil:
		return nil
	}
	encoded, _ := json.Marshal(*w.text)
	return encoded
}

// checked is what the answer to the write is read for beside what the caller asked to print: the parts that
// went out, and nothing else. Neither the id nor the issue is asked for the way a creation asks for them — the
// work item was addressed by a pair the caller wrote, and that is what a refusal names it by.
func (w changedWorkItem) checked() []requestedField {
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

func (w workItemRewritten) checked() []requestedField {
	return checkedWithSettings(w.written.checked(), w.workType, w.attributes)
}

func (w workItemRewritten) confirmedBy(a answer) *diag.Fault {
	return confirmedWorkItem(a, w.written.mismatches(a.objects[0]), w.workType, w.attributes, []render.Pair{
		{Key: issueOwner.String(), Value: render.NewString(w.issue)},
		{Key: idKey, Value: render.NewString(w.at.String())},
	})
}

// mismatches holds the answer against the parts the update sent and against those alone: what the call named
// nothing for is the work item as it stood, and a workflow that moved it is the server's word, not a
// disagreement with a write that said nothing about it.
func (w changedWorkItem) mismatches(item map[string]any) []mismatch {
	var wrong []mismatch
	if w.spent != nil {
		wrong = w.spent.confirmedBy(wrong, item[durationKey])
	}
	if w.day != nil {
		wrong = w.day.confirmedBy(wrong, item[dateKey])
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

// What a write carries before any of it is held against a project: the text of the issue, each part held to
// what YouTrack would keep of it, and the custom fields as the caller addressed them. A nil part is the caller
// who wrote no flag for it: the key is then absent from the body, which is a third thing beside prose and an
// explicit empty (ADR-0001). A creation always carries a title and an update carries the parts it is given.
type writtenIssue struct {
	summary     *string
	description *string
	named       []namedValue
	// The parts --clear names: the prose of the issue, and the custom fields as the caller addressed them. An
	// empty value is a third thing beside prose and no key at all, so it is said outright or not at all.
	clearsProse bool
	cleared     []string
}

// filedIssue is the whole of what a creation writes, read off the flags it was given: a new issue is filed
// with a title and keeps nothing a call does not put there, so there is nothing for it to empty.
func filedIssue(summary string, description *string, filled []string) (writtenIssue, *diag.Fault) {
	if fault := refuseRewrittenText(&summary, description, descriptionOfANewIssue); fault != nil {
		return writtenIssue{}, fault
	}
	named, fault := namedValues(filled)
	if fault != nil {
		return writtenIssue{}, fault
	}
	return writtenIssue{summary: &summary, description: description, named: named}, nil
}

// rewrittenIssue is the whole of what an update writes, read off the flags it was given.
func rewrittenIssue(summary, description *string, filled, cleared []string) (writtenIssue, *diag.Fault) {
	emptied, prose, fault := clearedFields(cleared)
	if fault != nil {
		return writtenIssue{}, fault
	}
	if prose && description != nil {
		return writtenIssue{}, &diag.Fault{Code: diag.BadUsage, Message: proseBothWays}
	}
	if fault := refuseRewrittenText(summary, description, descriptionOfAnIssue); fault != nil {
		return writtenIssue{}, fault
	}
	named, fault := namedValues(filled)
	if fault != nil {
		return writtenIssue{}, fault
	}
	return writtenIssue{summary: summary, description: description, named: named, clearsProse: prose, cleared: emptied}, nil
}

// A custom field as the caller wrote it: the name they addressed it by and the value they gave it.
type namedValue struct {
	name  string
	value string
}

// namedValues reads --field. The split is at the first = of the flag, so a value carrying one of its own goes
// out whole, and the name is taken as it was typed: a project may well call a field something with a space at
// the end of it.
func namedValues(filled []string) ([]namedValue, *diag.Fault) {
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
		if fault := refuseOwnName(flag, name); fault != nil {
			return nil, fault
		}
		named = append(named, namedValue{name: name, value: value})
	}
	return named, nil
}

// clearedFields reads --clear: the custom fields the call empties, and whether it empties the prose of the
// issue, which is no custom field of it. The title is no part a call may empty at all — YouTrack answers a
// write that empties it with Value for summary is required.
func clearedFields(cleared []string) ([]string, bool, *diag.Fault) {
	names := make([]string, 0, len(cleared))
	prose := false
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
			prose = true
		default:
			names = append(names, name)
		}
	}
	return names, prose, nil
}

// The title and the prose of an issue are no custom fields of it, whatever any project may name a field of its
// own: each goes by the flag it goes by wherever an issue is written.
func refuseOwnName(flag, name string) *diag.Fault {
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

// refuseRewrittenText refuses free text YouTrack would store as something other than what was written. The
// server does it in ways that are measured and certain, so the refusal comes before the write rather than as a
// mismatch over an issue that by then exists. emptyProse is why an empty description is refused, which is
// what leaving the flag out would have done instead, and that differs between filing an issue and changing one.
func refuseRewrittenText(summary, description *string, emptyProse string) *diag.Fault {
	if summary != nil {
		if fault := refuseRewritten("--summary", *summary, summaryEmpty, summaryRewrites()); fault != nil {
			return fault
		}
	}
	if description != nil {
		if fault := refuseRewritten("--description", *description, emptyProse, descriptionRewrites()); fault != nil {
			return fault
		}
	}
	return nil
}

// A rune YouTrack does not store as it arrived, against what it stores instead.
type rewrite struct {
	rune rune
	into string
}

// A title is one line: YouTrack turns each line ending of it into a space, a CRLF into two, and drops the NEL
// outright.
func summaryRewrites() []rewrite {
	return []rewrite{
		{rune: '\n', into: "a space"},
		{rune: '\r', into: "a space"},
		{rune: 0x85, into: "nothing at all"},
		{rune: 0x2028, into: "a space"},
		{rune: 0x2029, into: "a space"},
	}
}

// A description keeps every byte but the carriage return: a CRLF is stored as a line feed and a lone CR is
// dropped, so the prose that comes back is shorter than the prose that went out.
func descriptionRewrites() []rewrite {
	return []rewrite{{rune: '\r', into: "nothing at all"}}
}

const (
	summaryEmpty           = "is empty, and YouTrack files no issue without a title"
	descriptionOfANewIssue = "is empty, and YouTrack keeps an empty description as none: leave the flag out to " +
		"file the issue with no description at all"
	descriptionOfAnIssue = "is empty, and YouTrack keeps an empty description as none: --clear description " +
		"empties it outright, and a description the call does not write is left as the issue holds it"
)

func refuseRewritten(flag, text, empty string, rewrites []rewrite) *diag.Fault {
	if text == "" {
		return &diag.Fault{Code: diag.BadUsage, Message: flag + " " + empty}
	}
	if fault := refuseNoUTF8(flag, text); fault != nil {
		return fault
	}
	for _, rewritten := range rewrites {
		if strings.ContainsRune(text, rewritten.rune) {
			return &diag.Fault{Code: diag.BadUsage, Message: rewrittenAs(flag, rewritten)}
		}
	}
	return nil
}

// Free text that is no UTF-8 is refused wherever a caller writes one, empty string or not: the text of a work
// item is kept empty and refused for nothing else, so it stands here rather than under refuseRewritten.
func refuseNoUTF8(flag, text string) *diag.Fault {
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

func rewrittenAs(what string, rewritten rewrite) string {
	return fmt.Sprintf("%s holds U+%04X, which YouTrack stores as %s", what, rewritten.rune, rewritten.into)
}

// The body of a creation. The project is addressed by the id the metadata gave rather than by the code the
// caller typed: the code is how a human writes a project and the id is what the write is tied to.
type createdIssue struct {
	Project      addressedProject     `json:"project"`
	Summary      string               `json:"summary"`
	Description  *string              `json:"description,omitempty"`
	CustomFields []writtenCustomField `json:"customFields,omitempty"`
}

type addressedProject struct {
	ID string `json:"id"`
}

// The body of an update: the parts the call writes and not one key more. The issue is addressed by the path,
// and a part the body says nothing about is a part the issue keeps as it stands.
type updatedIssue struct {
	Summary *string `json:"summary,omitempty"`
	// Raw JSON rather than a string: the prose, an explicit null and no key at all are three things, and a
	// pointer tells only two of them apart.
	Description  json.RawMessage      `json:"description,omitempty"`
	CustomFields []writtenCustomField `json:"customFields,omitempty"`
}

// One custom field of the body. The field is addressed by the name it goes by rather than by the id of its
// binding: a localized name or one of another letter case answers 500 (ADR-0002). The $type is the one thing
// the server takes no field without, although the specification marks all three read-only.
type writtenCustomField struct {
	Type  string `json:"$type"`
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// A write held against the project it goes to: the text of the issue, and the custom fields it fills, each
// standing where the project puts it. A creation and an update differ in the body they become and in nothing
// above it: the same names are resolved against the same project and the same answer is held against the same
// values.
type write struct {
	text    writtenIssue
	fields  []writtenField
	project projectMetadata
}

// A custom field a write fills: the field as the project has it, the values as the caller wrote them, the same
// values as the body carries them, and the identities the answer is held against.
type writtenField struct {
	field   projectField
	kind    fieldType
	values  []string
	sent    []any
	checked []string
	// Whether --clear named the field, which is the call writing an empty value into it rather than any value
	// at all: values and checked are then empty, and the answer is held to holding nothing.
	cleared bool
}

// Marshalling strings, maps of them and structs of both cannot fail.
func (w write) body() []byte {
	body, _ := json.Marshal(createdIssue{
		Project:      addressedProject{ID: w.project.id},
		Summary:      *w.text.summary,
		Description:  w.text.description,
		CustomFields: w.elements(),
	})
	return body
}

func (w write) changes() []byte {
	body, _ := json.Marshal(updatedIssue{
		Summary:      w.text.summary,
		Description:  w.text.prose(),
		CustomFields: w.elements(),
	})
	return body
}

// prose is the description of the body: the text where the call writes one, an explicit null where it clears
// one, and nothing at all where it says neither.
func (w writtenIssue) prose() json.RawMessage {
	switch {
	case w.clearsProse:
		return json.RawMessage("null")
	case w.description == nil:
		return nil
	}
	encoded, _ := json.Marshal(*w.description)
	return encoded
}

func (w write) elements() []writtenCustomField {
	fields := make([]writtenCustomField, 0, len(w.fields))
	for _, field := range w.fields {
		fields = append(fields, field.element())
	}
	return fields
}

// An empty value is written the way the type holds one: null where the field holds one value, and an empty
// list where it holds several, which a null there is answered Field value cannot be null for. A field
// that holds one value is given one value or emptied, so nothing to send is the emptying of it.
func (f writtenField) element() writtenCustomField {
	var value any
	switch {
	case f.kind.isMultiValue:
		value = f.sent
	case len(f.sent) > 0:
		value = f.sent[0]
	}
	return writtenCustomField{Type: f.kind.sent, Name: f.field.naming.name, Value: value}
}

// checked is what the answer to the write is read for beside what the caller asked to print: every value that
// went out, so the check has it to compare, and the readable id, so a refusal can name the issue that by then
// exists whatever the caller asked for.
func (w write) checked() []requestedField {
	own := []requestedField{{name: idReadableKey}}
	if w.text.summary != nil {
		own = append(own, requestedField{name: summaryKey})
	}
	if w.text.description != nil || w.text.clearsProse {
		own = append(own, requestedField{name: descriptionKey})
	}
	if len(w.fields) > 0 {
		own = append(own, requestedField{name: customFieldsKey})
	}
	return own
}

// confirmedBy holds the answer against what the write sent: a 200 says the server took the body, not that what
// it kept is what went out, and printing the answer unchecked would hand a rewritten value back as the
// caller's own.
func (w write) confirmedBy(a answer) *diag.Fault {
	issue := a.objects[0]
	var wrong []mismatch
	if w.text.summary != nil {
		wrong = textMismatch(wrong, summaryKey, *w.text.summary, issue[summaryKey])
	}
	switch {
	case w.text.description != nil:
		wrong = textMismatch(wrong, descriptionKey, *w.text.description, issue[descriptionKey])
	case w.text.clearsProse:
		wrong = emptyMismatch(wrong, descriptionKey, issue[descriptionKey])
	}
	wrong, fault := w.fieldsConfirmedBy(a, wrong)
	switch {
	case fault != nil:
		return fault
	case len(wrong) == 0:
		return nil
	}
	return rewrittenByTheServer(a, knownAs(issueOwner.String(), writtenID(a, idReadableKey)), wrong)
}

// The custom fields come back as a block of their own, so they are read the way the document reads them and
// held against what went out value by value. A field the answer does not carry at all is a field the write
// did not reach, which is the same disagreement as a value that came back another.
func (w write) fieldsConfirmedBy(a answer, wrong []mismatch) ([]mismatch, *diag.Fault) {
	if len(w.fields) == 0 {
		return wrong, nil
	}
	n := nodes{answer: a}
	arrived, fault := n.readCustomFields(a.objects[0][customFieldsKey])
	if fault != nil {
		return nil, fault
	}
	held := make(map[string]issueCustomField, len(arrived))
	for _, field := range arrived {
		held[field.name] = field
	}
	for _, written := range w.fields {
		name := written.field.naming.name
		field, onTheIssue := held[name]
		if !onTheIssue {
			// A field the answer carries nowhere is a field the issue holds nothing in, which is what a call
			// that emptied it asked for and what a call that filled it did not get.
			if written.cleared {
				continue
			}
			wrong = append(wrong, mismatch{field: name, written: written.node(), arrived: render.NewNull()})
			continue
		}
		texts, fault := n.identityTexts(field)
		if fault != nil {
			return nil, fault
		}
		if sameValues(written.kind, written.checked, texts) {
			continue
		}
		wrong = append(wrong, mismatch{field: name, written: written.node(), arrived: valueNode(texts, written.kind)})
	}
	return wrong, nil
}

// Two sets of values name the same thing where each names every one of the other: the server answers in the
// order of the bundle rather than in the order the values were written and keeps one value of a value written
// twice, so neither the order nor the count of them is held to anything.
func sameValues(kind fieldType, sent, arrived []string) bool {
	return covers(kind, sent, arrived) && covers(kind, arrived, sent)
}

func covers(kind fieldType, all, some []string) bool {
	for _, value := range some {
		if !slices.ContainsFunc(all, func(held string) bool { return kind.sameValue(held, value) }) {
			return false
		}
	}
	return true
}

func (f writtenField) node() *render.Node {
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
	field   string
	written *render.Node
	arrived *render.Node
}

// A name the answer carries as something other than text — a null, a number, or nothing at all — is a value
// that is not what was written, whatever else it is.
func textMismatch(wrong []mismatch, field, sent string, value any) []mismatch {
	if arrived, isText := value.(string); isText && arrived == sent {
		return wrong
	}
	return append(wrong, mismatch{field: field, written: render.NewString(sent), arrived: asArrived(value)})
}

// A part the call emptied is a part the answer holds nothing in. Anything still standing there is the write
// disagreeing with itself as much as a value that came back another.
func emptyMismatch(wrong []mismatch, field string, value any) []mismatch {
	if value == nil {
		return wrong
	}
	return append(wrong, mismatch{field: field, written: render.NewNull(), arrived: asArrived(value)})
}

// Размер считается по потоку, поэтому файл, изменившийся при чтении, ошибкой не считается.
func sizeMismatch(wrong []mismatch, sent int64, value any) []mismatch {
	if arrived, isNumber := wholeNumber(value); isNumber && arrived == sent {
		return wrong
	}
	written := render.NewNumber(json.Number(strconv.FormatInt(sent, 10)))
	return append(wrong, mismatch{field: sizeKey, written: written, arrived: asArrived(value)})
}

// What the write left behind is the server's word by now, so the refusal names the entity it wrote and each
// value both ways rather than sending anything else to find out. identity is how the entity is addressed:
// what the answer brought back where the write is what brought it into being, what the caller wrote where it
// stood there already, and both an owner and a child where one alone names nothing.
func rewrittenByTheServer(a answer, identity []render.Pair, wrong []mismatch) *diag.Fault {
	entries := make([]*render.Node, 0, len(wrong))
	for _, m := range wrong {
		entries = append(entries, render.NewMap(
			render.Pair{Key: "field", Value: render.NewString(m.field)},
			render.Pair{Key: "written", Value: m.written},
			render.Pair{Key: "arrived", Value: m.arrived}))
	}
	details := append([]render.Pair{requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted())},
		append(identity, render.Pair{Key: "mismatch", Value: render.NewList(entries...)})...)
	message := "the write went through and the values under mismatch came back as something other than what was written"
	return &diag.Fault{Code: diag.UpstreamLied, Message: message, Details: details}
}

// knownAs is the one key an entity with an address of its own is named by.
func knownAs(named string, id *render.Node) []render.Pair {
	return []render.Pair{{Key: named, Value: id}}
}

func writtenID(a answer, name string) *render.Node {
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
	id      string
	code    string
	fields  []projectField
	arrived answer
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
			namingFields(),
		}},
	}
}

func (c *Client) projectToWrite(ctx context.Context, spec *schemas, code string) (projectMetadata, *diag.Fault) {
	a, fault := c.passing(ctx, spec, projectSchema, writeMetadataFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getProject(ctx, code, fields)
	})
	if fault != nil {
		return projectMetadata{}, fault
	}
	return readWriteMetadata(a, a.objects[0])
}

// The issue an update writes, as the one read before it sees it: the id the write is addressed by, the project
// its names are resolved against, and the class the server names each field the issue already holds by.
type issueToWrite struct {
	readable addressed
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

func (c *Client) readIssueToWrite(ctx context.Context, spec *schemas, id string) (issueToWrite, *diag.Fault) {
	a, fault := c.passing(ctx, spec, issueSchema, issueToWriteFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return issueToWrite{}, fault
	}
	readable, fault := addressedIn(a, a.objects[0], issueOwner, "an update")
	if fault != nil {
		return issueToWrite{}, fault
	}
	held, isObject := a.objects[0]["project"].(map[string]any)
	if !isObject {
		return issueToWrite{}, shapeFailure(a.response, a.body, "the project of the issue is not a JSON object")
	}
	project, fault := readWriteMetadata(a, held)
	if fault != nil {
		return issueToWrite{}, fault
	}
	kinds, fault := readIssueKinds(a)
	if fault != nil {
		return issueToWrite{}, fault
	}
	return issueToWrite{readable: readable, project: project, kinds: kinds}, nil
}

// The judgment of names says $type arrived, not that it is text, so what goes straight into the body is held
// to its shape here.
func readIssueKinds(a answer) (map[string]string, *diag.Fault) {
	items, isList := a.objects[0][customFieldsKey].([]any)
	if !isList {
		return nil, shapeFailure(a.response, a.body, "the custom fields of the issue are not a JSON array")
	}
	kinds := make(map[string]string, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.response, a.body, "a custom field of the issue is not a JSON object")
		}
		name, isNamed := object[nameKey].(string)
		kind, isText := object["$type"].(string)
		if !isNamed || !isText {
			return nil, shapeFailure(a.response, a.body, brokenIssueField)
		}
		place, isObject := object["projectCustomField"].(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.response, a.body, brokenBinding(name))
		}
		binding, isText := place[idKey].(string)
		if !isText {
			return nil, shapeFailure(a.response, a.body, brokenBinding(name))
		}
		kinds[binding] = kind
	}
	return kinds, nil
}

const brokenIssueField = "the name or the class of a custom field of the issue is not text"

const brokenProject = "the id or the short name of the project is not text"

// The judgment of names says a member arrived, not what it holds, so everything read out of the metadata is
// held to its shape here. project is where it stood in the answer: the answer itself where a creation read it
// off the project, and the project of the issue where an update read it off the issue.
func readWriteMetadata(a answer, project map[string]any) (projectMetadata, *diag.Fault) {
	id, isText := project[idKey].(string)
	code, isName := project["shortName"].(string)
	if !isText || !isName {
		return projectMetadata{}, shapeFailure(a.response, a.body, brokenProject)
	}
	items, isList := project[customFieldsKey].([]any)
	if !isList {
		return projectMetadata{}, shapeFailure(a.response, a.body, "the custom fields of the project are not a JSON array")
	}
	fields := make([]projectField, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return projectMetadata{}, shapeFailure(a.response, a.body, brokenField)
		}
		field, ok := readProjectField(object)
		if !ok {
			return projectMetadata{}, shapeFailure(a.response, a.body, brokenNaming)
		}
		fields = append(fields, field)
	}
	return projectMetadata{id: id, code: code, fields: fields, arrived: a}, nil
}

func readProjectField(object map[string]any) (projectField, bool) {
	id, isText := object[idKey].(string)
	named, isNamed := readNaming(object)
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
	field := customField{id: id, naming: named}
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

// filling is the write with every name of it resolved against the project the issue is filed in, which is the
// whole of what a name is held against: nothing of a name ever reaches the server, since YouTrack answers an
// unknown field name with a 500 (ADR-0002). named is the class the server itself gave each field the issue
// already holds, by the id of its binding, and nothing at all where the issue does not exist yet.
func (w writtenIssue) filling(project projectMetadata, named map[string]string) (write, *diag.Fault) {
	fields, fault := project.filled(w.named, w.cleared, named)
	if fault != nil {
		return write{}, fault
	}
	return write{text: w, fields: fields, project: project}, nil
}

// filled is the custom fields the call writes, in the order the project puts its fields in, so that a body
// says nothing about the order the flags were written in; the values of one field keep that order, since the
// server keeps what it is given.
//
// Every name that resolves to no field of the project is refused at once, and so is every value the call
// cannot send: a caller fixing one flag per attempt would read the project as many times over.
func (p projectMetadata) filled(named []namedValue, cleared []string, kinds map[string]string) ([]writtenField, *diag.Fault) {
	catalogue := make([]naming, 0, len(p.fields))
	for _, field := range p.fields {
		catalogue = append(catalogue, field.naming)
	}
	given := make([][]string, len(p.fields))
	emptied := make([]bool, len(p.fields))
	var unknown, ambiguous []*render.Node
	// A name that resolves to nothing stands in the refusal once, however many flags wrote it: a field that
	// holds several values takes --field once per value, so a misspelling there would be listed once per value.
	listed := make(map[string]bool, len(named)+len(cleared))
	place := func(name string) (int, bool) {
		places := answering(name, catalogue)
		switch {
		case len(places) == 1:
			return places[0], true
		case listed[name]:
		case len(places) == 0:
			unknown = append(unknown, unknownEntry(name, nearestNamed(name, catalogue)))
		default:
			ambiguous = append(ambiguous, ambiguousEntry(name, canonical(standingAt(catalogue, places))))
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
		return nil, p.refusing(diag.UnknownName, unknownMessage, "unknown", unknown)
	case len(ambiguous) > 0:
		return nil, p.refusing(diag.UnknownName, ambiguousMessage, "ambiguous", ambiguous)
	}
	return p.sendable(given, emptied, kinds)
}

func (p projectMetadata) sendable(given [][]string, emptied []bool, kinds map[string]string) ([]writtenField, *diag.Fault) {
	fields := make([]writtenField, 0, len(p.fields))
	var invalid []*render.Node
	for at, values := range given {
		if len(values) == 0 && !emptied[at] {
			continue
		}
		field := p.fields[at]
		kind, modelled := typeOf(field.naming)
		if !modelled {
			return nil, unmodelledType(field.naming, p.arrived)
		}
		// The class of a field the issue already carries is copied off the server word for word: it knows of a
		// StateMachineIssueCustomField, which no table of ytrack's can, and where it has said which class a
		// field is of, a class of ytrack's own would be a guess made beside an answer.
		if sent, onTheIssue := kinds[field.id]; onTheIssue {
			kind.sent = sent
		}
		written := writtenField{field: field, kind: kind, values: values, cleared: emptied[at]}
		switch {
		case emptied[at] && len(values) > 0:
			invalid = append(invalid, invalidEntry(field.naming.name, values[0], writtenAndEmptied))
			continue
		case emptied[at]:
			// An empty list rather than the nil one, which marshals as the null a multi-valued field is not
			// given; a field that holds one value carries no list at all.
			written.sent = []any{}
			fields = append(fields, written)
			continue
		case !kind.isMultiValue && len(values) > 1:
			reason := fmt.Sprintf("the custom field holds one value by its type, and the call gives it %d", len(values))
			invalid = append(invalid, invalidEntry(field.naming.name, values[1], reason))
			continue
		}
		for _, value := range values {
			sent, reason := kind.writtenValue(value)
			if reason != "" {
				invalid = append(invalid, invalidEntry(field.naming.name, value, reason))
				continue
			}
			written.sent = append(written.sent, sent.body)
			written.checked = append(written.checked, sent.identity)
		}
		fields = append(fields, written)
	}
	if len(invalid) > 0 {
		return nil, p.refusing(diag.BadUsage, invalidMessage, "invalid", invalid)
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
func (w write) missing() []string {
	var missing []string
	for _, field := range w.project.fields {
		if field.canBeEmpty || len(field.defaults) > 0 || w.fills(field) {
			continue
		}
		if _, hidden := w.hides(field); hidden {
			continue
		}
		missing = append(missing, field.naming.name)
	}
	return missing
}

// requiredEmptied is every custom field the call empties that the project lets no issue stand without, in the
// order the project puts its fields in and all of them at once, so that a caller is not answered one field per
// attempt.
func (w write) requiredEmptied() []string {
	var required []string
	for _, written := range w.fields {
		if written.cleared && !written.field.canBeEmpty {
			required = append(required, written.field.naming.name)
		}
	}
	return required
}

func (w write) fills(field projectField) bool {
	return slices.ContainsFunc(w.fields, func(written writtenField) bool { return written.field.id == field.id })
}

const (
	missingMessage   = "the custom fields under missing are required by the project and the call fills none of them"
	emptiedMessage   = "the custom fields under missing are required by the project and the call empties them"
	unknownMessage   = "the names under unknown are not custom fields of the project"
	ambiguousMessage = "the names under ambiguous are the names of more than one custom field of the project each"
	invalidMessage   = "the values under invalid are not values the fields they name can be given, and nothing was sent"
	hiddenMessage    = "the custom fields under invalid do not stand on the issue the call would file, and " +
		"nothing was sent"
	writtenAndEmptied = "the call writes a value into the custom field and empties it both, and one write " +
		"leaves it one way"
)

// A refusal the metadata of the project settled names that read as the request that was sent, since it is the
// only one that went out, and the project the names were held against.
func (p projectMetadata) refusing(code diag.Code, message, key string, entries []*render.Node) *diag.Fault {
	a := p.arrived
	details := []render.Pair{
		requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted()),
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

// hides is why the condition of the field keeps it off the issue this body files, and nothing where the field
// stands on it. YouTrack throws a field its condition hides out of a creation under a 200 and says nothing of
// it, so a field hidden here is neither sent nor required of the caller.
//
// It is held against the body alone, which is the whole of what a new issue holds. An update evaluates no
// condition: the issue it writes holds values the read before it never asked for, and the server itself
// refuses a write that would leave a field hidden.
//
// A condition of another kind, one watching a field of several values and one watching a field the project no
// longer has are not evaluated: what they hide is then the server's to say.
func (w write) hides(field projectField) (string, bool) {
	c := field.condition
	if !c.given || c.kind != fieldBasedCondition || c.controls == "" {
		return "", false
	}
	watched, found := fieldByID(w.project.fields, c.controls)
	if !found || watched.naming.isMultiValue {
		return "", false
	}
	held, filled := w.holds(watched)
	switch {
	case !filled:
		if c.showForNullValue {
			return "", false
		}
	case slices.ContainsFunc(c.values, func(name string) bool { return strings.EqualFold(name, held) }):
		return "", false
	}
	return hiddenBy(watched.naming.name, c, held, filled), true
}

// hidden is every custom field the call fills that a condition keeps off the issue the body files, in the
// order the project puts its fields in and all of them at once.
func (w write) hidden() []hiddenField {
	var hidden []hiddenField
	for _, written := range w.fields {
		reason, kept := w.hides(written.field)
		if !kept || len(written.values) == 0 {
			continue
		}
		hidden = append(hidden, hiddenField{name: written.field.naming.name, value: written.values[0], reason: reason})
	}
	return hidden
}

// A custom field the call fills that the issue would not hold, as the refusal names it.
type hiddenField struct {
	name   string
	value  string
	reason string
}

// What the issue this body files holds in the field: the value the call writes into it, and the value the
// project fills it with unasked where the call writes none.
func (w write) holds(field projectField) (string, bool) {
	for _, written := range w.fields {
		if written.field.id == field.id && len(written.values) > 0 {
			return written.values[0], true
		}
	}
	return field.filled()
}

// What a field holds on an issue the write does not name it on: the value the project fills it with unasked,
// and nothing where the project fills it with none.
func (f projectField) filled() (string, bool) {
	if len(f.defaults) == 0 {
		return "", false
	}
	return f.defaults[0], true
}

func hiddenBy(controls string, c fieldCondition, held string, filled bool) string {
	return fmt.Sprintf("the project shows the custom field on an issue whose %s holds %s, the issue this call "+
		"files holds %s in it, and YouTrack would file the issue without the value under a 200",
		controls, c.shownAt(), heldValue(held, filled))
}

// shownAt is the values a condition shows its field at, read as a caller reads them: a condition with no value
// to show its field at and no null to show it for hides it from every issue there is.
func (c fieldCondition) shownAt() string {
	shown := make([]string, 0, len(c.values)+1)
	for _, name := range c.values {
		shown = append(shown, render.Quote(name))
	}
	if c.showForNullValue {
		shown = append(shown, nothingHeld)
	}
	if len(shown) == 0 {
		return "no value at all"
	}
	return strings.Join(shown, " or ")
}

func heldValue(held string, filled bool) string {
	if !filled {
		return nothingHeld
	}
	return render.Quote(held)
}

const nothingHeld = "nothing at all"

func fieldByID(fields []projectField, id string) (projectField, bool) {
	for _, field := range fields {
		if field.id == id {
			return field, true
		}
	}
	return projectField{}, false
}
