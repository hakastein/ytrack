package youtrack

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// How a project names one custom field and what the field holds. ytrack writes this expression itself and
// reads every name of it, so it is a struct rather than a tree (ADR-0002). confirmed compares the whole of it
// against what the second request answered, so every member of it is a comparable value.
type naming struct {
	name          string
	localizedName localized
	valueType     string
	isMultiValue  bool
}

// What a project calls a custom field of its own, where it calls it anything: a project that does not sends
// null rather than leaving the name out, so having no name of its own is a value here and not the empty string.
// This is the one shape it takes — the cache writes it back as the server sent it.
type localized struct {
	name  string
	given bool
}

// readLocalized is the name a decoded answer holds, and false where what stands there is neither text nor null.
func readLocalized(value any) (localized, bool) {
	switch name := value.(type) {
	case string:
		return localized{name: name, given: true}, true
	case nil:
		return localized{}, true
	}
	return localized{}, false
}

func (l localized) MarshalJSON() ([]byte, error) {
	if !l.given {
		return []byte("null"), nil
	}
	return json.Marshal(l.name)
}

func (l *localized) UnmarshalJSON(content []byte) error {
	if string(content) == "null" {
		*l = localized{}
		return nil
	}
	var name string
	if err := json.Unmarshal(content, &name); err != nil {
		return err
	}
	*l = localized{name: name, given: true}
	return nil
}

func (l localized) is(name string) bool {
	return l.given && strings.EqualFold(name, l.name)
}

// forms is the name to measure a caller's against, and nothing where the project gave the field none.
func (l localized) forms() []string {
	if !l.given {
		return nil
	}
	return []string{l.name}
}

// answering is the places of a catalogue the name answers to, by the one rule a name is matched by:
// letter case aside, against the name a field goes by first and, where the name is the name of no field,
// against the translation a project gave one. A name of one field and the translation of another are told
// apart by that order, in any letter case, so neither is ambiguous.
func answering(name string, catalogue []naming) []int {
	var byName, byTranslation []int
	for at, field := range catalogue {
		switch {
		case strings.EqualFold(name, field.name):
			byName = append(byName, at)
		case field.localizedName.is(name):
			byTranslation = append(byTranslation, at)
		}
	}
	if len(byName) > 0 {
		return byName
	}
	return byTranslation
}

func standingAt(catalogue []naming, places []int) []naming {
	found := make([]naming, 0, len(places))
	for _, at := range places {
		found = append(found, catalogue[at])
	}
	return found
}

// nearestNamed is the names of a catalogue nearest what the caller asked for, by the one rule nearest holds: a
// field is as near as the nearer of the two names it answers to, and is suggested by the one it is addressed
// by. A caller near none of them is shown every name there is.
func nearestNamed(name string, catalogue []naming) []string {
	among := make([]suggestion, 0, len(catalogue))
	for _, field := range catalogue {
		among = append(among, suggestion{name: field.name, also: field.localizedName.forms()})
	}
	return nearest(name, among, canonical(catalogue))
}

// canonical is the name each field is addressed by, by code point: a list of names is read as a listing, and
// the order a server keeps its fields in is nobody's.
func canonical(catalogue []naming) []string {
	names := make([]string, 0, len(catalogue))
	for _, field := range catalogue {
		names = append(names, field.name)
	}
	slices.Sort(names)
	return names
}

// The metadata of one custom field: the naming a caller may address it by, and the id ytrack addresses it by.
type customField struct {
	id     string
	naming naming
}

const (
	brokenField  = "a custom field of the project is not a JSON object"
	brokenID     = "the id of a custom field is not text"
	brokenNaming = "the name or the type of a custom field is not of the shape the specification gives it"
)

// Where the values a custom field allows arrive from, written as field show asks for them. A user bundle keeps
// under values the users and groups it was built from, while the users the field allows are aggregatedUsers:
// reading values there would answer a different question (ADR-0002).
const (
	BundleValuesFields = "bundle(values(name,archived))"
	BundleUsersFields  = "bundle(aggregatedUsers(login))"
)

// How one value of a custom field is written where it is not written as it arrived: a duration out of the
// minutes it holds, a day or a moment out of a count of milliseconds since the epoch, prose out of text. A
// number keeps the digits the server sent, so the two forms of one differ only in what may stand there.
const (
	asText     = "text"
	asProse    = "prose"
	asDuration = "duration"
	asDay      = "day"
	asMoment   = "moment"
	asWhole    = "whole number"
	asFraction = "number"
)

// The seven types whose values ytrack reads itself. Every other type holds values that carry names of their
// own, and what a name stands for is the server's to resolve.
const (
	periodType  = "period"
	dateType    = "date"
	momentType  = "date and time"
	integerType = "integer"
	floatType   = "float"
	stringType  = "string"
	textType    = "text"
)

// The identity of a value: the one member of it that names the thing, empty where the value names itself,
// and the way that member is written. What else arrives — a full name, a presentation, HTML of the same text —
// is the server's way of showing a value to a human and is never printed.
type identity struct {
	member string
	form   string
}

// One row of the catalogue of custom-field types: what a field holds, whether it holds more than one of them,
// where the values it allows arrive from, empty where the type allows anything, what one value is printed as,
// and the class a write names the field by.
type fieldType struct {
	valueType    string
	isMultiValue bool
	values       string
	identity     identity
	// The $type of the field on an issue. A write has to send it — without it YouTrack answers 400 $type is
	// required — and the server checks only that its multiplicity matches, so the rows themselves are held
	// together by reading them back (ADR-0002).
	sent string
}

