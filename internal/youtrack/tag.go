package youtrack

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// What a tag holds where the caller writes no expression of their own: the name it is known by, the login of
// the user it belongs to — a name is an identity only in that pair, since a tag shared later carries the name
// of one already there — and the two sets the server keeps its readers in. visibleFor stands nowhere in it:
// it carries one group, so a tag shared with several prints one of them and a tag shared with a person alone
// prints null.
//
// Every key of it arrives under a 200 for a member's token as well as an admin's, measured against the
// polygon on the tags each of them owns, and the member is answered readSharingSettings about a tag of
// the admin's shared with a group they stand in. The other two sets arrive there as well, measured the same
// way, and stay out of this default all the same: a list is a line per tag and carries the one set that says
// who is shown it, while what the three hold about one tag is printed by the creation that wrote them.
const TagListFields = "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login))"

// What a creation prints: the whole of the list's default and beside it the two other sets, the one that says
// who may rename the tag and share it further and the one that says who may hang it. A list of tags would be
// two lines each the longer for them, while a creation prints one tag, and the caller who has just made it is
// the one who may want to see who it was shared with — the more so since --taggable-by writes the third set and
// nothing else in the document would say that it took.
//
// Every key of it arrives under a 200 for a member's token as well as an admin's, measured against the polygon
// on the tags each of them owns; the tag a creation prints is the caller's own either way.
const TagCreateFields = TagListFields +
	",updateSharingSettings(permittedGroups(name),permittedUsers(login))" +
	",tagSharingSettings(permittedGroups(name),permittedUsers(login))"

const (
	tagsPlural = "tags"
	tagSchema  = "Tag"
	// The word a refusal names the tag by, which is the word a caller writes it with. The other two are the
	// members of the answer the resolver reads.
	tagKey   = "tag"
	ownerKey = "owner"
	// The word a refusal names the login the caller narrowed the name down by, spelled as the flag that carries
	// it: the owner beside each candidate is the tag's, and this one is the caller's.
	ownedByKey = "owned_by"
)

const (
	groupSchema = "UserGroup"
	// The word a refusal names a group by, and the three members of a tag the sharing flags write.
	groupKey           = "group"
	readSharingKey     = "readSharingSettings"
	updateSharingKey   = "updateSharingSettings"
	tagSharingKey      = "tagSharingSettings"
	permittedGroupsKey = "permittedGroups"
	// The flags whose values are group names, spelled as a refusal about one of them names it.
	visibleForFlag   = "--visible-for"
	updateableByFlag = "--updateable-by"
	taggableByFlag   = "--taggable-by"
)

// ListTags is the call for one page of the tags the token is shown, with the fields of expression, or
// with them added to TagListFields when it starts with +; nil is the caller leaning on the default whole.
func ListTags(expression *string, page Page) (Call, *diag.Fault) {
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	requested, fault := tagFields(expression, TagListFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listTags(ctx, spec, requested, page)
	}, nil
}

// tagFields is an expression of a command that prints tags. A nil expression is the caller leaning on the
// default whole, and then nothing in the tree is theirs to answer for.
func tagFields(expression *string, defaults string) ([]requestedField, *diag.Fault) {
	if expression == nil {
		return theDefault(defaults, false)
	}
	return parseFields(*expression, defaults)
}

// The tags of a token are one collection and the API answers it in one request, so the machinery of every
// other list of the tool holds here unchanged: the page goes out with $top, and the whole is read off a
// second pass over ids alone where the page fills the limit. Nothing counts tags for the server — /api/tags
// carries no counter and no search that could be counted instead.
func (c *Client) listTags(ctx context.Context, spec *schemas, requested []requestedField, page Page) (*render.Node, *diag.Fault) {
	return c.selection(ctx, spec, tagsPlural, "[]"+tagSchema, requested, page, c.getTags)
}

// CreateTag is the call that makes a tag of that name, owned by whoever the token belongs to, shared with the
// groups of shared, and prints it with the fields of expression, or with them added to TagCreateFields when it
// starts with +; nil is the caller leaning on the default whole. A tag named no group at all is personal.
func CreateTag(name string, shared TagSharing, expression *string) (Call, *diag.Fault) {
	if fault := refuseNoTagName(name); fault != nil {
		return nil, fault
	}
	if fault := shared.refuseNoGroupName(); fault != nil {
		return nil, fault
	}
	requested, fault := tagFields(expression, TagCreateFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.tagCreated(ctx, spec, name, shared, requested)
	}, nil
}

// TagSharing is the three sets of sharing a creation writes, as the caller wrote them. Nothing here is
// resolved: the catalogue of groups is read only where a flag names one, and that is a question for the call
// rather than for the arguments.
//
// The three go together from the flag that wrote one to the member of a tag it is written into, and never as
// three arguments beside each other: all three hold the names of groups and nothing else, so only the name and
// the type tell one from another. A set standing where another belongs is a compile error here rather than a
// group granted a right nobody gave it.
type TagSharing struct {
	ShownTo   ShownTo
	ChangedBy ChangedBy
	Tagging   Tagging
}

// The three rights YouTrack keeps apart, each the value of the flag that writes it. None of them is the member
// visibleFor of a tag, which carries one group and answers for no right at all.
type (
	// ShownTo is what --visible-for wrote: the groups that are shown the tag.
	ShownTo []string
	// ChangedBy is what --updateable-by wrote: the groups that may rename the tag and share it further.
	ChangedBy []string
	// Tagging is what --taggable-by wrote: the groups that may hang the tag on issues and articles.
	Tagging []string
)

