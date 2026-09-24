package youtrack

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// Where the article stands in the tree of the knowledge base, who wrote it, how it is marked, and last the
// prose it is written for. The project is left out: it is the prefix of the readable id already, and moving an
// article renumbers it, so the id is what names it either way.
const ArticleShowFields = "idReadable,summary,reporter(login),created,updated,tags(name)," +
	"parentArticle(idReadable,summary),childArticles(idReadable,summary),content"

// A record of a selection is one line, and these answer what every reader of a search asks first: which article
// it is and what it is about. Everything else an article carries — its tree, its prose, its comments — is what
// ytrack article show prints, a whole article at a time.
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
	parts, fault := filedArticle(code, summary, content, parent)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleShowFields, articleComments().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.articleCreated(ctx, spec, parts, requested)
	}, nil
}

// One POST is the whole command where no parent is named: the project is not read first, since YouTrack
// answers a code it has none of with a 404 naming it, and the answer carries the article the write filed, so
// nothing is read back either. A parent costs one read before it.
func (c *Client) articleCreated(ctx context.Context, spec *schemas, parts writtenArticle, requested []requestedField) (*render.Node, *diag.Fault) {
	filed, fault := c.filedUnder(ctx, spec, parts)
	if fault != nil {
		return nil, fault
	}
	body := filed.body()
	return c.writing(ctx, spec, articleSchema, asking(requested, filed.checked()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.createArticle(ctx, body, fields)
	}, filed.confirmedBy, writtenNode(requested))
}

// An article as the read before a write finds it: the internal id a body addresses it by, the readable id the
// path and the check of the answer go by, and the project it stands in. It is what a creation reads of the
// parent it files under and what an update reads of the article it writes into, since both need the same three.
type articleToWrite struct {
	id       string
	readable addressed
	project  string
	// The answer it arrived in, so a refusal raised before the write names the request that was sent.
	arrived answer
}

// filedUnder is the creation with the parent it names resolved against the server, and the creation as it
// stands where it names none. Neither mistake a parent can carry is left to the answer: YouTrack files an
// article under a parent it has none of at the root of the tree, and one under a parent of another project in
// that other project, both under a 200 and without a word, so by the time the answer disagreed the article
// would exist.
func (c *Client) filedUnder(ctx context.Context, spec *schemas, parts writtenArticle) (articleFiledUnder, *diag.Fault) {
	filed := articleFiledUnder{project: parts.project, summary: parts.summary, content: parts.content}
	if parts.parentID == nil {
		return filed, nil
	}
	found, fault := c.readArticleToWrite(ctx, spec, *parts.parentID, parentFlag)
	if fault != nil {
		return articleFiledUnder{}, fault
	}
	if !strings.EqualFold(found.project, parts.project) {
		return articleFiledUnder{}, found.inAnotherProject(parts.project, filedAcrossProjects)
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
func (c *Client) readArticleToWrite(ctx context.Context, spec *schemas, id, what string) (articleToWrite, *diag.Fault) {
	a, fault := c.passing(ctx, spec, articleSchema, articleToWriteFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getArticle(ctx, id, fields)
	})
	if fault != nil {
		return articleToWrite{}, fault
	}
	return readArticleToWrite(a, what)
}

// The judgment of names says the members arrived, not what they hold, so everything a write reads out of that
// answer is held to its shape here, and the readable id to the form of an article besides: this is the one read
// every write of an article goes by, so an id no answer is held to would reach the write as a path segment.
func readArticleToWrite(a answer, what string) (articleToWrite, *diag.Fault) {
	readable, fault := addressedIn(a, a.objects[0], articleOwner, what)
	if fault != nil {
		return articleToWrite{}, fault
	}
	found := a.objects[0]
	id, isText := found[idKey].(string)
	code, isNamed := memberOf(found[projectKey], shortNameKey).(string)
	if !isText || !isNamed {
		message := "the id or the project of the article is not text"
		return articleToWrite{}, shapeFailure(a.response, a.body, message)
	}
	return articleToWrite{id: id, readable: readable, project: code, arrived: a}, nil
}

// The parent is no path segment of its own — the body carries the internal id — but it is named by the readable
// id in every refusal and in the check of the write, so it is held to the form of an article all the same.
const parentFlag = "--" + parentKey

// The call names one project and the parent stands in another. The refusal names both either way; what YouTrack
// would do about it differs by the verb, so the message is the caller's.
func (p articleToWrite) inAnotherProject(code, message string) *diag.Fault {
	a := p.arrived
	details := []render.Pair{
		requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted()),
		{Key: projectKey, Value: render.NewString(code)},
		{Key: parentKey, Value: render.NewString(p.readable.String())},
		{Key: "parent_project", Value: render.NewString(p.project)},
	}
	return &diag.Fault{Code: diag.BadUsage, Message: message, Details: details}
}

