# Metric Source: Datadog

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
*   `api_key`: Datadog API key. See
    [API and application keys](https://docs.datadoghq.com/api/?lang=go#overview)
    on getting your API key.
*   `application_key`: Datadog Application key.
*   `destination`: name of the Stackdriver destination that query result will be
    written to. Destinations need to be explicitly listed in the
    `stackdriver_destinations` section of the configuration file.
*   `cumulative`: a boolean flag describing whether query result should be
    imported as a cumulative metric (a monotonically increasing counter). See
    [Cumulative metrics](#cumulative-metrics) section below for more details.

All parameters are required, except for `cumulative` (which defaults to `false`).

For metrics that have measurements more often than every minute, you might
also want to append the `.rollup()` function to avoid
[aggregation](https://docs.datadoghq.com/graphing/faq/what-is-the-granularity-of-my-graphs-am-i-seeing-raw-data-or-aggregates-on-my-graph/)
on Datadog's side.

Please keep in mind the following details about Datadog API:

*   There is an API rate limit of
    [300 queries per hour](https://docs.datadoghq.com/api/?lang=python#rate-limiting)
    that applies to the whole organization. Even if ts-bridge is the only user
    of the Query API, it still means you can only import 5 metrics if you are
    querying every minute (which is the default). The limit can be
    [raised](https://docs.datadoghq.com/api/?lang=bash#rate-limiting).
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

## Cumulative metrics

Cumulative metrics are supported through Datadog's `cumsum()` function, which
returns a monotonically increasing time series with a sum of all measurements
since the given 'start time'.

Often, for Datadog to provide a cumulative sum of all measurements,
the `.as_count()` suffix needs to be appended to metric name. Otherwise
measurements might be provided as per-second rates rather than exact counts.

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
  there are multiple measurements for this metric reported per minute. See
  [rollup documentation](https://docs.datadoghq.com/graphing/functions/rollup/)
  for more.

## Benchmark Tests

Benchmarking tests have been added to analyse the performance of data conversion
under large loads. These tests will use testdata with an exponential amount of data
points (10, 100, 1000 and 10000), stored in the form of JSON files.

You can run these tests using the following command inside this directory:

```
go test -bench=. -benchtime=30s
```
