# Metric Source: InfluxDB

To import a metric from InfluxDB, ts-bridge regularly runs a configured query
against the [InfluxDB API](https://docs.influxdata.com/influxdb/v1.7/tools/api/).

Metrics imported from InfluxDB are defined in the `influxdb_metrics` section of
`app/metrics.yaml`. The following parameters need to be specified for each
metric:

*   `name`: base name of the metric. While exporting to Stackdriver, this name
    will be prefixed with `custom.googleapis.com/influxdb/`.
*   `query`: [InfluxQL](https://docs.influxdata.com/influxdb/v1.7/query_language/)
    query. This needs to return a single time series (tags/labels are not
    supported yet).
*   `database`: InfluxDB database which the query is to be executed against.
*   `endpoint`: base URL of your InfluxDB service.
*   `auth.username`: username used for authentication. If none is provided,
    no authentication will be passed.
*   `auth.password`: password used for authentication.
*   `time_aggregated`: a boolean flag describing whether the query is
    time aggregated (i.e. containing `GROUP BY time(x)`).
*   `cumulative`: a boolean flag describing whether query result should be
    imported as a cumulative metric (a monotonically increasing counter). See
    [Cumulative metrics](#cumulative-metrics) section below for more details.
*   `destination`: name of the Stackdriver destination that query result will be
    written to. Destinations need to be explicitly listed in the
    `stackdriver_destinations` section of the configuration file.

All parameters other than `auth` and the boolean flags (`time_aggregated` and
`cumulative` defaults to `false`) are required.

For metrics that have measurements more often than every minute, you might
consider time aggregating the query to avoid importing too many points. For
example, a gauge metric query such as:

`SELECT free_disk_space FROM sys_disk`

can be time aggregated as the following:

`SELECT MEAN(free_disk_space) FROM sys_disk GROUP BY time(1m)`.

When using queries with time aggregation, ts-bridge requires the
`time_aggregated` flag to be set to `true` in the metric's YAML configuration.
Otherwise, unexpected errors may arise when attempting to process these time
series.

Currently only InfluxDB 1.x series is supported.

## Cumulative metrics

Cumulative metrics are supported through InfluxDB's `CUMULATIVE_SUM` function,
which returns a montonically increasing time series with a sum of all
measurements since the given 'start time'.

This can be combined with aggregation to create counter like metrics. For
example, a popular metric required for SLIs may be:

`SELECT CUMULATIVE_SUM(COUNT(requests)) FROM nginx_access_log GROUP BY time(1m)`.

Note that the above style of nested aggregation (i.e. having a function inside
of `CUMULATIVE_SUM()`) is only supported in InfluxDB 1.7. To achieve the same
effect in InfluxDB 1.6, a subquery can be defined instead:

`SELECT CUMULATIVE_SUM(*) FROM (SELECT COUNT(request) FROM nginx_access_log
GROUP BY time(1m))`.

When using the above example, make sure to set `cumulative` to `true`, and
`time_aggregated` to `true` if applicable.

### Zero value intervals

For time aggregated cumulative queries, if no rows exist within the queried
time frame, then the cumulative query will return no results. Often however,
this is not an indication of having no data, but rather having timestamps with
value 0. To avoid this, we can use the
[fill function](https://docs.influxdata.com/influxdb/v1.6/query_language/data_exploration/#group-by-time-intervals-and-fill)
provided such as:

`SELECT CUMULATIVE_SUM(COUNT(requests)) FROM nginx_access_log GROUP BY time(1m)
FILL(0)`.
