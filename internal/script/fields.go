package script

import (
	"strings"

	"github.com/hakastein/cliche"
)

const fieldsType = "fields"

func types() map[string]cliche.Type {
	return map[string]cliche.Type{fieldsType: {
		Usage:   "YouTrack fields `expression`; +expr adds to the default",
		Default: true,
		Value: func(def any, given []any) any {
			var text string
			if len(given) > 0 {
				text = given[len(given)-1].(string)
			}
			return fields(def.(string), text)
		},
	}}
}

// The SDK's fields= has no defaults, so a command that has them adds them itself.
func fields(defaults, given string) string {
	given = strings.TrimSpace(given)
	added, adds := strings.CutPrefix(given, "+")
	switch {
	case given == "" || adds && strings.TrimSpace(added) == "":
		return defaults
	case adds:
		return defaults + "," + strings.TrimSpace(added)
	}
	return given
}
