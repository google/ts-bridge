// This is the official recommended way to keep track of go tooling that shouldn't be compiled in. This file will never
// be compiled; it is used simply to record the dependency. Build constraint name is not particularly important, 'tools'
// is chosen for consistency.
// See: https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
// +build tools

package tools

import (
	_ "github.com/go-bindata/go-bindata/v3"
	_ "github.com/golang/mock/mockgen"
)
