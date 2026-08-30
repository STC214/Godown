package buildinfo

// Version is replaced by release builds through -ldflags. Development builds
// keep the explicit dev value and therefore never compare as a published build.
var Version = "0.0.0-dev"

const (
	GitHubOwner = "STC214"
	GitHubRepo  = "Godown"
)
