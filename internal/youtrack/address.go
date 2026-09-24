package youtrack

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// What stands between the code and the number of an article, and nowhere else in a readable id.
const articleMarker = "A"

// form is what a string a command was given an entity by turns out to be. Which API the command goes to is
// settled by it, once, before the first request, so no command tries one endpoint and then the other.
type form int

const (
	// Neither readable id, and no internal id either.
	noForm form = iota
	issueForm
	articleForm
	internalForm
)

// formOf is the one classifier of an identifier ytrack is given: the API a command goes to is settled here,
// before its first request. No string is of two forms at once — a project code holds no dash, so an issue
// carries one and an article two, and a code opens with a letter where an internal id opens with a digit.
func formOf(arg string) form {
	code, rest, dashed := strings.Cut(arg, "-")
	if !dashed {
		return noForm
	}
	// The grammar of the code is the whole of what parts a readable id from an internal one.
	if !isProjectCode(code) {
		if isInternalID(arg) {
			return internalForm
		}
		return noForm
	}
	if digits(rest) {
		return issueForm
	}
	if marker, written := articleMarkerOf(arg); written && marker == articleMarker {
		return articleForm
	}
	return noForm
}

// A project code is a letter and then letters, digits and underscores: that is the form YouTrack documents for
// the codes of an instance, and the letter first is what parts a readable id from the internal id that opens
// with a digit.
func isProjectCode(arg string) bool {
	for i, r := range arg {
		if i == 0 && !unicode.IsLetter(r) {
			return false
		}
		if i > 0 && !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' {
			return false
		}
	}
	return arg != ""
}

// articleMarkerOf is what a string written as the id of an article carries between the code and the number,
// and whether it is written that way at all. The marker itself is not read here: DEV-a-1 is no article to the
// server, and a refusal that says which letter is wrong is worth telling from one that says nothing.
func articleMarkerOf(arg string) (string, bool) {
	code, rest, dashed := strings.Cut(arg, "-")
	if !dashed || !isProjectCode(code) {
		return "", false
	}
	marker, number, dashed := strings.Cut(rest, "-")
	if !dashed || !digits(number) {
		return "", false
	}
	return marker, true
}

// The generated client would send ".", ".." or an empty code to another endpoint, so the
// form is checked before any request.
func parseProjectCode(arg string) (string, *diag.Fault) {
	if !isProjectCode(arg) {
		message := fmt.Sprintf("project code %s is not a letter followed by letters, digits or underscores", render.Quote(arg))
		return "", &diag.Fault{Code: diag.BadUsage, Message: message}
	}
	return arg, nil
}

// ownerKind is which of the two entities that carry a readable id a command was given. Each owns comments,
// attachments and tags of its own, and each has an API of its own to reach them through.
type ownerKind int

const (
	issueOwner ownerKind = iota
	articleOwner
)

// Only for refusal text; the API's own names for the kinds are separate values.
func (k ownerKind) String() string {
	if k == articleOwner {
		return "article"
	}
	return "issue"
}

// owner is the entity a command works on, told apart by the form of the id alone.
type owner struct {
	kind ownerKind
	id   string
}

// parseOwner is where a command that works on either entity settles which of them it was given. An internal id
// and a string of neither form are refused here, before anything is sent.
func parseOwner(arg string) (owner, *diag.Fault) {
	switch shape := formOf(arg); shape {
	case issueForm:
		return owner{kind: issueOwner, id: arg}, nil
	case articleForm:
		return owner{kind: articleOwner, id: arg}, nil
	default:
		return owner{}, refusedID(arg, shape)
	}
}

// parseIssueID narrows parseOwner to the one entity the API of issues holds. The id of an article is refused
// rather than sent: /api/issues answers 404 for DEV-A-1, and that answer would tell the caller the issue is
// not there instead of that the string they wrote names an article.
func parseIssueID(arg string) (string, *diag.Fault) {
	found, fault := parseOwner(arg)
	if fault != nil {
		return "", fault
	}
	if found.kind != issueOwner {
		return "", refusedID(arg, articleForm)
	}
	return found.id, nil
}