// The twenty types the instance publishes in a catalogue of its own. The table is irregular — state has no
// multi-valued form, the scalar types have no bundle at all — so it is written out (ADR-0002).
func fieldTypes() []fieldType {
	named := identity{member: nameKey, form: asText}
	byLogin := identity{member: loginKey, form: asText}
	return []fieldType{
		{valueType: "enum", isMultiValue: false, values: BundleValuesFields, identity: named, sent: "SingleEnumIssueCustomField"},
		{valueType: "enum", isMultiValue: true, values: BundleValuesFields, identity: named, sent: "MultiEnumIssueCustomField"},
		{valueType: "state", isMultiValue: false, values: BundleValuesFields, identity: named, sent: "StateIssueCustomField"},
		{valueType: "version", isMultiValue: false, values: BundleValuesFields, identity: named, sent: "SingleVersionIssueCustomField"},
		{valueType: "version", isMultiValue: true, values: BundleValuesFields, identity: named, sent: "MultiVersionIssueCustomField"},
		{valueType: "build", isMultiValue: false, values: BundleValuesFields, identity: named, sent: "SingleBuildIssueCustomField"},
		{valueType: "build", isMultiValue: true, values: BundleValuesFields, identity: named, sent: "MultiBuildIssueCustomField"},
		{valueType: "ownedField", isMultiValue: false, values: BundleValuesFields, identity: named, sent: "SingleOwnedIssueCustomField"},
		{valueType: "ownedField", isMultiValue: true, values: BundleValuesFields, identity: named, sent: "MultiOwnedIssueCustomField"},
		{valueType: "user", isMultiValue: false, values: BundleUsersFields, identity: byLogin, sent: "SingleUserIssueCustomField"},
		{valueType: "user", isMultiValue: true, values: BundleUsersFields, identity: byLogin, sent: "MultiUserIssueCustomField"},
		{valueType: "group", isMultiValue: false, identity: named, sent: "SingleGroupIssueCustomField"},
		{valueType: "group", isMultiValue: true, identity: named, sent: "MultiGroupIssueCustomField"},
		{valueType: periodType, isMultiValue: false, identity: identity{member: "minutes", form: asDuration}, sent: "PeriodIssueCustomField"},
		{valueType: textType, isMultiValue: false, identity: identity{member: "text", form: asProse}, sent: "TextIssueCustomField"},
		{valueType: dateType, isMultiValue: false, identity: identity{form: asDay}, sent: "DateIssueCustomField"},
		{valueType: momentType, isMultiValue: false, identity: identity{form: asMoment}, sent: "SimpleIssueCustomField"},
		{valueType: integerType, isMultiValue: false, identity: identity{form: asWhole}, sent: "SimpleIssueCustomField"},
		{valueType: floatType, isMultiValue: false, identity: identity{form: asFraction}, sent: "SimpleIssueCustomField"},
		{valueType: stringType, isMultiValue: false, identity: identity{form: asText}, sent: "SimpleIssueCustomField"},
	}
}

// typeOf is the row of the table the field's own type stands on.
func typeOf(n naming) (fieldType, bool) {
	for _, t := range fieldTypes() {
		if t.valueType == n.valueType && t.isMultiValue == n.isMultiValue {
			return t, true
		}
	}
	return fieldType{}, false
}

// typeNamed is the row a value type stands on whichever multiplicity, for the reader that has the type and not
// the field: the two rows of one type name a value alike, and a journal sends a value by the type alone.
func typeNamed(valueType string) (fieldType, bool) {
	for _, t := range fieldTypes() {
		if t.valueType == valueType {
			return t, true
		}
	}
	return fieldType{}, false
}

// One value of a custom field on its way out: what the body carries, and the identity that value has, which is
// what the answer to the write is held against and what issue show would print for it.
type sentValue struct {
	body     any
	identity string
}

// writtenValue is one value of the field as the body carries it, or the reason the call cannot carry it, which
// is told to the caller before anything is sent. A value YouTrack would keep as something other than what was
// written is refused here rather than held against the answer: by then the write would have happened.
func (t fieldType) writtenValue(text string) (sentValue, string) {
	if text == "" {
		return sentValue{}, t.filledWithNothing()
	}
	switch t.valueType {
	case periodType:
		return writtenPeriod(text)
	case dateType:
		return writtenDay(text)
	case momentType:
		return writtenMoment(text)
	case integerType:
		return writtenWhole(text)
	case floatType:
		return writtenFraction(text)
	case stringType:
		return writtenLine(text)
	case textType:
		return writtenProse(text)
	}
	// The name is not resolved here: the values of one field of one live project run to hundreds, and the server
	// resolves them in any letter case and says so word for word where it finds none.
	return sentValue{body: map[string]string{t.identity.member: text}, identity: text}, ""
}

// A field is filled with something, emptied outright or left alone. YouTrack keeps an empty string and an
// empty text as no value at all, and no other type has an empty value to keep, so nothing here is a call
// meaning to empty a field and saying so nowhere.
func (t fieldType) filledWithNothing() string {
	const leftAlone = "; a field is emptied by --clear Name and a field the call does not name is left as it stands"
	switch {
	case t.valueType == stringType || t.valueType == textType:
		return fmt.Sprintf("YouTrack keeps a %s field it is given nothing for as holding nothing at all",
			t.valueType) + leftAlone
	case t.namedByAName():
		return fmt.Sprintf("a value of a %s field is a name, and no value is named by nothing", t.valueType) + leftAlone
	}
	return fmt.Sprintf("no value of a %s field is empty", t.valueType) + leftAlone
}

// A period is written the way ytrack prints one: in hours and minutes, and in neither days nor seconds. A day
// of the server is the working day of the instance — P1D is eight hours there (ADR-0002) — so an ISO day would
// mean one thing here and another anywhere else.
func writtenPeriod(text string) (sentValue, string) {
	minutes, read := periodMinutes(text)
	switch {
	case !read:
		return sentValue{}, "a period is written in hours and minutes, as in PT1H30M, PT90M or PT0M: a day of " +
			"YouTrack is the working day of the instance, and a second is no part of what a period field holds"
	case minutes > math.MaxInt32:
		return sentValue{}, fmt.Sprintf("a period field holds at most %d minutes", math.MaxInt32)
	}
	return sentValue{body: writtenMinutes{Minutes: minutes}, identity: duration(minutes)}, ""
}

