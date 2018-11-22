Time Series Bridge is a tool that can be used to import metrics from one
monitoring system into another. It regularly runs a specific query against a
source monitoring system (currently only Datadog) and writes new time series
results into the destination system (currently only Stackdriver).

ts-bridge is an App Engine Standard app written in Go.

# Table of Contents

1.  [Setup Guide](#setup-guide)
1.  [metrics.yaml Configuration](#metricsyaml-configuration)
1.  [App Configuration](#app-configuration)
1.  [Status Page](#status-page)
1.  [Internal Monitoring](#internal-monitoring)
1.  [Troubleshooting](#troubleshooting)
1.  [Development](#development)
1.  [Support](#support)

# Setup Guide

In brief, to set up the ts-bridge app:

1.  Create a GCP project that will host the app
1.  Configure metrics for import
1.  Deploy the app and let it auto-import your metrics every minute

The following sections will guide you through this process.

## Create and Set Up a Google Cloud Project

We recommend making the project that hosts ts-bridge separate from the rest of
your infrastructure so infrastructure failures will not affect monitoring and
monitoring failues will not affect infrastructure.

1.  Log in to [GCP](https://console.cloud.google.com) and
    [create a new Google Cloud project](https://console.cloud.google.com/projectcreate)
1.  Ensure the new project is
    [linked to a billing account](https://console.cloud.google.com/billing/linkedaccount)
    (Note that the Stackdriver free tier can accommodate up to about 220
    metrics. If you have already consumed your free quota with other usage, the
    incremental cost per metric is between US$2.32 and US$0.55 per month,
    depending on which pricing tier you are already in.)
1.  Enable [stackdriver monitoring](https://console.cloud.google.com/monitoring)
    for the new project. When prompted:
    *   Create a new stackdriver account for the project
    *   Monitor only the new project (it should be selected by default)
    *   Skip AWS setup and agent installation
    *   Choose whether to receive email status reports for the project

## Set Up A Dev Environment

We recommend using [Cloud Shell](https://cloud.google.com/shell/) to prepare
ts-bridge for deployment to ensure a consistent and stable working environment.
If you need a dev environment that you can share among multiple users, consider
using a git repository and
[open-in-cloud-shell links](https://cloud.google.com/shell/docs/open-in-cloud-shell).

1.  If you are **not** using Cloud Shell:
    *   Install [go](https://golang.org/)
    *   Download and install the
        [Cloud SDK for Go](https://cloud.google.com/sdk/docs/)
        *   Initialize with the following commands to set the linked project and
            auth cookie:
        *   `gcloud init`
        *   `gcloud auth application-default login`
    *   Download and install the
        [original App Engine SDK for Go](https://cloud.google.com/appengine/docs/standard/go/download)
        (Expand the "Previous App Engine SDK" header to see it)
1.  Clone the ts-bridge source
    *   `go get github.com/google/ts-bridge/...`
    *   The ts-bridge source code should appear in
        ~/gopath/src/github.com/google/ts-bridge/

## End To End Test (Dev Server)

1.  Ensure that you either have **Owner** permissions for the whole Cloud
    project, or at minimum the **Monitoring Editor** role
1.  Create a ts-bridge config with no metrics
    *   `cd ~/gopath/src/github.com/google/ts-bridge/app; cp metrics.yaml.example metrics.yaml`
    *   Edit the yaml file, remove the datadog\_metrics sample content, and copy
        in the name of the project you just created into the
        stackdriver\_destinations section.
    *   Your `metrics.yaml` file should look like this:
    ```
    datadog_metrics:
    stackdriver_destinations:
      - name: stackdriver
        project_id: "your_project_name"
    ```
1.  Turn on the status page (uncomment #ENABLE\_STATUS\_PAGE: "yes" in `app.yaml`)
1.  Launch a dev server
    *   `dev_appserver.py app.yaml --port 18080`
1.  Test via localhost/sync
    *   `curl http://localhost:18080/sync`
1.  Verify that no error messages are shown. Troubleshooting guide:

    | Error message | Remedy |
    | --- | --- |
    | ERROR: StatsCollector: rpc error: code = PermissionDenied desc = The caller does not have permission | Ensure the authenticating user has at least the "Monitoring Editor" role |

1.  Configure metrics by getting your
    [API and application keys](https://docs.datadoghq.com/api/?lang=go#overview)
    from Datadog and copying over your Datadog queries into `metrics.yaml`
    *   Your `metrics.yaml` file should now look something like this:
    ```
    datadog_metrics:
      - name: your_first_metric_name
        query: "your metric query (copied from your Datadog dashboard)"
        api_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        application_key: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
        destination: stackdriver
      - name: your_second_metric_name
        query: "your metric query (copied from your Datadog dashboard)"
        api_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        application_key: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
        destination: stackdriver
    stackdriver_destinations:
      - name: stackdriver
        project_id: "your_project_name"
    ```
    *   see [below](#metrics-yaml-configuration) for configuration details
1.  Test metric ingestion via localhost/sync
    *   `curl http://localhost:18080/sync`
1.  Verify that metrics are visible on status page
    *   In Cloud Shell, click the ‘web preview’ button and change the port to
        18080
    *   If running on a local workstation, browse to http://localhost:18080/
1.  Verify that metrics are visible in the
    [Stackdriver UI](https://app.google.stackdriver.com/metrics-explorer)
1.  Kill the local dev server

## Deploy In Production

1.  Ensure that you either have **Owner** permissions for the whole Cloud
    project, or at minimum the **App Engine Admin** and **Cloud Scheduler
    Admin** roles
1.  Disable the status page (comment out ENABLE\_STATUS\_PAGE: "yes" in
    `app.yaml`)
    *   See [below](#status-page) if you'd like to keep the status page enabled
        in prod.
1.  Create the App Engine application
    *   `gcloud app create`
    *   Choose the App Engine region. If you are using ts-bridge to import
        metrics originating from a system running on GCP, you should run
        ts-bridge in a different Cloud region from the system itself to ensure
        independent failure domains.
1.  Deploy app
    *   `goapp deploy -application <your_project_name> -version live`
1.  Verify in the Stackdriver metrics explorer that metrics are being imported
    once a minute

# metrics.yaml Configuration

Metric sources and targets are configured in the `app/metrics.yaml` file.

## Metric Sources

### Datadog

To import a metric from Datadog, ts-bridge regularly runs a configured query
against the
[Datadog Query API](https://docs.datadoghq.com/api/?lang=python#query-time-series-points).

Metrics imported from Datadog are defined in the `datadog_metrics` section of
`app/metrics.yaml`. The following parameters need to be specified for each
metric:

*   `name`: base name of the metric. While exporting to Stackdriver, this name
    will be prefixed with `custom.googleapis.com/datadog/`.
*   `query`: Datadog query expression. This needs to return a single time series
    (tags/labels are not supported yet).
*   `api_key`: Datadog API key.
*   `application_key`: Datadog Application key.
*   `destination`: name of the Stackdriver destination that query result will be
    written to. Destinations need to be explicitly listed in the
    `stackdriver_destinations` section of the configuration file.
*   `cumulative`: a boolean flag describing whether query result should be
    imported as a cumulative metric (a monotonically increasing counter). See
    [Cumulative metrics](#cumulative-metrics) section below for more details.

All parameters are required, except for `cumulative` (that defaults to `false`).

Please keep in mind the following details about Datadog API:

*   There is an API rate limit of
    [300 queries per hour](https://docs.datadoghq.com/api/?lang=python#rate-limiting)
    that applies to the whole organization. Even if ts-bridge is the only user
    of the Query API, it still means you can only import 5 metrics if you are
    querying every minute (which is the default). The limit can be raised.
*   If you are using a
    [rollup](https://docs.datadoghq.com/graphing/miscellaneous/functions/#rollup)
    function as part of your query, Datadog will return a single point per each
    rollup interval. If rollup interval is longer than the importing period of
    ts-bridge, some import operations will fetch 0 new points. For example, if
    your query is producing a 10-minute ratio ( `xxx.rollup(sum, 600) /
    yyy.rollup(sum, 600)`) and you are using the default importing period (1
    minute), ts-bridge will still issue the query every minute, however Datadog
    will only return a single point once every 10 minutes.
*   If you are not using the `rollup` function, Datadog will return points at
    maximum possible resolution (unless the query covers a very long time
    interval). Please keep in mind that Datadog might return more than 1 point
    per minute, and all points will be written to Stackdriver, even though
    Stackdriver does not allow querying with
    [alignment period](https://cloud.google.com/monitoring/charts/metrics-selector#alignment)
    shorter than 1 minute.

## Metric Destinations

### Stackdriver

Imported metrics can be written to multiple destination Stackdriver projects,
even though in practice we expect a single instance of Time Series Bridge to
write to a single project (usually matching the GCP project where the ts-bridge
is running).

Stackdriver destinations are listed in the `stackdriver_destinations` section of
the `app/metrics.yaml` file. The following parameters can be specified for each
destination:

*   `name`: name of the Stackdriver destination. It's only used internally by
    ts-bridge to match imported metrics with destinations.
*   `project_id`: name of the Stackdriver project that metrics will be written
    to. This parameter is optional; if not specified, the same project where
    ts-bridge is running will be used.

If you are using ts-bridge to write metrics to a different Stackdriver project
from the one it's running in, you will need to grant `roles/monitoring.editor`
IAM permission to the service account used by the ts-bridge App Engine app to
allow it to read and write Stackdriver metrics.

# App Configuration

## Importing period

Time Series Bridge attempts to import all configured metrics regularly. This is
driven by the
[App Engine Cron Service](https://cloud.google.com/appengine/docs/standard/go/config/cron)
which is configured in `app/cron.yaml`. By default metrics are imported every
minute.

## Global settings

Some other settings can be set globally as App Engine environment variables via
the `env_variables` section of `app/app.yaml`.

*   `CONFIG_FILE`: name of the metric configuration file (`metrics.yaml`).
*   `SD_LOOKBACK_INTERVAL`: time interval used while searching for recent data
    in Stackdriver. This is also the default backfill interval for when no
    recent points are found. This interval should be kept reasonably short to
    avoid fetching too much data from Stackdriver on each update.
    *   You might be tempted to increase this significantly to backfill historic
        values. Please keep in mind that Stackdriver
        [does not allow](https://cloud.google.com/monitoring/custom-metrics/creating-metrics#writing-ts)
        writing points that are more than 24 hours old. Also, Datadog
        downsamples values to
        [keep the number of points in each response below ~300](https://docs.datadoghq.com/getting_started/from_the_query_to_the_graph/#how).
        This means that a single request can only cover a time period of 5 hours
        if you are aiming to get a point per minute.
*   `UPDATE_TIMEOUT`: the total time that updating all metrics is allowed to
    take. The incoming HTTP request from App Engine Cron will fail if it takes
    longer than this, and a subsequent update will be triggered again.
*   `UPDATE_PARALLELISM`: number of metric updates that are performed in
    parallel. Parallel updates are scheduled using goroutines and still happen
    in the context of a single incoming HTTP request, and setting this value too
    high might result in the App Engine instance running out of RAM.
*   `DATADOG_MIN_POINT_AGE`: minimum age of a data point returned by Datadog
    that makes it eligible for being written. Points that are very fresh
    (default is 1.5 minutes) are ignored, since Datadog might return incomplete
    data for them if some input data is delayed.
*   `DATADOG_COUNTER_RESET_INTERVAL`: while importing counters, ts-bridge needs
    to reset 'start time' regularly to keep the query time window small enough
    to avoid [aggregation](https://docs.datadoghq.com/graphing/faq/what-is-the-granularity-of-my-graphs-am-i-seeing-raw-data-or-aggregates-on-my-graph/)
    on Datadog side. This parameter defines how often a new start time is
    chosen. 30 minutes should be sufficient for metrics that have a point
    every 10 seconds. See [Cumulative metrics](#cumulative-metrics) section
    below for more details.
*   `ENABLE_STATUS_PAGE`: can be set to 'yes' to enable the status web page
    (disabled by default).

You can use `--env_var` flag to override these environment variables while
running the app via `dev_appserver.py`.

# Cumulative metrics

Stackdriver supports cumulative metrics, which are monotonically increasing
counters. Such metrics allow calculating deltas and rates over different
[alignment periods](https://cloud.google.com/monitoring/custom-metrics/reading-metrics#aligning).

While Datadog does not have first-class support for cumulative metrics, it
is possible to use the
[cumsum](https://docs.datadoghq.com/graphing/functions/arithmetic/#cumulative-sum)
query function to retreive a cumulative sum. Time Series Bridge can use
result of such queries and import them as cumulative metrics, but such
queries need to be explicitly annotated with a `cumulative` option in
`metrics.yaml` being set to `true`.

For queries that are marked as `cumulative`, ts-bridge will regularly
choose a 'start time' and then issue queries with that time passed in
the [from](https://docs.datadoghq.com/api/?lang=python#query-timeseries-points)
API parameter. As the result, Datadog will return a monotonically
increasing time series with a sum of all measurements since 'start time'.
To avoid aggregation of multiple points into one on Datadog side,
'start time' regularly gets moved forward, keeping the query time window
short (see `DATADOG_COUNTER_RESET_INTERVAL`).
Such resets are handled correctly by Stackdriver, since it requires
explicit start time to be provided for cumulative metric points.

Often, for Datadog to provide a cumulative sum of all measurements,
the `.as_count()` suffix needs to be appended to metric name. Otherwise
measurements might be provided as per-second rates rather than exact counts.

For metrics that have measurements more often than every minute, you might
also want to append the `.rollup()` function as
[described below](#writing-points-to-stackdriver-too-frequently).

For example, to import the counter metric called `http_requests` as a
cumulative metric to Stackdriver, you might configure the following query in
ts-bridge (and set `cumulative` to `true`):

    cumsum(sum:http_requests{*}.as_count().rollup(sum, 60))

To unpack this:

* `cumsum()` makes Datadog return a cumulative sum of measurements;
* `sum:` prefix ensures that sum is used as the aggregation method if there
  are multiple time series with the same metric name but different tags
  (for example, reported from different machines);
* `.as_count()` suffix gathers actual measurements rather than per-second
  rates;
* `.rollup(sum, 60)` aggregates values into 60-second intervals in case
  there are multiple measurements for this metric reported per minute.

# Status Page

If the `ENABLE_STATUS_PAGE` environment variable is set to 'yes', the index page
of the App Engine app shows a list of configured metrics along with import
status for each metric. This might be useful for debugging, however it is
disabled by default to avoid publicly exposing a list of configured metrics
(App Engine HTTP endpoints are publicly available by default).

If you choose to leave the status page enabled, we recommend configuring
[Identity-Aware Proxy](https://pantheon.corp.google.com/security/iap/project)
(IAP) for the Cloud project in which ts-bridge is running. You can use IAP to
restrict access to ts-bridge to a specific Google group or a list of Google
accounts.

# Internal Monitoring

Time Series Bridge uses [OpenCensus](https://opencensus.io/) to report several
metrics to Stackdriver:

*   `metric_import_latencies`: per-metric import latency (in ms). This metric
    has a `metric_name` field.
*   `import_latencies`: total time it took to import all metrics (in ms). If
    this becomes larger than `UPDATE_TIMEOUT`, some metrics might not be
    imported, and you might need to increase `UPDATE_PARALLELISM` or
    `UPDATE_TIMEOUT`.
*   `oldest_metric_age`: oldest time since the last written point across all
    metrics (in ms). This metric can be used to detect queries that no longer
    return any data.

All metrics are reported as Stackdriver custom metrics and have names prefixed
by `custom.googleapis.com/opencensus/ts_bridge/`

# Troubleshooting

This section describes common issues you might experience with ts-bridge.

## Writing points to Stackdriver too frequently

If your query returns more than 1 point per minute, you might be seeing the
following error from Stackdriver:

> One or more TimeSeries could not be written: One or more points were written more frequently than the maximum sampling period configured for the metric.

Stackdriver documentation
[recommends](https://cloud.google.com/monitoring/custom-metrics/creating-metrics#writing-ts)
to not add points to the same time series faster than once per minute. If your
Datadog query returns multiple points per minute, you can use the
[rollup](https://docs.datadoghq.com/graphing/functions/rollup/) function in
your query to aggregate multiple points. For example, instead of a query like
this:

    sum:http_request_count{environment:prod}

You can use a query like this:

    sum:http_request_count{environment:prod}.rollup(sum, 60)

In this example, `rollup()` will make sure the query returns a single point
per minute, which will be a sum of all points within that minute.

# Development

*   Set up a dev environment as per the [Setup Guide](#setup-guide) above.
*   Create a `metrics.yaml` file in `app/`
*   Run the app locally using dev\_appserver: `cd app/ && dev_appserver.py
    app.yaml --port 18080`
*   The app should be available at http://localhost:18080/
    *   Note, dev\_appserver does not support App Engine cron, so you'll need to
        run `curl http://localhost:18080/sync` to import metrics
*   Run tests: `go test ./...`
    *   If you've changed interfaces, run `go generate ./...` to update mocks
*   If you've changed dependencies, run `dep ensure` to update vendored
    libraries and `Gopkg.lock`

If you'd like to contribute a patch, please see contribution guidelines in
[CONTRIBUTING.md](https://github.com/google/ts-bridge/blob/master/CONTRIBUTING.md).

# Support

This is not an officially supported Google product.
