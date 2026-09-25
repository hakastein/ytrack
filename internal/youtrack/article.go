package youtrack

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const ArticleShowFields = "idReadable,summary,reporter(login),created,updated,tags(name)," +
	"parentArticle(idReadable,summary),childArticles(idReadable,summary),content"

const ArticleListFields = "idReadable,summary"

const (
	articleSchema   = "Article"
	articlesPlural  = "articles"
	articlesListing = "[]" + articleSchema
)

// ShowArticle is the call for the article of that id with the fields of expression, or with them added to
// ArticleShowFields when it starts with +; nil is the caller leaning on the default whole. As many of its
// comments are printed as comments asks for.
func ShowArticle(id string, expression *string, comments Comments) (Call, *diag.Fault) {
	id, fault := parseArticleID(id)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleShowFields, commentsOfAShow)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.showArticle(ctx, spec, id, requested, comments)
	}, nil
}

// CreateArticle is the call that files an article in the project of that code, titled summary and, where
// content is given, carrying that text. Where parent is given, the article is written under the article of
// that readable id. It prints the article as it stands after the write, with the fields of expression, or with
// them added to ArticleShowFields when it starts with +; nil is the caller leaning on the default whole.
func CreateArticle(code, summary string, content, parent, expression *string) (Call, *diag.Fault) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	parts, fault := parseArticleCreate(code, summary, content, parent)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleShowFields, articleCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.createArticle(ctx, spec, parts, requested)
	}, nil
}

// One POST is the whole command where no parent is named: the project is not read first, since YouTrack
// answers a code it has none of with a 404 naming it, and the answer carries the article the write filed, so
// nothing is read back either. A parent costs one read before it.
func (c *Client) createArticle(ctx context.Context, spec *schemas, parts articleCreateInput, requested []requestedField) (*render.Node, *diag.Fault) {
	filed, fault := c.resolveCreateParent(ctx, spec, parts)
	if fault != nil {
		return nil, fault
	}
	body := filed.body()
	return c.write(ctx, spec, articleSchema, withFields(requested, filed.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateArticle(ctx, body, fields)
	}, filed.verify, writeResultNode(requested))
}

// An article as the read before a write finds it: the internal id a body addresses it by, the readable id the
// path and the check of the answer go by, and the project it stands in. It is what a creation reads of the
// parent it files under and what an update reads of the article it writes into, since both need the same three.
type articleRef struct {
	id       string
	readable readableID
	project  string
	response decodedResponse
}

// resolveCreateParent is the creation with the parent it names resolved against the server, and the creation as it
// stands where it names none. Neither mistake a parent can carry is left to the answer: YouTrack files an
// article under a parent it has none of at the root of the tree, and one under a parent of another project in
// that other project, both under a 200 and without a word, so by the time the answer disagreed the article
// would exist.
func (c *Client) resolveCreateParent(ctx context.Context, spec *schemas, parts articleCreateInput) (articleCreate, *diag.Fault) {
	filed := articleCreate{project: parts.project, summary: parts.summary, content: parts.content}
	if parts.parentID == nil {
		return filed, nil
	}
	found, fault := c.readArticleToWrite(ctx, spec, *parts.parentID, parentFlag)
	if fault != nil {
		return articleCreate{}, fault
	}
	if !strings.EqualFold(found.project, parts.project) {
		return articleCreate{}, found.crossProjectFault(parts.project, createAcrossProjectsMessage)
	}
	filed.parent = &found
	return filed, nil
}

// What the read before a write asks of the article: the id a body carries, the id the answer is held against,
// and the project no article may be filed across.
func articleToWriteFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: idReadableKey},
		{name: projectKey, children: []requestedField{{name: shortNameKey}}},
	}
}

// what is the part of the call the readable id stands for, which is what a refusal over its form names: the
// update itself where the article is the one being written, and the flag where it is the parent.
func (c *Client) readArticleToWrite(ctx context.Context, spec *schemas, id, what string) (articleRef, *diag.Fault) {
	a, fault := c.request(ctx, spec, articleSchema, articleToWriteFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetArticle(ctx, id, fields)
	})
	if fault != nil {
		return articleRef{}, fault
	}
	return readArticleToWrite(a, what)
}