const (
	filedAcrossProjects = "--parent names an article of another project, and YouTrack would file the new " +
		"article in the project of the parent rather than in the one the call names"
	writtenAcrossProjects = "--parent names an article of another project, and an article hangs from a parent " +
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
	parts, fault := rewrittenArticle(summary, content, parent, cleared)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleShowFields, articleComments().commentsOfAWrite())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.articleUpdated(ctx, spec, id, parts, requested)
	}, nil
}

// An update reads the article before it writes it. That read turns an article that is not there into a
// not_found before anything is written, and it settles the id the write is addressed by: the server answers
// dev-A-7 and 177-58 for the same article, and a write addressed by the argument would be a write to a name
// nothing checked.
func (c *Client) articleUpdated(ctx context.Context, spec *schemas, id string, parts changedArticle, requested []requestedField) (*render.Node, *diag.Fault) {
	article, fault := c.readArticleToWrite(ctx, spec, id, "an update")
	if fault != nil {
		return nil, fault
	}
	written, fault := c.writtenUnder(ctx, spec, article, parts)
	if fault != nil {
		return nil, fault
	}
	body := written.changes()
	return c.writing(ctx, spec, articleSchema, asking(requested, written.checked()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.updateArticle(ctx, article.readable, body, fields)
	}, written.confirmedBy, writtenNode(requested))
}

// writtenUnder is the update with the parent it names resolved against the server, and the update as it stands
// where it names none or takes the parent away. Neither mistake a parent can carry is left to the answer:
// YouTrack takes the standing parent off an article moved under one it has none of, under a 200 and without a
// word, and answers a move that would close the line of an article with a 500 of a servlet — one that says
// nothing was written where nothing was.
func (c *Client) writtenUnder(ctx context.Context, spec *schemas, article articleToWrite, parts changedArticle) (articleWrittenUnder, *diag.Fault) {
	written := articleWrittenUnder{
		summary:       parts.summary,
		content:       parts.content,
		clearsContent: parts.clearsContent,
		clearsParent:  parts.clearsParent,
	}
	if parts.parentID == nil {
		return written, nil
	}
	parent, line, fault := c.lineAbove(ctx, spec, *parts.parentID)
	if fault != nil {
		return articleWrittenUnder{}, fault
	}
	if !strings.EqualFold(parent.project, article.project) {
		return articleWrittenUnder{}, parent.inAnotherProject(article.project, writtenAcrossProjects)
	}
	if fault := parent.closingTheLine(article, line); fault != nil {
		return articleWrittenUnder{}, fault
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
const ancestryStep = 10

// What the read of a parent asks for: what any read before a write asks, and above it the line the parent hangs
// from, so a cycle is settled without one request per step.
func articleAboveFields() []requestedField {
	var above []requestedField
	for range ancestryStep {
		above = []requestedField{{name: parentArticleKey, children: append(
			[]requestedField{{name: idKey}, {name: idReadableKey}}, above...)}}
	}
	return append(articleToWriteFields(), above...)
}

// lineAbove is the parent as a write needs it and the whole line it hangs from, the parent itself first. The
// line is read ancestryStep steps to a request, and where it has not ended by then the next request starts from
// the last step that arrived: the deepest member of the answer carries no parent of its own, which is what tells
// a line that ran out of the expression from one that reached the root. What ends the reading is therefore a
// step that arrived and never the shape of the expression, so that no expression can make it read on forever.
func (c *Client) lineAbove(ctx context.Context, spec *schemas, id string) (articleToWrite, []ancestor, *diag.Fault) {
	var parent articleToWrite
	var line []ancestor
	seen := map[string]bool{}
	for at := id; ; {
		a, fault := c.passing(ctx, spec, articleSchema, articleAboveFields(), func(ctx context.Context, fields string) (*http.Response, error) {
			return c.getArticle(ctx, at, fields)
		})
		if fault != nil {
			return articleToWrite{}, nil, fault
		}
		object := a.objects[0]
		if line == nil {
			parent, fault = readArticleToWrite(a, parentFlag)
			if fault != nil {
				return articleToWrite{}, nil, fault
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
			above, hangs := value.(map[string]any)
			if !hangs {
				return parent, line, nil
			}
			step, fault := readAncestor(a, above)
			if fault != nil {
				return articleToWrite{}, nil, fault
			}
			if seen[step.id] {
				return articleToWrite{}, nil, lineOfItsOwn(a, step)
			}
			seen[step.id] = true
			line = append(line, step)
			object = above
		}
		// The request that follows starts from a step this one brought: an answer that carried none says nothing
		// about where the line goes, and asking again would be asking what was just asked. An answer carries none
		// where it holds no parent at all — a member nothing asked for is a member the server leaves out.
		if len(line) == read {
			return articleToWrite{}, nil, lineWithoutAnEnd(a, line[read-1])
		}
		at = line[len(line)-1].id
	}
}

// The judgment of names says the members arrived, not what they hold, so every step of the line is held to its
// shape here.
func readAncestor(a answer, above map[string]any) (ancestor, *diag.Fault) {
	id, isText := above[idKey].(string)
	readable, isReadable := above[idReadableKey].(string)
	if !isText || !isReadable {
		return ancestor{}, shapeFailure(a.response, a.body, "the id or the readable id of an ancestor is not text")
	}
	return ancestor{id: id, readable: readable}, nil
}

// An article standing twice in the line above it is a tree the knowledge base cannot hold, so it is the answer
// that is wrong rather than the call: the line is read no further, since reading it further would not end.
func lineOfItsOwn(a answer, twice ancestor) *diag.Fault {
	details := []render.Pair{
		requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted()),
		{Key: articleOwner.String(), Value: render.NewString(twice.readable)},
	}
	message := "the article under article stands twice in the line of parents the server answered with, and no " +
		"article hangs from itself"
	return &diag.Fault{Code: diag.UpstreamLied, Message: message, Details: details}
}

// A line that neither reaches the root nor grows by a step is a tree the knowledge base cannot hold either: the
// answer puts the article under article nowhere, and the request under request is the whole of what was asked.
func lineWithoutAnEnd(a answer, at ancestor) *diag.Fault {
	details := []render.Pair{
		requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted()),
		{Key: articleOwner.String(), Value: render.NewString(at.readable)},
	}
	message := "the server answered no parent for the article under article and no root above it either, and a " +
		"line read on from there would be read from the same place again"
	return &diag.Fault{Code: diag.UpstreamLied, Message: message, Details: details}
}

// closingTheLine is why the article cannot be moved under this parent: the parent is the article itself or hangs
// from it, and either way the move would close the line into a ring. YouTrack answers the first honestly and the
// second with a 500 of a servlet, which would be a write_uncertain over an instance nothing
// touched, so it is settled here.
func (p articleToWrite) closingTheLine(article articleToWrite, line []ancestor) *diag.Fault {
	at := slices.IndexFunc(line, func(step ancestor) bool { return step.id == article.id })
	if at < 0 {
		return nil
	}
	chain := make([]*render.Node, 0, at+1)
	for _, step := range line[:at+1] {
		chain = append(chain, render.NewString(step.readable))
	}
	a := p.arrived
	details := []render.Pair{
		requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted()),
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
		return c.deletion(ctx, spec, articleOwner, articleSchema, func(ctx context.Context, fields string) (*http.Response, error) {
			return c.getArticle(ctx, id, fields)
		}, c.deleteArticle)
	}, nil
}