// A group is addressed by its name, and YouTrack keeps none under an empty one, so a flag given nothing names
// no group there could be — refused before anything is sent, like an empty name of a tag.
func (shared TagSharing) refuseNoGroupName() *diag.Fault {
	for _, flag := range []struct {
		name   string
		values []string
	}{
		{name: visibleForFlag, values: shared.ShownTo},
		{name: updateableByFlag, values: shared.ChangedBy},
		{name: taggableByFlag, values: shared.Tagging},
	} {
		if slices.Contains(flag.values, "") {
			message := flag.name + " is empty, and YouTrack keeps no group under an empty name: it takes the " +
				"name of a group the tag is shared with"
			return &diag.Fault{Code: diag.BadUsage, Message: message}
		}
	}
	return nil
}

func (shared TagSharing) any() bool {
	return len(shared.ShownTo) > 0 || len(shared.ChangedBy) > 0 || len(shared.Tagging) > 0
}

// The creation is one POST where the call shares the tag with nobody: a name another tag of the owner already
// carries is the server's own refusal, and the answer carries the tag the write made, so nothing is read back
// either. A call that names a group costs the catalogue of groups before it and nothing more.
func (c *Client) tagCreated(ctx context.Context, spec *schemas, name string, shared TagSharing, requested []requestedField) (*render.Node, *diag.Fault) {
	written, fault := c.tagToWrite(ctx, spec, name, shared)
	if fault != nil {
		return nil, fault
	}
	body := written.body()
	return c.writing(ctx, spec, tagSchema, asking(requested, written.checked()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.createTag(ctx, body, fields)
	}, written.confirmedBy, writtenNode(requested))
}

// Whether the groups are read turns on the flags the call was given and never on what the instance holds: a
// creation of a personal tag costs the one request it always cost, and a token that may not read groups at all
// is refused only where the call asked for a group.
func (c *Client) tagToWrite(ctx context.Context, spec *schemas, name string, shared TagSharing) (tagWritten, *diag.Fault) {
	if !shared.any() {
		return tagWritten{name: name}, nil
	}
	catalogue, fault := c.groupsShown(ctx, spec)
	if fault != nil {
		return tagWritten{}, fault
	}
	groups, fault := catalogue.resolving(shared)
	if fault != nil {
		return tagWritten{}, fault
	}
	return tagWritten{name: name, groups: groups}, nil
}

// The whole of what a creation carries, with every group settled by the read before the write: the body, what
// the answer is read for and the check of it are built from it and from nothing else. tagToWrite is the one
// place that makes one, which is what holds the reading of the groups before the write in place of an order
// kept by hand.
type tagWritten struct {
	name string
	// The three sets as they arrived from the flags, still under their own names: a set is nil where the flag
	// was not written at all — that is the tag YouTrack shares as it pleases rather than an empty set the call
	// asked for, so the member stays out of the body.
	groups sharingWritten
}

// The body of a creation: the name and the sets the call named. No $type stands here — the server takes the tag
// without one — and no id either, which is the server's to give.
//
// The three sets go under their own members although the specification marks all three read-only: the server
// writes them, and it is visibleFor that cannot say what the flags say, carrying one group where a tag is
// shared with several and null where it is shared with a person alone. Hanging a tag on an issue is a right of
// tagSharingSettings and of no other member — being shown a tag grants none of it.
type createdTag struct {
	Name                  string            `json:"name"`
	ReadSharingSettings   *sharedWithGroups `json:"readSharingSettings,omitempty"`
	UpdateSharingSettings *sharedWithGroups `json:"updateSharingSettings,omitempty"`
	TagSharingSettings    *sharedWithGroups `json:"tagSharingSettings,omitempty"`
}

// One set of sharing as a write carries it: the groups alone, each by the internal id, which is the one form
// the member takes — a group under {name} there is answered 400 To find an entity of type UserGroup, specify
// its ID. The people of a set are no part of what the flags write, and a member left out is one the server
// settles itself.
type sharedWithGroups struct {
	PermittedGroups []addressedGroup `json:"permittedGroups"`
}

type addressedGroup struct {
	ID string `json:"id"`
}

// Marshalling strings and structs of them cannot fail.
func (w tagWritten) body() []byte {
	body, _ := json.Marshal(createdTag{
		Name:                  w.name,
		ReadSharingSettings:   sharingOf(w.groups.shownTo),
		UpdateSharingSettings: sharingOf(w.groups.changedBy),
		TagSharingSettings:    sharingOf(w.groups.tagging),
	})
	return body
}

func sharingOf(groups []groupID) *sharedWithGroups {
	if groups == nil {
		return nil
	}
	permitted := make([]addressedGroup, 0, len(groups))
	for _, group := range groups {
		permitted = append(permitted, addressedGroup{ID: group.id})
	}
	return &sharedWithGroups{PermittedGroups: permitted}
}

// checked is what the answer to the write is read for beside what the caller asked to print: the name that went
// out and the ids of each set that did, so the check has them to compare. The ids are asked for although a
// record prints the names — the body addressed the groups by id, and that is what the answer is held to. No id
// of the tag is asked for the way a creation of an issue asks for one: the internal id of a tag is no address a
// caller may write, so a refusal names the tag by the name it was written under.
func (w tagWritten) checked() []requestedField {
	own := []requestedField{{name: nameKey}}
	if w.groups.shownTo != nil {
		own = append(own, sharingChecked(readSharingKey))
	}
	if w.groups.changedBy != nil {
		own = append(own, sharingChecked(updateSharingKey))
	}
	if w.groups.tagging != nil {
		own = append(own, sharingChecked(tagSharingKey))
	}
	return own
}

func sharingChecked(set string) requestedField {
	return requestedField{name: set, children: []requestedField{
		{name: permittedGroupsKey, children: []requestedField{{name: idKey}}},
	}}
}