// The body of a period is the minutes and nothing else: the ISO duration beside them is the value's id, and a
// write carrying that is answered 200 while the field is left empty (ADR-0002).
type writtenMinutes struct {
	Minutes int64 `json:"minutes"`
}

// periodMinutes is PT, hours, minutes — at least one of the two — and nothing else.
func periodMinutes(text string) (int64, bool) {
	rest, isPeriod := strings.CutPrefix(text, "PT")
	if !isPeriod {
		return 0, false
	}
	hours, rest, hoursGiven, read := countBefore(rest, 'H')
	if !read {
		return 0, false
	}
	minutes, rest, minutesGiven, read := countBefore(rest, 'M')
	if !read || rest != "" || (!hoursGiven && !minutesGiven) {
		return 0, false
	}
	return min(hours*60, beyondAPeriod) + minutes, true
}

// One minute past what a period field holds, which is where a count of them stops growing: a number too long
// to read is refused for the same reason as one merely too large.
const beyondAPeriod = math.MaxInt32 + 1

// countBefore is the whole number written before mark and what follows it; given is false where mark does not
// stand in the text at all, and read is false where what stands before it is no run of digits.
func countBefore(text string, mark byte) (count int64, rest string, given, read bool) {
	before, after, marked := strings.Cut(text, string(mark))
	if !marked {
		return 0, text, false, true
	}
	if before == "" {
		return 0, "", false, false
	}
	for _, digit := range []byte(before) {
		if digit < '0' || digit > '9' {
			return 0, "", false, false
		}
		count = min(count*10+int64(digit-'0'), beyondAPeriod)
	}
	return count, after, true, true
}

// A date field holds a day and no moment of one: YouTrack stores every one of them at noon UTC, so that no
// time zone reads a day as another (ADR-0002).
func writtenDay(text string) (sentValue, string) {
	day, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return sentValue{}, "a date field holds a day, written as in 2026-09-16"
	}
	return sentValue{body: noonUTC(day), identity: day.Format(time.DateOnly)}, ""
}

// The millisecond a calendar day is written as: noon UTC of it. Every time zone from UTC−12 to UTC+11:59 reads
// that moment as the same day, which midnight UTC would not — there it is the day before for every zone west
// of it (ADR-0002).
func noonUTC(day time.Time) int64 {
	return time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC).UnixMilli()
}

// A moment carries the offset it was written in: a moment without one is a different moment in every time zone
// that reads it, and the server would file it in its own.
func writtenMoment(text string) (sentValue, string) {
	moment, err := time.Parse(time.RFC3339, text)
	switch {
	case err != nil:
		return sentValue{}, "a date and time field holds a moment, written as in 2026-08-31T03:00:00.123+03:00, " +
			"with the offset from UTC on it"
	case moment.Nanosecond()%int(time.Millisecond) != 0:
		return sentValue{}, "YouTrack keeps a moment to the millisecond, and this one is written finer than that"
	}
	milliseconds := moment.UnixMilli()
	return sentValue{body: milliseconds, identity: momentText(milliseconds)}, ""
}

func momentText(milliseconds int64) string {
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}

// An integer field is an int32 of the server: it answers a larger number with "Слишком большое значение" and
// takes 2.5 for 2 without a word.
func writtenWhole(text string) (sentValue, string) {
	count, err := strconv.ParseInt(text, 10, 32)
	if err != nil {
		return sentValue{}, fmt.Sprintf("an integer field holds a whole number between %d and %d",
			math.MinInt32, math.MaxInt32)
	}
	return sentValue{body: count, identity: strconv.FormatInt(count, 10)}, ""
}

// A float field holds a float64, and the server answers with the shortest decimal that reads back as the same
// one: 123456789.123456789 comes back 123456789.12345679, which is the same number written the server's way.
func writtenFraction(text string) (sentValue, string) {
	number, isNumber := jsonNumber(text)
	if !isNumber {
		return sentValue{}, "a float field holds a number written the way JSON writes one, as in 1.5, -0.25 or 1e3"
	}
	held, err := number.Float64()
	if err != nil || math.IsInf(held, 0) || math.IsNaN(held) {
		return sentValue{}, "a float field holds a finite number, and this one is past the largest one there is"
	}
	return sentValue{body: held, identity: fractionText(held)}, ""
}

// The shortest decimal that reads back as the same float64, which is what both the encoder of the body and the
// server's own answer write.
func fractionText(number float64) string {
	return strconv.FormatFloat(number, 'g', -1, 64)
}

// The grammar of a number is JSON's own, read by the decoder every answer is read by: a leading plus, a
// hexadecimal, an infinity and a NaN are numbers nowhere in it, and neither is a number with a space around it.
func jsonNumber(text string) (json.Number, bool) {
	value, isJSON := decode([]byte(text))
	number, isNumber := value.(json.Number)
	return number, isJSON && isNumber && number.String() == text
}

// A string field keeps what it is given but the edges of it: YouTrack trims every space standing there, turns
// a line or a paragraph separator into a space wherever it stands and drops a NEL outright.
func writtenLine(text string) (sentValue, string) {
	if !utf8.ValidString(text) {
		return sentValue{}, noUTF8("the value")
	}
	for _, rewritten := range lineRewrites() {
		if strings.ContainsRune(text, rewritten.rune) {
			return sentValue{}, rewrittenAs("the value", rewritten)
		}
	}
	if strings.TrimFunc(text, trimmedOff) != text {
		return sentValue{}, "YouTrack trims the spaces off a string, so it would keep less than what was written"
	}
	return sentValue{body: text, identity: text}, ""
}

