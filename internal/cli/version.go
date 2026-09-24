package cli

import (
	"runtime/debug"

	"github.com/hakastein/ytrack/internal/render"
)

// versionNode is the version of the module and the checkout that go build stamped into the binary, out of the
// stamp cmd/ytrack read off the image of the binary and handed to Run. A build made outside a git checkout, or
// with -buildvcs=false, carries no vcs setting and says null; a binary the Go toolchain did not build carries
// no stamp at all, and then all three are null.
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
