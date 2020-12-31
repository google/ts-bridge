// Package version records the verion of the ts-bridge project.
// It also includes helper methods to return a user-agent that can be used in requests.
package version

import (
	"fmt"
	"regexp"
	"runtime"

	"google.golang.org/api/googleapi"
)

const (
	// Project is the name of this project.
	Project = "ts-bridge"
)

// googAPIREgex matches the useragent string from the google API libraries.
var googAPIRegex = regexp.MustCompile("google-api-go-client/\\d+.\\d+(.\\d+)?")

// Variables that can be changed for tests.
var (
	// Revision is the main version revision of the Project.
	version = Revision()

	// rt is the Go runtime version.
	rt = runtime.Version()

	// goos is the os.
	goos = runtime.GOOS

	// goarch is the architechture.
	goarch = runtime.GOARCH
)

// Revision returns the current revision of this project.
func Revision() string {
	return Ver
}

// UserAgent returns a useragent string of the format:
// "<google-client-lib>/ver Project/Version (go-runtime; os/architecture)
func UserAgent() string {
	return fmt.Sprintf("%s %s/%s (%s; %s/%s)", googleapi.UserAgent, Project, version, rt, goos, goarch)
}