func readArticleToWrite(a decodedResponse, what string) (articleRef, *diag.Fault) {
	readable, fault := readableIDAt(a, a.objects[0], articleOwner, what)
	if fault != nil {
		return articleRef{}, fault
	}
	found := a.objects[0]
	id, isText := found[idKey].(string)
	code, isNamed := memberOf(found[projectKey], shortNameKey).(string)
	if !isText || !isNamed {
		message := "the id or the project of the article is not text"
		return articleRef{}, shapeFailure(a.httpResponse, a.body, message)
	}
	return articleRef{id: id, readable: readable, project: code, response: a}, nil
}

// The parent is no path segment of its own — the body carries the internal id — but it is named by the readable
// id in every refusal and in the check of the write, so it is held to the form of an article all the same.
const parentFlag = "--" + parentKey

// The call names one project and the parent stands in another. The refusal names both either way; what YouTrack
// would do about it differs by the verb, so the message is the caller's.
func (p articleRef) crossProjectFault(code, message string) *diag.Fault {
	a := p.response
	details := []render.Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: projectKey, Value: render.NewString(code)},
		{Key: parentKey, Value: render.NewString(p.readable.String())},
		{Key: "parent_project", Value: render.NewString(p.project)},
	}
	return &diag.Fault{Code: diag.BadUsage, Message: message, Details: details}
}

const (
	createAcrossProjectsMessage = "--parent names an article of another project, and YouTrack would file the new " +
		"article in the project of the parent rather than in the one the call names"
	updateAcrossProjectsMessage = "--parent names an article of another project, and an article hangs from a parent " +
		"of its own project alone"
)

// UpdateArticle is the call that writes the parts given into the article of that id: the title where summary is
// given, the text where content is, the article it hangs from where parent is, and an empty text or no parent at
// all where cleared names them. A part the call does not give is left as the article holds it. It prints the
// article as it stands after the write, with the fields of expression, or with them added to ArticleShowFields
// when it starts with +; nil is the caller leaning on the default whole.
func UpdateArticle(id string, summary, content, parent *string, cleared []string, expression *string) (Call, *diag.Fault) {
	id, fault := parseArticleID(id)
	if fault != nil {
		return nil, fault
	}
	if summary == nil && content == nil && parent == nil && len(cleared) == 0 {
		return nil, &diag.Fault{Code: diag.BadUsage, Message: nothingToWriteIntoAnArticle}
	}
	parts, fault := parseArticleUpdate(summary, content, parent, cleared)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleShowFields, articleCommentTarget().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.updateArticle(ctx, spec, id, parts, requested)
	}, nil
}

// An update reads the article before it writes it. That read turns an article that is not there into a
// not_found before anything is written, and it settles the id the write is addressed by: the server answers
// dev-A-7 and 177-58 for the same article, and a write addressed by the argument would be a write to a name
// nothing checked.
func (c *Client) updateArticle(ctx context.Context, spec *schemas, id string, parts articleUpdateInput, requested []requestedField) (*render.Node, *diag.Fault) {
	article, fault := c.readArticleToWrite(ctx, spec, id, "an update")
	if fault != nil {
		return nil, fault
	}
	written, fault := c.resolveUpdateParent(ctx, spec, article, parts)
	if fault != nil {
		return nil, fault
	}
	body := written.body()
	return c.write(ctx, spec, articleSchema, withFields(requested, written.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiUpdateArticle(ctx, article.readable, body, fields)
	}, written.verify, writeResultNode(requested))
}

// resolveUpdateParent is the update with the parent it names resolved against the server, and the update as it stands
// where it names none or takes the parent away. Neither mistake a parent can carry is left to the answer:
// YouTrack takes the standing parent off an article moved under one it has none of, under a 200 and without a
// word, and answers a move that would close the line of an article with a 500 of a servlet — one that says
// nothing was written where nothing was.
func (c *Client) resolveUpdateParent(ctx context.Context, spec *schemas, article articleRef, parts articleUpdateInput) (articleUpdate, *diag.Fault) {
	written := articleUpdate{
		summary:       parts.summary,
		content:       parts.content,
		clearsContent: parts.clearsContent,
		clearsParent:  parts.clearsParent,
	}
	if parts.parentID == nil {
		return written, nil
	}
	parent, line, fault := c.readAncestors(ctx, spec, *parts.parentID)
	if fault != nil {
		return articleUpdate{}, fault
	}
	if !strings.EqualFold(parent.project, article.project) {
		return articleUpdate{}, parent.crossProjectFault(article.project, updateAcrossProjectsMessage)
	}
	if fault := parent.checkNoCycle(article, line); fault != nil {
		return articleUpdate{}, fault
	}
	written.parent = &parent
	return written, nil
}

