package version

import (
	"fmt"
	"testing"

	"google.golang.org/api/googleapi"
)

func TestUserAgent(t *testing.T) {
	rt = "go1.2.3"
	goos = "linux"
	goarch = "amd64"
	version = "0.1"

	if !googAPIRegex.MatchString(googleapi.UserAgent) {
		t.Errorf("unexpected google api user agent: got(%v)", googleapi.UserAgent)
	}

	want := fmt.Sprintf("%s ts-bridge/0.1 (go1.2.3; linux/amd64)", googleapi.UserAgent)
	if got := UserAgent(); got != want {
		t.Errorf("incorrect useragent string; got(%v), want(%v)", got, want)
	}
}