// confirmedBy holds the answer against what the write sent: a 200 says the server took the body, not that what
// it kept is what went out. The runes YouTrack is measured to cut off a name are refused before anything
// is sent, so a name that comes back another is something nobody has measured, and the tag that by then exists
// carries it.
func (w tagWritten) confirmedBy(a answer) *diag.Fault {
	tag := a.objects[0]
	wrong := textMismatch(nil, nameKey, w.name, tag[nameKey])
	wrong = sharingMismatch(wrong, readSharingKey, w.groups.shownTo, tag[readSharingKey])
	wrong = sharingMismatch(wrong, updateSharingKey, w.groups.changedBy, tag[updateSharingKey])
	wrong = sharingMismatch(wrong, tagSharingKey, w.groups.tagging, tag[tagSharingKey])
	if len(wrong) == 0 {
		return nil
	}
	return rewrittenByTheServer(a, knownAs(tagKey, render.NewString(w.name)), wrong)
}

// A set is held to the ids that went out and to nothing else: a set the call never named is one the server
// keeps as it pleases, and the order of the one it named is the server's own, so the two are
// held together as sets. A group missing from what came back, or one standing there that nobody wrote, is the
// write having come out other than it went — over a tag that by then exists.
func sharingMismatch(wrong []mismatch, set string, written []groupID, value any) []mismatch {
	if written == nil {
		return wrong
	}
	sent := make([]string, 0, len(written))
	for _, group := range written {
		sent = append(sent, group.id)
	}
	permitted := memberOf(value, permittedGroupsKey)
	arrived, isGroups := groupIDsOf(permitted)
	if isGroups && sameIDs(sent, arrived) {
		return wrong
	}
	held := asArrived(permitted)
	if isGroups {
		held = textList(arrived)
	}
	return append(wrong, mismatch{field: set + "." + permittedGroupsKey, written: textList(sent), arrived: held})
}

// The judgment of names says permittedGroups arrived, not that every group in it carries an id that is text.
func groupIDsOf(value any) ([]string, bool) {
	items, isList := value.([]any)
	if !isList {
		return nil, false
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		id, isText := memberOf(item, idKey).(string)
		if !isText {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func sameIDs(sent, arrived []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(sent)), slices.Sorted(slices.Values(arrived)))
}

// DeleteTag is the call that destroys the one tag of that name the token is shown, owned by the login of
// ownedBy or by whoever owns it where that is nil, and prints the name and the owner of the tag it destroyed.
func DeleteTag(name string, ownedBy *string) (Call, *diag.Fault) {
	sought, fault := theTagSought(name, ownedBy)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.tagDeleted(ctx, spec, sought)
	}, nil
}

// tagSought is what a verb of one tag was given to find it by: the name it is addressed by, and the login of
// its owner where --owned-by narrowed the name down — the pair is what tells one tag from another.
type tagSought struct {
	name string
	// Empty where the flag was never written: a login of no characters is refused before anything is sent.
	owner string
}

// A name no tag could carry and a login of no characters are refused for the same reason: neither names
// anything the instance holds, and an empty login narrows the name down to nothing.
//
// Nothing else about the login is judged, and parseLogin is not the judge of it: that one refuses the forms
// the server would read as something other than the login it was given, and this login reaches no endpoint at
// all. It is matched against the owners of the catalogue the resolver reads either way, so a login no user
// carries is answered there with the owners the name does answer to, each spelled as a login is — which is
// the same answer a name shaped like an internal id gets, and for the same reason.
func theTagSought(name string, ownedBy *string) (tagSought, *diag.Fault) {
	if fault := refuseNoTagName(name); fault != nil {
		return tagSought{}, fault
	}
	if ownedBy == nil {
		return tagSought{name: name}, nil
	}
	if *ownedBy == "" {
		return tagSought{}, &diag.Fault{Code: diag.BadUsage, Message: emptyTagOwner}
	}
	return tagSought{name: name, owner: *ownedBy}, nil
}

// A name no tag of YouTrack could carry is refused before anything is sent: it keeps none under an empty name,
// none under bytes that are no UTF-8, and none carrying a rune of the trimming at either edge. So such a name
// resolves to no tag there is, and a creation under it would leave a tag called something else.
func refuseNoTagName(name string) *diag.Fault {
	switch {
	case name == "":
		return &diag.Fault{Code: diag.BadUsage, Message: emptyTagName}
	case !utf8.ValidString(name):
		return &diag.Fault{Code: diag.BadUsage, Message: tagNameOfNoUTF8}
	}
	if first, _ := utf8.DecodeRuneInString(name); trimmedOffATagName(first) {
		return &diag.Fault{Code: diag.BadUsage, Message: edgeOfATagName("begins", first)}
	}
	if last, _ := utf8.DecodeLastRuneInString(name); trimmedOffATagName(last) {
		return &diag.Fault{Code: diag.BadUsage, Message: edgeOfATagName("ends", last)}
	}
	return nil
}

// The runes YouTrack cuts off both edges of the name of a tag before it keeps it, which is Kotlin's
// Char.isWhitespace and no Unicode category of its own: the NEL, the zero width space and the BOM are left
// where they stand, and the file, group, record and unit separators are cut although none of them is a space.
// Measured on the polygon rune by rune, at each edge and inside a name, where every one of these was cut and
// every one of those four kept.
func trimmedOffATagName(r rune) bool {
	return r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r' ||
		(r >= 0x1C && r <= 0x1F) || unicode.In(r, unicode.Zs, unicode.Zl, unicode.Zp)
}

const (
	emptyTagName    = "--name is empty, and YouTrack keeps no tag under an empty name"
	tagNameOfNoUTF8 = "--name is no valid UTF-8, and no name YouTrack keeps is: bytes that are none name no tag " +
		"there could be"
	emptyTagOwner = "--owned-by is empty, and YouTrack keeps no user under an empty login: it takes the login of " +
		"the user the tag belongs to"
)

