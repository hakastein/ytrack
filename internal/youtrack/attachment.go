package youtrack

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// What an attachment holds where the caller writes no expression of their own: which attachment it is, which
// the deletion goes by; what the file is called; how large it is and what kind of file it is, which is what
// settles whether it is worth fetching at all; and the link it is fetched by. A name is no identity here — an
// issue can carry the same name many times over.
//
// Every key of it arrives under a 200 for a member's token as well as an admin's, so no key of the default
// costs a reader their document.
const AttachmentListFields = "id,name,size,mimeType,url"

const attachmentsPlural = "attachments"

const (
	sizeKey = "size"
	// The key the id of an attachment stands under in a refusal about it, which is what the caller calls the
	// thing the command wrote.
	attachmentKey = "attachment"
	// The name YouTrack declares the one part of the form under. The server takes any name — files, file and
	// files[1] were each answered 200 — and this is the one the specification writes.
	filePart = "files[0]"
)

// attachmentTarget is the one kind of entity attachments hang from: which schema an attachment of it stands at, which
// the server names in $type on every object it sends; the name of the property that attachment holds its owner
// under; and which of the two kinds it is.
//
// Both names are the specification's own — IssueAttachment.issue and ArticleAttachment.article. That the
// second reads like the word a refusal names the owner by is a coincidence of the API's naming and nothing to
// lean on: ownerKind.String() is text for a reader, and no request is built out of it, so rewording a refusal
// leaves every request as it was.
type attachmentTarget struct {
	schema string
	// The name the owner hangs under, which is what goes out in fields= and what the answer is read by.
	owner string
	kind  ownerKind
}

func issueAttachmentTarget() attachmentTarget {
	return attachmentTarget{schema: issueAttachmentSchema, owner: "issue", kind: issueOwner}
}

func articleAttachmentTarget() attachmentTarget {
	return attachmentTarget{schema: articleAttachmentSchema, owner: "article", kind: articleOwner}
}

// attachmentTargetOf is the machinery of the kind of owner a command was given, which is the one place the two
// kinds are told apart: the schema an answer stands at and the name the owner arrives under both follow from
// it rather than from a second reading of the id.
func attachmentTargetOf(kind ownerKind) attachmentTarget {
	if kind == articleOwner {
		return articleAttachmentTarget()
	}
	return issueAttachmentTarget()
}

// The schema of an answer carrying several of them, which is what a page of a list and what one write files
// both arrive as.
func (h attachmentTarget) listSchema() string {
	return "[]" + h.schema
}

// ListAttachments is the call for one page of the attachments of the issue or the article of that readable id,
// with the fields of expression, or with them added to AttachmentListFields when it starts with +; nil is the
// caller leaning on the default whole.
func ListAttachments(id string, expression *string, page Page) (Call, *diag.Fault) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	requested, fault := attachmentFields(expression)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listAttachments(ctx, spec, at, requested, page)
	}, nil
}

// The attachments of an owner are one collection and the subresource answers it in one request, so the
// machinery of every other list of the tool holds here unchanged: the page goes out with $top, and the whole
// is read off a second pass over ids alone where the page fills the limit. Neither an issue nor an article
// carries a counter of its attachments — commentsCount has no counterpart — so there is nothing else to count
// them by.
func (c *Client) listAttachments(ctx context.Context, spec *schemas, at owner, requested []requestedField, page Page) (*render.Node, *diag.Fault) {
	return c.listPage(ctx, spec, attachmentsPlural, attachmentTargetOf(at.kind).listSchema(), requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.getAttachments(ctx, at, fields, w)
	})
}

// Which API the list goes to is settled by the kind of its owner and by nothing else: each kind has one
// operation of its own, and there is no branch that sends a request for an owner of neither kind — parseOwner
// left none.
func (c *Client) getAttachments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	if at.kind == articleOwner {
		return c.apiGetArticleAttachments(ctx, at, fields, w)
	}
	return c.apiGetIssueAttachments(ctx, at, fields, w)
}

// attachmentFields is an expression of a command that prints attachments. A nil expression is the caller
// leaning on the default whole, and then nothing in the tree is theirs to answer for.
func attachmentFields(expression *string) ([]requestedField, *diag.Fault) {
	if expression == nil {
		return parseDefault(AttachmentListFields, false)
	}
	return parseFields(*expression, AttachmentListFields)
}

// AttachedFile is the file a creation sends: the name it would be attached under, which is the last element
// of the path the caller wrote, and the bytes of it, which are read as the request goes out.
type AttachedFile struct {
	Name string
	Body io.ReadCloser
}

// FileOpener is how a creation is handed its file. It runs after the owner has been read and never before, so a
// call that names neither an owner ytrack can address nor a file anyone could open is answered about the
// owner; and it keeps every path of the caller's own filesystem out of this package.
type FileOpener func() (AttachedFile, *diag.Fault)