func lineRewrites() []rewrite {
	return []rewrite{
		{rune: 0x85, into: "nothing at all"},
		{rune: 0x2028, into: "a space"},
		{rune: 0x2029, into: "a space"},
	}
}

// What the server takes off the ends of a string: every space Unicode knows, and the four separators below it
// that Unicode does not call one.
func trimmedOff(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1C && r <= 0x1F)
}

// A text field keeps every byte of what it is given — a carriage return and a trailing space among them — and
// the one thing it does not keep is nothing at all.
func writtenProse(text string) (sentValue, string) {
	if !utf8.ValidString(text) {
		return sentValue{}, noUTF8("the value")
	}
	return sentValue{body: writtenText{Text: text}, identity: text}, ""
}

type writtenText struct {
	Text string `json:"text"`
}

// text is one arrived value as the identity of it reads, out of the member the identity names it by. It is the
// check of a write and what a document prints, so the same column settles both.
func (i identity) text(held any) (string, bool) {
	switch i.form {
	case asDuration:
		minutes, isWhole := wholeNumber(held)
		return duration(minutes), isWhole
	case asDay, asMoment:
		count, isWhole := wholeNumber(held)
		if !isWhole {
			return "", false
		}
		if i.form == asDay {
			return time.UnixMilli(count).UTC().Format(time.DateOnly), true
		}
		return momentText(count), true
	case asWhole:
		count, isWhole := wholeNumber(held)
		return strconv.FormatInt(count, 10), isWhole
	case asFraction:
		number, isNumber := held.(json.Number)
		if !isNumber {
			return "", false
		}
		count, err := number.Float64()
		return fractionText(count), err == nil
	}
	text, isText := held.(string)
	return text, isText
}

// printed is what the document writes for a value the identity was already read out of, and false where what
// stands there is of another shape. A number keeps the digits the server wrote: the identity of one is the
// number rather than the digits, and rewriting them would be ytrack saying one number two ways.
func (n nodes) printed(i identity, held any) (*render.Node, bool) {
	if i.form == asWhole || i.form == asFraction {
		number, isNumber := held.(json.Number)
		if !isNumber {
			return nil, false
		}
		return render.NewNumber(number), true
	}
	text, read := i.text(held)
	switch {
	case !read:
		return nil, false
	case i.form == asProse:
		return n.prose(text), true
	}
	return render.NewString(text), true
}

// Whether one value of the field is a name the server resolves — a value of a bundle, a user, a group — rather
// than something ytrack reads itself.
func (t fieldType) namedByAName() bool {
	return t.identity.member != "" && t.identity.form == asText
}

// Two values name one thing where the server would read them as one: a bundle value, a user and a group
// resolve in any letter case, while a value ytrack read itself was written the one way its type writes it.
func (t fieldType) sameValue(written, arrived string) bool {
	if t.namedByAName() {
		return strings.EqualFold(written, arrived)
	}
	return written == arrived
}

// What a value has to arrive as for its identity to be read out of it, as a refusal names it.
func (i identity) shape() string {
	switch i.form {
	case asDuration:
		return "a whole number of minutes"
	case asDay, asMoment:
		return "a whole number of milliseconds since the epoch"
	case asWhole, asFraction:
		return "a number"
	}
	return "text"
}

// identityTexts is what the field holds, each value named as its type names it, and nothing for a value the
// field is empty in.
func (n nodes) identityTexts(f issueCustomField) ([]string, *diag.Fault) {
	values, fault := n.valuesOf(f)
	if fault != nil {
		return nil, fault
	}
	texts := make([]string, 0, len(values))
	for _, value := range values {
		held, holds, fault := n.held(f, value)
		if fault != nil {
			return nil, fault
		}
		if !holds {
			continue
		}
		text, read := f.kind.identity.text(held)
		if !read {
			return nil, n.notOfTheShape(f)
		}
		texts = append(texts, text)
	}
	return texts, nil
}

const (
	customFieldsKey   = "customFields"
	customFieldSchema = "IssueCustomField"
)

// What ytrack asks of every custom field of an issue: the name the project calls it, the members an identity
// may be read out of, and the type and the place among the project's fields, which say how to print the value
// and where to put it. Only the name and the value ever reach the document. translated adds the name a
// project gave the field, which is read where a name of a default is matched against the block: taken whole,
// a block of thirty fields would pay for it on every one of them.
func customFieldsAsked(translated bool) []requestedField {
	held := []requestedField{{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}, {name: "isMultiValue"}}}}
	if translated {
		held = append(held, requestedField{name: "localizedName"})
	}
	return []requestedField{
		{name: nameKey},
		{name: "value", children: identityMembers()},
		{name: "projectCustomField", children: []requestedField{
			{name: idKey},
			{name: "ordinal"},
			{name: "field", children: held},
		}},
	}
}

// The members the table reads identities out of, each asked for once: the request and the table cannot drift
// apart, since one is written off the other.
func identityMembers() []requestedField {
	var members []requestedField
	for _, t := range fieldTypes() {
		if t.identity.member == "" {
			continue
		}
		members = merge(members, requestedField{name: t.identity.member})
	}
	return members
}

// One custom field of an issue as ytrack reads it: what the project calls it, what it holds, what its type
// says about printing that, and where the project puts it among its fields.
type issueCustomField struct {
	name    string
	value   any
	kind    fieldType
	ordinal int64
	// The id of the binding to the project, which settles the order of two fields given one ordinal.
	binding string
	// What the project calls the field, where the request asked for it: a name of a default is matched
	// against both, the way the server matches customFields=.
	localizedName localized
}