// where is the edge the rune stands at, said as the message reads it.
func edgeOfATagName(where string, r rune) string {
	return fmt.Sprintf("--name %s with U+%04X, which YouTrack cuts off the edges of the name of a tag: the tag "+
		"would be kept under a name other than the one written, and no tag it keeps carries one there", where, r)
}

// The whole of what the deletion does: the one tag of that name among the tags the token is shown, held to the
// form its id has to have before it becomes a path segment, and then destroyed. What is printed comes from the
// read rather than from the deletion, which is answered with nothing at all: the DELETE went to that id, so the
// name and the owner printed are the ones it took away.
func (c *Client) tagDeleted(ctx context.Context, spec *schemas, sought tagSought) (*render.Node, *diag.Fault) {
	found, fault := c.tagNamed(ctx, spec, sought)
	if fault != nil {
		return nil, fault
	}
	tag, fault := found.addressed()
	if fault != nil {
		return nil, fault
	}
	if fault := writingNothing(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.deleteTag(ctx, tag)
	}); fault != nil {
		return nil, found.about(fault)
	}
	return objectNode(found.arrived, printedTagFields(), found.object, nil)
}

// tagged is the one kind of entity a tag hangs from: the schema the read before the write judges its answer
// against, which the server names in $type, and which of the two kinds it is — the word a refusal calls it by
// and the API every request about it goes to both follow from that and from no second reading of the id.
//
// The answer to the write is a Tag for either kind, so no schema of the tagging itself stands here: what the
// two differ in is the owner and nothing else.
type tagged struct {
	schema string
	kind   ownerKind
}

func taggedIssue() tagged {
	return tagged{schema: issueSchema, kind: issueOwner}
}

func taggedArticle() tagged {
	return tagged{schema: articleSchema, kind: articleOwner}
}

// tagsOf is the machinery of the kind of owner a command was given, which is the one place the two kinds are
// told apart.
func tagsOf(kind ownerKind) tagged {
	if kind == articleOwner {
		return taggedArticle()
	}
	return taggedIssue()
}

// AddTag is the call that hangs the tag of that name on the issue or the article of that readable id, and
// prints the owner beside the tag it now carries.
func AddTag(id, name string, ownedBy *string) (Call, *diag.Fault) {
	return tagWrite(id, name, ownedBy, (*Client).tagHung)
}

// RemoveTag is the call that takes the tag of that name off the issue or the article of that readable id,
// leaving the tag itself standing, and prints the owner beside the tag that is off it.
func RemoveTag(id, name string, ownedBy *string) (Call, *diag.Fault) {
	return tagWrite(id, name, ownedBy, (*Client).tagTakenOff)
}

// The two verbs are given the same arguments and hold them to the same rules before anything is sent: a string
// of neither form names no owner ytrack addresses, and a name no tag of YouTrack could carry, or a login no
// user of it could carry, names no tag there is.
func tagWrite(id, name string, ownedBy *string, write func(*Client, context.Context, *schemas, owner, tagSought) (*render.Node, *diag.Fault)) (Call, *diag.Fault) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	sought, fault := theTagSought(name, ownedBy)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return write(c, ctx, spec, at, sought)
	}, nil
}

// The write answers with the tag it hung, so what is printed comes from it: the id that went out is held
// against the id that came back, and the name and the owner beside it are the server's own word about that id.
func (c *Client) tagHung(ctx context.Context, spec *schemas, at owner, sought tagSought) (*render.Node, *diag.Fault) {
	hung, fault := c.taggingOf(ctx, spec, at, sought)
	if fault != nil {
		return nil, fault
	}
	body := hung.body()
	node, fault := c.writing(ctx, spec, tagSchema, resolvedTagFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.addTag(ctx, hung.hangs.kind, hung.on, body, fields)
	}, hung.confirmedBy, hung.printed(addedKey))
	if fault != nil {
		return nil, hung.about(fault)
	}
	return node, nil
}

// The removal goes to the tag under the owner and never to /api/tags, which is what leaves the tag standing for
// its owner and for everyone it was shared with: DeleteTag is another operation, and no branch here reaches it.
// The answer is nothing at all, so what is printed comes from the read that resolved the name — the DELETE went
// to that id, so the name and the owner printed are the ones it took off.
func (c *Client) tagTakenOff(ctx context.Context, spec *schemas, at owner, sought tagSought) (*render.Node, *diag.Fault) {
	off, fault := c.taggingOf(ctx, spec, at, sought)
	if fault != nil {
		return nil, fault
	}
	if fault := writingNothing(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.removeTag(ctx, off.hangs.kind, off.on, off.tag)
	}); fault != nil {
		return nil, off.about(notOnTheOwner(off.hangs.kind, fault))
	}
	tag, fault := objectNode(off.found.arrived, printedTagFields(), off.found.object, nil)
	if fault != nil {
		return nil, fault
	}
	return off.document(removedKey, tag), nil
}

// A 404 from the removal is the owner not carrying that tag, and the server says so by naming the tag that does
// exist — ADR-0005 keeps not_found for an identifier, and here the identifier resolved to a tag the instance
// holds. What to do next is the same either way, so only the sentence is the command's own and the server's
// words stand under it as they came.
func notOnTheOwner(kind ownerKind, fault *diag.Fault) *diag.Fault {
	if fault.Code != diag.NotFound {
		return fault
	}
	fault.Message = fmt.Sprintf("the tag is not on the %s, and the tag itself stands: nothing was taken off, and "+
		"which tags the %s carries is read by ytrack %s show --fields tags(name)", kind, kind, kind)
	return fault
}

