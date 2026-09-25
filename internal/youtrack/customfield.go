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
type fieldInfo struct {
	name          string
	localizedName optionalName
	valueType     string
	isMultiValue  bool
}

// What a project calls a custom field of its own, where it calls it anything: a project that does not sends
// null rather than leaving the name out, so having no name of its own is a value here and not the empty string.
// This is the one shape it takes — the cache writes it back as the server sent it.
type optionalName struct {
	name  string
	given bool
}

// readLocalized is the name a decoded answer holds, and false where what stands there is neither text nor null.
func readLocalized(value any) (optionalName, bool) {
	switch name := value.(type) {
	case string:
		return optionalName{name: name, given: true}, true
	case nil:
		return optionalName{}, true
	}
	return optionalName{}, false
}

func (l optionalName) MarshalJSON() ([]byte, error) {
	if !l.given {
		return []byte("null"), nil
	}
	return json.Marshal(l.name)
}

func (l *optionalName) UnmarshalJSON(content []byte) error {
	if string(content) == "null" {
		*l = optionalName{}
		return nil
	}
	var name string
	if err := json.Unmarshal(content, &name); err != nil {
		return err
	}
	*l = optionalName{name: name, given: true}
	return nil
}

func (l optionalName) is(name string) bool {
	return l.given && strings.EqualFold(name, l.name)
}

// forms is the name to measure a caller's against, and nothing where the project gave the field none.
func (l optionalName) forms() []string {
	if !l.given {
		return nil
	}
	return []string{l.name}
}

