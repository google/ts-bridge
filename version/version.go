// Version records the verion of the ts-bridge project.
// It also includes helper methods to return a user-agent that can be used in requests.
package version

import (
	"fmt"
	"runtime"

	"google.golang.org/api/googleapi"
)

const (
	// Project is the name of this project.
	Project = "ts-bridge"

	// Ver is the current version of this project.
	Ver = "0.1"
)

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

	// googClient is the google API client version.
	googClient = googleapi.UserAgent
)

// Revision returns the current revision of this project.
func Revision() string {
	return Ver
}

// UserAgent returns a useragent string of the format:
// "<google-client-lib>/ver Project/Version (go-runtime; os/architecture)
func UserAgent() string {
	return fmt.Sprintf("%s %s/%s (%s; %s/%s)", googClient, Project, version, rt, goos, goarch)
}