// The owner is read before the name is resolved, and that order is the whole of what the read is for: an owner
// the instance has none of is a not_found before a catalogue of tags is ever asked for, which is what holds the
// table of addresses to one request; the readable id it answers is what the write is addressed by and what the
// document prints, since the argument was never checked and dev-7 and DEV-7 reach the same issue.
func (c *Client) taggingOf(ctx context.Context, spec *schemas, at owner, sought tagSought) (tagging, *diag.Fault) {
	hangs := tagsOf(at.kind)
	on, fault := c.ownerToTag(ctx, spec, hangs, at)
	if fault != nil {
		return tagging{}, fault
	}
	found, fault := c.tagNamed(ctx, spec, sought)
	if fault != nil {
		return tagging{}, fault
	}
	tag, fault := found.addressed()
	if fault != nil {
		return tagging{}, fault
	}
	return tagging{on: on, hangs: hangs, found: found, tag: tag}, nil
}

// The read before the write, which is a read and not a write: nothing hangs on the answer but the readable id,
// and an owner that is not there is the server's own 404.
func (c *Client) ownerToTag(ctx context.Context, spec *schemas, hangs tagged, at owner) (addressed, *diag.Fault) {
	requested := []requestedField{{name: idReadableKey}}
	a, fault := c.passing(ctx, spec, hangs.schema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getOwnerToTag(ctx, at, fields)
	})
	if fault != nil {
		return addressed{}, fault
	}
	return addressedIn(a, a.objects[0], hangs.kind, "a tagging")
}

// Which API the read goes to is settled by the kind of the owner and by nothing else: each kind has one
// operation of its own, and there is no branch that sends a request for an owner of neither kind — parseOwner
// left none.
func (c *Client) getOwnerToTag(ctx context.Context, at owner, fields string) (*http.Response, error) {
	if at.kind == articleOwner {
		return c.getArticle(ctx, at.id, fields)
	}
	return c.getIssue(ctx, at.id, fields, nil)
}

// The same rule for the write, which goes to the owner the read named rather than to the one the caller wrote.
func (c *Client) addTag(ctx context.Context, kind ownerKind, on addressed, body []byte, fields string) (*http.Response, error) {
	if kind == articleOwner {
		return c.addArticleTag(ctx, on, body, fields)
	}
	return c.addIssueTag(ctx, on, body, fields)
}

// And for the removal, which is the write of its own operation: neither branch of it reaches /api/tags.
func (c *Client) removeTag(ctx context.Context, kind ownerKind, on addressed, tag tagID) (*http.Response, error) {
	if kind == articleOwner {
		return c.removeArticleTag(ctx, on, tag)
	}
	return c.removeIssueTag(ctx, on, tag)
}

// tagging is the whole of what either verb carries once both reads are through: the owner as the server
// addresses it, the kind it is, and the tag the name resolved to, as the catalogue sent it and as a path
// segment. The body, the check of the answer, the document and every refusal after the reads are built from it
// and from nothing else, which is what holds the two reads before the write in place of an order kept by hand.
type tagging struct {
	on    addressed
	hangs tagged
	found resolvedTag
	tag   tagID
}

// The body of a tagging: the id the resolver gave and not one key more. The member takes no other form — a tag
// under {name} is answered 400 To find an entity of type Tag, specify its ID, and so is an empty object — and no
// $type stands here either, since the server takes the tag without one.
type hungTag struct {
	ID string `json:"id"`
}

// Marshalling a string and a struct of one cannot fail.
func (h tagging) body() []byte {
	body, _ := json.Marshal(hungTag{ID: h.tag.id})
	return body
}

// confirmedBy holds the answer against what the write sent: a 200 says the server took the body, not that the
// tag it hung is the tag the name resolved to. Nothing else of the tag is held to anything — the id is the whole
// of what was written, and the name and the owner printed beside it are the server's own word about that id.
func (h tagging) confirmedBy(a answer) *diag.Fault {
	if arrived, isText := a.objects[0][idKey].(string); isText && arrived == h.tag.id {
		return nil
	}
	message := fmt.Sprintf("the tag the %s carries came back under an id other than the one the name resolved to",
		h.hangs.kind)
	return shapeFailure(a.response, a.body, message)
}

// printed is the document a tagging leaves: the owner by the readable id the read gave, and the delta under
// key. What the owner holds afterwards is no part of it — reading that would be a second snapshot, and the
// issues of a tag are unbounded.
func (h tagging) printed(key string) func(answer) (*render.Node, *diag.Fault) {
	return func(a answer) (*render.Node, *diag.Fault) {
		tag, fault := objectNode(a, printedTagFields(), a.objects[0], nil)
		if fault != nil {
			return nil, fault
		}
		return h.document(key, tag), nil
	}
}

func (h tagging) document(key string, tag *render.Node) *render.Node {
	return render.NewMap(
		render.Pair{Key: idReadableKey, Value: render.NewString(h.on.String())},
		render.Pair{Key: key, Value: tag})
}

// A refusal about the write names both halves of what the call was about: the owner, by the readable id the
// read before it gave, and the tag, by the name the caller wrote. The owner is put in after the tag so that the
// two read in the order the call was written, and every verb of a tag names its tag in the one place below.
func (h tagging) about(fault *diag.Fault) *diag.Fault {
	named := h.found.about(fault)
	named.Details = afterTheRequest(named.Details,
		render.Pair{Key: h.hangs.kind.String(), Value: render.NewString(h.on.String())})
	return named
}

// tagID is the internal id of a tag, held to the form YouTrack gives every entity of an instance before it
// reaches the path of a request: (resolvedTag).addressed is the one place that makes one.
type tagID struct {
	id string
}

// What the resolver asks of every tag: the id the deletion is addressed by, and beside it the pair that tells
// one tag from another, which is what a refusal lists its candidates by and what the deletion prints.
func resolvedTagFields() []requestedField {
	return append([]requestedField{{name: idKey}}, printedTagFields()...)
}