// findMatches is the places of a catalogue the name answers to, by the one rule a name is matched by:
// letter case aside, against the name a field goes by first and, where the name is the name of no field,
// against the translation a project gave one. A name of one field and the translation of another are told
// apart by that order, in any letter case, so neither is ambiguous.
func findMatches(name string, catalogue []fieldInfo) []int {
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

func pick(catalogue []fieldInfo, places []int) []fieldInfo {
	found := make([]fieldInfo, 0, len(places))
	for _, at := range places {
		found = append(found, catalogue[at])
	}
	return found
}

// nearestNamed is the names of a catalogue nearest what the caller asked for, by the one rule nearest holds: a
// field is as near as the nearer of the two names it answers to, and is suggested by the one it is addressed
// by. A caller near none of them is shown every name there is.
func nearestNamed(name string, catalogue []fieldInfo) []string {
	among := make([]suggestion, 0, len(catalogue))
	for _, field := range catalogue {
		among = append(among, suggestion{name: field.name, also: field.localizedName.forms()})
	}
	return nearest(name, among, canonical(catalogue))
}

// canonical is the name each field is addressed by, by code point: a list of names is read as a listing, and
// the order a server keeps its fields in is nobody's.
func canonical(catalogue []fieldInfo) []string {
	names := make([]string, 0, len(catalogue))
	for _, field := range catalogue {
		names = append(names, field.name)
	}
	slices.Sort(names)
	return names
}

// The metadata of one custom field: the naming a caller may address it by, and the id ytrack addresses it by.
type customField struct {
	id   string
	info fieldInfo
}

const (
	brokenField     = "a custom field of the project is not a JSON object"
	brokenID        = "the id of a custom field is not text"
	brokenFieldInfo = "the name or the type of a custom field is not of the shape the specification gives it"
)

// Where the values a custom field allows arrive from, written as field show asks for them. A user bundle keeps
// under values the users and groups it was built from, while the users the field allows are aggregatedUsers:
// reading values there would answer a different question (ADR-0002).
const (
	BundleValuesFields = "bundle(values(name,archived))"
	BundleUsersFields  = "bundle(aggregatedUsers(login))"
)

const (
	asString   = "text"
	asText     = "prose"
	asDuration = "duration"
	asDay      = "day"
	asDateTime = "moment"
	asInteger  = "whole number"
	asFloat    = "number"
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

type valueKey struct {
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
	valueKey     valueKey
	// The $type of the field on an issue. A write has to send it — without it YouTrack answers 400 $type is
	// required — and the server checks only that its multiplicity matches, so the rows themselves are held
	// together by reading them back (ADR-0002).
	sent string
}

// The twenty types the instance publishes in a catalogue of its own. The table is irregular — state has no
// multi-valued form, the scalar types have no bundle at all — so it is written out (ADR-0002).
func fieldTypes() []fieldType {
	named := valueKey{member: nameKey, form: asString}
	byLogin := valueKey{member: loginKey, form: asString}
	return []fieldType{
		{valueType: "enum", isMultiValue: false, values: BundleValuesFields, valueKey: named, sent: "SingleEnumIssueCustomField"},
		{valueType: "enum", isMultiValue: true, values: BundleValuesFields, valueKey: named, sent: "MultiEnumIssueCustomField"},
		{valueType: "state", isMultiValue: false, values: BundleValuesFields, valueKey: named, sent: "StateIssueCustomField"},
		{valueType: "version", isMultiValue: false, values: BundleValuesFields, valueKey: named, sent: "SingleVersionIssueCustomField"},
		{valueType: "version", isMultiValue: true, values: BundleValuesFields, valueKey: named, sent: "MultiVersionIssueCustomField"},
		{valueType: "build", isMultiValue: false, values: BundleValuesFields, valueKey: named, sent: "SingleBuildIssueCustomField"},
		{valueType: "build", isMultiValue: true, values: BundleValuesFields, valueKey: named, sent: "MultiBuildIssueCustomField"},
		{valueType: "ownedField", isMultiValue: false, values: BundleValuesFields, valueKey: named, sent: "SingleOwnedIssueCustomField"},
		{valueType: "ownedField", isMultiValue: true, values: BundleValuesFields, valueKey: named, sent: "MultiOwnedIssueCustomField"},
		{valueType: "user", isMultiValue: false, values: BundleUsersFields, valueKey: byLogin, sent: "SingleUserIssueCustomField"},
		{valueType: "user", isMultiValue: true, values: BundleUsersFields, valueKey: byLogin, sent: "MultiUserIssueCustomField"},
		{valueType: "group", isMultiValue: false, valueKey: named, sent: "SingleGroupIssueCustomField"},
		{valueType: "group", isMultiValue: true, valueKey: named, sent: "MultiGroupIssueCustomField"},
		{valueType: periodType, isMultiValue: false, valueKey: valueKey{member: "minutes", form: asDuration}, sent: "PeriodIssueCustomField"},
		{valueType: textType, isMultiValue: false, valueKey: valueKey{member: "text", form: asText}, sent: "TextIssueCustomField"},
		{valueType: dateType, isMultiValue: false, valueKey: valueKey{form: asDay}, sent: "DateIssueCustomField"},
		{valueType: momentType, isMultiValue: false, valueKey: valueKey{form: asDateTime}, sent: "SimpleIssueCustomField"},
		{valueType: integerType, isMultiValue: false, valueKey: valueKey{form: asInteger}, sent: "SimpleIssueCustomField"},
		{valueType: floatType, isMultiValue: false, valueKey: valueKey{form: asFloat}, sent: "SimpleIssueCustomField"},
		{valueType: stringType, isMultiValue: false, valueKey: valueKey{form: asString}, sent: "SimpleIssueCustomField"},
	}
}

// typeOf is the row of the table the field's own type stands on.
func typeOf(n fieldInfo) (fieldType, bool) {
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
type encodedValue struct {
	body     any
	valueKey string
}

// encodeValue is one value of the field as the body carries it, or the reason the call cannot carry it, which
// is told to the caller before anything is sent. A value YouTrack would keep as something other than what was
// written is refused here rather than held against the answer: by then the write would have happened.
func (t fieldType) encodeValue(text string) (encodedValue, string) {
	if text == "" {
		return encodedValue{}, t.emptyValueReason()
	}
	switch t.valueType {
	case periodType:
		return encodePeriod(text)
	case dateType:
		return encodeDate(text)
	case momentType:
		return encodeDateTime(text)
	case integerType:
		return encodeInteger(text)
	case floatType:
		return encodeFloat(text)
	case stringType:
		return encodeString(text)
	case textType:
		return encodeText(text)
	}
	// The name is not resolved here: the values of one field of one live project run to hundreds, and the server
	// resolves them in any letter case and says so word for word where it finds none.
	return encodedValue{body: map[string]string{t.valueKey.member: text}, valueKey: text}, ""
}

// A field is filled with something, emptied outright or left alone. YouTrack keeps an empty string and an
// empty text as no value at all, and no other type has an empty value to keep, so nothing here is a call
// meaning to empty a field and saying so nowhere.
func (t fieldType) emptyValueReason() string {
	const leftAlone = "; a field is emptied by --clear Name and a field the call does not name is left as it stands"
	switch {
	case t.valueType == stringType || t.valueType == textType:
		return fmt.Sprintf("YouTrack keeps a %s field it is given nothing for as holding nothing at all",
			t.valueType) + leftAlone
	case t.isNamedValue():
		return fmt.Sprintf("a value of a %s field is a name, and no value is named by nothing", t.valueType) + leftAlone
	}
	return fmt.Sprintf("no value of a %s field is empty", t.valueType) + leftAlone
}

// A period is written the way ytrack prints one: in hours and minutes, and in neither days nor seconds. A day
// of the server is the working day of the instance — P1D is eight hours there (ADR-0002) — so an ISO day would
// mean one thing here and another anywhere else.
func encodePeriod(text string) (encodedValue, string) {
	minutes, read := periodMinutes(text)
	switch {
	case !read:
		return encodedValue{}, "a period is written in hours and minutes, as in PT1H30M, PT90M or PT0M: a day of " +
			"YouTrack is the working day of the instance, and a second is no part of what a period field holds"
	case minutes > math.MaxInt32:
		return encodedValue{}, fmt.Sprintf("a period field holds at most %d minutes", math.MaxInt32)
	}
	return encodedValue{body: minutesBody{Minutes: minutes}, valueKey: duration(minutes)}, ""
}

// The body of a period is the minutes and nothing else: the ISO duration beside them is the value's id, and a
// write carrying that is answered 200 while the field is left empty (ADR-0002).
type minutesBody struct {
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
	return min(hours*60, periodMinutesLimit) + minutes, true
}

// One minute past what a period field holds, which is where a count of them stops growing: a number too long
// to read is refused for the same reason as one merely too large.
const periodMinutesLimit = math.MaxInt32 + 1

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
		count = min(count*10+int64(digit-'0'), periodMinutesLimit)
	}
	return count, after, true, true
}

// A date field holds a day and no moment of one: YouTrack stores every one of them at noon UTC, so that no
// time zone reads a day as another (ADR-0002).
func encodeDate(text string) (encodedValue, string) {
	day, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return encodedValue{}, "a date field holds a day, written as in 2026-09-16"
	}
	return encodedValue{body: noonUTC(day), valueKey: day.Format(time.DateOnly)}, ""
}

