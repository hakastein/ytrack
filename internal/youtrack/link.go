package youtrack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const (
	linkSchema   = "IssueLink"
	linksKey     = "links"
	issuesKey    = "issues"
	directionKey = "direction"
	linkTypeKey  = "linkType"
	// How many issues the server holds in a slot. It arrives from every slot of every issue, of the dev
	// instance and of a live one alike, and the specification declares it nowhere.
	issuesSizeKey  = "issuesSize"
	sourceToTarget = "sourceToTarget"
	targetToSource = "targetToSource"
	// What the instance calls the two ends of a type in the language of its own interface; a type the instance
	// has no translation for sends null for one and an empty string for the other.
	localizedSourceToTarget = "localizedSourceToTarget"
	localizedTargetToSource = "localizedTargetToSource"
	// A directed type stands in two slots of an issue, one for each end it may be at; at the target end the
	// link is read from the target back to the source.
	inward  = "INWARD"
	outward = "OUTWARD"
	both    = "BOTH"
)

// LinkListFields is what an issue at the other end of a link is printed by unasked: which issue it is and what
// it is about. A user with no role on the project is sent both, so no key of the default costs a reader their
// document.
const LinkListFields = "idReadable,summary"

// The three names the specification declares an IssueLink at, which are the slots an issue holds its links in.
func linkSlotNames() []string {
	return []string{linksKey, "parent", "subtasks"}
}

// ListLinks is the call for the links of the issue of that id, with the fields of expression printed of every
// issue at their other ends, or with them added to LinkListFields when it starts with +; nil is the caller
// leaning on the default whole.
func ListLinks(id string, expression *string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := linkFields(spec, expression)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listLinks(ctx, spec, id, requested)
	}, nil
}

// linkFields is the expression of link list read into the tree the request carries it in: every issue printed
// stands at the other end of a link, so what the caller wrote goes under the slots of the issue asked for. That
// is where the rules an expression of issues is held to then find it, and it is why a custom field named there
// is refused — it would name a field of a project other than the one the name was read against.
func linkFields(spec *schemas, expression *string) ([]requestedField, *diag.Fault) {
	written := LinkListFields
	partner, fault := theDefault(LinkListFields, false)
	if expression != nil {
		written = *expression
		partner, fault = parseFields(written, LinkListFields)
	}
	if fault != nil {
		return nil, fault
	}
	requested := []requestedField{
		{name: linksKey, children: []requestedField{{name: issuesKey, children: partner}}},
	}
	if fault := issueComments().refuse(spec, written, requested, issueComments().commentsOfAList()); fault != nil {
		return nil, fault
	}
	if fault := refuseCustomFieldNames(spec, issueSchema, written, requested); fault != nil {
		return nil, fault
	}
	if fault := refuseLinkParts(spec, issueSchema, written, requested); fault != nil {
		return nil, fault
	}
	return requested, nil
}

// AddLink is the call that links the issue of id to the issue of partner under phrase, and prints the links of
// the first issue as they stand afterwards, with the fields of expression printed of every issue at the other
// end of one, or with them added to LinkListFields when it starts with +; nil is the caller leaning on the
// default whole.
func AddLink(id, phrase, partner string, expression *string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	partner, fault = parseIssueID(partner)
	if fault != nil {
		return nil, fault
	}
	if fault := usablePhrase(phrase); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := linkFields(spec, expression)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.linkAdded(ctx, spec, id, phrase, partner, requested)
	}, nil
}

// RemoveLink is the call that takes the link under phrase between the issue of id and the issue of partner
// away, and prints the link it took away. There is no expression: what is printed is the identity of that link
// and the issues at its ends are gone from one another by the time anything could be asked of them.
func RemoveLink(id, phrase, partner string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	partner, fault = parseIssueID(partner)
	if fault != nil {
		return nil, fault
	}
	if fault := usablePhrase(phrase); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.linkRemoved(ctx, spec, id, phrase, partner)
	}, nil
}

// A phrase is held against the phrases of the issue itself and never reaches YouTrack, so one that matches
// nothing there is to match is refused before the issue is read at all.
func usablePhrase(phrase string) *diag.Fault {
	switch {
	case phrase == "":
		return &diag.Fault{Code: diag.BadUsage, Message: emptyPhrase}
	case !utf8.ValidString(phrase):
		message := fmt.Sprintf("phrase %s is no valid UTF-8, and the phrases a link goes by are text", render.Quote(phrase))
		return &diag.Fault{Code: diag.BadUsage, Message: message}
	}
	return nil
}

const emptyPhrase = "the phrase is empty: a link goes by the phrase ytrack link list prints it under, as in " +
	`ytrack link add DEV-1 "depends on" DEV-2`