func printedTagFields() []requestedField {
	return []requestedField{
		{name: nameKey},
		{name: ownerKey, children: []requestedField{{name: loginKey}}},
	}
}

// The one tag a name was resolved to: the object it arrived as, so what is printed is what the server sent,
// and the answer it stood in, so a refusal after it names the request that was read.
type resolvedTag struct {
	arrived answer
	object  map[string]any
	name    string
}

// A tag as the resolver compares it: the pair that is its identity, and nothing besides. The id is left in the
// object until the tag is chosen — an id of the wrong shape is worth refusing over the tag that was asked for
// and over no other.
type shownTag struct {
	name  string
	owner string
}

// tagNamed is the whole resolution of a name, and it is ytrack's own: the catalogue of everything the token is
// shown is read in one request and matched here. query is no way to do it — it matches a prefix and folds
// letter case in a way of its own, so a tag it left out would look like one that is not there, and a name that
// resolves to nothing needs the whole catalogue to suggest from anyway.
func (c *Client) tagNamed(ctx context.Context, spec *schemas, sought tagSought) (resolvedTag, *diag.Fault) {
	requested := resolvedTagFields()
	a, fault := c.passing(ctx, spec, "[]"+tagSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getTags(ctx, fields, whole)
	})
	if fault != nil {
		return resolvedTag{}, fault
	}
	shown, fault := tagsShown(a)
	if fault != nil {
		return resolvedTag{}, fault
	}
	at, fault := theTagNamed(a, sought, shown)
	if fault != nil {
		return resolvedTag{}, fault
	}
	return resolvedTag{arrived: a, object: a.objects[at], name: sought.name}, nil
}

// The judgment of names says the members arrived, not what they hold, so the pair every tag is told apart by is
// held to its shape here: a name or a login that is not text is a catalogue nothing can be resolved against.
func tagsShown(a answer) ([]shownTag, *diag.Fault) {
	shown := make([]shownTag, 0, len(a.objects))
	for _, object := range a.objects {
		name, isText := object[nameKey].(string)
		if !isText {
			return nil, shapeFailure(a.response, a.body, "the name of a tag is not text")
		}
		login, isText := memberOf(object[ownerKey], loginKey).(string)
		if !isText {
			message := fmt.Sprintf("the login of the owner of the tag %s is not text", render.Quote(name))
			return nil, shapeFailure(a.response, a.body, message)
		}
		shown = append(shown, shownTag{name: name, owner: login})
	}
	return shown, nil
}