// customFields is the block an issue's custom fields are printed as: the name each field goes by
// against what it holds. Asked for by name, the fields stand in the order of the names and an empty one is
// printed empty; taken whole, they stand in the order the project puts its fields in and an empty one is left
// out.
func (n nodes) customFields(asked requestedField, value any) (*render.Node, *diag.Fault) {
	fields, fault := n.readCustomFields(value)
	if fault != nil {
		return nil, fault
	}
	if asked.children == nil {
		return n.wholeBlock(fields)
	}
	return n.namedFields(asked.children, fields)
}

func (n nodes) readCustomFields(value any) ([]issueCustomField, *diag.Fault) {
	arrived, isList := value.([]any)
	if !isList {
		return nil, n.lied("the custom fields of the issue arrived as something other than an array")
	}
	fields := make([]issueCustomField, 0, len(arrived))
	named := make(map[string]bool, len(arrived))
	for _, item := range arrived {
		field, fault := n.readCustomField(item)
		if fault != nil {
			return nil, fault
		}
		// Two fields of one name would print as one key, and which of them survived would be the renderer's
		// choice rather than anything the server said.
		if named[field.name] {
			return nil, n.lied(fmt.Sprintf("two custom fields of the issue are named %s", render.Quote(field.name)))
		}
		named[field.name] = true
		fields = append(fields, field)
	}
	return fields, nil
}

func (n nodes) wholeBlock(fields []issueCustomField) (*render.Node, *diag.Fault) {
	slices.SortStableFunc(fields, inProjectOrder)
	pairs := make([]render.Pair, 0, len(fields))
	for _, field := range fields {
		printed, holds, fault := n.holds(field)
		if fault != nil {
			return nil, fault
		}
		if holds {
			pairs = append(pairs, render.FromData(field.name, printed))
		}
	}
	return render.NewMap(pairs...), nil
}

// namedFields is the fields the names asked for, in the order they were asked in. A field the issue does not
// hold — one its project never bound or one a condition hides — gets no key at all: null would say the issue
// has it and holds nothing in it, and that is a different thing to say.
//
// A name is matched here by the one rule a name is matched by, letter case aside and by the translation too:
// the server filters customFields= that way, so a name held byte for byte would let a field the
// request itself asked for arrive and go unprinted.
func (n nodes) namedFields(asked []requestedField, fields []issueCustomField) (*render.Node, *diag.Fault) {
	held := make([]naming, 0, len(fields))
	for _, field := range fields {
		held = append(held, naming{name: field.name, localizedName: field.localizedName})
	}
	pairs := make([]render.Pair, 0, len(asked))
	for _, name := range asked {
		places := answering(name.name, held)
		if len(places) == 0 {
			continue
		}
		field := fields[places[0]]
		printed, holds, fault := n.holds(field)
		if fault != nil {
			return nil, fault
		}
		if !holds {
			printed = emptyValue(field.kind)
		}
		pairs = append(pairs, render.FromData(field.name, printed))
	}
	return render.NewMap(pairs...), nil
}

// What empty looks like is what the type would have held: no value, or no values.
func emptyValue(kind fieldType) *render.Node {
	if kind.isMultiValue {
		return render.NewList()
	}
	return render.NewNull()
}

// The order is the project's own: the ordinal it gave each binding and, where two bindings share one, the
// number of the binding. The order the array arrives in is the global ordinal of the prototype, which two
// installations of one polygon need not agree on.
func inProjectOrder(a, b issueCustomField) int {
	return cmp.Or(cmp.Compare(a.ordinal, b.ordinal), compareBindings(a.binding, b.binding))
}

func compareBindings(a, b string) int {
	first, isNumbered := bindingNumbers(a)
	second, alsoNumbered := bindingNumbers(b)
	if !isNumbered || !alsoNumbered {
		return strings.Compare(a, b)
	}
	return cmp.Or(cmp.Compare(first[0], second[0]), cmp.Compare(first[1], second[1]))
}

// A binding is numbered N-M, and the numbers grow past what their text would put in order: 180-9 comes before
// 180-10.
func bindingNumbers(id string) ([2]int, bool) {
	before, after, dashed := strings.Cut(id, "-")
	if !dashed {
		return [2]int{}, false
	}
	first, firstErr := strconv.Atoi(before)
	second, secondErr := strconv.Atoi(after)
	if firstErr != nil || secondErr != nil {
		return [2]int{}, false
	}
	return [2]int{first, second}, true
}

func (n nodes) readCustomField(item any) (issueCustomField, *diag.Fault) {
	object, isObject := item.(map[string]any)
	if !isObject {
		return issueCustomField{}, n.lied("a custom field of the issue is not a JSON object")
	}
	name, isText := object[nameKey].(string)
	if !isText {
		return issueCustomField{}, n.lied("the name of a custom field of the issue is not text")
	}
	// Anything but an object leaves place nil, and reading a member of it is then the same refusal readBinding
	// gives a binding of the wrong shape: a nil map holds nothing.
	place, _ := object["projectCustomField"].(map[string]any)
	binding, named, whole := readBinding(place)
	if !whole {
		return issueCustomField{}, n.lied(brokenBinding(name))
	}
	ordinal, isWhole := wholeNumber(place["ordinal"])
	if !isWhole {
		message := fmt.Sprintf("the place of the custom field %s among the fields of the project is no whole number", render.Quote(name))
		return issueCustomField{}, n.lied(message)
	}
	kind, modelled := typeOf(named)
	if !modelled {
		return issueCustomField{}, unmodelledType(named, n.answer)
	}
	return issueCustomField{name: name, value: object["value"], kind: kind, ordinal: ordinal,
		binding: binding, localizedName: named.localizedName}, nil
}

func brokenBinding(name string) string {
	return fmt.Sprintf("the project's field the custom field %s stands for is not of the shape the "+
		"specification gives it", render.Quote(name))
}

