module github.com/google/ts-bridge

go 1.16

require (
	cloud.google.com/go/datastore v1.19.0
	cloud.google.com/go/monitoring v1.21.1
	cloud.google.com/go/profiler v0.3.1
	contrib.go.opencensus.io/exporter/prometheus v0.4.2
	contrib.go.opencensus.io/exporter/stackdriver v0.13.10
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/dustin/go-humanize v1.0.1
	github.com/go-bindata/go-bindata/v3 v3.1.3
	github.com/golang/mock v1.7.0-rc.1
	github.com/golang/protobuf v1.5.4
	github.com/influxdata/influxdb1-client v0.0.0-20200827194710-b269163b24ab
	github.com/influxdata/influxql v1.1.0
	github.com/kisielk/errcheck v1.6.0 // indirect
	github.com/pkg/profile v1.7.0
	github.com/sirupsen/logrus v1.9.3
	github.com/timshannon/bolthold v0.0.0-20200817130212-4a25ab140645
	github.com/zorkian/go-datadog-api v2.30.0+incompatible
	go.etcd.io/bbolt v1.3.5 // indirect
	go.opencensus.io v0.24.0
	google.golang.org/api v0.218.0
	google.golang.org/genproto v0.0.0-20241021214115-324edc3d5d38
	google.golang.org/genproto/googleapis/api v0.0.0-20241209162323-e6fa225c2576
	google.golang.org/grpc v1.69.4
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/validator.v2 v2.0.1
	gopkg.in/yaml.v2 v2.4.0
)

replace golang.org/x/text v0.3.6 => golang.org/x/text v0.3.8 // CVE-2021-38561

replace golang.org/x/text v0.3.7 => golang.org/x/text v0.3.8 // CVE-2022-32149

replace github.com/prometheus/prometheus v0.35.0 => github.com/prometheus/prometheus/v2 v2.7.1 // CVE-2019-3826