// The millisecond a calendar day is written as: noon UTC of it. Every time zone from UTC−12 to UTC+11:59 reads
// that moment as the same day, which midnight UTC would not — there it is the day before for every zone west
// of it (ADR-0002).
func noonUTC(day time.Time) int64 {
	return time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC).UnixMilli()
}

// A moment carries the offset it was written in: a moment without one is a different moment in every time zone
// that reads it, and the server would file it in its own.
func encodeDateTime(text string) (encodedValue, string) {
	moment, err := time.Parse(time.RFC3339, text)
	switch {
	case err != nil:
		return encodedValue{}, "a date and time field holds a moment, written as in 2026-08-31T03:00:00.123+03:00, " +
			"with the offset from UTC on it"
	case moment.Nanosecond()%int(time.Millisecond) != 0:
		return encodedValue{}, "YouTrack keeps a moment to the millisecond, and this one is written finer than that"
	}
	milliseconds := moment.UnixMilli()
	return encodedValue{body: milliseconds, valueKey: formatDateTime(milliseconds)}, ""
}

func formatDateTime(milliseconds int64) string {
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}

// An integer field is an int32 of the server: it answers a larger number with "Слишком большое значение" and
// takes 2.5 for 2 without a word.
func encodeInteger(text string) (encodedValue, string) {
	count, err := strconv.ParseInt(text, 10, 32)
	if err != nil {
		return encodedValue{}, fmt.Sprintf("an integer field holds a whole number between %d and %d",
			math.MinInt32, math.MaxInt32)
	}
	return encodedValue{body: count, valueKey: strconv.FormatInt(count, 10)}, ""
}