// theTagNamed is where a name becomes one tag. Letter case is the server's to fold: it answers dev for DEV and
// keeps one tag per owner under names that differ by case alone, so the match is EqualFold. Two tags a caller
// is shown may carry the same name — one of them shared by whoever owns it — and then the one written exactly
// as it stands wins; where that settles nothing, the name names more than one tag and the call ends there.
//
// The owner narrows the candidates before any of that: a name two owners carry is settled by whose it is, and
// the byte for byte rule is left the rest — two tags of one owner differing by letter case alone.
func theTagNamed(a answer, sought tagSought, shown []shownTag) (int, *diag.Fault) {
	var named []int
	for at, tag := range shown {
		if strings.EqualFold(sought.name, tag.name) {
			named = append(named, at)
		}
	}
	candidates := named
	if sought.owner != "" {
		candidates = nil
		for _, at := range named {
			if strings.EqualFold(sought.owner, shown[at].owner) {
				candidates = append(candidates, at)
			}
		}
	}
	switch {
	case len(candidates) == 0 && len(named) > 0:
		return 0, noTagOfThatOwner(a, sought, shown, named)
	case len(candidates) == 0:
		return 0, noTagNamed(a, sought.name, shown)
	case len(candidates) == 1:
		return candidates[0], nil
	}
	var exact []int
	for _, at := range candidates {
		if shown[at].name == sought.name {
			exact = append(exact, at)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	return 0, severalTagsNamed(a, sought.name, shown, candidates)
}

// A tag and a group are each addressed by the name a caller writes it by, and that name is what the catalogue
// of a refusal is built of. It is read through a method because a type parameter reaches the methods of a type
// and never its members.
type shownByName interface {
	nameShown() string
}

func (t shownTag) nameShown() string   { return t.name }
func (g shownGroup) nameShown() string { return g.name }

// The names of a catalogue in code point order, one rule for the tags and for the groups: the catalogue is
// nobody's listing, and a caller near none of the names needs to see what there is. Neither what they are
// nearest nor what a name of two answers to should read one way one run and another the next, and a refusal
// about a tag and one about a group order their suggestions alike because they order them here. A name two
// entries carry stands twice, since that is what the token is shown.
func namesShown[E shownByName](shown []E) []string {
	names := make([]string, 0, len(shown))
	for _, entry := range shown {
		names = append(names, entry.nameShown())
	}
	slices.Sort(names)
	return names
}

func noTagNamed(a answer, name string, shown []shownTag) *diag.Fault {
	entry := render.NewMap(
		render.Pair{Key: tagKey, Value: render.NewString(name)},
		render.Pair{Key: "nearest", Value: textList(nearestNames(name, namesShown(shown)))})
	message := "the name under unknown is no tag this token is shown"
	return unresolvedTag(a, "unknown", message, entry)
}

// The candidates are every tag the name answers to, each named by the pair that tells it from the others.
func severalTagsNamed(a answer, name string, shown []shownTag, candidates []int) *diag.Fault {
	entry := render.NewMap(
		render.Pair{Key: tagKey, Value: render.NewString(name)},
		render.Pair{Key: "candidates", Value: tagsListed(shown, candidates)})
	message := "the name under ambiguous is the name of more than one tag this token is shown"
	return unresolvedTag(a, "ambiguous", message, entry)
}

// The candidates stand as they were before the flag: the name did resolve, so the names nearest it would answer
// a question nobody asked, while the owners it answers to are the one thing left to write.
func noTagOfThatOwner(a answer, sought tagSought, shown []shownTag, named []int) *diag.Fault {
	entry := render.NewMap(
		render.Pair{Key: tagKey, Value: render.NewString(sought.name)},
		render.Pair{Key: ownedByKey, Value: render.NewString(sought.owner)},
		render.Pair{Key: "candidates", Value: tagsListed(shown, named)})
	message := "no tag this token is shown under the name under unknown belongs to the login beside it"
	return unresolvedTag(a, "unknown", message, entry)
}

// The tags at those places of the catalogue, each named by the pair that tells it from the others; they are
// ordered by that pair rather than by where the server put them, so the same refusal reads the same way
// whatever order the catalogue arrived in.
func tagsListed(shown []shownTag, at []int) *render.Node {
	found := make([]shownTag, 0, len(at))
	for _, where := range at {
		found = append(found, shown[where])
	}
	slices.SortFunc(found, func(one, other shownTag) int {
		return cmp.Or(strings.Compare(one.name, other.name), strings.Compare(one.owner, other.owner))
	})
	entries := make([]*render.Node, 0, len(found))
	for _, tag := range found {
		entries = append(entries, render.NewMap(
			render.Pair{Key: nameKey, Value: render.NewString(tag.name)},
			render.Pair{Key: ownerKey, Value: render.NewString(tag.owner)}))
	}
	return render.NewList(entries...)
}

// A name that resolved to no one tag names the request it was held against — the reading of the catalogue,
// which is the only request there was — and what to write instead. Nothing was destroyed, and the caller may
// send the call again once they have written a name that resolves.
func unresolvedTag(a answer, key, message string, entry *render.Node) *diag.Fault {
	details := []render.Pair{
		requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted()),
		{Key: key, Value: render.NewList(entry)},
	}
	return &diag.Fault{Code: diag.UnknownName, Message: message, Details: details}
}

func textList(texts []string) *render.Node {
	items := make([]*render.Node, 0, len(texts))
	for _, text := range texts {
		items = append(items, render.NewString(text))
	}
	return render.NewList(items...)
}

// addressed is the id of the resolved tag, held to the form of an internal id before the deletion is built out
// of it: ".." or an empty string there would reach /api/tags itself, which is the collection every tag of the
// token stands in. The id arrived from the server, so a form it does not have is the server's word being wrong
// rather than the caller's, and nothing has been destroyed by the time it is read.
func (r resolvedTag) addressed() (tagID, *diag.Fault) {
	id, isText := r.object[idKey].(string)
	switch {
	case !isText:
		message := fmt.Sprintf("the id of the tag named %s is not text", render.Quote(r.name))
		return tagID{}, shapeFailure(r.arrived.response, r.arrived.body, message)
	case !isInternalID(id):
		message := fmt.Sprintf("the tag named %s arrived under the id %s, and a deletion is addressed by the "+
			"internal id the server gives every entity, which is digits, a dash and digits", render.Quote(r.name), render.Quote(id))
		return tagID{}, shapeFailure(r.arrived.response, r.arrived.body, message)
	}
	return tagID{id: id}, nil
}

// A refusal about what a verb did to the tag names it by the name the caller wrote, right after the request and
// before whatever the server said. The request carries the internal id the resolver found, which is no address
// a caller may write and nothing they typed, so a document without the name says nothing about which tag
// the verb left alone — and all three verbs of one tag name it the same key, so one handler of refusals reads
// them all.
func (r resolvedTag) about(fault *diag.Fault) *diag.Fault {
	fault.Details = afterTheRequest(fault.Details, render.Pair{Key: tagKey, Value: render.NewString(r.name)})
	return fault
}

// groupID is the internal id of a group, held to the form YouTrack gives every entity before it reaches the
// body of a write: groupsResolved.addressed is the one place that makes one.
type groupID struct {
	id string
}

// A group as the resolver compares it: the name a caller writes it by and the id the body addresses it by.
type shownGroup struct {
	name string
	id   string
}

// The catalogue of groups the token is shown, beside the answer it arrived in, so a refusal after it names the
// request that was read.
type shownGroups struct {
	arrived answer
	groups  []shownGroup
}

// The catalogue is read whole and matched here, the way the catalogue of tags is: /api/groups takes no search
// at all, and a name that resolves to nothing needs every group there is to suggest from. /api/admin/groups
// does not exist on either instance, so this is the one listing of groups there is.
func (c *Client) groupsShown(ctx context.Context, spec *schemas) (shownGroups, *diag.Fault) {
	requested := []requestedField{{name: idKey}, {name: nameKey}}
	a, fault := c.passing(ctx, spec, "[]"+groupSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getGroups(ctx, fields, everything)
	})
	if fault != nil {
		return shownGroups{}, fault
	}
	groups := make([]shownGroup, 0, len(a.objects))
	for _, object := range a.objects {
		name, isText := object[nameKey].(string)
		if !isText {
			return shownGroups{}, shapeFailure(a.response, a.body, "the name of a group is not text")
		}
		id, isText := object[idKey].(string)
		if !isText {
			message := fmt.Sprintf("the id of the group named %s is not text", render.Quote(name))
			return shownGroups{}, shapeFailure(a.response, a.body, message)
		}
		groups = append(groups, shownGroup{name: name, id: id})
	}
	return shownGroups{arrived: a, groups: groups}, nil
}