// The links of an issue come whole in one answer: the collection nested under fields= is not cut down, while
// the subresource of a slot is cut down to 42 and costs a request per slot. No request of them takes $skip, so
// the list takes no page and prints the whole, and what the counters are judged by is the count the server sends
// beside each slot.
func (c *Client) listLinks(ctx context.Context, spec *schemas, id string, requested []requestedField) (*render.Node, *diag.Fault) {
	asked := cloneFields(requested)
	issueBlocks(spec, composedIssue(), asked)
	for i := range asked {
		if asked[i].name == linksKey {
			asked[i].children = askedOfLinkDocument(asked[i].children, partnerFields(requested))
		}
	}
	answer, fault := c.request(ctx, spec, issueSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return nil, fault
	}
	// A partner is one record of the document, so its prose is written on the line of that record.
	return printing(answer, onOneLine).linkDocument(partnerFields(requested), answer.objects[0])
}

// linkDocument is the document the links of an issue print as, wherever the issue was read: what the server
// says its slots hold, how much of that was printed, and the block of phrases. link add prints it off the issue nested in the answer to its own write, so one command reads the
// same as the other. The issue itself is not in it: the caller named it, as they name the owner of every list.
func (n nodes) linkDocument(partner []requestedField, issue map[string]any) (*render.Node, *diag.Fault) {
	block, held, printed, fault := n.linkListing(partner, issue[linksKey])
	if fault != nil {
		return nil, fault
	}
	pairs := counters(held, held.beyond(printed), printed)
	return render.NewMap(append(pairs, render.Pair{Key: linksKey, Value: block})...), nil
}

// partnerBlocks is what goes out for an issue at the other end of a link: what the caller asked of it with the
// custom fields and the links of an issue filled in, since a partner is an issue and those blocks are read
// through a composition of the tool's own wherever one stands. link list fills them the same way, so one
// command asks for a partner exactly as the other does.
func partnerBlocks(spec *schemas, requested []requestedField) []requestedField {
	asked := []requestedField{{name: linksKey, children: []requestedField{
		{name: issuesKey, children: partnerFields(requested)},
	}}}
	issueBlocks(spec, composedIssue(), asked)
	return printedPartner(asked[0].children)
}

// partnerFields is what the caller asked of the issues at the other end, out of the tree the request carries.
func partnerFields(requested []requestedField) []requestedField {
	for _, field := range requested {
		if field.name == linksKey {
			return printedPartner(field.children)
		}
	}
	return nil
}

// linkListing is the block of phrases beside the two counts the document opens with: how many issues the
// server says it holds in the slots in all, and how many of them were printed. The total and what arrived are
// read off one answer, so a total short of what arrived is the answer contradicting itself rather than a
// collection that moved between two requests.
func (n nodes) linkListing(partner []requestedField, value any) (*render.Node, count, int, *diag.Fault) {
	slots, fault := n.linkSlots(value)
	if fault != nil {
		return nil, count{}, 0, fault
	}
	held := 0
	for _, slot := range slots {
		size, isCount := wholeNumber(slot[issuesSizeKey])
		if !isCount || size < 0 {
			return nil, count{}, 0, n.lied("how many issues a link of the issue holds is no whole number of them")
		}
		held += int(size)
	}
	block, arrived, printed, fault := n.linkBlock(partner, slots)
	if fault != nil {
		return nil, count{}, 0, fault
	}
	if held < arrived {
		message := fmt.Sprintf("the issues linked to arrived %d at a time and the links of the issue hold %d of "+
			"them in all", arrived, held)
		return nil, count{}, 0, n.lied(message)
	}
	return block, counted(held), printed, nil
}

// eachLinkSlot hands visit every link slot the caller wrote, wherever an issue stands below the schema of at.
func eachLinkSlot(spec *schemas, at string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	for _, name := range linkSlotNames() {
		namesAt(spec, at, issueSchema, name, requested, nil, visit)
	}
}

// A slot is printed as the phrase it goes by against the issues at its other end, so the issues are the only
// thing there is to ask of it: its id, its direction and its type are how YouTrack holds a link, not what the
// link is.
func refuseLinkParts(spec *schemas, at, expression string, requested []requestedField) *diag.Fault {
	var fault *diag.Fault
	eachLinkSlot(spec, at, requested, func(parents []string, field *requestedField) {
		for _, child := range field.children {
			if child.name == issuesKey || fault != nil {
				continue
			}
			message := fmt.Sprintf("fields %s: %s is printed as the phrase each link goes by against the issues "+
				"it holds, so %s is the only name that stands under it",
				render.Quote(expression), fieldPath(parents, field.name), issuesKey)
			fault = &diag.Fault{Code: diag.BadUsage, Message: message}
		}
	})
	return fault
}

