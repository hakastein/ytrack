package cli

import (
	"runtime/debug"

	"github.com/hakastein/go-youtrack"
)

func versionNode(build *debug.BuildInfo) *youtrack.Node {
	version, revision, modified := youtrack.NewNull(), youtrack.NewNull(), youtrack.NewNull()
	if build != nil {
		version = youtrack.NewString(build.Main.Version)
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = youtrack.NewString(setting.Value)
			case "vcs.modified":
				modified = youtrack.NewBool(setting.Value == "true")
			}
		}
	}
	return youtrack.NewMap(
		youtrack.Pair{Key: "version", Value: version},
		youtrack.Pair{Key: "revision", Value: revision},
		youtrack.Pair{Key: "modified", Value: modified},
	)
}
