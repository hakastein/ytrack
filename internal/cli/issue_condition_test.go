package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rejection      = "Причина отклонения"
	rejectionValue = "Дубль"
)

func stateElement(name string) string {
	return `{"$type":"StateBundleElement","name":"` + name + `","isResolved":false,"localizedName":null}`
}

func conditionOfAnotherKind() string {
	return `{"$type":"CustomFieldCondition","id":"98-1"}`
}

func watchingNothing() string {
	return `{"$type":"FieldBasedCondition","showForNullValue":false,"field":null,"values":[]}`
}

func TestIssueCreateSendsNoValueAConditionHides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		state     writableField
		condition string
		writes    []string
		hidden    bool
	}{
		{
			name:      "the project files the field it watches with a value it does not show at",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, defaults: []string{"Новая"}},
			condition: onlyWhen("180-14", false, "Отклонена", "Закрыта"),
			hidden:    true,
		},
		{
			name:      "nothing at all stands in the field it watches",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true},
			condition: onlyWhen("180-14", false, "Отклонена"),
			hidden:    true,
		},
		{
			name:      "the call writes a value it shows at into the field it watches",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, defaults: []string{"Новая"}},
			condition: onlyWhen("180-14", false, "Отклонена", "Закрыта"),
			writes:    []string{"--field", "State=отклонена"},
		},
		{
			name:      "nothing stands in the field it watches and it shows for nothing",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true},
			condition: onlyWhen("180-14", true, "Отклонена"),
		},
		{
			name:      "the project files the field it watches with a value it shows at",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, defaults: []string{"Отклонена"}},
			condition: onlyWhen("180-14", false, "Отклонена"),
		},
		{
			name: "the field it watches holds more than one value",
			state: writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true,
				isMultiValue: true, defaults: []string{"Новая"}},
			condition: onlyWhen("180-14", false, "Отклонена"),
		},
		{
			name:      "it shows the field for nothing at all besides the values it names",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, defaults: []string{"Новая"}},
			condition: onlyWhen("180-14", true, "Отклонена"),
			hidden:    true,
		},
		{
			name:      "it names no value and shows the field for no null either",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, defaults: []string{"Новая"}},
			condition: onlyWhen("180-14", false),
			hidden:    true,
		},
		{
			name:      "it is of a kind ytrack cannot evaluate",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, defaults: []string{"Новая"}},
			condition: conditionOfAnotherKind(),
		},
		{
			name:      "it watches no field at all",
			state:     writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true, defaults: []string{"Новая"}},
			condition: watchingNothing(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := projectResponse(tc.state, writableField{id: "180-23", name: rejection, valueType: "enum",
				canBeEmpty: true, condition: tc.condition})
			held := []receivedField{{name: rejection, valueType: "enum", ordinal: "2", binding: "180-23",
				value: bundleElement(rejectionValue)}}
			creation := noCreation(t)
			if !tc.hidden {
				if len(tc.writes) > 0 {
					held = append(held, receivedField{name: "State", valueType: "state", binding: "180-14",
						value: stateElement("Отклонена")})
				}
				creation = respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", receivedFields(held...)))
			}
			server := creating(t, respondWith(http.StatusOK, metadata), creation)
			argv := append([]string{"issue", "create", "DEV", "--summary", "x",
				"--field", rejection + "=" + rejectionValue}, tc.writes...)

			got := runWith(t, server.env(), argv...)

			if !tc.hidden {
				require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
				assert.Contains(t, sentFieldTypes(t, server.asks()[1]), rejection)
				return
			}
			found := requireFault(t, got)
			assert.Equal(t, "bad_usage", found.code)
			assert.Equal(t, writeMetadataRequest(server.url, "DEV"), detailNamed(t, found, "request"))
			assert.Equal(t, "DEV", detailNamed(t, found, "project"))
			requireInvalidFieldWithAnyReason(t, found, rejection, rejectionValue)
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func requireInvalidFieldWithAnyReason(t *testing.T, found faultDocument, field, value string) {
	t.Helper()
	invalid, ok := detailNamed(t, found, "invalid").([]any)
	require.True(t, ok, "invalid: %v", detailNamed(t, found, "invalid"))
	require.Len(t, invalid, 1)
	row, ok := invalid[0].([]detail)
	require.True(t, ok, "invalid[0]: %v", invalid[0])
	require.Len(t, row, 3)
	assert.Equal(t, detail{"field", field}, row[0])
	assert.Equal(t, detail{"value", value}, row[1])
	assert.Equal(t, "reason", row[2].key)
	assert.NotEmpty(t, row[2].value)
}

func TestIssueCreateRequiresTheFieldTheBodyUncovers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		writes  []string
		missing []any
	}{
		{
			name: "the body leaves the field it watches as the project files it",
		},
		{
			name:    "the body uncovers the field and fills it with nothing",
			writes:  []string{"--field", "State=Отклонена"},
			missing: []any{rejection},
		},
		{
			name:   "the body uncovers the field and fills it",
			writes: []string{"--field", "State=Отклонена", "--field", rejection + "=" + rejectionValue},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := projectResponse(
				writableField{id: "180-14", name: "State", valueType: "state", canBeEmpty: true,
					defaults: []string{"Новая"}},
				writableField{id: "180-23", name: rejection, valueType: "enum",
					condition: onlyWhen("180-14", false, "Отклонена")},
			)
			held := make([]receivedField, 0, 2)
			for _, written := range tc.writes {
				switch written {
				case "State=Отклонена":
					held = append(held, receivedField{name: "State", valueType: "state", binding: "180-14",
						value: stateElement("Отклонена")})
				case rejection + "=" + rejectionValue:
					held = append(held, receivedField{name: rejection, valueType: "enum", ordinal: "2",
						binding: "180-23", value: bundleElement(rejectionValue)})
				}
			}
			creation := respondWith(http.StatusOK, createdIssueWith("DEV-7", "x", "null", receivedFields(held...)))
			if tc.missing != nil {
				creation = noCreation(t)
			}
			server := creating(t, respondWith(http.StatusOK, metadata), creation)
			argv := append([]string{"issue", "create", "DEV", "--summary", "x"}, tc.writes...)

			got := runWith(t, server.env(), argv...)

			if tc.missing == nil {
				require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
				assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(server))
				return
			}
			found := requireFault(t, got)
			assert.Equal(t, "missing_required", found.code)
			assert.Equal(t, tc.missing, detailNamed(t, found, "missing"))
			assert.Equal(t, []string{http.MethodGet}, sentMethods(server))
		})
	}
}