// askedOfLink is what goes out for a slot: what the caller asked of the issues at its other end, and beside it
// the end this issue stands at with both phrases of the type, which together settle the one phrase to print.
func askedOfLink(callers []requestedField) []requestedField {
	return asking([]requestedField{{name: issuesKey, children: printedPartner(callers)}}, phraseOfALink()...)
}

// phraseOfALink is what the one phrase a slot prints under is read off: the end this issue stands at and both
// phrases of the type, since which of the two is the phrase follows the end.
func phraseOfALink() []requestedField {
	return []requestedField{
		{name: directionKey},
		{name: linkTypeKey, children: []requestedField{
			{name: sourceToTarget},
			{name: targetToSource},
		}},
	}
}

// askedOfLinkDocument is everything linkDocument prints a slot from, merged into what the tree asking for the
// slot already names: the phrase, the count the counters are judged by and the issues at the other end. link
// list asks it of the issue it read and link add of the issue nested in the answer to its write, so a member
// the printer needs reaches both documents or neither.
func askedOfLinkDocument(asked, partner []requestedField) []requestedField {
	own := append(phraseOfALink(),
		requestedField{name: issuesSizeKey},
		requestedField{name: issuesKey, children: partner})
	return asking(asked, own...)
}

// printedPartner is what the caller asked of the issues at the other end of a link. Asked nothing of them —
// neither under the slot nor under issues — a partner is printed by the id it is addressed by.
func printedPartner(callers []requestedField) []requestedField {
	for _, field := range callers {
		if field.name == issuesKey && field.children != nil {
			return cloneFields(field.children)
		}
	}
	return []requestedField{{name: idReadableKey}}
}

// links is the block a link slot of an issue prints as: the phrase the link goes by against the issues at
// its other end, in the order the server keeps the slots in. A slot holding no issue is no link at all and is
// left out, and the id the server addresses a slot by never reaches the document.
func (n nodes) links(field requestedField, value any) (*render.Node, *diag.Fault) {
	slots, fault := n.linkSlots(value)
	if fault != nil {
		return nil, fault
	}
	block, _, _, fault := n.linkBlock(printedPartner(field.children), slots)
	return block, fault
}

// linkBlock is that block, in the order the slots and their issues arrived, beside how many issues arrived and
// how many went into it, which is what link list counts what it printed by.
func (n nodes) linkBlock(partner []requestedField, slots []map[string]any) (*render.Node, int, int, *diag.Fault) {
	arrived, printed := 0, 0
	printedBy := make(map[string]bool, len(slots))
	pairs := make([]render.Pair, 0, len(slots))
	for _, slot := range slots {
		partners, fault := n.partners(slot)
		if fault != nil {
			return nil, 0, 0, fault
		}
		if len(partners) == 0 {
			continue
		}
		phrase, fault := n.phrase(slot)
		if fault != nil {
			return nil, 0, 0, fault
		}
		// Two slots of one phrase would print as one key, and which of them survived would be the renderer's
		// choice rather than anything the server said.
		if printedBy[phrase] {
			return nil, 0, 0, n.lied(fmt.Sprintf("two links of the issue go by the phrase %s", render.Quote(phrase)))
		}
		printedBy[phrase] = true
		arrived += len(partners)
		records, fault := n.objectsAt(issueSchema, partner, partners)
		if fault != nil {
			return nil, 0, 0, fault
		}
		printed += len(records)
		pairs = append(pairs, render.FromData(phrase, render.NewList(records...)))
	}
	return render.NewMap(pairs...), arrived, printed, nil
}

// A slot arrives as an array under links and as one object under parent and subtasks; a slot the server sent
// nothing for holds no issue either way, so it is no link rather than a lie.
func (n nodes) linkSlots(value any) ([]map[string]any, *diag.Fault) {
	arrived := []any{value}
	if list, isList := value.([]any); isList {
		arrived = list
	}
	slots := make([]map[string]any, 0, len(arrived))
	for _, item := range arrived {
		if item == nil {
			continue
		}
		slot, isObject := item.(map[string]any)
		if !isObject {
			return nil, n.lied("a link of the issue is not a JSON object")
		}
		slots = append(slots, slot)
	}
	return slots, nil
}