// parseArticleID narrows parseOwner to the one entity the API of articles holds. The id of an issue is refused
// rather than sent: /api/articles answers 404 for DEV-1 as it does for an article nobody wrote, and that answer
// would tell the caller no such article exists instead of that the string they wrote names an issue.
func parseArticleID(arg string) (string, *diag.Fault) {
	found, fault := parseOwner(arg)
	if fault != nil {
		return "", fault
	}
	if found.kind != articleOwner {
		return "", refusedID(arg, issueForm)
	}
	return found.id, nil
}

// childID is the internal id of an entity that hangs from an issue or an article and has no readable id of its
// own. It stands in the path of every request about that entity, so it takes the same care as addressed does:
// parseChildID is the one place that makes one, and nothing else reaches a path segment.
type childID struct {
	id string
}

// The id as a document prints it and as a refusal names the entity by.
func (c childID) String() string {
	return c.id
}

// parseChildID is where a command that works on one child of an issue or an article settles the form of the id
// it was given, before anything is sent. The generated client resolves the segment against the server, so a
// string of another form would reach another endpoint altogether: an empty one turns a write into the
// collection, which adds a second comment rather than changing the one that was named, and ".." turns it into
// the owner itself. noun is what the child is called and hangsFrom what it hangs from, which are the two
// words the refusal sends the caller back to: a comment hangs from either entity that carries a readable id,
// a work item from an issue and from nothing else.
//
// Leading zeros pass: the server matches the id of a child exactly and answers 404 for 7-02 where 7-2 stands,
// so nothing here has to guess at what the instance numbers its entities with.
func parseChildID(noun, hangsFrom, arg string) (childID, *diag.Fault) {
	if !isInternalID(arg) {
		message := fmt.Sprintf("%s id %s is not an internal id, which is digits, a dash and digits, as in "+
			"7-12: the %s is addressed by the one ytrack prints for it under the %ss of %s it hangs from",
			noun, render.Quote(arg), noun, noun, hangsFrom)
		return childID{}, &diag.Fault{Code: diag.BadUsage, Message: message}
	}
	return childID{id: arg}, nil
}

// addressed is a readable id an answer gave, already held to the form ytrack sends. Every request that carries
// a readable id in its path takes one of these in place of a string, so no such request can be built out of an
// id nothing held to a form: addressedBy is the one place that makes one.
type addressed struct {
	readable string
}

// The id as a document prints it and as a refusal names the entity by.
func (a addressed) String() string {
	return a.readable
}

// addressedBy is the readable id a read gave, held to the form ytrack sends before the write that follows goes
// out: what arrived becomes a path segment, and "..", a slash or an empty string would reach an endpoint other
// than the entity that was read. what names the write in the refusal.
func addressedBy(a answer, kind ownerKind, what string) (addressed, *diag.Fault) {
	return addressedIn(a, a.objects[0], kind, what)
}

// addressedIn is addressedBy for a readable id that stands under a name of the answer rather than at its root:
// a work item carries none of its own, and the issue it hangs from is what a write about it is addressed by.
func addressedIn(a answer, holder map[string]any, kind ownerKind, what string) (addressed, *diag.Fault) {
	readable, isText := holder[idReadableKey].(string)
	if !isText {
		message := fmt.Sprintf("the readable id of the %s arrived as something other than a string", kind)
		return addressed{}, shapeFailure(a.response, a.body, message)
	}
	if found, fault := parseOwner(readable); fault != nil || found.kind != kind {
		message := fmt.Sprintf("the %s arrived with %s for a readable id, and %s is addressed by the readable "+
			"id the server gave", kind, render.Quote(readable), what)
		return addressed{}, shapeFailure(a.response, a.body, message)
	}
	return addressed{readable: readable}, nil
}

