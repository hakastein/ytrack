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

type fieldInfo struct {
	name          string
	localizedName optionalName
	valueType     string
	isMultiValue  bool
}

type optionalName struct {
	name  string
	given bool
}

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

func (l optionalName) forms() []string {
	if !l.given {
		return nil
	}
	return []string{l.name}
}

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

func nearestNamed(name string, catalogue []fieldInfo) []string {
	among := make([]suggestion, 0, len(catalogue))
	for _, field := range catalogue {
		among = append(among, suggestion{name: field.name, also: field.localizedName.forms()})
	}
	return nearest(name, among, canonical(catalogue))
}

func canonical(catalogue []fieldInfo) []string {
	names := make([]string, 0, len(catalogue))
	for _, field := range catalogue {
		names = append(names, field.name)
	}
	slices.Sort(names)
	return names
}

type customField struct {
	id   string
	info fieldInfo
}

const (
	brokenField     = "a custom field of the project is not a JSON object"
	brokenID        = "the id of a custom field is not text"
	brokenFieldInfo = "the name or the type of a custom field is not of the shape the specification gives it"
)

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

type fieldType struct {
	valueType    string
	isMultiValue bool
	values       string
	valueKey     valueKey
	sent         string
}

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

func typeOf(n fieldInfo) (fieldType, bool) {
	for _, t := range fieldTypes() {
		if t.valueType == n.valueType && t.isMultiValue == n.isMultiValue {
			return t, true
		}
	}
	return fieldType{}, false
}

func typeNamed(valueType string) (fieldType, bool) {
	for _, t := range fieldTypes() {
		if t.valueType == valueType {
			return t, true
		}
	}
	return fieldType{}, false
}

type encodedValue struct {
	body     any
	valueKey string
}

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
	return encodedValue{body: map[string]string{t.valueKey.member: text}, valueKey: text}, ""
}

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

type minutesBody struct {
	Minutes int64 `json:"minutes"`
}

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
	return min(hours*60, pastMaxInt32) + minutes, true
}

const pastMaxInt32 = math.MaxInt32 + 1

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
		count = min(count*10+int64(digit-'0'), pastMaxInt32)
	}
	return count, after, true, true
}

func encodeDate(text string) (encodedValue, string) {
	day, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return encodedValue{}, "a date field holds a day, written as in 2026-09-16"
	}
	return encodedValue{body: noonUTC(day), valueKey: day.Format(time.DateOnly)}, ""
}

func noonUTC(day time.Time) int64 {
	return time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC).UnixMilli()
}

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

func encodeInteger(text string) (encodedValue, string) {
	count, err := strconv.ParseInt(text, 10, 32)
	if err != nil {
		return encodedValue{}, fmt.Sprintf("an integer field holds a whole number between %d and %d",
			math.MinInt32, math.MaxInt32)
	}
	return encodedValue{body: count, valueKey: strconv.FormatInt(count, 10)}, ""
}

func encodeFloat(text string) (encodedValue, string) {
	number, isNumber := jsonNumber(text)
	if !isNumber {
		return encodedValue{}, "a float field holds a number written the way JSON writes one, as in 1.5, -0.25 or 1e3"
	}
	held, err := number.Float64()
	if err != nil || math.IsInf(held, 0) || math.IsNaN(held) {
		return encodedValue{}, "a float field holds a finite number, and this one is past the largest one there is"
	}
	return encodedValue{body: held, valueKey: shortestDecimal(held)}, ""
}

func shortestDecimal(number float64) string {
	return strconv.FormatFloat(number, 'g', -1, 64)
}

func jsonNumber(text string) (json.Number, bool) {
	value, isJSON := decode([]byte(text))
	number, isNumber := value.(json.Number)
	nothingAround := number.String() == text
	return number, isJSON && isNumber && nothingAround
}

func encodeString(text string) (encodedValue, string) {
	if !utf8.ValidString(text) {
		return encodedValue{}, noUTF8("the value")
	}
	for _, rewritten := range stringFieldRewrites() {
		if strings.ContainsRune(text, rewritten.rune) {
			return encodedValue{}, rewrittenAs("the value", rewritten)
		}
	}
	if strings.TrimFunc(text, trimmedByYouTrack) != text {
		return encodedValue{}, "YouTrack trims the spaces off a string, so it would keep less than what was written"
	}
	return encodedValue{body: text, valueKey: text}, ""
}