func (n nodes) partners(slot map[string]any) ([]map[string]any, *diag.Fault) {
	arrived, isList := slot[issuesKey].([]any)
	if !isList {
		return nil, n.lied("the issues of a link of the issue arrived as something other than an array")
	}
	partners := make([]map[string]any, 0, len(arrived))
	for _, item := range arrived {
		partner, isObject := item.(map[string]any)
		if !isObject {
			return nil, n.lied("an issue at the other end of a link of the issue is not a JSON object")
		}
		partners = append(partners, partner)
	}
	return partners, nil
}

// phrase is the one phrase a slot is read by: the end the issue stands at picks which of the two the type
// carries, and an undirected type leaves the other one empty.
func (n nodes) phrase(slot map[string]any) (string, *diag.Fault) {
	direction, isText := slot[directionKey].(string)
	if !isText {
		return "", n.lied("the direction of a link of the issue is not text")
	}
	kind, isObject := slot[linkTypeKey].(map[string]any)
	if !isObject {
		return "", n.lied("the type of a link of the issue is not a JSON object")
	}
	read := namesOfAnEnd(direction)[0]
	phrase, isText := kind[read].(string)
	if !isText {
		return "", n.lied(fmt.Sprintf("the %s of a link type of the issue is not text", read))
	}
	if phrase == "" {
		return "", n.lied("a link of the issue holds issues and the phrase it goes by is empty")
	}
	return phrase, nil
}

// The write goes out to the slot the pre-read of the issue gave, and only once the phrase has been settled
// against that same read: a phrase carries the end of the link it names, and a slot whose id disagrees with
// the end the server put it at would turn the link around under a 200.
func (c *Client) linkWriteOn(ctx context.Context, spec *schemas, id, phrase, partner string) (linkWrite, *diag.Fault) {
	source, fault := c.issueLinking(ctx, spec, id)
	if fault != nil {
		return linkWrite{}, fault
	}
	slot, fault := source.slotOf(phrase)
	if fault != nil {
		return linkWrite{}, fault
	}
	other, fault := c.issueLinked(ctx, spec, partner)
	if fault != nil {
		return linkWrite{}, fault
	}
	if other.id == source.id {
		return linkWrite{}, refusingLink(other.a, source.readable, diag.BadUsage, oneIssue,
			render.Pair{Key: "partner", Value: render.NewString(other.readable)})
	}
	return linkWrite{source: source, partner: other, slot: slot}, nil
}

func (c *Client) linkAdded(ctx context.Context, spec *schemas, id, phrase, partner string, requested []requestedField) (*render.Node, *diag.Fault) {
	w, fault := c.linkWriteOn(ctx, spec, id, phrase, partner)
	if fault != nil {
		return nil, fault
	}
	// Marshalling a struct of one string cannot fail.
	body, _ := json.Marshal(linkedPartner{ID: w.partner.id})
	node, fault := c.write(ctx, spec, issueSchema, writtenLinkFields(partnerBlocks(spec, requested)),
		func(ctx context.Context, fields string) (*http.Response, error) {
			return c.addLinkedIssue(ctx, w.source.readable, w.slot.id, body, fields)
		}, w.confirmedBy, w.documenting(partnerFields(requested)))
	if fault != nil {
		return nil, w.named(fault)
	}
	return node, nil
}

// A removal is settled by the same read a write is, and nothing of it is checked beforehand: the server
// answers a link it holds none of with a 404 instead of a silent 200, so the status is the whole of the
// judgment and there is nothing left to read back.
func (c *Client) linkRemoved(ctx context.Context, spec *schemas, id, phrase, partner string) (*render.Node, *diag.Fault) {
	w, fault := c.linkWriteOn(ctx, spec, id, phrase, partner)
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.removeLinkedIssue(ctx, w.source.readable, w.slot.id, w.partner.id)
	}); fault != nil {
		return nil, w.named(noSuchLink(fault))
	}
	return w.removed(), nil
}

// What the server writes under that 404 names the partner, an issue that is there, so the message says what is
// not: the link. The text it sent stands beside it in upstream_message word for word.
func noSuchLink(fault *diag.Fault) *diag.Fault {
	if fault.Code == diag.NotFound {
		fault.Message = noLinkToRemove
	}
	return fault
}

const noLinkToRemove = "the issue holds no link under that phrase to the partner, and a link is taken away " +
	"from the end the phrase names"

const oneIssue = "the issue and the partner are one issue, and YouTrack answers a link of an issue to itself " +
	"with a 200 and writes nothing"

// The body of the write. The partner is addressed by the internal id the read before it gave: YouTrack answers
// an id of {"id": "DEV-15"} with 400 For input string: "DEV", and the readable id it does take goes under
// another name, so one form goes out and it is the one every instance takes.
type linkedPartner struct {
	ID string `json:"id"`
}