// readBinding is the id the project binds the field by and the type the binding carries; false where the
// answer holds either in some other shape.
func readBinding(place map[string]any) (binding string, named naming, ok bool) {
	binding, isText := place[idKey].(string)
	if !isText {
		return "", naming{}, false
	}
	field, isObject := place["field"].(map[string]any)
	if !isObject {
		return "", naming{}, false
	}
	kind, isObject := field[fieldTypeKey].(map[string]any)
	if !isObject {
		return "", naming{}, false
	}
	valueType, isText := kind[valueTypeKey].(string)
	isMultiValue, isFlag := kind["isMultiValue"].(bool)
	if !isText || !isFlag {
		return "", naming{}, false
	}
	// Asked for only where a name of a default has to be matched against the block, so what stands here where
	// nobody asked is nothing rather than a name of no project.
	translated, isName := readLocalized(field["localizedName"])
	if !isName {
		return "", naming{}, false
	}
	return binding, naming{localizedName: translated, valueType: valueType, isMultiValue: isMultiValue}, true
}

// valuesOf is the values the field holds, one item however many of them there are, and nothing where it holds
// none. A list where the type holds one value and a bare value where it holds several are the server
// contradicting its own catalogue: multiplicity is the one thing it checks of a write as well (ADR-0002).
func (n nodes) valuesOf(f issueCustomField) ([]any, *diag.Fault) {
	values, isList := f.value.([]any)
	switch {
	case f.value == nil:
		return nil, nil
	case isList && !f.kind.isMultiValue:
		return nil, n.lied(fmt.Sprintf("the custom field %s holds one value by its type and arrived as a list", render.Quote(f.name)))
	case !isList && f.kind.isMultiValue:
		message := fmt.Sprintf("the custom field %s holds more than one value by its type and arrived as "+
			"something other than a list", render.Quote(f.name))
		return nil, n.lied(message)
	case !isList:
		return []any{f.value}, nil
	}
	return values, nil
}

// holds is what the field holds as the document prints it, and false where it holds nothing: no value at all,
// an empty list, or a value whose identity is empty, such as a text field with no text.
func (n nodes) holds(f issueCustomField) (*render.Node, bool, *diag.Fault) {
	values, fault := n.valuesOf(f)
	if fault != nil {
		return nil, false, fault
	}
	items := make([]*render.Node, 0, len(values))
	for _, value := range values {
		node, holds, fault := n.identityOf(f, value)
		if fault != nil {
			return nil, false, fault
		}
		if holds {
			items = append(items, node)
		}
	}
	switch {
	case len(items) == 0:
		return nil, false, nil
	case !f.kind.isMultiValue:
		return items[0], true, nil
	}
	return render.NewList(items...), true, nil
}

// held is the member of a value its type reads the identity out of, and false where the field is empty there.
// A member the type declares and the value does not carry is the server contradicting its own catalogue.
func (n nodes) held(f issueCustomField, value any) (any, bool, *diag.Fault) {
	member := f.kind.identity.member
	if member == "" {
		return value, true, nil
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		return nil, false, n.noIdentity(f, member)
	}
	inside, arrived := object[member]
	if !arrived {
		return nil, false, n.noIdentity(f, member)
	}
	if inside == nil {
		return nil, false, nil
	}
	return inside, true, nil
}

// identityOf is one value as the type of its field names it.
func (n nodes) identityOf(f issueCustomField, value any) (*render.Node, bool, *diag.Fault) {
	held, holds, fault := n.held(f, value)
	if fault != nil || !holds {
		return nil, false, fault
	}
	node, read := n.printed(f.kind.identity, held)
	if !read {
		return nil, false, n.notOfTheShape(f)
	}
	return node, true, nil
}

func (n nodes) noIdentity(f issueCustomField, member string) *diag.Fault {
	message := fmt.Sprintf("the value of the custom field %s holds no %s, which is what a field of its type "+
		"is named by", render.Quote(f.name), member)
	return n.lied(message)
}

// What a refusal is about is the member an identity is read out of, or the value itself where the value is
// the identity.
func (n nodes) notOfTheShape(f issueCustomField) *diag.Fault {
	held := fmt.Sprintf("the value of the custom field %s", render.Quote(f.name))
	if member := f.kind.identity.member; member != "" {
		held = fmt.Sprintf("the %s of the custom field %s", member, render.Quote(f.name))
	}
	return n.lied(held + " is not " + f.kind.identity.shape())
}

// A period is printed out of the minutes it holds rather than out of the ISO duration the server writes
// beside them: there a day is the working day of the instance, so P1D means eight hours (ADR-0002).
func duration(minutes int64) string {
	written := ""
	if hours := minutes / 60; hours != 0 {
		written += strconv.FormatInt(hours, 10) + "H"
	}
	if rest := minutes % 60; rest != 0 {
		written += strconv.FormatInt(rest, 10) + "M"
	}
	if written == "" {
		written = "0M"
	}
	return "PT" + written
}

// A name is resolved against the catalogue of the whole instance rather than against the fields of a project:
// a token with no role on a project is sent those empty, while this catalogue reaches every reader of
// the instance.
const customFieldCatalogue = "[]CustomField"

const brokenCatalogue = "a custom field of the instance is named in some shape other than text"

func catalogueFields() []requestedField {
	return []requestedField{{name: nameKey}, {name: "localizedName"}}
}

func (c *Client) customFieldCatalogue(ctx context.Context, spec *schemas) (answer, []naming, *diag.Fault) {
	a, fault := c.request(ctx, spec, customFieldCatalogue, catalogueFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getCustomFields(ctx, fields, everything)
	})
	if fault != nil {
		return answer{}, nil, fault
	}
	catalogue := make([]naming, 0, len(a.objects))
	for _, object := range a.objects {
		found, ok := readCatalogued(object)
		if !ok {
			return answer{}, nil, shapeFailure(a.response, a.body, brokenCatalogue)
		}
		catalogue = append(catalogue, found)
	}
	return a, catalogue, nil
}

