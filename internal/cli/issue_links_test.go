package cli_test

import (
	"slices"

	"go.yaml.in/yaml/v3"
)

func keysOf(node *yaml.Node) []string {
	keys := []string{}
	for pair := range slices.Chunk(node.Content, 2) {
		keys = append(keys, pair[0].Value)
	}
	return keys
}