// An issue a link command read before it wrote anything: the ids the write is addressed by and, where the read
// asked for them, the slots the server holds the links of that issue in.
type linkedIssue struct {
	a        answer
	id       string
	readable string
	slots    []linkSlot
}

// A slot of an issue as the catalogue reads it: the id the server addresses it by, the end the issue stands at,
// the type it is of, the phrase link list prints it under and every phrase it answers to, that one first.
type linkSlot struct {
	id        string
	direction string
	kind      string
	phrase    string
	names     []string
}

// What the read before a write asks of the issue it writes on: the ids it is addressed by and, of every slot,
// the id it goes by, the end the issue stands at and every phrase its type answers to. One request settles the
// catalogue as the server applies it to this very issue, so /api/issueLinkTypes is never called and no slot id
// is ever composed out of a type and a suffix.
func linkCatalogueFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: idReadableKey},
		{name: linksKey, children: []requestedField{
			{name: idKey},
			{name: directionKey},
			{name: linkTypeKey, children: []requestedField{
				{name: idKey},
				{name: sourceToTarget},
				{name: targetToSource},
				{name: localizedSourceToTarget},
				{name: localizedTargetToSource},
			}},
		}},
	}
}

func (c *Client) issueLinking(ctx context.Context, spec *schemas, id string) (linkedIssue, *diag.Fault) {
	read, fault := c.readLinkedIssue(ctx, spec, id, linkCatalogueFields())
	if fault != nil {
		return linkedIssue{}, fault
	}
	slots, fault := printing(read.a, onOneLine).linkCatalogue(read.a.objects[0][linksKey])
	if fault != nil {
		return linkedIssue{}, fault
	}
	read.slots = slots
	return read, nil
}

// The issue at the other end is read for its internal id and for nothing else: the server would answer a body
// naming an issue it has none of with a 400 of three different texts, one of which sends the caller looking for
// a field they never wrote.
func (c *Client) issueLinked(ctx context.Context, spec *schemas, id string) (linkedIssue, *diag.Fault) {
	return c.readLinkedIssue(ctx, spec, id, []requestedField{{name: idKey}, {name: idReadableKey}})
}

func (c *Client) readLinkedIssue(ctx context.Context, spec *schemas, id string, requested []requestedField) (linkedIssue, *diag.Fault) {
	a, fault := c.request(ctx, spec, issueSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return linkedIssue{}, fault
	}
	readable, fault := addressedIn(a, a.objects[0], issueOwner, "a link")
	if fault != nil {
		return linkedIssue{}, fault
	}
	internal, isText := a.objects[0][idKey].(string)
	if !isText || !isInternalID(internal) {
		message := "the issue arrived with something other than an internal id of the instance for an id, and " +
			"that is what YouTrack takes an issue at the other end of a link by"
		return linkedIssue{}, shapeFailure(a.response, a.body, message)
	}
	return linkedIssue{a: a, id: internal, readable: readable.String()}, nil
}

func (n nodes) linkCatalogue(value any) ([]linkSlot, *diag.Fault) {
	arrived, fault := n.heldLinks(value)
	if fault != nil {
		return nil, fault
	}
	slots := make([]linkSlot, 0, len(arrived))
	for _, link := range arrived {
		id, isText := link.held[idKey].(string)
		if !isText {
			return nil, n.lied("the id of a link of the issue is not text")
		}
		phrase, named, fault := n.slotNames(link.direction, link.kind)
		if fault != nil {
			return nil, fault
		}
		slots = append(slots, linkSlot{id: id, direction: link.direction, kind: link.typeID, phrase: phrase, names: named})
	}
	return slots, nil
}

// A link of an issue as far as both readers of one read it alike: the end the issue stands at and the type,
// which is the type's own id beside the whole of what the server sent for it.
type heldLink struct {
	held      map[string]any
	direction string
	kind      map[string]any
	typeID    string
}

// heldLinks is the slots of an issue read that far, wherever they arrived: the catalogue a phrase is resolved
// against and the answer to a write hold a link in one shape, so a lie about that shape is one refusal.
func (n nodes) heldLinks(value any) ([]heldLink, *diag.Fault) {
	arrived, fault := n.linkSlots(value)
	if fault != nil {
		return nil, fault
	}
	links := make([]heldLink, 0, len(arrived))
	for _, held := range arrived {
		direction, isEnd := held[directionKey].(string)
		if !isEnd {
			return nil, n.lied("the direction of a link of the issue is not text")
		}
		kind, isObject := held[linkTypeKey].(map[string]any)
		if !isObject {
			return nil, n.lied("the type of a link of the issue is not a JSON object")
		}
		typeID, isText := kind[idKey].(string)
		if !isText {
			return nil, n.lied("the id of a link type of the issue is not text")
		}
		links = append(links, heldLink{held: held, direction: direction, kind: kind, typeID: typeID})
	}
	return links, nil
}