// A float field holds a float64, and the server answers with the shortest decimal that reads back as the same
// one: 123456789.123456789 comes back 123456789.12345679, which is the same number written the server's way.
func encodeFloat(text string) (encodedValue, string) {
	number, isNumber := jsonNumber(text)
	if !isNumber {
		return encodedValue{}, "a float field holds a number written the way JSON writes one, as in 1.5, -0.25 or 1e3"
	}
	held, err := number.Float64()
	if err != nil || math.IsInf(held, 0) || math.IsNaN(held) {
		return encodedValue{}, "a float field holds a finite number, and this one is past the largest one there is"
	}
	return encodedValue{body: held, valueKey: fractionText(held)}, ""
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
func encodeString(text string) (encodedValue, string) {
	if !utf8.ValidString(text) {
		return encodedValue{}, noUTF8("the value")
	}
	for _, rewritten := range lineRewrites() {
		if strings.ContainsRune(text, rewritten.rune) {
			return encodedValue{}, rewrittenAs("the value", rewritten)
		}
	}
	if strings.TrimFunc(text, isTrimmedRune) != text {
		return encodedValue{}, "YouTrack trims the spaces off a string, so it would keep less than what was written"
	}
	return encodedValue{body: text, valueKey: text}, ""
}

func lineRewrites() []charReplacement {
	return []charReplacement{
		{rune: 0x85, into: "nothing at all"},
		{rune: 0x2028, into: "a space"},
		{rune: 0x2029, into: "a space"},
	}
}

// What the server takes off the ends of a string: every space Unicode knows, and the four separators below it
// that Unicode does not call one.
func isTrimmedRune(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1C && r <= 0x1F)
}

// A text field keeps every byte of what it is given — a carriage return and a trailing space among them — and
// the one thing it does not keep is nothing at all.
func encodeText(text string) (encodedValue, string) {
	if !utf8.ValidString(text) {
		return encodedValue{}, noUTF8("the value")
	}
	return encodedValue{body: textBody{Text: text}, valueKey: text}, ""
}

type textBody struct {
	Text string `json:"text"`
}

