package cli_test

import (
	"cmp"
	"strconv"
	"strings"
)

const customFieldsFields = "customFields(name,value(name,login,minutes,text)," +
	"projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue))))"

const translatedCustomFieldsFields = "customFields(name,value(name,login,minutes,text)," +
	"projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue),localizedName)))"

type receivedField struct {
	name         string
	translate    string
	valueType    string
	isMultiValue bool
	ordinal      string
	binding      string
	value        string
}

func (f receivedField) sent() string {
	ordinal, binding := f.ordinal, f.binding
	if ordinal == "" {
		ordinal = "1"
	}
	if binding == "" {
		binding = "180-1"
	}
	return `{"$type":"IssueCustomField","name":` + strconv.Quote(f.name) +
		`,"value":` + cmp.Or(f.value, "null") +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":` + strconv.Quote(binding) +
		`,"ordinal":` + ordinal +
		`,"field":{"$type":"CustomField","fieldType":{"$type":"FieldType","valueType":` +
		strconv.Quote(f.valueType) + `,"isMultiValue":` + strconv.FormatBool(f.isMultiValue) + `},` +
		`"localizedName":` + localizedNameOrNull(f.translate) + `}}}`
}

func bundleElement(name string) string {
	return `{"$type":"EnumBundleElement","name":` + strconv.Quote(name) +
		`,"localizedName":null,"presentation":` + strconv.Quote(name+" (presentation)") + `}`
}

func receivedFields(fields ...receivedField) string {
	sent := make([]string, 0, len(fields))
	for _, field := range fields {
		sent = append(sent, field.sent())
	}
	return "[" + strings.Join(sent, ",") + "]"
}

func issueWithFields(fields ...receivedField) string {
	return `{"$type":"Issue","idReadable":"DEV-1","customFields":` + receivedFields(fields...) + `}`
}