// One text carries every form: id "<argument>" <reason>, so a refusal names what the string is rather than
// only that it is not what the command wanted.
func refusedID(arg string, wrong form) *diag.Fault {
	reason := notAReadableID
	marker, written := articleMarkerOf(arg)
	switch {
	case wrong == internalForm:
		reason = anInternalID
	case wrong == articleForm:
		reason = anArticleID
	case wrong == issueForm:
		reason = anIssueID
	case written && marker == strings.ToLower(articleMarker):
		reason = notAReadableID + lowerCaseMarker
	}
	return &diag.Fault{Code: diag.BadUsage, Message: fmt.Sprintf("id %s %s", render.Quote(arg), reason)}
}

// What a refusal says of the string it was given, one text to a form of it.
const (
	notAReadableID = "is neither the readable id of an issue, which is a project code, a dash and a number, as " +
		"in DEV-1, nor that of an article, which carries an A between them, as in DEV-A-1"
	lowerCaseMarker = "; the A of an article is upper case, and the server has no article under a lower case one"
	anInternalID    = "is an internal id, which addresses an entity that has no readable id of its own; an " +
		"issue is addressed by the id ytrack prints as idReadable, as in DEV-1, and an article by DEV-A-1"
	anArticleID = "is the readable id of an article, and an issue is a project code, a dash and a number, as " +
		"in DEV-1"
	anIssueID = "is the readable id of an issue, and an article carries an A between the code and the number, " +
		"as in DEV-A-1"
)

// A user is addressed by login and by nothing else. Each form refused below the server answers for, with another
// user or from another endpoint, so it is refused before any request rather than printed as an answer.
//
// A login seldom holds a space, takes the shape of an id of either kind or reads me, so the forms here cost next
// to nobody their own login. A permissive form would: a login may hold "@", Cyrillic letters and upper case,
// and it may look like an email address.
func parseLogin(arg string) (string, *diag.Fault) {
	switch {
	// The generated client resolves "./users/<login>" against the server, so "" and "." would ask for the
	// collection of users and ".." for /api/.
	case arg == "", arg == ".", arg == "..":
		return "", refusedLogin(arg, "would reach an endpoint other than the one user it names")
	// A space makes the string a name rather than a login, and the way from a name to a login is worth naming.
	case strings.ContainsFunc(arg, unicode.IsSpace):
		return "", refusedLogin(arg, "holds a space, which no login does, and "+findByName(arg))
	case isInternalID(arg):
		return "", refusedLogin(arg, "is the internal id of a user, which the server reads in place of a login")
	case isHubID(arg):
		return "", refusedLogin(arg, "is a Hub id, which the server reads in place of a login")
	// Only in lower case: ME and Me the server has no user for.
	case arg == "me":
		return "", refusedLogin(arg, "is the owner of the token to the server, not a login of its own")
	}
	return arg, nil
}

// One text carries every form: login "<argument>" <reason>, so a refusal names what was addressed whatever it
// was taken for.
func refusedLogin(arg, reason string) *diag.Fault {
	return &diag.Fault{Code: diag.BadUsage, Message: fmt.Sprintf("login %s %s", render.Quote(arg), reason)}
}

// The internal id YouTrack gives every entity of the instance: digits, a dash, digits. An issue, a user and a
// custom field are told apart by what they are addressed by instead, so the form is one rule for all of them.
func isInternalID(arg string) bool {
	number, rest, dashed := strings.Cut(arg, "-")
	return dashed && digits(number) && digits(rest)
}

// A Hub id: hexadecimal digits in either letter case, in groups of 8-4-4-4-12.
func isHubID(arg string) bool {
	sizes := [...]int{8, 4, 4, 4, 12}
	groups := strings.Split(arg, "-")
	if len(groups) != len(sizes) {
		return false
	}
	for i, group := range groups {
		if !hexDigits(group, sizes[i]) {
			return false
		}
	}
	return true
}

// One or more digits and nothing else.
func digits(text string) bool {
	if text == "" {
		return false
	}
	for i := range len(text) {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

// Exactly count hexadecimal digits and nothing else.
func hexDigits(text string, count int) bool {
	if len(text) != count {
		return false
	}
	for i := range len(text) {
		c := text[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}