// CreateAttachment is the call that attaches the file open gives to the issue or the article of that readable
// id, and prints the attachment as the server kept it, with the fields of expression, or with them added to
// AttachmentListFields when it starts with +; nil is the caller leaning on the default whole.
func CreateAttachment(id string, open FileOpener, expression *string) (Call, *diag.Fault) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	file, fault := open()
	if fault != nil {
		return nil, fault
	}
	sent, fault := newUpload(file)
	if fault != nil {
		return nil, fault
	}
	requested, fault := attachmentFields(expression)
	if fault != nil {
		_ = sent.body.Close()
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.createAttachment(ctx, spec, at, sent, requested)
	}, nil
}

// One POST is the whole command. The owner is not read first: an owner the instance has none of, and one the
// token may not see, are both answered 404 by the server itself. The price is that the bytes of a large
// file may go out before that 404 does.
func (c *Client) createAttachment(ctx context.Context, spec *schemas, at owner, sent *upload, requested []requestedField) (*render.Node, *diag.Fault) {
	body, contentType, confirmed := sent.form()
	return c.write(ctx, spec, attachmentTargetOf(at.kind).listSchema(), withFields(requested, sent.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateAttachment(ctx, at, contentType, body, fields)
	}, confirmed, writeResultNode(requested))
}

// Which API a file goes to is settled by the kind of its owner and by nothing else: each kind has one
// operation of its own, and there is no branch that sends a request for an owner of neither kind — parseOwner
// left none.
func (c *Client) apiCreateAttachment(ctx context.Context, at owner, contentType string, body io.Reader, fields string) (*http.Response, error) {
	if at.kind == articleOwner {
		return c.apiCreateArticleAttachment(ctx, at, contentType, body, fields)
	}
	return c.apiCreateIssueAttachment(ctx, at, contentType, body, fields)
}

// DeleteAttachment is the call that takes the attachment of that id off the issue or the article of that
// readable id for good, and prints the attachment as the read before the deletion found it.
func DeleteAttachment(id, attachment string) (Call, *diag.Fault) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	file, fault := parseChildID(attachmentKey, "the issue or the article", attachment)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.deleteAttachment(ctx, spec, at, file)
	}, nil
}

// The read before the write is what makes the deletion answerable for what it says: a DELETE is answered
// with nothing, an attachment the owner has none of is a not_found here with nothing destroyed, and the owner
// the server names is both what the path of the DELETE is built from and what the document prints — the
// argument was never checked, and dev-7 and DEV-7 reach the same issue.
func (c *Client) deleteAttachment(ctx context.Context, spec *schemas, at owner, file childID) (*render.Node, *diag.Fault) {
	target := attachmentTargetOf(at.kind)
	requested := []requestedField{
		{name: idKey},
		{name: nameKey},
		{name: target.owner, children: []requestedField{{name: idReadableKey}}},
	}
	found, fault := c.request(ctx, spec, target.schema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getAttachment(ctx, at, file, fields)
	})
	if fault != nil {
		return nil, fault
	}
	theOwner, fault := attachmentOwner(found, target, file)
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiDeleteAttachment(ctx, at.kind, theOwner, file)
	}); fault != nil {
		return nil, fault
	}
	return objectNode(found, requested, found.objects[0], nil)
}

// attachmentOwner is the entity the deletion goes through, read off the answer rather than off the argument. The
// attachment is held to being the one that was asked for: an answer about another id would be printed as the
// caller's own and destroyed under it, and nothing else in the call would catch that.
func attachmentOwner(a decodedResponse, target attachmentTarget, file childID) (readableID, *diag.Fault) {
	received, isText := a.objects[0][idKey].(string)
	if !isText || received != file.id {
		message := fmt.Sprintf("the %s asked for under id %s arrived under another id", attachmentKey, render.Quote(file.String()))
		return readableID{}, shapeFailure(a.httpResponse, a.body, message)
	}
	holder, isObject := a.objects[0][target.owner].(map[string]any)
	if !isObject {
		message := fmt.Sprintf("the %s the %s hangs from arrived as something other than an object",
			target.kind, attachmentKey)
		return readableID{}, shapeFailure(a.httpResponse, a.body, message)
	}
	return readableIDAt(a, holder, target.kind, "a deletion")
}

// Which API the read goes to is settled by the kind of the owner and by nothing else, as it is for every other
// call about attachments.
func (c *Client) getAttachment(ctx context.Context, at owner, file childID, fields string) (*http.Response, error) {
	if at.kind == articleOwner {
		return c.apiGetArticleAttachment(ctx, at, file, fields)
	}
	return c.apiGetIssueAttachment(ctx, at, file, fields)
}

// The same rule for the deletion, which goes to the owner the read named rather than to the one the caller
// wrote.
func (c *Client) apiDeleteAttachment(ctx context.Context, kind ownerKind, at readableID, file childID) (*http.Response, error) {
	if kind == articleOwner {
		return c.apiDeleteArticleAttachment(ctx, at, file)
	}
	return c.apiDeleteIssueAttachment(ctx, at, file)
}

