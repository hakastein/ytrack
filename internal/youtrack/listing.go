package youtrack

import (
	"context"
	"fmt"
	"math"
	"net/http"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// topAll is the $top of a request for all there is.
const topAll = -1

// Page is the part of a selection a list prints: at most Limit records, after the first Skip of them.
type Page struct {
	Limit int
	Skip  int
}

// The specification declares $top and $skip int32.
func (p Page) parse() *diag.Fault {
	return p.validate(math.MaxInt32)
}

// most is the largest limit: a list that asks for one record past its page takes one short of int32.
func (p Page) validate(most int) *diag.Fault {
	if p.Limit < 1 || p.Limit > most {
		message := fmt.Sprintf("limit %d is not between 1 and %d", p.Limit, most)
		return &diag.Fault{Code: diag.BadUsage, Message: message}
	}
	if p.Skip < 0 || p.Skip > math.MaxInt32 {
		message := fmt.Sprintf("skip %d is not between 0 and %d", p.Skip, math.MaxInt32)
		return &diag.Fault{Code: diag.BadUsage, Message: message}
	}
	return nil
}

func (p Page) window() window {
	return window{top: int32(p.Limit), skip: int32(p.Skip)}
}

// window is what one request of a list asks for: at most top records, after the first skip of them.
type window struct {
	top  int32
	skip int32
}

var allRecords = window{top: topAll}

// $skip goes out only where the caller asked to pass records over, so a first page is the request it always was.
func (w window) skipped() *int32 {
	if w.skip == 0 {
		return nil
	}
	return &w.skip
}

// pageFetcher is the one question a list asks: the records it finds within the window.
type pageFetcher func(ctx context.Context, fields string, w window) (*http.Response, error)

// count is how many records a search finds in all, or that the server would not say how many.
type count struct {
	total int
	known bool
}

func counted(total int) count {
	return count{total: total, known: true}
}

// Отдельно от count: запись за лимитом доказывает обрезку, но не даёт общего числа.
type truncation struct {
	left  bool
	known bool
}

func truncated(left bool) truncation {
	return truncation{left: left, known: true}
}

func (c count) truncationAt(shown int) truncation {
	return truncation{left: c.total > shown, known: c.known}
}

// A list is one command's page of records and the count of what it was taken from: where the records arrive,
// under what name they are printed, what the caller asked of each, which page of them and how a page of them is
// asked for.
type list struct {
	// Запись может содержать адрес инстанса, поэтому для чтения нужен клиент, а не только каталог.
	client    *Client
	spec      *schemas
	plural    string
	schema    string
	requested []requestedField
	page      Page
	fetchPage pageFetcher
	// The expression the request carries where it is not the one a record prints: a command that fills names of
	// its own in has one, and every other list leaves it empty.
	sentFields []requestedField
	// How the whole of the records is counted, where the page does not say it by itself.
	countTotal func(ctx context.Context) (count, *diag.Fault)
}

// A record prints what the caller asked for, and the request carries the same until a command fills names of
// its own into it.
func (l list) requestFields() requestFields {
	if l.sentFields != nil {
		return requestFields{sent: l.sentFields, output: l.requested}
	}
	return requestFields{sent: l.requested, output: l.requested}
}

// listPage is one page of what ask finds, under the document of a list, counted by a pass over ids alone. The
// page and the count are asked by the one ask given here, so the count is of the records the page came from and
// a second question, over the whole catalogue, has nowhere to be written.
func (c *Client) listPage(ctx context.Context, spec *schemas, plural, schema string, requested []requestedField, page Page, fetchPage pageFetcher) (*render.Node, *diag.Fault) {
	return c.newList(spec, plural, schema, requested, nil, page, fetchPage).fetch(ctx)
}

func (c *Client) newList(spec *schemas, plural, schema string, requested, sentFields []requestedField, page Page, fetchPage pageFetcher) list {
	l := list{client: c, spec: spec, plural: plural, schema: schema, requested: requested, sentFields: sentFields, page: page, fetchPage: fetchPage}
	l.countTotal = func(ctx context.Context) (count, *diag.Fault) {
		ids, fault := c.read(ctx, spec, schema, []requestedField{{name: idKey}}, func(ctx context.Context, fields string) (*http.Response, error) {
			return fetchPage(ctx, fields, allRecords)
		})
		if fault != nil {
			return count{}, fault
		}
		return counted(len(ids)), nil
	}
	return l
}

// fetch is the document of a list. When the count is asked, what is made of it and how the document reads
// are the same for every list of the tool: a page short of the limit ends the selection, so the records it
// passed over and the records it holds are the whole of it, and only a page that fills the limit, or one that
// holds nothing after records passed over, is counted at all.
func (l list) fetch(ctx context.Context) (*render.Node, *diag.Fault) {
	page, fault := l.client.readList(ctx, l.spec, l.schema, l.requestFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return l.fetchPage(ctx, fields, l.page.window())
	})
	if fault != nil {
		return nil, fault
	}
	returned := len(page)
	if returned > l.page.Limit {
		details := []render.Pair{{Key: "limit", Value: intNode(l.page.Limit)}, {Key: "returned", Value: intNode(returned)}}
		message := "more " + l.plural + " arrived than the limit asked for"
		return nil, &diag.Fault{Code: diag.UpstreamInvalid, Message: message, Details: details}
	}
	found := counted(l.page.Skip + returned)
	if returned == l.page.Limit || returned == 0 && l.page.Skip > 0 {
		if found, fault = l.countTotal(ctx); fault != nil {
			return nil, fault
		}
		if found.known && returned > 0 && found.total < l.page.Skip+returned {
			details := []render.Pair{{Key: "total", Value: intNode(found.total)}, {Key: "returned", Value: intNode(returned)}}
			message := "fewer " + l.plural + " were counted than arrived: they changed between the requests"
			return nil, &diag.Fault{Code: diag.UpstreamFailed, Message: message, Details: details}
		}
	}
	return listDocument(l.plural, found, found.truncationAt(l.page.Skip+returned), page), nil
}

func countedListDocument(plural string, found count, records []*render.Node) *render.Node {
	return listDocument(plural, found, found.truncationAt(len(records)), records)
}

func truncatedListDocument(plural string, found count, left bool, records []*render.Node) *render.Node {
	return listDocument(plural, found, truncated(left), records)
}

// Счётчик null, когда неизвестен: полная страница счётного списка ничего не доказывает, а запись за
// лимитом — только обрезку.
func listDocument(plural string, found count, left truncation, records []*render.Node) *render.Node {
	return render.NewMap(append(counters(found, left, len(records)),
		render.Pair{Key: plural, Value: render.NewList(records...)})...)
}

func counters(found count, left truncation, returned int) []render.Pair {
	total, truncated := render.NewNull(), render.NewNull()
	if found.known {
		total = intNode(found.total)
	}
	if left.known {
		truncated = render.NewBool(left.left)
	}
	return []render.Pair{
		{Key: "total", Value: total},
		{Key: "returned", Value: intNode(returned)},
		{Key: "truncated", Value: truncated},
	}
}