// The catalogue says what a field is called and nothing of what it holds: the type of a field on an issue
// arrives with the issue itself.
func readCatalogued(object map[string]any) (naming, bool) {
	name, isText := object[nameKey].(string)
	if !isText {
		return naming{}, false
	}
	translated, isName := readLocalized(object["localizedName"])
	if !isName {
		return naming{}, false
	}
	return naming{name: name, localizedName: translated}, true
}

// resolveCustomFields turns the names the caller wrote into the names the instance keeps its fields under,
// which are the names that go out and the keys that are printed. It is asked name by name rather than tree by
// tree: an expression holding none of the caller's own — the default of a selection names two custom fields —
// reads no catalogue at all, so a default costs no request and fails no token with no role on a project.
func (c *Client) resolveCustomFields(ctx context.Context, spec *schemas, requested []requestedField) *diag.Fault {
	named := namedCustomFields(spec, requested)
	if named == nil || !slices.ContainsFunc(named.children, theirOwn) {
		return nil
	}
	a, catalogue, fault := c.customFieldCatalogue(ctx, spec)
	if fault != nil {
		return fault
	}
	resolved, fault := resolveNames(a, requested, named.children, catalogue)
	if fault != nil {
		return fault
	}
	named.children = resolved
	return nil
}

// Whose name it is settles what it is held against: a name the caller wrote is resolved against the catalogue
// of the instance, and a name of a default is held against the answer alone.
func theirOwn(name requestedField) bool {
	return name.theirs
}

func ytrackOwn(name requestedField) bool {
	return !name.theirs
}

// namesOfADefault is whether a name ytrack wrote itself stands among the custom fields asked for. Such a name
// was held against no catalogue, so the answer is the one place it meets a name of the instance, and the
// translation of each field is read for that alone.
func namesOfADefault(spec *schemas, requested []requestedField) bool {
	named := namedCustomFields(spec, requested)
	return named != nil && slices.ContainsFunc(named.children, ytrackOwn)
}

// A name no field answers to and a name more than one field answers to are both the end of the call, since
// neither says which field was meant.
func resolveNames(a answer, requested, asked []requestedField, catalogue []naming) ([]requestedField, *diag.Fault) {
	var resolved []requestedField
	var unknown, ambiguous []*render.Node
	for _, name := range asked {
		// A name of ytrack's own is held against nothing: there is no caller to hand it back to, and the
		// catalogue it would be read from is a request the default does not pay for.
		if !theirOwn(name) {
			resolved = merge(resolved, name)
			continue
		}
		places := answering(name.name, catalogue)
		written := fieldPath([]string{customFieldsKey}, writtenName(name))
		switch {
		case len(places) == 0:
			unknown = append(unknown, unknownEntry(written, nearestNamed(name.name, catalogue)))
		case len(places) > 1:
			ambiguous = append(ambiguous, ambiguousEntry(written, canonical(standingAt(catalogue, places))))
		default:
			// Two names of one field — the name itself and the translation of it — are one key, standing
			// where the first of them stood.
			resolved = merge(resolved, requestedField{name: catalogue[places[0]].name, theirs: true})
		}
	}
	switch {
	case len(unknown) > 0:
		message := "the names under unknown are not custom fields of the instance"
		return nil, unresolvedNames(a, requested, "unknown", message, unknown)
	case len(ambiguous) > 0:
		message := "the names under ambiguous are the names of more than one custom field of the instance each"
		return nil, unresolvedNames(a, requested, "ambiguous", message, ambiguous)
	}
	return resolved, nil
}

// The request the refusal names is the one that was sent — the reading of the catalogue — since the issue
// itself was never asked for: a name that resolves to no field says nothing about which issue it was for.
func unresolvedNames(a answer, requested []requestedField, key, message string, entries []*render.Node) *diag.Fault {
	against := render.Pair{Key: "fields", Value: render.NewString(walk(requested))}
	return unknownNames(a.response, against, key, message, entries)
}

// A refusal over a name names the request the name was held against, what it was held against — the expression
// of a command that resolves several, the project of one that resolves one — and the names that did not
// resolve, each with what to write instead.
func unknownNames(response *http.Response, against render.Pair, key, message string, entries []*render.Node) *diag.Fault {
	details := []render.Pair{
		requestDetail(response.Request.Method, response.Request.URL.Redacted()),
		against,
		{Key: key, Value: render.NewList(entries...)},
	}
	return &diag.Fault{Code: diag.UnknownName, Message: message, Details: details}
}

func ambiguousEntry(field string, candidates []string) *render.Node {
	names := make([]*render.Node, 0, len(candidates))
	for _, name := range candidates {
		names = append(names, render.NewString(name))
	}
	return render.NewMap(
		render.Pair{Key: "field", Value: render.NewString(field)},
		render.Pair{Key: "candidates", Value: render.NewList(names...)})
}

// defaultFields is what field show prints where the caller leans on the default: the base every field is
// printed by, and after it the place this field's own values arrive from. A type outside the catalogue has no
// default, since where its values live is the one thing that cannot be guessed.
func defaultFields(n naming) (string, bool) {
	t, modelled := typeOf(n)
	switch {
	case !modelled:
		return "", false
	case t.values == "":
		return FieldListFields, true
	}
	return FieldListFields + "," + t.values, true
}

