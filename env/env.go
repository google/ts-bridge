// Package env deals with inferring the GCP environment and its settings.
package env

import "os"

// TODO(temikus): this should really be a standalone lib, something similar to https://github.com/googleapis/google-cloud-ruby/tree/master/google-cloud-env

// IsAppEngine checks if the code is running in AppEngine by checking GAE_ENV variable
func IsAppEngine() bool {
	_, set := os.LookupEnv("GAE_ENV")
	return set
}

// AppEngineProject returns the cloud project GAE App is running in
// 	 Note: this is the official way to get this information within GAE.
func AppEngineProject() string {
	return os.Getenv("GOOGLE_CLOUD_PROJECT")
}
