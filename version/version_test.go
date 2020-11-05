package version

import "testing"

func TestUserAGent(t *testing.T) {
	rt = "go1.2.3"
	goos = "linux"
	goarch = "amd64"
	version = "0.1"
	googClient = "googleapi-go/0.1"

	want := "googleapi-go/0.1 ts-bridge/0.1 (go1.2.3; linux/amd64)"
	if got := UserAgent(); got != want {
		t.Errorf("incorrect useragent string; got(%v), want(%v)", got, want)
	}
}