func stringFieldRewrites() []charReplacement {
	return []charReplacement{
		{rune: 0x85, into: "nothing at all"},
		{rune: 0x2028, into: "a space"},
		{rune: 0x2029, into: "a space"},
	}
}

func trimmedByYouTrack(r rune) bool {
	return unicode.IsSpace(r) || isInformationSeparator(r)
}

func isInformationSeparator(r rune) bool {
	return r >= '\x1c' && r <= '\x1f'
}

func encodeText(text string) (encodedValue, string) {
	if !utf8.ValidString(text) {
		return encodedValue{}, noUTF8("the value")
	}
	return encodedValue{body: textBody{Text: text}, valueKey: text}, ""
}

type textBody struct {
	Text string `json:"text"`
}

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
		return shortestDecimal(count), err == nil
	}
	text, isText := held.(string)
	return text, isText
}

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

func (t fieldType) isNamedValue() bool {
	return t.valueKey.member != "" && t.valueKey.form == asString
}

func (t fieldType) sameValue(written, received string) bool {
	if t.isNamedValue() {
		return strings.EqualFold(written, received)
	}
	return written == received
}

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

type issueCustomField struct {
	name          string
	value         any
	kind          fieldType
	ordinal       int64
	binding       string
	localizedName optionalName
}

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

func (n converter) selectedFieldsNode(asked []requestedField, fields []issueCustomField) (*render.Node, *diag.Fault) {
	onIssue := make([]fieldInfo, 0, len(fields))
	for _, field := range fields {
		onIssue = append(onIssue, fieldInfo{name: field.name, localizedName: field.localizedName})
	}
	pairs := make([]render.Pair, 0, len(asked))
	for _, name := range asked {
		matched := findMatches(name.name, onIssue)
		if len(matched) == 0 {
			continue
		}
		field := fields[matched[0]]
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
	translated, isName := readLocalized(field["localizedName"])
	if !isName {
		return "", fieldInfo{}, false
	}
	return binding, fieldInfo{localizedName: translated, valueType: valueType, isMultiValue: isMultiValue}, true
}

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

func (n converter) wrongShapeFault(f issueCustomField) *diag.Fault {
	held := fmt.Sprintf("the value of the custom field %s", render.Quote(f.name))
	if member := f.kind.valueKey.member; member != "" {
		held = fmt.Sprintf("the %s of the custom field %s", member, render.Quote(f.name))
	}
	return n.malformed(held + " is not " + f.kind.valueKey.shape())
}

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

func fromCaller(name requestedField) bool {
	return name.fromCaller
}

func fromDefault(name requestedField) bool {
	return !name.fromCaller
}

func hasDefaultNames(spec *schemas, requested []requestedField) bool {
	named := namedCustomFields(spec, requested)
	return named != nil && slices.ContainsFunc(named.children, fromDefault)
}

func resolveNames(a decodedResponse, requested, asked []requestedField, catalogue []fieldInfo) ([]requestedField, *diag.Fault) {
	var resolved []requestedField
	var unknown, ambiguous []*render.Node
	for _, name := range asked {
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

func unresolvedNames(a decodedResponse, requested []requestedField, key, message string, entries []*render.Node) *diag.Fault {
	against := render.Pair{Key: "fields", Value: render.NewString(formatFields(requested))}
	return unknownNames(a.httpResponse, against, key, message, entries)
}

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

func metadataFields() []requestedField {
	return []requestedField{{name: "customFields", children: []requestedField{{name: idKey}, fieldInfoFields()}}}
}

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

func lookUp(name string, fields []customField) (customField, bool) {
	places := findMatches(name, fieldInfos(fields))
	if len(places) != 1 {
		return customField{}, false
	}
	return fields[places[0]], true
}

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

func fieldInfos(fields []customField) []fieldInfo {
	catalogue := make([]fieldInfo, 0, len(fields))
	for _, field := range fields {
		catalogue = append(catalogue, field.info)
	}
	return catalogue
}

func (f customField) hasValidID() bool {
	return isInternalID(f.id)
}

func invalidFieldIDFault(id string, a decodedResponse) *diag.Fault {
	message := fmt.Sprintf("the id %s of a custom field is not two numbers with a dash between them", render.Quote(id))
	return shapeFailure(a.httpResponse, a.body, message)
}

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
