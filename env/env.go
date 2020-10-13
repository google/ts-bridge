package env

import "os"

// TODO(temikus): this should really be a standalone lib, something similar to https://github.com/googleapis/google-cloud-ruby/tree/master/google-cloud-env

// IsAppEngine checks if the code is running in AppEngine by checking GAE_ENV variable
func IsAppEngine() bool {
	_, set := os.LookupEnv("GAE_ENV")
	return set
}

// Extract google cloud project from environment
// 	 Note: this is separate from AppEngine project since other tools/services use this env var
func GoogleCloudProject() string {
	return os.Getenv("GOOGLE_CLOUD_PROJECT")
}

// AppEngineProject returns the cloud project GAE App is running in
// 	 Note: this is the official way to get this information within GAE.
func AppEngineProject() string {
	return GoogleCloudProject()
}
