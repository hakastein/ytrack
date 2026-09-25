package youtrack

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/hakastein/ytrack/internal/ytapi"
)

// The only file allowed to import internal/ytapi. That package holds no hand-written
// line, so the directive that generates it lives here.
//go:generate go -C ../.. tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.json

func (c *Client) api() *ytapi.Client {
	return &ytapi.Client{
		// A generated request resolves ./admin/... against Server, which therefore ends in a slash.
		Server:         c.address.JoinPath("api/").String(),
		Client:         c.httpClient,
		RequestEditors: []ytapi.RequestEditorFn{c.authorize},
	}
}

func (c *Client) apiGetProject(ctx context.Context, code, fields string) (*http.Response, error) {
	return c.api().GetProject(ctx, code, &ytapi.GetProjectParams{Fields: &fields})
}

func (c *Client) apiGetProjects(ctx context.Context, fields string, w window) (*http.Response, error) {
	return c.api().GetProjects(ctx, &ytapi.GetProjectsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiGetProjectCustomFields(ctx context.Context, code, fields string, top int32) (*http.Response, error) {
	return c.api().GetProjectCustomFields(ctx, code, &ytapi.GetProjectCustomFieldsParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetProjectCustomField(ctx context.Context, code, id, fields string) (*http.Response, error) {
	return c.api().GetProjectCustomField(ctx, code, id, &ytapi.GetProjectCustomFieldParams{Fields: &fields})
}

func (c *Client) apiGetCustomFields(ctx context.Context, fields string, top int32) (*http.Response, error) {
	return c.api().GetCustomFields(ctx, &ytapi.GetCustomFieldsParams{Fields: &fields, Top: &top})
}

// named is the custom fields the answer is cut down to, one query parameter each; none of them leaves the
// whole block alone.
func (c *Client) apiGetIssue(ctx context.Context, id, fields string, named []string) (*http.Response, error) {
	params := ytapi.GetIssueParams{Fields: &fields}
	if len(named) > 0 {
		params.CustomFields = &named
	}
	return c.api().GetIssue(ctx, id, &params)
}

// A write: it files an issue and answers with the issue it filed, so fields go out with it. Without them the
// answer is {id, $type} alone, and reading the new issue back would take a second request.
func (c *Client) apiCreateIssue(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueParams{Fields: &fields}
	return c.api().CreateIssueWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

// Без fields YouTrack отвечает только {id, $type}.
func (c *Client) apiUpdateIssue(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateIssueParams{Fields: &fields}
	return c.api().UpdateIssueWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiAddLinkedIssue(ctx context.Context, id, link string, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AddLinkedIssueParams{Fields: &fields}
	return c.api().AddLinkedIssueWithBody(ctx, id, link, &params, jsonContentType, bytes.NewReader(body))
}

// Партнёр только по внутреннему id: на читаемый YouTrack отвечает 404 Entity with id DEV-16 not found и
// оставляет связь.
func (c *Client) apiRemoveLinkedIssue(ctx context.Context, id, link, partner string) (*http.Response, error) {
	return c.api().RemoveLinkedIssue(ctx, id, link, partner)
}

// A write: the issue is gone once the server has answered, and no fields go out with the request, since the
// specification declares none for it and the answer carries nothing to name.
func (c *Client) apiDeleteIssue(ctx context.Context, at readableID) (*http.Response, error) {
	return c.api().DeleteIssue(ctx, at.readable)
}

// named is the custom fields every record is cut down to, one query parameter each; none of them leaves the
// whole block alone.
func (c *Client) apiGetIssues(ctx context.Context, query, fields string, named []string, w window) (*http.Response, error) {
	params := ytapi.GetIssuesParams{Query: &query, Fields: &fields, Top: &w.top, Skip: w.skipped()}
	if len(named) > 0 {
		params.CustomFields = &named
	}
	return c.api().GetIssues(ctx, &params)
}

// A POST that reads: it answers how many issues a search finds and writes nothing, so an answer that
// never arrives leaves the instance as it was.
func (c *Client) apiCountIssues(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CountIssuesParams{Fields: &fields}
	return c.api().CountIssuesWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

// A POST that reads: it marks a search up and writes nothing.
func (c *Client) apiAssistSearch(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AssistSearchParams{Fields: &fields}
	return c.api().AssistSearchWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

// reverse goes out on every read of the journal: newest first is the order the command prints, and the server
// sends the oldest first without it, so $skip passes over the newest. start, end and author are not sent — a
// journal is cut down by its categories alone.
func (c *Client) apiGetIssueActivities(ctx context.Context, id, categories, fields string, w window) (*http.Response, error) {
	reverse := true
	params := ytapi.GetIssueActivitiesParams{
		Categories: &categories,
		Reverse:    &reverse,
		Fields:     &fields,
		Top:        &w.top,
		Skip:       w.skipped(),
	}
	return c.api().GetIssueActivities(ctx, id, &params)
}

// The link types of the instance, which every reader of it sees, limited token and all.
func (c *Client) apiGetIssueLinkTypes(ctx context.Context, fields string, top int32) (*http.Response, error) {
	return c.api().GetIssueLinkTypes(ctx, &ytapi.GetIssueLinkTypesParams{Fields: &fields, Top: &top})
}

// Операция не принимает $top и возвращает всех детей.
func (c *Client) apiGetArticle(ctx context.Context, id, fields string) (*http.Response, error) {
	return c.api().GetArticle(ctx, id, &ytapi.GetArticleParams{Fields: &fields})
}

func (c *Client) apiGetArticles(ctx context.Context, query, fields string, w window) (*http.Response, error) {
	return c.api().GetArticles(ctx, &ytapi.GetArticlesParams{Fields: &fields, Top: &w.top, Skip: w.skipped(), Query: &query})
}

// The children of one article, the subresource rather than the collection nested in the article: that one takes
// neither $top nor $skip. The parent is addressed by the argument the caller typed: nothing is read before its
// children are listed.
func (c *Client) apiGetArticleChildArticles(ctx context.Context, parent, fields string, w window) (*http.Response, error) {
	params := ytapi.GetArticleChildArticlesParams{Fields: &fields, Top: &w.top, Skip: w.skipped()}
	return c.api().GetArticleChildArticles(ctx, parent, &params)
}

// Без fields YouTrack отвечает только {id, $type}.
func (c *Client) apiCreateArticle(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateArticleParams{Fields: &fields}
	return c.api().CreateArticleWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiUpdateArticle(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateArticleParams{Fields: &fields}
	return c.api().UpdateArticleWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

// Вместе со статьёй удаляются все её дочерние статьи.
func (c *Client) apiDeleteArticle(ctx context.Context, at readableID) (*http.Response, error) {
	return c.api().DeleteArticle(ctx, at.readable)
}

// A write: it adds a comment to the issue and answers with the comment it added, so fields go out with it.
// The owner is addressed by the argument the caller typed rather than by an id a read gave — nothing is read
// before a comment is written — and it takes an owner rather than a string, since parseOwner is the one
// place that makes one and a string of no form would reach an endpoint other than the comments of that issue.
func (c *Client) apiCreateIssueComment(ctx context.Context, at owner, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueCommentParams{Fields: &fields}
	return c.api().CreateIssueCommentWithBody(ctx, at.id, &params, jsonContentType, bytes.NewReader(body))
}

// A write, and the same in every respect for the knowledge base: the article is addressed by the argument, and
// the answer carries the comment it added.
func (c *Client) apiCreateArticleComment(ctx context.Context, at owner, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateArticleCommentParams{Fields: &fields}
	return c.api().CreateArticleCommentWithBody(ctx, at.id, &params, jsonContentType, bytes.NewReader(body))
}

// The comments of one issue, the subresource rather than the collection nested in the issue, so the limit goes
// out as $top on every request and the count is a pass over the same path. The owner is a type rather than a string, since parseOwner is the one place
// that makes one and a string of no form would reach an endpoint other than the comments of that issue.
func (c *Client) apiGetIssueComments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.api().GetIssueComments(ctx, at.id, &ytapi.GetIssueCommentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

// A read, and the same in every respect for the knowledge base.
func (c *Client) apiGetArticleComments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.api().GetArticleComments(ctx, at.id, &ytapi.GetArticleCommentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

// A read, not a write: it answers whether the comment was taken back by whoever wrote it, which is what
// settles whether the update below goes out at all. Both segments of the path are types rather than strings,
// since parseOwner and parseChildID are the only places that make one.
func (c *Client) apiGetIssueComment(ctx context.Context, at owner, comment childID, fields string) (*http.Response, error) {
	params := ytapi.GetIssueCommentParams{Fields: &fields}
	return c.api().GetIssueComment(ctx, at.id, comment.id, &params)
}

// A write: it changes the comment and answers with the comment as it stands afterwards, so fields go out with
// it. muteUpdateNotifications is not sent — whoever is subscribed to the issue hears about the change, and
// silencing that would be the tool deciding something the caller never asked for.
func (c *Client) apiUpdateIssueComment(ctx context.Context, at owner, comment childID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateIssueCommentParams{Fields: &fields}
	return c.api().UpdateIssueCommentWithBody(ctx, at.id, comment.id, &params, jsonContentType, bytes.NewReader(body))
}

// A write, and the same in every respect for the knowledge base. No read stands before it: an article keeps no
// comment its author took back, so there is nothing to ask about.
func (c *Client) apiUpdateArticleComment(ctx context.Context, at owner, comment childID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateArticleCommentParams{Fields: &fields}
	return c.api().UpdateArticleCommentWithBody(ctx, at.id, comment.id, &params, jsonContentType, bytes.NewReader(body))
}

// A write: the comment is gone once the server has answered, and no fields go out with the request, since the
// specification declares none for it and the answer carries nothing to name.
func (c *Client) apiDeleteIssueComment(ctx context.Context, at owner, comment childID) (*http.Response, error) {
	return c.api().DeleteIssueComment(ctx, at.id, comment.id)
}

// A write, and the same in every respect for the knowledge base.
func (c *Client) apiDeleteArticleComment(ctx context.Context, at owner, comment childID) (*http.Response, error) {
	return c.api().DeleteArticleComment(ctx, at.id, comment.id)
}

// The work items of one issue, which is addressed by the argument the caller typed: nothing is read before they
// are listed.
func (c *Client) apiGetIssueWorkItems(ctx context.Context, id, fields string, w window) (*http.Response, error) {
	return c.api().GetIssueWorkItems(ctx, id, &ytapi.GetIssueWorkItemsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

// A write: it adds a work item to the issue and answers with the work item it added, so fields go out with it.
// The issue is addressed by the argument the caller typed — nothing is read before time is written — and the
// answer carries the issue as it stands afterwards, which is where the time spent on it is read off.
func (c *Client) apiCreateIssueWorkItem(ctx context.Context, id string, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueWorkItemParams{Fields: &fields}
	return c.api().CreateIssueWorkItemWithBody(ctx, id, &params, jsonContentType, bytes.NewReader(body))
}

// A write: it changes the work item and answers with the work item as it stands afterwards, so fields go out
// with it. The issue is addressed by the argument the caller typed, or by the readable id the read that
// resolved a type gave; the work item takes a childID rather than a string, since parseChildID is the one place
// that makes one and an empty segment would turn the write into a creation.
func (c *Client) apiUpdateIssueWorkItem(ctx context.Context, id string, item childID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateIssueWorkItemParams{Fields: &fields}
	return c.api().UpdateIssueWorkItemWithBody(ctx, id, item.id, &params, jsonContentType, bytes.NewReader(body))
}

// A read, not a write: it answers with the work item a removal is about to destroy, which is the whole of
// what is left to print once the removal has gone through.
func (c *Client) apiGetIssueWorkItem(ctx context.Context, id string, item childID, fields string) (*http.Response, error) {
	params := ytapi.GetIssueWorkItemParams{Fields: &fields}
	return c.api().GetIssueWorkItem(ctx, id, item.id, &params)
}

// A write: the work item is gone once the server has answered, and no fields go out with the request — the
// specification declares none for it and the server ignores them (measured). The issue is addressed by the
// readable id the read before the removal gave, which is what taking an addressed rather than a string says.
func (c *Client) apiDeleteIssueWorkItem(ctx context.Context, at readableID, item childID) (*http.Response, error) {
	return c.api().DeleteIssueWorkItem(ctx, at.readable, item.id)
}

// The attachments of one issue, the subresource rather than the collection nested in the issue: without $top
// the server stops at 42 of them, so the limit goes out on every request. The owner is a type rather than a string, since parseOwner is the one
// place that makes one and a string of no form would reach an endpoint other than the attachments of that issue.
func (c *Client) apiGetIssueAttachments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.api().GetIssueAttachments(ctx, at.id, &ytapi.GetIssueAttachmentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

// A read, and the same in every respect for the knowledge base.
func (c *Client) apiGetArticleAttachments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.api().GetArticleAttachments(ctx, at.id, &ytapi.GetArticleAttachmentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

// A write: it attaches the file to the issue and answers with an array holding the attachment it filed, so
// fields go out with it. The body is a reader of a length nobody knows — the multipart is written as the
// request goes out — and net/http sends a body of unknown length chunked, which both APIs take.
// muteUpdateNotifications is not sent: whoever watches the issue hears about the file, and silencing that
// would be the tool deciding something the caller never asked for.
func (c *Client) apiCreateIssueAttachment(ctx context.Context, at owner, contentType string, body io.Reader, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueAttachmentParams{Fields: &fields}
	return c.api().CreateIssueAttachmentWithBody(ctx, at.id, &params, contentType, body)
}

// A write, and the same in every respect for the knowledge base — except that the specification declares this
// one to take JSON, which the instance answers 500 for in every form it was measured in, so it is sent the
// multipart of an issue's attachment instead (ADR-0004).
func (c *Client) apiCreateArticleAttachment(ctx context.Context, at owner, contentType string, body io.Reader, fields string) (*http.Response, error) {
	params := ytapi.CreateArticleAttachmentParams{Fields: &fields}
	return c.api().CreateArticleAttachmentWithBody(ctx, at.id, &params, contentType, body)
}

// The read a deletion makes before it destroys anything: an attachment the owner has none of is answered 404
// here, and the owner it names is where the path of the DELETE comes from. The owner is still the argument the
// caller wrote — nothing canonical is in hand yet — and the id is a childID, since parseChildID is the one
// place that makes one.
func (c *Client) apiGetIssueAttachment(ctx context.Context, at owner, file childID, fields string) (*http.Response, error) {
	return c.api().GetIssueAttachment(ctx, at.id, file.id, &ytapi.GetIssueAttachmentParams{Fields: &fields})
}

// A read, and the same in every respect for the knowledge base.
func (c *Client) apiGetArticleAttachment(ctx context.Context, at owner, file childID, fields string) (*http.Response, error) {
	return c.api().GetArticleAttachment(ctx, at.id, file.id, &ytapi.GetArticleAttachmentParams{Fields: &fields})
}

// A write: the file is gone once the server has answered, and no fields go out with the request, since the
// specification declares none for it and the answer carries nothing to name. The owner is the readable id the
// read gave, never the argument the caller typed, which is what taking an addressed rather than a string
// says.
func (c *Client) apiDeleteIssueAttachment(ctx context.Context, at readableID, file childID) (*http.Response, error) {
	return c.api().DeleteIssueAttachment(ctx, at.readable, file.id)
}

// A write, and the same in every respect for the knowledge base.
func (c *Client) apiDeleteArticleAttachment(ctx context.Context, at readableID, file childID) (*http.Response, error) {
	return c.api().DeleteArticleAttachment(ctx, at.readable, file.id)
}

// The tags the token is shown, the whole collection rather than the tags of one user: without $top the server
// stops at 42 of them, so the limit goes out on every request. query is not sent — it matches a prefix of
// a name and folds letter case in a way of its own, so a tag it left out would look like one that is not
// there.
func (c *Client) apiGetTags(ctx context.Context, fields string, w window) (*http.Response, error) {
	return c.api().GetTags(ctx, &ytapi.GetTagsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

// A write: it makes a tag and answers with the tag it made, so fields go out with it. Without them the answer
// is {id, $type} alone, and reading the new tag back would take a second request (5.16).
func (c *Client) apiCreateTag(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateTagParams{Fields: &fields}
	return c.api().CreateTagWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

// A write: the tag is gone once the server has answered, for its owner and for everyone it was shared with,
// and no fields go out with the request — the server ignores them here and answers with nothing to name. The
// id is a tagID rather than a string, since the resolver is the one place that makes one and a string of no
// form would reach /api/tags itself, the collection rather than the one tag.
func (c *Client) apiDeleteTag(ctx context.Context, tag tagID) (*http.Response, error) {
	return c.api().DeleteTag(ctx, tag.id)
}

// A write: it hangs a tag on the issue and answers with the tag it hung, so fields go out with it. Without them
// the answer is {id, $type} alone, and the id the check is made of would take a second request (5.16). The owner
// is the readable id the read before the write gave, never the argument the caller typed, which is what
// taking an addressed rather than a string says.
func (c *Client) apiAddIssueTag(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AddIssueTagParams{Fields: &fields}
	return c.api().AddIssueTagWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

// A write, and the same in every respect for the knowledge base.
func (c *Client) apiAddArticleTag(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AddArticleTagParams{Fields: &fields}
	return c.api().AddArticleTagWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

// A write: it takes the tag off the issue and leaves the tag itself standing, for its owner and for everyone it
// was shared with — that is what makes this another operation than DeleteTag rather than another way to reach
// it. No fields go out with the request: the specification declares none, and the answer carries nothing to
// name. The owner is the readable id the read before the write gave, and the tag a tagID, since the resolver is
// the one place that makes one.
func (c *Client) apiRemoveIssueTag(ctx context.Context, at readableID, tag tagID) (*http.Response, error) {
	return c.api().RemoveIssueTag(ctx, at.readable, tag.id)
}

// A write, and the same in every respect for the knowledge base.
func (c *Client) apiRemoveArticleTag(ctx context.Context, at readableID, tag tagID) (*http.Response, error) {
	return c.api().RemoveArticleTag(ctx, at.readable, tag.id)
}

// The groups of the instance, the whole collection: without $top the server stops at 42 of them, and a group
// left out would look like one that is not there. The endpoint takes no search at all, so the catalogue is the
// only way to ask, and /api/admin/groups exists on neither instance. GetGroups is the name the generator
// gives the operation itself, so the record of overlay it would take is none.
func (c *Client) apiGetGroups(ctx context.Context, fields string, top int32) (*http.Response, error) {
	return c.api().GetGroups(ctx, &ytapi.GetGroupsParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetUser(ctx context.Context, login, fields string) (*http.Response, error) {
	return c.api().GetUser(ctx, login, &ytapi.GetUserParams{Fields: &fields})
}

func (c *Client) apiGetUsers(ctx context.Context, search, fields string, w window) (*http.Response, error) {
	return c.api().GetUsers(ctx, &ytapi.GetUsersParams{Fields: &fields, Top: &w.top, Skip: w.skipped(), Query: &search})
}

func (c *Client) apiGetCurrentUser(ctx context.Context, fields string) (*http.Response, error) {
	return c.api().GetCurrentUser(ctx, &ytapi.GetCurrentUserParams{Fields: &fields})
}