// slotNames is the phrase a slot is printed under, which is the one a refusal names as well, and after it every
// other name the slot answers to. A name the server sent empty or null is no name at all — it would answer to a
// caller who wrote nothing.
func (n nodes) slotNames(direction string, kind map[string]any) (string, []string, *diag.Fault) {
	read := namesOfAnEnd(direction)
	phrase := ""
	var named []string
	for i, name := range read {
		text, isText := kind[name].(string)
		if !isText && kind[name] != nil {
			return "", nil, n.lied(fmt.Sprintf("the %s of a link type of the issue is neither text nor null", name))
		}
		if i == 0 {
			phrase = text
		}
		if text != "" {
			named = append(named, text)
		}
	}
	return phrase, named, nil
}

// namesOfAnEnd is which of the names a type carries the end the issue stands at is read by, the phrase the slot
// is printed under first: an undirected type is read from either side, and a direction of neither end — one a
// later YouTrack may send — from the side a type is written from.
func namesOfAnEnd(direction string) []string {
	switch direction {
	case inward:
		return []string{targetToSource, localizedTargetToSource}
	case both:
		return []string{sourceToTarget, localizedSourceToTarget, targetToSource, localizedTargetToSource}
	}
	return []string{sourceToTarget, localizedSourceToTarget}
}

// slotOf is the slot the phrase names, resolved against the slots of this very issue and against nothing else,
// and held to the grammar of a slot id before anything goes out.
func (s linkedIssue) slotOf(phrase string) (linkSlot, *diag.Fault) {
	named := s.answering(phrase)
	if twin, alike := twoOfAPhrase(named); alike {
		message := fmt.Sprintf("two links of the issue go by the phrase %s, and neither of them can be named "+
			"by it", render.Quote(twin))
		return linkSlot{}, s.refusing(diag.UpstreamLied, message,
			render.Pair{Key: "phrase", Value: render.NewString(twin)})
	}
	slot, found := theOneSlot(named, phrase)
	if !found {
		return linkSlot{}, s.unknownPhrase(phrase, named)
	}
	if !slot.addressable() {
		return linkSlot{}, s.refusing(diag.UpstreamLied, unreadableSlot,
			render.Pair{Key: "phrase", Value: render.NewString(slot.phrase)})
	}
	return slot, nil
}

// answering is the slots the phrase answers to, letter case aside and nothing else: spaces are not trimmed, ё
// is no е and a Latin c is no Cyrillic с, since the phrase that goes into the document is the server's own and
// a caller who copied it back gets it back.
func (s linkedIssue) answering(phrase string) []linkSlot {
	var named []linkSlot
	for _, slot := range s.slots {
		if slot.phrase == "" {
			continue
		}
		if slices.ContainsFunc(slot.names, func(name string) bool { return strings.EqualFold(name, phrase) }) {
			named = append(named, slot)
		}
	}
	return named
}

// Where a phrase answers to more than one slot — a translation of one end written the way another end is
// spelled — the slot whose own phrase was written byte for byte wins, and nothing else does: the two are then
// the caller's to tell apart.
func theOneSlot(named []linkSlot, phrase string) (linkSlot, bool) {
	if len(named) == 1 {
		return named[0], true
	}
	for _, slot := range named {
		if slot.phrase == phrase {
			return slot, true
		}
	}
	return linkSlot{}, false
}

// Two of the slots the caller's phrase answered to printed under one phrase themselves are two links that
// phrase names neither of: fixing the spelling would reach neither, and which of them a write reached would be
// ytrack's choice rather than anything the server said. Twins under a phrase nobody wrote are that phrase's
// own business, and the slots this one named are written as they always were.
func twoOfAPhrase(named []linkSlot) (string, bool) {
	seen := make(map[string]bool, len(named))
	for _, slot := range named {
		if seen[slot.phrase] {
			return slot.phrase, true
		}
		seen[slot.phrase] = true
	}
	return "", false
}

