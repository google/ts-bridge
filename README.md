# Time Series Bridge (ts-bridge)

Time Series Bridge is a tool that can be used to import metrics from one
monitoring system to another. It regularly runs a specific query against
a source monitoring system (currently only Datadog) and writes result into the
destination system (currently only Stackdriver).

ts-bridge is an App Engine Standard app written in Go.

## Quick Start

* Create a GCP project that will host the Time Series Bridge.
  * We recommend running ts-bridge in a project separate from the rest of your
    infrastructure.
  * If you are using ts-bridge to import metrics originating from a system
    running on GCP, you might want to run it in a different failure domain (Cloud
    region) from the system itself.
* Define metrics being imported in the configuration file (`app/metrics.yaml`).
* Install [App Engine SDK for Go](https://cloud.google.com/appengine/docs/standard/go/download)
  (follow the "Download and install the original App Engine SDK for Go" links).
  Add it into `$PATH`.
  * Please make sure you install "App Engine SDK for Go" rather than "Cloud SDK
    for Go".
* Deploy the app using the `goapp` tool from the SDK (`$APP_ID` should be the
  name of the Cloud project created to host the app): \
  `cd app/ && goapp deploy -application $APP_ID -version live`

## Monitoring systems

### Datadog

To import a metric from Datadog, ts-bridge regularly runs a configured query
against the [Datadog Query
API](https://docs.datadoghq.com/api/?lang=python#query-time-series-points).

Metrics imported from Datadog are defined in the `datadog_metrics` section of
the configuration file (`app/metrics.yaml`). The following parameters need to
be specified for each metric:

* `name`: base name of the metric. While exporting to Stackdriver, this name
  will be prefixed with `custom.googleapis.com/datadog/`.
* `query`: Datadog query expression. This needs to return a single time series
  (tags/labels are not supported yet).
* `api_key`: Datadog API key.
* `application_key`: Datadog Application key.
* `destination`: name of the Stackdriver destination that query result will be
  written to. Destinations need to be explicitly listed in the
  `stackdriver_destinations` section of the configuration file.

All parameters are required.

Please keep in mind the following details about Datadog API:

* There is an API rate limit of [300 queries per hour](
  https://docs.datadoghq.com/api/?lang=python#rate-limiting) that applies to
  the whole organization. Even if ts-bridge is the only user of the Query API,
  it still means you can only import 5 metrics if you are querying every minute
  (which is the default). The limit can be raised.
* If you are using a [rollup](
  https://docs.datadoghq.com/graphing/miscellaneous/functions/#rollup) function
  as part of your query, Datadog will return a single point per each rollup
  interval. If rollup interval is longer than the importing period of
  ts-bridge, some import operations will fetch 0 new points. For example, if
  your query is producing a 10-minute ratio (
  `xxx.rollup(sum, 600) / yyy.rollup(sum, 600)`) and you are using the default
  importing period (1 minute), ts-bridge will still issue the query every
  minute, however Datadog will only return a single point once every 10 minutes.
* If you are not using the `rollup` function, Datadog will return points at
  maximum possible resolution (unless the query covers a very long time
  interval). Please keep in mind that Datadog might return more than 1 point
  per minute, and all points will be written to Stackdriver, even though
  Stackdriver does not allow querying with [alignment period](
  https://cloud.google.com/monitoring/charts/metrics-selector#alignment)
  shorter than 1 minute.

## Stackdriver

Imported metrics can be written to multiple destination Stackdriver projects,
even though in practice we expect a single instance of Time Series Bridge to
write to a single project (usually matching the GCP project where the
ts-bridge is running).

Stackdriver destinations are listed in the `stackdriver_destinations`
section of the configuration file. The following parameters can be specified
for each destination:

* `name`: name of the Stackdriver destination. It's only used internally by
  ts-bridge to match imported metrics with destinations.
* `project_id`: name of the Stackdriver project that metrics will be written
  to. This parameter is optional; if not specified, the same project where
  ts-bridge is running will be used.

If you are using ts-bridge to write metrics to a different Stackdriver project
than the one it's running in, you will need to grant `roles/monitoring.editor`
IAM permission to the service account used by the ts-bridge App Engine app to
allow it to read and write Stackdriver metrics.

## Configuration settings

### Importing period

Time Series Bridge attempts to import all configured metrics regularly. This
is driven by the [App Engine Cron Service](
https://cloud.google.com/appengine/docs/standard/go/config/cron) which is
configured in `app/cron.yaml`. By default metrics are imported every minute.

### Global settings

Some other settings can be set globally as App Engine environment variables via
the `env_variables` section of `app/app.yaml`.

* `CONFIG_FILE`: name of the metric configuration file (`metrics.yaml`).
* `SD_LOOKBACK_INTERVAL`: time interval used while searching for recent data in
  Stackdriver. This is also the default backfill interval for when no recent
  points are found. This interval should be kept reasonably short to
  avoid fetching too much data from Stackdriver on each update.
  * You might be tempted to increase this significantly to backfill historic
    values. Please keep in mind that Stackdriver [does not allow](
    https://cloud.google.com/monitoring/custom-metrics/creating-metrics#writing-ts)
    writing points that are more than 24 hours old. Also, Datadog downsamples
    values to [keep the number of points in each response below ~300](
    https://docs.datadoghq.com/getting_started/from_the_query_to_the_graph/#how).
    This means that a single request can only cover a time period of 5 hours if
    you are aiming to get a point per minute.
* `UPDATE_TIMEOUT`: the total time that updating all metrics is allowed to take.
  The incoming HTTP request from App Engine Cron will fail if it takes longer
  than this, and a subsequent update will be triggered again.
* `UPDATE_PARALLELISM`: number of metric updates that are performed in parallel.
  Parallel updates are scheduled using goroutines and still happen in the
  context of a single incoming HTTP request, and setting this value too high
  might result in the App Engine instance running out of RAM.
* `ENABLE_STATUS_PAGE`: can be set to 'yes' to enable the status web page
  (disabled by default).

You can use `--env_var` flag to override these environment variables while
running the app via `dev_appserver.py`.

## Status Web Page

If `ENABLE_STATUS_PAGE` environment variable is set to 'yes', the index page of
the App Engine app shows a list of configured metrics along with import status
for each metric. This might be useful for debugging, however is disabled by
default to avoid publicly exposing a list of configured metrics (App Engine HTTP
endpoints are publicly available by default).

If you choose to leave the status page enabled, we recommend configuring
[Identity-Aware Proxy](https://pantheon.corp.google.com/security/iap/project)
(IAP) for the Cloud project where ts-bridge is running. You can use IAP to
restrict access to ts-bridge by a specific Google group or a list of
Google accounts.

## Monitoring

Time Series Bridge uses [OpenCensus](https://opencensus.io/) to report several
metrics to Stackdriver:

* `metric_import_latencies`: per-metric import latency (in ms). This
  metric has a `metric_name` field.
* `import_latencies`: total time it took to import all metrics (in ms). If this
  becomes larger than `UPDATE_TIMEOUT`, some metrics might not be imported, and
  you might need to increase `UPDATE_PARALLELISM` or `UPDATE_TIMEOUT`.
* `oldest_metric_age`: oldest time since the last written point across all metrics
  (in ms). This metric can be used to detect queries that no longer return any
  data.

All metrics are reported as Stackdriver custom metrics and have names prefixed by
`custom.googleapis.com/opencensus/ts_bridge/`

## Development

* Install [App Engine SDK for Go](https://cloud.google.com/appengine/docs/standard/go/download)
  (follow the "Download and install the original App Engine SDK for Go" links).
  Add it into `$PATH`
* Get the code: `go get github.com/google/ts-bridge`
* Create a `metrics.yaml` file in `app/`
* Run the app locally using dev_appserver:
  `cd app/ && dev_appserver.py app.yaml --port 18080`
* The app should be available at http://localhost:18080/
  * Note, dev_appserver does not support App Engine cron, so you'll need to run
    `curl http://localhost:18080/sync` to import metrics
* Run tests: `go test ./...`
  * If you've changed interfaces, run `go generate ./...` to update mocks
* If you've changed dependencies, run `dep ensure` to update vendored libraries
  and `Gopkg.lock`

If you'd like to contribute a patch, please see contribution guidelines in
CONTRIBUTING.md.

## Support

This is not an officially supported Google product.