// The three sets of sharing as the body carries them: every name of TagSharing has become the internal id a
// group is addressed by. They are three types for the same reason the names they came from are — a set goes
// into the member it belongs to and into no other, and nothing here may hand one member the groups of another.
type sharingWritten struct {
	shownTo   shownToGroups
	changedBy changedByGroups
	tagging   taggingGroups
}

type (
	shownToGroups   []groupID
	changedByGroups []groupID
	taggingGroups   []groupID
)

// resolving turns the names the three flags wrote into the ids the body carries. Every name of the call is read
// before any of it is refused, so a call naming three groups that are not there is answered once rather than
// three times over.
func (g shownGroups) resolving(shared TagSharing) (sharingWritten, *diag.Fault) {
	of := &groupsResolved{shown: g}
	groups := sharingWritten{
		shownTo:   shared.ShownTo.resolvedBy(of),
		changedBy: shared.ChangedBy.resolvedBy(of),
		tagging:   shared.Tagging.resolvedBy(of),
	}
	if fault := of.refusal(); fault != nil {
		return sharingWritten{}, fault
	}
	return groups, nil
}

// Every set is read by the one rule and comes back as the set it went in as: the type of the ids is the type of
// the names, so a set resolved into the place of another is a compile error rather than a body sent with the
// rights of two flags swapped.
func (names ShownTo) resolvedBy(r *groupsResolved) shownToGroups     { return r.each(names) }
func (names ChangedBy) resolvedBy(r *groupsResolved) changedByGroups { return r.each(names) }
func (names Tagging) resolvedBy(r *groupsResolved) taggingGroups     { return r.each(names) }

// What reading the names of all three flags gathered: the names no group answers to, the names more than one
// answers to, and the first id of a shape no body may carry.
type groupsResolved struct {
	shown     shownGroups
	unknown   []*render.Node
	ambiguous []*render.Node
	broken    *diag.Fault
}

// each is the ids of one flag, in the order the caller wrote its names, with a group named twice standing once:
// the same group written twice is one member of the set either way, and the answer would come back holding it
// once. A flag the call never wrote resolves to nothing at all, which keeps its set out of the body.
func (r *groupsResolved) each(names []string) []groupID {
	if len(names) == 0 {
		return nil
	}
	ids := make([]groupID, 0, len(names))
	for _, name := range names {
		group, found := r.one(name)
		if found && !slices.Contains(ids, group) {
			ids = append(ids, group)
		}
	}
	return ids
}

// one is where a name becomes one group, by the rule the name of a tag is read under: letter case is the
// server's to fold, so the match is EqualFold, and where more than one group answers, the one written exactly
// as it stands wins.
func (r *groupsResolved) one(name string) (groupID, bool) {
	var candidates []shownGroup
	for _, group := range r.shown.groups {
		if strings.EqualFold(name, group.name) {
			candidates = append(candidates, group)
		}
	}
	if len(candidates) == 0 {
		r.unknown = append(r.unknown, render.NewMap(
			render.Pair{Key: groupKey, Value: render.NewString(name)},
			render.Pair{Key: "nearest", Value: textList(nearestNames(name, namesShown(r.shown.groups)))}))
		return groupID{}, false
	}
	if len(candidates) > 1 {
		var exact []shownGroup
		for _, group := range candidates {
			if group.name == name {
				exact = append(exact, group)
			}
		}
		if len(exact) != 1 {
			// The candidates are named by the name alone: a group has no owner to tell it from another, and the
			// internal id it goes by is no name a caller may write.
			r.ambiguous = append(r.ambiguous, render.NewMap(
				render.Pair{Key: groupKey, Value: render.NewString(name)},
				render.Pair{Key: "candidates", Value: textList(namesShown(candidates))}))
			return groupID{}, false
		}
		candidates = exact
	}
	return r.addressed(candidates[0])
}

// The id of the group comes from the server and goes into the body, so it is held to the form YouTrack gives
// every entity: anything else there is answered 400 Invalid structure of entity id, and nothing a caller wrote
// would be what was wrong about it.
func (r *groupsResolved) addressed(group shownGroup) (groupID, bool) {
	if isInternalID(group.id) {
		return groupID{id: group.id}, true
	}
	if r.broken == nil {
		message := fmt.Sprintf("the group named %s arrived under the id %s, and a tag is shared with the internal "+
			"id the server gives every entity, which is digits, a dash and digits", render.Quote(group.name), render.Quote(group.id))
		r.broken = shapeFailure(r.shown.arrived.response, r.shown.arrived.body, message)
	}
	return groupID{}, false
}

// Every name of the call that resolved to no one group is answered together, and nothing is written: the call
// names the request the names were held against — the reading of the groups, which is the only one there was —
// and what to write instead.
func (r *groupsResolved) refusal() *diag.Fault {
	if r.broken != nil {
		return r.broken
	}
	if len(r.unknown) == 0 && len(r.ambiguous) == 0 {
		return nil
	}
	a := r.shown.arrived
	details := []render.Pair{requestDetail(a.response.Request.Method, a.response.Request.URL.Redacted())}
	if len(r.unknown) > 0 {
		details = append(details, render.Pair{Key: "unknown", Value: render.NewList(r.unknown...)})
	}
	if len(r.ambiguous) > 0 {
		details = append(details, render.Pair{Key: "ambiguous", Value: render.NewList(r.ambiguous...)})
	}
	return &diag.Fault{Code: diag.UnknownName, Message: unresolvedGroups(len(r.unknown), len(r.ambiguous)), Details: details}
}

func unresolvedGroups(unknown, ambiguous int) string {
	switch {
	case ambiguous == 0:
		return "the names under unknown are no groups this token is shown"
	case unknown == 0:
		return "each name under ambiguous is the name of more than one group this token is shown"
	}
	return "the names under unknown are no groups this token is shown, and each name under ambiguous is the " +
		"name of more than one"
}