// fieldsToPrint is what the caller asked field show for: their own expression, or the default of the field's
// own type where they leaned on it. Only a default has to know where the values of a field live, so a caller
// who wrote the whole expression is answered whatever the type is; modelled is false where they leaned on a
// default the catalogue has none of.
func fieldsToPrint(expression *string, n naming) (requested []requestedField, modelled bool, fault *diag.Fault) {
	defaults := ""
	if expression == nil || addsToTheDefault(*expression) {
		if defaults, modelled = defaultFields(n); !modelled {
			return nil, false, nil
		}
	}
	_, requested, fault = theExpression(expression, defaults, false)
	return requested, true, fault
}

// A type the catalogue does not model is named off the answer it arrived in, and asking again brings the same
// type back, so the refusal is over the answer rather than over the request that could be sent again.
func unmodelledType(n naming, a answer) *diag.Fault {
	message := fmt.Sprintf("valueType %s with isMultiValue %t is not one of the twenty custom-field types ytrack models",
		render.Quote(n.valueType), n.isMultiValue)
	return shapeFailure(a.response, a.body, message)
}

func namingFields() requestedField {
	return requestedField{name: "field", children: []requestedField{
		{name: nameKey},
		{name: "localizedName"},
		{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}, {name: "isMultiValue"}}},
	}}
}

// Every custom field of a project arrives in one request off the project itself: neither the settings of the
// custom fields of the instance nor /api/commands is ever asked (ADR-0002).
func metadataFields() []requestedField {
	return []requestedField{{name: "customFields", children: []requestedField{{name: idKey}, namingFields()}}}
}

// What the metadata of a project is kept under in the cache is the request that would read it again, so an
// expression that grows in a later version leaves what was written under the old one unreachable.
func metadataTarget(code string) string {
	return "/api/admin/projects/" + code + "?fields=" + walk(metadataFields())
}

// The judgment of names says a member arrived, not what it holds, so everything read out of an answer is held
// to its shape here.
func readMetadata(a answer) ([]customField, *diag.Fault) {
	items, isList := a.objects[0]["customFields"].([]any)
	if !isList {
		return nil, shapeFailure(a.response, a.body, "the custom fields of the project are not a JSON array")
	}
	fields := make([]customField, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.response, a.body, brokenField)
		}
		id, isText := object[idKey].(string)
		if !isText {
			return nil, shapeFailure(a.response, a.body, brokenID)
		}
		named, ok := readNaming(object)
		if !ok {
			return nil, shapeFailure(a.response, a.body, brokenNaming)
		}
		fields = append(fields, customField{id: id, naming: named})
	}
	return fields, nil
}

func readNaming(object map[string]any) (naming, bool) {
	field, isObject := object["field"].(map[string]any)
	if !isObject {
		return naming{}, false
	}
	name, isText := field[nameKey].(string)
	if !isText {
		return naming{}, false
	}
	kind, isObject := field[fieldTypeKey].(map[string]any)
	if !isObject {
		return naming{}, false
	}
	valueType, isText := kind[valueTypeKey].(string)
	if !isText {
		return naming{}, false
	}
	isMultiValue, isBool := kind["isMultiValue"].(bool)
	if !isBool {
		return naming{}, false
	}
	translated, isName := readLocalized(field["localizedName"])
	if !isName {
		return naming{}, false
	}
	return naming{name: name, localizedName: translated, valueType: valueType, isMultiValue: isMultiValue}, true
}

// lookUp is the one field of the project the caller named. Nothing of the name reaches the server: YouTrack
// answers an unknown field name with 500 (ADR-0002).
func lookUp(name string, fields []customField) (customField, bool) {
	places := answering(name, namings(fields))
	if len(places) != 1 {
		return customField{}, false
	}
	return fields[places[0]], true
}

// unresolved refuses a name lookUp found no one field for: with every field the name answers to where it
// answers to several, and with the names nearest it where it answers to none.
func unresolved(a answer, code, name string, fields []customField) *diag.Fault {
	catalogue := namings(fields)
	if places := answering(name, catalogue); len(places) > 0 {
		message := "the name under unknown belongs to more than one custom field of the project"
		return unknownField(a.response, code, name, canonical(standingAt(catalogue, places)), message)
	}
	message := "the name under unknown is not a custom field of the project"
	return unknownField(a.response, code, name, nearestNamed(name, catalogue), message)
}

func unknownField(response *http.Response, code, name string, nearest []string, message string) *diag.Fault {
	against := render.Pair{Key: "project", Value: render.NewString(code)}
	return unknownNames(response, against, "unknown", message, []*render.Node{unknownEntry(name, nearest)})
}

// namings is what the fields are called, which is the whole of what a name is resolved against: the field
// show reads off a project and the field an issue names are resolved by one rule.
func namings(fields []customField) []naming {
	catalogue := make([]naming, 0, len(fields))
	for _, field := range fields {
		catalogue = append(catalogue, field.naming)
	}
	return catalogue
}

// The generated client turns ".", ".." and an empty id into another endpoint, so the id is held to its form
// before it goes into a path.
func (f customField) addressable() bool {
	return isInternalID(f.id)
}

func unaddressableID(id string, a answer) *diag.Fault {
	message := fmt.Sprintf("the id %s of a custom field is not two numbers with a dash between them", render.Quote(id))
	return shapeFailure(a.response, a.body, message)
}

// Between the two requests the project may rename the field or change what it holds, and then the id no longer
// addresses what the name resolved to.
func (n naming) confirmed(a answer, code string) *diag.Fault {
	answered, ok := readNaming(a.objects[0])
	if !ok {
		return shapeFailure(a.response, a.body, brokenNaming)
	}
	if answered == n {
		return nil
	}
	details := append(answerDetails(a.response),
		render.Pair{Key: "project", Value: render.NewString(code)},
		render.Pair{Key: "field", Value: render.NewString(n.name)},
		bodyDetail(a.body))
	message := "the custom field the id addresses is no longer the one the name resolved to"
	return &diag.Fault{Code: diag.UpstreamFailed, Message: message, Details: details}
}
