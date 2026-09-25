package cli

import (
	"runtime/debug"

	"github.com/hakastein/ytrack/internal/render"
)

func versionNode(build *debug.BuildInfo) *render.Node {
	version, revision, modified := render.NewNull(), render.NewNull(), render.NewNull()
	if build != nil {
		version = render.NewString(build.Main.Version)
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = render.NewString(setting.Value)
			case "vcs.modified":
				modified = render.NewBool(setting.Value == "true")
			}
		}
	}
	return render.NewMap(
		render.Pair{Key: "version", Value: version},
		render.Pair{Key: "revision", Value: revision},
		render.Pair{Key: "modified", Value: modified},
	)
}