// One step of the line an article hangs from: the internal id a cycle is settled by and the readable id a
// refusal names it with.
type ancestor struct {
	id       string
	readable string
}

// How many steps of the line one request asks for. The line arrives as one nested member per step, so a deeper
// article costs another request rather than a longer expression.
const ancestorsPerRequest = 10

// What the read of a parent asks for: what any read before a write asks, and above it the line the parent hangs
// from, so a cycle is settled without one request per step.
func ancestorFields() []requestedField {
	var parentObject []requestedField
	for range ancestorsPerRequest {
		parentObject = []requestedField{{name: parentArticleKey, children: append(
			[]requestedField{{name: idKey}, {name: idReadableKey}}, parentObject...)}}
	}
	return append(articleToWriteFields(), parentObject...)
}

func (c *Client) readAncestors(ctx context.Context, spec *schemas, id string) (articleRef, []ancestor, *diag.Fault) {
	var parent articleRef
	var line []ancestor
	seen := map[string]bool{}
	for at := id; ; {
		a, fault := c.request(ctx, spec, articleSchema, ancestorFields(), func(ctx context.Context, fields string) (*http.Response, error) {
			return c.apiGetArticle(ctx, at, fields)
		})
		if fault != nil {
			return articleRef{}, nil, fault
		}
		object := a.objects[0]
		if line == nil {
			parent, fault = readArticleToWrite(a, parentFlag)
			if fault != nil {
				return articleRef{}, nil, fault
			}
			line = append(line, ancestor{id: parent.id, readable: parent.readable.String()})
			seen[parent.id] = true
		}
		read := len(line)
		for {
			value, asked := object[parentArticleKey]
			if !asked {
				break
			}
			parentObject, isObject := value.(map[string]any)
			if !isObject {
				return parent, line, nil
			}
			step, fault := readAncestor(a, parentObject)
			if fault != nil {
				return articleRef{}, nil, fault
			}
			if seen[step.id] {
				return articleRef{}, nil, ancestorCycleFault(a, step)
			}
			seen[step.id] = true
			line = append(line, step)
			object = parentObject
		}
		// The request that follows starts from a step this one brought: an answer that carried none says nothing
		// about where the line goes, and asking again would be asking what was just asked. An answer carries none
		// where it holds no parent at all — a member nothing asked for is a member the server leaves out.
		if len(line) == read {
			return articleRef{}, nil, brokenAncestryFault(a, line[read-1])
		}
		at = line[len(line)-1].id
	}
}

func readAncestor(a decodedResponse, parent map[string]any) (ancestor, *diag.Fault) {
	id, isText := parent[idKey].(string)
	readable, isReadable := parent[idReadableKey].(string)
	if !isText || !isReadable {
		return ancestor{}, shapeFailure(a.httpResponse, a.body, "the id or the readable id of an ancestor is not text")
	}
	return ancestor{id: id, readable: readable}, nil
}

// An article standing twice in the line above it is a tree the knowledge base cannot hold, so it is the answer
// that is wrong rather than the call: the line is read no further, since reading it further would not end.
func ancestorCycleFault(a decodedResponse, twice ancestor) *diag.Fault {
	details := []render.Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: articleOwner.String(), Value: render.NewString(twice.readable)},
	}
	message := "the article under article stands twice in the line of parents the server answered with, and no " +
		"article hangs from itself"
	return &diag.Fault{Code: diag.UpstreamInvalid, Message: message, Details: details}
}

// A line that neither reaches the root nor grows by a step is a tree the knowledge base cannot hold either: the
// answer puts the article under article nowhere, and the request under request is the whole of what was asked.
func brokenAncestryFault(a decodedResponse, at ancestor) *diag.Fault {
	details := []render.Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: articleOwner.String(), Value: render.NewString(at.readable)},
	}
	message := "the server answered no parent for the article under article and no root above it either, and a " +
		"line read on from there would be read from the same place again"
	return &diag.Fault{Code: diag.UpstreamInvalid, Message: message, Details: details}
}