// ListArticles is the call for one page of the articles the search query finds, with the fields of expression, or
// with them added to ArticleListFields when it starts with +; nil is the caller leaning on the default whole.
func ListArticles(query string, expression *string, page Page) (Call, *diag.Fault) {
	if fault := refuseUnreadableQuery(query); fault != nil {
		return nil, fault
	}
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := articleFields(spec, expression, ArticleListFields, articleComments().commentsOfAList())
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
	return c.selection(ctx, spec, articlesPlural, articlesListing, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.getArticles(ctx, query, fields, w)
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
	requested, fault := articleFields(spec, expression, ArticleListFields, articleComments().commentsOfAList())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.selection(ctx, spec, articlesPlural, articlesListing, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
			return c.getArticleChildArticles(ctx, parent, fields, w)
		})
	}, nil
}

// articleFields is an expression of a command that prints articles, held to what an article may be asked for. A
// nil expression is the caller leaning on the default whole, and then nothing in the tree is theirs to answer
// for. because is why comments stand nowhere in it, which a show and a selection say differently.
func articleFields(spec *schemas, expression *string, defaults, because string) ([]requestedField, *diag.Fault) {
	written := defaults
	requested, fault := theDefault(defaults, false)
	if expression != nil {
		written = *expression
		requested, fault = parseFields(written, defaults)
	}
	if fault != nil {
		return nil, fault
	}
	if fault := articleComments().refuse(spec, written, requested, because); fault != nil {
		return nil, fault
	}
	return requested, nil
}

// One GET is the whole article: the operation takes no $top, and the children of an article arrive all of
// them, so neither the tree below it nor the prose nor the comments cost a second request.
func (c *Client) showArticle(ctx context.Context, spec *schemas, id string, requested []requestedField, comments Comments) (*render.Node, *diag.Fault) {
	held := articleComments()
	answer, fault := c.passing(ctx, spec, articleSchema, comments.merged(held, requested), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getArticle(ctx, id, fields)
	})
	if fault != nil {
		return nil, fault
	}
	article := answer.objects[0]
	own, fault := comments.pair(held, answer, article)
	if fault != nil {
		return nil, fault
	}
	return objectNode(answer, requested, article, own)
}