// The phrases nearest what the caller wrote are measured against every name a slot answers to, translations
// among them, and shown as the phrase link list prints: a caller shown a translation would write it back and be
// answered the same way. Where the phrase is near none of them, every phrase the issue has is shown.
func (s linkedIssue) unknownPhrase(phrase string, named []linkSlot) *diag.Fault {
	nearby := canonicalPhrases(named)
	if len(named) == 0 {
		among := make([]suggestion, 0, len(s.slots))
		for _, slot := range s.slots {
			if slot.phrase != "" {
				among = append(among, suggestion{name: slot.phrase, also: slot.names[1:]})
			}
		}
		nearby = nearest(phrase, among, canonicalPhrases(s.slots))
	}
	entry := render.NewMap(
		render.Pair{Key: "phrase", Value: render.NewString(phrase)},
		render.Pair{Key: "nearest", Value: render.NewList(names(nearby)...)})
	return s.refusing(diag.UnknownName, unknownPhrase, render.Pair{Key: "unknown", Value: render.NewList(entry)})
}

const unknownPhrase = "the phrase under unknown is no phrase a link of the issue goes by"

// canonicalPhrases is the phrase each slot is printed under, by code point: a list of names is read as a
// listing, and the order the server keeps its slots in is nobody's.
func canonicalPhrases(slots []linkSlot) []string {
	phrases := make([]string, 0, len(slots))
	for _, slot := range slots {
		if slot.phrase != "" {
			phrases = append(phrases, slot.phrase)
		}
	}
	slices.Sort(phrases)
	return phrases
}

// The suffix of a slot id says which end of the type it is, and YouTrack reads an id without one as the inward
// end on a GET, a POST and a DELETE alike: an id whose suffix disagrees with the end the answer put it at would
// write the link the other way round without saying so. The specification declares the grammar nowhere, so it
// is held to here and closed on the instance by the contract tests.
func (slot linkSlot) addressable() bool {
	number, rest, dashed := strings.Cut(slot.id, "-")
	if !dashed || !digits(number) {
		return false
	}
	switch slot.direction {
	case both:
		return digits(rest)
	case outward:
		return strings.HasSuffix(rest, "s") && digits(strings.TrimSuffix(rest, "s"))
	case inward:
		return strings.HasSuffix(rest, "t") && digits(strings.TrimSuffix(rest, "t"))
	}
	return false
}

const unreadableSlot = "the server addresses the link by an id ytrack cannot read the end of: a link an issue " +
	"stands at either end of is addressed by digits, a dash and digits, one it stands at the source of by the " +
	"same and an s, and one it stands at the target of by the same and a t"

// A refusal a link command settled before it wrote anything names the read that settled it, since that is the
// only request that went out, and the issue the phrases were read off. The id the server addresses a slot by
// and the internal id of a partner stand in request and in no key that names anything.
func refusingLink(a answer, readable string, code diag.Code, message string, own ...render.Pair) *diag.Fault {
	details := []render.Pair{
		requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted()),
		{Key: "issue", Value: render.NewString(readable)},
	}
	return &diag.Fault{Code: code, Message: message, Details: append(details, own...)}
}

func (s linkedIssue) refusing(code diag.Code, message string, own ...render.Pair) *diag.Fault {
	return refusingLink(s.a, s.readable, code, message, own...)
}

// A write of a link, held together from the read that settled it: the two issues by the ids each of them is
// named and addressed by, and the slot the write goes to.
type linkWrite struct {
	source  linkedIssue
	partner linkedIssue
	slot    linkSlot
}

// What the answer to the write carries: the partner, the slot of the partner the issue now stands in, and
// inside it the issue with the whole of its own links. Both ends of the one answer are read — the partner holds
// the issue at the end opposite the phrase, the issue holds the partner at the end of the phrase — and the
// issue's own slots are what the document is printed from, so a write shows the state it left behind, an ousted
// parent and duplicates carried over among it.
func writtenLinkFields(partner []requestedField) []requestedField {
	source := []requestedField{
		{name: idKey},
		{name: linksKey, children: askedOfLinkDocument(askedOfAnsweredSlot(),
			asking([]requestedField{{name: idKey}}, partner...))},
	}
	return []requestedField{
		{name: idKey},
		{name: linksKey, children: append(askedOfAnsweredSlot(),
			requestedField{name: issuesKey, children: source})},
	}
}

// askedOfAnsweredSlot is what a slot of the answer to a write is read by at either end: the type and the end,
// which together settle which link the slot is (answeredSlots).
func askedOfAnsweredSlot() []requestedField {
	return []requestedField{
		{name: directionKey},
		{name: linkTypeKey, children: []requestedField{{name: idKey}}},
	}
}

// A 200 says YouTrack took the body, not that it wrote the link the phrase named: a link written the other way
// round comes back under a 200 as well, and the one answer holds both ends to tell them apart.
func (w linkWrite) confirmedBy(a answer) *diag.Fault {
	source, fault := w.sourceIn(a)
	if fault != nil {
		return fault
	}
	slots, fault := printing(a, onOneLine).answeredSlots(source[linksKey])
	if fault != nil {
		return fault
	}
	if _, held := issueIn(slots, w.slot.kind, w.slot.direction, w.partner.id); !held {
		return liedAboutTheWrite(a, "the issue does not hold the partner at the end of the link the phrase names")
	}
	return nil
}