// checkNoCycle is why the article cannot be moved under this parent: the parent is the article itself or hangs
// from it, and either way the move would close the line into a ring. YouTrack answers the first honestly and the
// second with a 500 of a servlet, which would be a write_uncertain over an instance nothing
// touched, so it is settled here.
func (p articleRef) checkNoCycle(article articleRef, line []ancestor) *diag.Fault {
	at := slices.IndexFunc(line, func(step ancestor) bool { return step.id == article.id })
	if at < 0 {
		return nil
	}
	chain := make([]*render.Node, 0, at+1)
	for _, step := range line[:at+1] {
		chain = append(chain, render.NewString(step.readable))
	}
	a := p.response
	details := []render.Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: articleOwner.String(), Value: render.NewString(article.readable.String())},
		{Key: parentKey, Value: render.NewString(p.readable.String())},
		{Key: "chain", Value: render.NewList(chain...)},
	}
	message := "--parent names the article itself or one written under it, and chain runs from the parent up to " +
		"the article: an article hangs from no line of its own"
	return &diag.Fault{Code: diag.BadUsage, Message: message, Details: details}
}

// DeleteArticle is the call that deletes the article of that id, everything written under it with it, and
// prints the id the server knows it by. What the one GET before it costs is worth more here than anywhere
// else: what is destroyed is a whole subtree rather than one entity.
func DeleteArticle(id string) (Call, *diag.Fault) {
	id, fault := parseArticleID(id)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.deleteOwner(ctx, spec, articleOwner, articleSchema, func(ctx context.Context, fields string) (*http.Response, error) {
			return c.apiGetArticle(ctx, id, fields)
		}, c.apiDeleteArticle)
	}, nil
}

// ListArticles is the call for one page of the articles the search query finds, with the fields of expression, or
// with them added to ArticleListFields when it starts with +; nil is the caller leaning on the default whole.
func ListArticles(query string, expression *string, page Page) (Call, *diag.Fault) {
	if fault := rejectUnreadableQuery(query); fault != nil {
		return nil, fault
	}
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleListFields, articleCommentTarget().commentsOfAList())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listArticles(ctx, spec, query, requested, page)
	}, nil
}

// The knowledge base counts nothing of its own — there is no counter beside the search the way there is for
// issues — so the whole of what the search finds is read off a second page over ids alone, the way a list of
// projects or of users is counted. The search itself is never looked at: the markup of /api/search/assist reads
// the language of issues, which is not the language an article is found by, so a selection of articles asks
// nothing of it.
func (c *Client) listArticles(ctx context.Context, spec *schemas, query string, requested []requestedField, page Page) (*render.Node, *diag.Fault) {
	return c.listPage(ctx, spec, articlesPlural, articlesListing, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.apiGetArticles(ctx, query, fields, w)
	})
}

// ListChildArticles is one page of the children of the article of that readable id, with the fields of
// expression, or with them added to ArticleListFields when it starts with +; nil is the caller leaning on the
// default whole.
func ListChildArticles(parent string, expression *string, page Page) (Call, *diag.Fault) {
	parent, fault := parseArticleID(parent)
	if fault != nil {
		return nil, fault
	}
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleListFields, articleCommentTarget().commentsOfAList())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listPage(ctx, spec, articlesPlural, articlesListing, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
			return c.apiGetArticleChildArticles(ctx, parent, fields, w)
		})
	}, nil
}

// articleFields is an expression of a command that prints articles, held to what an article may be asked for. A
// nil expression is the caller leaning on the default whole, and then nothing in the tree is theirs to answer
// for. because is why comments stand nowhere in it, which a show and a selection say differently.
func articleFields(spec *schemas, expression *string, defaults, because string) ([]requestedField, *diag.Fault) {
	written := defaults
	requested, fault := parseDefault(defaults, false)
	if expression != nil {
		written = *expression
		requested, fault = parseFields(written, defaults)
	}
	if fault != nil {
		return nil, fault
	}
	if fault := articleCommentTarget().reject(spec, written, requested, because); fault != nil {
		return nil, fault
	}
	return requested, nil
}

func (c *Client) showArticle(ctx context.Context, spec *schemas, id string, requested []requestedField, comments Comments) (*render.Node, *diag.Fault) {
	held := articleCommentTarget()
	decoded, fault := c.request(ctx, spec, articleSchema, comments.merged(held, requested), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetArticle(ctx, id, fields)
	})
	if fault != nil {
		return nil, fault
	}
	article := decoded.objects[0]
	own, fault := comments.pair(held, decoded, article)
	if fault != nil {
		return nil, fault
	}
	return objectNode(decoded, requested, article, own)
}