// text is one arrived value as the identity of it reads, out of the member the identity names it by. It is the
// check of a write and what a document prints, so the same column settles both.
func (i valueKey) text(held any) (string, bool) {
	switch i.form {
	case asDuration:
		minutes, isWhole := parseInt64(held)
		return duration(minutes), isWhole
	case asDay, asDateTime:
		count, isWhole := parseInt64(held)
		if !isWhole {
			return "", false
		}
		if i.form == asDay {
			return time.UnixMilli(count).UTC().Format(time.DateOnly), true
		}
		return formatDateTime(count), true
	case asInteger:
		count, isWhole := parseInt64(held)
		return strconv.FormatInt(count, 10), isWhole
	case asFloat:
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

// keyValueNode is what the document writes for a value the identity was already read out of, and false where what
// stands there is of another shape. A number keeps the digits the server wrote: the identity of one is the
// number rather than the digits, and rewriting them would be ytrack saying one number two ways.
func (n converter) keyValueNode(i valueKey, held any) (*render.Node, bool) {
	if i.form == asInteger || i.form == asFloat {
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
	case i.form == asText:
		return n.textNode(text), true
	}
	return render.NewString(text), true
}

// Whether one value of the field is a name the server resolves — a value of a bundle, a user, a group — rather
// than something ytrack reads itself.
func (t fieldType) isNamedValue() bool {
	return t.valueKey.member != "" && t.valueKey.form == asString
}

// Two values name one thing where the server would read them as one: a bundle value, a user and a group
// resolve in any letter case, while a value ytrack read itself was written the one way its type writes it.
func (t fieldType) sameValue(written, received string) bool {
	if t.isNamedValue() {
		return strings.EqualFold(written, received)
	}
	return written == received
}

// What a value has to arrive as for its identity to be read out of it, as a refusal names it.
func (i valueKey) shape() string {
	switch i.form {
	case asDuration:
		return "a whole number of minutes"
	case asDay, asDateTime:
		return "a whole number of milliseconds since the epoch"
	case asInteger, asFloat:
		return "a number"
	}
	return "text"
}

// valueKeys is what the field holds, each value named as its type names it, and nothing for a value the
// field is empty in.
func (n converter) valueKeys(f issueCustomField) ([]string, *diag.Fault) {
	values, fault := n.valuesOf(f)
	if fault != nil {
		return nil, fault
	}
	texts := make([]string, 0, len(values))
	for _, value := range values {
		held, present, fault := n.rawKeyValue(f, value)
		if fault != nil {
			return nil, fault
		}
		if !present {
			continue
		}
		text, read := f.kind.valueKey.text(held)
		if !read {
			return nil, n.wrongShapeFault(f)
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
		if t.valueKey.member == "" {
			continue
		}
		members = merge(members, requestedField{name: t.valueKey.member})
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
	localizedName optionalName
}

// customFields is the block an issue's custom fields are printed as: the name each field goes by
// against what it holds. Asked for by name, the fields stand in the order of the names and an empty one is
// printed empty; taken whole, they stand in the order the project puts its fields in and an empty one is left
// out.
func (n converter) customFields(asked requestedField, value any) (*render.Node, *diag.Fault) {
	fields, fault := n.readCustomFields(value)
	if fault != nil {
		return nil, fault
	}
	if asked.children == nil {
		return n.allFieldsNode(fields)
	}
	return n.selectedFieldsNode(asked.children, fields)
}

func (n converter) readCustomFields(value any) ([]issueCustomField, *diag.Fault) {
	received, isList := value.([]any)
	if !isList {
		return nil, n.malformed("the custom fields of the issue arrived as something other than an array")
	}
	fields := make([]issueCustomField, 0, len(received))
	named := make(map[string]bool, len(received))
	for _, item := range received {
		field, fault := n.readCustomField(item)
		if fault != nil {
			return nil, fault
		}
		// Two fields of one name would print as one key, and which of them survived would be the renderer's
		// choice rather than anything the server said.
		if named[field.name] {
			return nil, n.malformed(fmt.Sprintf("two custom fields of the issue are named %s", render.Quote(field.name)))
		}
		named[field.name] = true
		fields = append(fields, field)
	}
	return fields, nil
}

func (n converter) allFieldsNode(fields []issueCustomField) (*render.Node, *diag.Fault) {
	slices.SortStableFunc(fields, inProjectOrder)
	pairs := make([]render.Pair, 0, len(fields))
	for _, field := range fields {
		printed, present, fault := n.valueNode(field)
		if fault != nil {
			return nil, fault
		}
		if present {
			pairs = append(pairs, render.FromData(field.name, printed))
		}
	}
	return render.NewMap(pairs...), nil
}

// selectedFieldsNode is the fields the names asked for, in the order they were asked in. A field the issue does not
// hold — one its project never bound or one a condition hides — gets no key at all: null would say the issue
// has it and holds nothing in it, and that is a different thing to say.
//
// A name is matched here by the one rule a name is matched by, letter case aside and by the translation too:
// the server filters customFields= that way, so a name held byte for byte would let a field the
// request itself asked for arrive and go unprinted.
func (n converter) selectedFieldsNode(asked []requestedField, fields []issueCustomField) (*render.Node, *diag.Fault) {
	held := make([]fieldInfo, 0, len(fields))
	for _, field := range fields {
		held = append(held, fieldInfo{name: field.name, localizedName: field.localizedName})
	}
	pairs := make([]render.Pair, 0, len(asked))
	for _, name := range asked {
		places := findMatches(name.name, held)
		if len(places) == 0 {
			continue
		}
		field := fields[places[0]]
		printed, present, fault := n.valueNode(field)
		if fault != nil {
			return nil, fault
		}
		if !present {
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

func (n converter) readCustomField(item any) (issueCustomField, *diag.Fault) {
	object, isObject := item.(map[string]any)
	if !isObject {
		return issueCustomField{}, n.malformed("a custom field of the issue is not a JSON object")
	}
	name, isText := object[nameKey].(string)
	if !isText {
		return issueCustomField{}, n.malformed("the name of a custom field of the issue is not text")
	}
	// Anything but an object leaves place nil, and reading a member of it is then the same refusal readBinding
	// gives a binding of the wrong shape: a nil map holds nothing.
	place, _ := object["projectCustomField"].(map[string]any)
	binding, named, whole := readBinding(place)
	if !whole {
		return issueCustomField{}, n.malformed(brokenBinding(name))
	}
	ordinal, isWhole := parseInt64(place["ordinal"])
	if !isWhole {
		message := fmt.Sprintf("the place of the custom field %s among the fields of the project is no whole number", render.Quote(name))
		return issueCustomField{}, n.malformed(message)
	}
	kind, modelled := typeOf(named)
	if !modelled {
		return issueCustomField{}, unmodelledType(named, n.response)
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
func readBinding(place map[string]any) (binding string, named fieldInfo, ok bool) {
	binding, isText := place[idKey].(string)
	if !isText {
		return "", fieldInfo{}, false
	}
	field, isObject := place["field"].(map[string]any)
	if !isObject {
		return "", fieldInfo{}, false
	}
	kind, isObject := field[fieldTypeKey].(map[string]any)
	if !isObject {
		return "", fieldInfo{}, false
	}
	valueType, isText := kind[valueTypeKey].(string)
	isMultiValue, isFlag := kind["isMultiValue"].(bool)
	if !isText || !isFlag {
		return "", fieldInfo{}, false
	}
	// Asked for only where a name of a default has to be matched against the block, so what stands here where
	// nobody asked is nothing rather than a name of no project.
	translated, isName := readLocalized(field["localizedName"])
	if !isName {
		return "", fieldInfo{}, false
	}
	return binding, fieldInfo{localizedName: translated, valueType: valueType, isMultiValue: isMultiValue}, true
}

// valuesOf is the values the field holds, one item however many of them there are, and nothing where it holds
// none. A list where the type holds one value and a bare value where it holds several are the server
// contradicting its own catalogue: multiplicity is the one thing it checks of a write as well (ADR-0002).
func (n converter) valuesOf(f issueCustomField) ([]any, *diag.Fault) {
	values, isList := f.value.([]any)
	switch {
	case f.value == nil:
		return nil, nil
	case isList && !f.kind.isMultiValue:
		return nil, n.malformed(fmt.Sprintf("the custom field %s holds one value by its type and arrived as a list", render.Quote(f.name)))
	case !isList && f.kind.isMultiValue:
		message := fmt.Sprintf("the custom field %s holds more than one value by its type and arrived as "+
			"something other than a list", render.Quote(f.name))
		return nil, n.malformed(message)
	case !isList:
		return []any{f.value}, nil
	}
	return values, nil
}

func (n converter) valueNode(f issueCustomField) (*render.Node, bool, *diag.Fault) {
	values, fault := n.valuesOf(f)
	if fault != nil {
		return nil, false, fault
	}
	items := make([]*render.Node, 0, len(values))
	for _, value := range values {
		node, present, fault := n.valueKeyNode(f, value)
		if fault != nil {
			return nil, false, fault
		}
		if present {
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

// rawKeyValue is the member of a value its type reads the identity out of, and false where the field is empty there.
// A member the type declares and the value does not carry is the server contradicting its own catalogue.
func (n converter) rawKeyValue(f issueCustomField, value any) (any, bool, *diag.Fault) {
	member := f.kind.valueKey.member
	if member == "" {
		return value, true, nil
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		return nil, false, n.missingKeyFault(f, member)
	}
	inside, ok := object[member]
	if !ok {
		return nil, false, n.missingKeyFault(f, member)
	}
	if inside == nil {
		return nil, false, nil
	}
	return inside, true, nil
}

// valueKeyNode is one value as the type of its field names it.
func (n converter) valueKeyNode(f issueCustomField, value any) (*render.Node, bool, *diag.Fault) {
	held, present, fault := n.rawKeyValue(f, value)
	if fault != nil || !present {
		return nil, false, fault
	}
	node, read := n.keyValueNode(f.kind.valueKey, held)
	if !read {
		return nil, false, n.wrongShapeFault(f)
	}
	return node, true, nil
}

func (n converter) missingKeyFault(f issueCustomField, member string) *diag.Fault {
	message := fmt.Sprintf("the value of the custom field %s holds no %s, which is what a field of its type "+
		"is named by", render.Quote(f.name), member)
	return n.malformed(message)
}

// What a refusal is about is the member an identity is read out of, or the value itself where the value is
// the identity.
func (n converter) wrongShapeFault(f issueCustomField) *diag.Fault {
	held := fmt.Sprintf("the value of the custom field %s", render.Quote(f.name))
	if member := f.kind.valueKey.member; member != "" {
		held = fmt.Sprintf("the %s of the custom field %s", member, render.Quote(f.name))
	}
	return n.malformed(held + " is not " + f.kind.valueKey.shape())
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

func (c *Client) customFieldCatalogue(ctx context.Context, spec *schemas) (decodedResponse, []fieldInfo, *diag.Fault) {
	a, fault := c.request(ctx, spec, customFieldCatalogue, catalogueFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetCustomFields(ctx, fields, topAll)
	})
	if fault != nil {
		return decodedResponse{}, nil, fault
	}
	catalogue := make([]fieldInfo, 0, len(a.objects))
	for _, object := range a.objects {
		found, ok := readCatalogueEntry(object)
		if !ok {
			return decodedResponse{}, nil, shapeFailure(a.httpResponse, a.body, brokenCatalogue)
		}
		catalogue = append(catalogue, found)
	}
	return a, catalogue, nil
}

// The catalogue says what a field is called and nothing of what it holds: the type of a field on an issue
// arrives with the issue itself.
func readCatalogueEntry(object map[string]any) (fieldInfo, bool) {
	name, isText := object[nameKey].(string)
	if !isText {
		return fieldInfo{}, false
	}
	translated, isName := readLocalized(object["localizedName"])
	if !isName {
		return fieldInfo{}, false
	}
	return fieldInfo{name: name, localizedName: translated}, true
}

// resolveCustomFields turns the names the caller wrote into the names the instance keeps its fields under,
// which are the names that go out and the keys that are printed. It is asked name by name rather than tree by
// tree: an expression holding none of the caller's own — the default of a selection names two custom fields —
// reads no catalogue at all, so a default costs no request and fails no token with no role on a project.
func (c *Client) resolveCustomFields(ctx context.Context, spec *schemas, requested []requestedField) *diag.Fault {
	named := namedCustomFields(spec, requested)
	if named == nil || !slices.ContainsFunc(named.children, fromCaller) {
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
func fromCaller(name requestedField) bool {
	return name.fromCaller
}

func fromDefault(name requestedField) bool {
	return !name.fromCaller
}

// hasDefaultNames is whether a name ytrack wrote itself stands among the custom fields asked for. Such a name
// was held against no catalogue, so the answer is the one place it meets a name of the instance, and the
// translation of each field is read for that alone.
func hasDefaultNames(spec *schemas, requested []requestedField) bool {
	named := namedCustomFields(spec, requested)
	return named != nil && slices.ContainsFunc(named.children, fromDefault)
}

// A name no field answers to and a name more than one field answers to are both the end of the call, since
// neither says which field was meant.
func resolveNames(a decodedResponse, requested, asked []requestedField, catalogue []fieldInfo) ([]requestedField, *diag.Fault) {
	var resolved []requestedField
	var unknown, ambiguous []*render.Node
	for _, name := range asked {
		// A name of ytrack's own is held against nothing: there is no caller to hand it back to, and the
		// catalogue it would be read from is a request the default does not pay for.
		if !fromCaller(name) {
			resolved = merge(resolved, name)
			continue
		}
		places := findMatches(name.name, catalogue)
		written := fieldPath([]string{customFieldsKey}, formatName(name))
		switch {
		case len(places) == 0:
			unknown = append(unknown, unknownEntry(written, nearestNamed(name.name, catalogue)))
		case len(places) > 1:
			ambiguous = append(ambiguous, ambiguousEntry(written, canonical(pick(catalogue, places))))
		default:
			// Two names of one field — the name itself and the translation of it — are one key, standing
			// where the first of them stood.
			resolved = merge(resolved, requestedField{name: catalogue[places[0]].name, fromCaller: true})
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
func unresolvedNames(a decodedResponse, requested []requestedField, key, message string, entries []*render.Node) *diag.Fault {
	against := render.Pair{Key: "fields", Value: render.NewString(formatFields(requested))}
	return unknownNames(a.httpResponse, against, key, message, entries)
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
func defaultFields(n fieldInfo) (string, bool) {
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
func fieldsToPrint(expression *string, n fieldInfo) (requested []requestedField, modelled bool, fault *diag.Fault) {
	defaults := ""
	if expression == nil || extendsDefault(*expression) {
		if defaults, modelled = defaultFields(n); !modelled {
			return nil, false, nil
		}
	}
	_, requested, fault = fieldsOrDefault(expression, defaults, false)
	return requested, true, fault
}

// A type the catalogue does not model is named off the answer it arrived in, and asking again brings the same
// type back, so the refusal is over the answer rather than over the request that could be sent again.
func unmodelledType(n fieldInfo, a decodedResponse) *diag.Fault {
	message := fmt.Sprintf("valueType %s with isMultiValue %t is not one of the twenty custom-field types ytrack models",
		render.Quote(n.valueType), n.isMultiValue)
	return shapeFailure(a.httpResponse, a.body, message)
}

func fieldInfoFields() requestedField {
	return requestedField{name: "field", children: []requestedField{
		{name: nameKey},
		{name: "localizedName"},
		{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}, {name: "isMultiValue"}}},
	}}
}

// Every custom field of a project arrives in one request off the project itself: neither the settings of the
// custom fields of the instance nor /api/commands is ever asked (ADR-0002).
func metadataFields() []requestedField {
	return []requestedField{{name: "customFields", children: []requestedField{{name: idKey}, fieldInfoFields()}}}
}

// What the metadata of a project is kept under in the cache is the request that would read it again, so an
// expression that grows in a later version leaves what was written under the old one unreachable.
func metadataTarget(code string) string {
	return "/api/admin/projects/" + code + "?fields=" + formatFields(metadataFields())
}

func readMetadata(a decodedResponse) ([]customField, *diag.Fault) {
	items, isList := a.objects[0]["customFields"].([]any)
	if !isList {
		return nil, shapeFailure(a.httpResponse, a.body, "the custom fields of the project are not a JSON array")
	}
	fields := make([]customField, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, shapeFailure(a.httpResponse, a.body, brokenField)
		}
		id, isText := object[idKey].(string)
		if !isText {
			return nil, shapeFailure(a.httpResponse, a.body, brokenID)
		}
		named, ok := readFieldInfo(object)
		if !ok {
			return nil, shapeFailure(a.httpResponse, a.body, brokenFieldInfo)
		}
		fields = append(fields, customField{id: id, info: named})
	}
	return fields, nil
}

func readFieldInfo(object map[string]any) (fieldInfo, bool) {
	field, isObject := object["field"].(map[string]any)
	if !isObject {
		return fieldInfo{}, false
	}
	name, isText := field[nameKey].(string)
	if !isText {
		return fieldInfo{}, false
	}
	kind, isObject := field[fieldTypeKey].(map[string]any)
	if !isObject {
		return fieldInfo{}, false
	}
	valueType, isText := kind[valueTypeKey].(string)
	if !isText {
		return fieldInfo{}, false
	}
	isMultiValue, isBool := kind["isMultiValue"].(bool)
	if !isBool {
		return fieldInfo{}, false
	}
	translated, isName := readLocalized(field["localizedName"])
	if !isName {
		return fieldInfo{}, false
	}
	return fieldInfo{name: name, localizedName: translated, valueType: valueType, isMultiValue: isMultiValue}, true
}

// lookUp is the one field of the project the caller named. Nothing of the name reaches the server: YouTrack
// answers an unknown field name with 500 (ADR-0002).
func lookUp(name string, fields []customField) (customField, bool) {
	places := findMatches(name, fieldInfos(fields))
	if len(places) != 1 {
		return customField{}, false
	}
	return fields[places[0]], true
}

// unresolved refuses a name lookUp found no one field for: with every field the name answers to where it
// answers to several, and with the names nearest it where it answers to none.
func unresolved(a decodedResponse, code, name string, fields []customField) *diag.Fault {
	catalogue := fieldInfos(fields)
	if places := findMatches(name, catalogue); len(places) > 0 {
		message := "the name under unknown belongs to more than one custom field of the project"
		return unknownField(a.httpResponse, code, name, canonical(pick(catalogue, places)), message)
	}
	message := "the name under unknown is not a custom field of the project"
	return unknownField(a.httpResponse, code, name, nearestNamed(name, catalogue), message)
}

func unknownField(response *http.Response, code, name string, nearest []string, message string) *diag.Fault {
	against := render.Pair{Key: "project", Value: render.NewString(code)}
	return unknownNames(response, against, "unknown", message, []*render.Node{unknownEntry(name, nearest)})
}

// fieldInfos is what the fields are called, which is the whole of what a name is resolved against: the field
// show reads off a project and the field an issue names are resolved by one rule.
func fieldInfos(fields []customField) []fieldInfo {
	catalogue := make([]fieldInfo, 0, len(fields))
	for _, field := range fields {
		catalogue = append(catalogue, field.info)
	}
	return catalogue
}

// The generated client turns ".", ".." and an empty id into another endpoint, so the id is held to its form
// before it goes into a path.
func (f customField) hasValidID() bool {
	return isInternalID(f.id)
}

func invalidFieldIDFault(id string, a decodedResponse) *diag.Fault {
	message := fmt.Sprintf("the id %s of a custom field is not two numbers with a dash between them", render.Quote(id))
	return shapeFailure(a.httpResponse, a.body, message)
}

// Between the two requests the project may rename the field or change what it holds, and then the id no longer
// addresses what the name resolved to.
func (n fieldInfo) verifyUnchanged(a decodedResponse, code string) *diag.Fault {
	answered, ok := readFieldInfo(a.objects[0])
	if !ok {
		return shapeFailure(a.httpResponse, a.body, brokenFieldInfo)
	}
	if answered == n {
		return nil
	}
	details := append(responseDetails(a.httpResponse),
		render.Pair{Key: "project", Value: render.NewString(code)},
		render.Pair{Key: "field", Value: render.NewString(n.name)},
		bodyDetail(a.body))
	message := "the custom field the id addresses is no longer the one the name resolved to"
	return &diag.Fault{Code: diag.UpstreamFailed, Message: message, Details: details}
}