func TestIssueCreateHoldsTheDevProjectToItsCondition(t *testing.T) {
	t.Parallel()
	t.Run("a value the condition of the project hides", func(t *testing.T) {
		t.Parallel()
		dev := devInstance(t)
		argv := append([]string{"issue", "create", "DEV", "--summary", contractTitle(t)}, devRequired()...)
		argv = append(argv, "--field", "State=Новая", "--field", rejection+"="+rejectionValue)

		got := runWith(t, dev.env(), argv...)

		found := requireFault(t, got)
		assert.Equal(t, "bad_usage", found.code)
		requireInvalidFieldWithAnyReason(t, found, rejection, rejectionValue)
		assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
	})
	t.Run("the field the same call uncovers and fills with nothing", func(t *testing.T) {
		t.Parallel()
		dev := devInstance(t)
		argv := append([]string{"issue", "create", "DEV", "--summary", contractTitle(t)}, devRequired()...)
		argv = append(argv, "--field", "State=Отклонена")

		got := runWith(t, dev.env(), argv...)

		found := requireFault(t, got)
		assert.Equal(t, "missing_required", found.code)
		assert.Equal(t, []any{rejection}, detailNamed(t, found, "missing"))
		assert.Equal(t, []string{http.MethodGet}, sentMethods(dev))
	})
	t.Run("the field the same call uncovers and fills", func(t *testing.T) {
		t.Parallel()
		dev := devInstance(t)
		argv := append([]string{"issue", "create", "DEV", "--summary", contractTitle(t)}, devRequired()...)
		argv = append(argv, "--field", "State=Отклонена", "--field", rejection+"="+rejectionValue)

		got := runWith(t, dev.env(), argv...)

		require.Equal(t, 0, got.code, "stderr: %s", got.stderr)
		assert.Empty(t, got.stderr)
		mapping := requireMapping(t, "stdout", got.stdout)
		readable := nodeAt(t, mapping, "idReadable").Value
		require.Regexp(t, `^DEV-[0-9]+$`, readable)
		t.Cleanup(func() { removeIssue(t, dev, readable) })

		assert.Equal(t, "Отклонена", nodeAt(t, mapping, "customFields", "State").Value)
		assert.Equal(t, rejectionValue, nodeAt(t, mapping, "customFields", rejection).Value)
		assert.Equal(t, []string{http.MethodGet, http.MethodPost}, sentMethods(dev))
	})
}