// sourceIn is the issue the call named first, as the answer to the write carries it: under the slot of the
// partner that stands at the end opposite the phrase.
func (w linkWrite) sourceIn(a answer) (map[string]any, *diag.Fault) {
	if id, isText := a.objects[0][idKey].(string); !isText || id != w.partner.id {
		return nil, liedAboutTheWrite(a, "the write was answered with an issue other than the partner it named")
	}
	slots, fault := printing(a, onOneLine).answeredSlots(a.objects[0][linksKey])
	if fault != nil {
		return nil, fault
	}
	source, held := issueIn(slots, w.slot.kind, opposite(w.slot.direction), w.source.id)
	if !held {
		return nil, liedAboutTheWrite(a, "the partner does not hold the issue at the end the other side of the link is read from")
	}
	return source, nil
}

// A removal is answered with nothing, so what it prints is the identity of the link it took away, read off the
// two issues before it went: the shape is the one link list prints links in, under a key of its own, since a
// reader who took removed for links would read a link that is gone as one the issue still holds.
func (w linkWrite) removed() *render.Node {
	record := render.NewMap(render.Pair{Key: idReadableKey, Value: render.NewString(w.partner.readable)})
	return render.NewMap(
		render.Pair{Key: idReadableKey, Value: render.NewString(w.source.readable)},
		render.Pair{Key: removedKey, Value: render.NewMap(
			render.FromData(w.slot.phrase, render.NewList(record)))})
}

// documenting is how the answer to the write prints: the issue the call named first, as that answer carries
// it, as the document link list prints by what the caller asked of the issues at the other end.
func (w linkWrite) documenting(printed []requestedField) func(answer) (*render.Node, *diag.Fault) {
	return func(a answer) (*render.Node, *diag.Fault) {
		source, fault := w.sourceIn(a)
		if fault != nil {
			return nil, fault
		}
		return printing(a, onOneLine).linkDocument(printed, source)
	}
}

// A slot of the answer to a write: the type and the end settle which link it is, and the issues are where the
// two ends of that link are looked for.
type answeredSlot struct {
	direction string
	kind      string
	issues    []map[string]any
}

func (n nodes) answeredSlots(value any) ([]answeredSlot, *diag.Fault) {
	arrived, fault := n.heldLinks(value)
	if fault != nil {
		return nil, fault
	}
	slots := make([]answeredSlot, 0, len(arrived))
	for _, link := range arrived {
		issues, fault := n.partners(link.held)
		if fault != nil {
			return nil, fault
		}
		slots = append(slots, answeredSlot{direction: link.direction, kind: link.typeID, issues: issues})
	}
	return slots, nil
}

func issueIn(slots []answeredSlot, kind, direction, id string) (map[string]any, bool) {
	for _, slot := range slots {
		if slot.kind != kind || slot.direction != direction {
			continue
		}
		for _, issue := range slot.issues {
			if held, isText := issue[idKey].(string); isText && held == id {
				return issue, true
			}
		}
	}
	return nil, false
}

// The link an issue stands at the source of is the one its partner stands at the target of. An undirected type
// has one end, read the same from either side.
func opposite(direction string) string {
	switch direction {
	case inward:
		return outward
	case outward:
		return inward
	}
	return direction
}

// What the answer left behind is the server's word by now, so the refusal names the request rather than sending
// anything else to find out. Nothing of the write itself stands here: the issue, the phrase and the
// partner are put into every refusal of a link command by named.
func liedAboutTheWrite(a answer, message string) *diag.Fault {
	details := []render.Pair{requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted())}
	return &diag.Fault{Code: diag.UpstreamLied, Message: message, Details: details}
}

// named is the refusal about the write with the three names that mean the same on any instance put right after
// the request: request and upstream_* carry the id the server addresses the slot by and the internal id of the
// partner word for word, and a caller reads what to write again beside them.
func (w linkWrite) named(fault *diag.Fault) *diag.Fault {
	at := 0
	if len(fault.Details) > 0 && fault.Details[0].Key == "request" {
		at = 1
	}
	fault.Details = slices.Insert(fault.Details, at,
		render.Pair{Key: "issue", Value: render.NewString(w.source.readable)},
		render.Pair{Key: "phrase", Value: render.NewString(w.slot.phrase)},
		render.Pair{Key: "partner", Value: render.NewString(w.partner.readable)})
	return fault
}