// upload is the file as the request carries it: the name the part is written under and the bytes of it. How
// many of those went out is no part of it — that count belongs to the one pass that wrote them, and form hands
// it back as the check it is read by, so there is no order to remember here and no way to write it the other
// way round.
type upload struct {
	name string
	body io.ReadCloser
}

// newUpload is the file held to the name YouTrack would keep it under, with the file closed and nothing sent
// where that name is one the server would rewrite.
func newUpload(file AttachedFile) (*upload, *diag.Fault) {
	if fault := checkFileName(file.Name); fault != nil {
		_ = file.Body.Close()
		return nil, fault
	}
	return &upload{name: file.Name, body: file.Body}, nil
}

func (u *upload) form() (io.ReadCloser, string, func(decodedResponse) *diag.Fault) {
	reading, writing := io.Pipe()
	form := multipart.NewWriter(writing)
	// Written by the goroutine that fills the pipe and read once the answer is in hand, which are two
	// goroutines, so the count is atomic rather than plain.
	var sent atomic.Int64
	go func() {
		defer u.body.Close()
		part, err := form.CreateFormFile(filePart, u.name)
		if err != nil {
			_ = writing.CloseWithError(err)
			return
		}
		written, err := io.Copy(part, u.body)
		sent.Store(written)
		if err != nil {
			_ = writing.CloseWithError(err)
			return
		}
		_ = writing.CloseWithError(form.Close())
	}()
	confirmed := func(a decodedResponse) *diag.Fault {
		return u.verify(a, sent.Load())
	}
	return reading, form.FormDataContentType(), confirmed
}

// verifyFields is what the answer to the write is read for beside what the caller asked to print: the name the file
// went out under and the count of bytes that went with it, which are the two the server is held to.
func (u *upload) verifyFields() []requestedField {
	return []requestedField{{name: nameKey}, {name: sizeKey}}
}

func (u *upload) verify(a decodedResponse, sent int64) *diag.Fault {
	if len(a.objects) != 1 {
		return ambiguousAttachmentFault(a)
	}
	filed := a.objects[0]
	wrong := textMismatch(nil, nameKey, u.name, filed[nameKey])
	wrong = sizeMismatch(wrong, sent, filed[sizeKey])
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, knownAs(attachmentKey, responseID(a, idKey)), wrong)
}

// The file went out whole and the answer names no one attachment it became, so there is nothing to print and
// nothing to say about what the instance now holds; attachment list is where that is read.
func ambiguousAttachmentFault(a decodedResponse) *diag.Fault {
	details := []render.Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: "actual_count", Value: intNode(len(a.objects))},
		bodyDetail(a.body),
	}
	message := "one file was sent and the answer carries something other than the one attachment it was filed as"
	return &diag.Fault{Code: diag.UpstreamInvalid, Message: message, Details: details}
}

// The most a rune at either end of a name may be for YouTrack to keep it: the server trims a name the way
// Java's String.trim does, by the code unit rather than by what Unicode calls a space, so U+00A0 stays and a
// tab goes.
const maxTrimmedRune = ' '

// checkFileName holds the name of the file to what YouTrack would keep it under. Every rule here is a
// rewrite the server makes without a word, after the file is stored: a check afterwards would report a
// mismatch over an attachment that by then exists, so the refusal stands before anything is sent.
//
// A slash cannot reach here — the name is the last element of a path — and everything not named below goes as
// it was written, spaces, semicolons, tabs inside and all.
func checkFileName(name string) *diag.Fault {
	because, rewritten := rewrittenName(name)
	if !rewritten {
		return nil
	}
	message := fmt.Sprintf("the file would be attached as %s, and %s. Attach a copy of it made under another name",
		render.Quote(name), because)
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func rewrittenName(name string) (because string, rewritten bool) {
	switch {
	case !utf8.ValidString(name):
		return "the bytes of that name are no valid UTF-8: YouTrack would keep each byte it cannot read as U+FFFD", true
	case strings.Contains(name, `\`):
		return `it holds a backslash, and YouTrack keeps only what stands after the last one`, true
	case strings.Contains(name, `"`):
		// The writer escapes a quote with a backslash, as RFC 7578 has no other way to carry one, and the
		// server then reads that backslash as a separator of its own.
		return `it holds a double quote, which the form carries escaped with a backslash, and YouTrack keeps only what stands after that backslash`, true
	case strings.ContainsAny(name, "\r\n"):
		return "it holds a carriage return or a line feed, which the form carries as %0D or %0A, and YouTrack keeps those four characters", true
	}
	first, _ := utf8.DecodeRuneInString(name)
	last, _ := utf8.DecodeLastRuneInString(name)
	if first <= maxTrimmedRune || last <= maxTrimmedRune {
		return fmt.Sprintf("it begins or ends with a character no greater than U+%04X, which YouTrack trims away", maxTrimmedRune), true
	}
	return "", false
}
