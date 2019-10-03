// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package stackdriver

import (
	"context"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/appengine/log"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

//go:generate mockgen -destination=../mocks/mock_sd_metric_client.go -package=mocks github.com/google/ts-bridge/stackdriver MetricClient

// MetricClient defines Stackdriver functions used by the metric adapter.
type MetricClient interface {
	CreateMetricDescriptor(context.Context, *monitoringpb.CreateMetricDescriptorRequest) (*metricpb.MetricDescriptor, error)
	GetMetricDescriptor(context.Context, *monitoringpb.GetMetricDescriptorRequest) (*metricpb.MetricDescriptor, error)
	DeleteMetricDescriptor(ctx context.Context, req *monitoringpb.DeleteMetricDescriptorRequest) error
	CreateTimeSeries(context.Context, *monitoringpb.CreateTimeSeriesRequest) error
	ListTimeSeries(context.Context, *monitoringpb.ListTimeSeriesRequest) ([]*monitoringpb.TimeSeries, error)
	Close() error
}

// Adapter allows querying and writing Stackdriver metrics.
type Adapter struct {
	c                MetricClient
	lookBackInterval time.Duration
}

// NewAdapter returns a new Stackdriver adapter.
func NewAdapter(ctx context.Context) (*Adapter, error) {
	c, err := newClient(ctx)
	if err != nil {
		return nil, err
	}

	d, err := time.ParseDuration(os.Getenv("SD_LOOKBACK_INTERVAL"))
	if err != nil {
		return nil, fmt.Errorf("Could not parse SD_LOOKBACK_INTERVAL duration: %v", err)
	}

	return &Adapter{c, d}, nil
}

// Close closes the underlying metric client.
func (a *Adapter) Close() error {
	return a.c.Close()
}

// listTimeSeries returns a list of SD TimeSeries for a given metric name.
func (a *Adapter) listTimeSeries(ctx context.Context, project, name string) ([]*monitoringpb.TimeSeries, error) {
	endTs, err := ptypes.TimestampProto(time.Now())
	if err != nil {
		return nil, err
	}
	startTs, err := ptypes.TimestampProto(time.Now().Add(-a.lookBackInterval))
	if err != nil {
		return nil, err
	}
	return a.c.ListTimeSeries(ctx, &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", project),
		Filter: fmt.Sprintf(`metric.type = "%s"`, name),
		Interval: &monitoringpb.TimeInterval{
			StartTime: startTs,
			EndTime:   endTs,
		},
	})
}

// getDescriptor returns a metric descriptor for a given metric.
func (a *Adapter) getDescriptor(ctx context.Context, project, name string) (*metricpb.MetricDescriptor, error) {
	metric := fmt.Sprintf("projects/%s/metricDescriptors/%s", project, name)
	desc, err := a.c.GetMetricDescriptor(ctx, &monitoringpb.GetMetricDescriptorRequest{Name: metric})
	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("GetMetricDescriptor error: %s, name: %v", err, name)
	}
	return desc, nil
}

// setDescriptor installs a metric descriptor for a given metric. If there is an existing metric descriptor
// that is different, it will be deleted first.
func (a *Adapter) setDescriptor(ctx context.Context, project, name string, desc *metricpb.MetricDescriptor) error {
	desc.Name = fmt.Sprintf("projects/%s/metricDescriptors/%s", project, desc.Type)

	current, err := a.getDescriptor(ctx, project, name)
	if err != nil {
		return fmt.Errorf("Error while getting descriptor for %s: %s", name, err)
	}
	// Metric descriptors cannot be updated in-place, and deleting a descriptor requries the metric
	// to not be used for alerts. This is why the descriptor is only deleted and recreated if absolutely
	// necessary, i.e. when metric kind or value type is different.
	if current.GetMetricKind() == desc.GetMetricKind() && current.GetValueType() == desc.GetValueType() {
		return nil
	}
	if current != nil {
		log.Infof(ctx, "Deleting existing metric descriptor (%v) which is different from desired (%v)", current, desc)
		err = a.c.DeleteMetricDescriptor(ctx, &monitoringpb.DeleteMetricDescriptorRequest{Name: current.Name})
		if err != nil {
			return fmt.Errorf("DeleteMetricDescriptor error: %s", err)
		}
	}
	log.Infof(ctx, "Creating a new metric descriptor: %v", desc.Name)
	_, err = a.c.CreateMetricDescriptor(ctx, &monitoringpb.CreateMetricDescriptorRequest{
		Name:             fmt.Sprintf("projects/%s", project),
		MetricDescriptor: desc,
	})
	if err != nil {
		return fmt.Errorf("CreateMetricDescriptor error: %s, descriptor: %v", err, desc)
	}
	return nil
}

// LatestTimestamp determines the timestamp of a latest point for a given metric in SD.
// If metric does not exist, a timestamp which is `lookBackInterval` ago in the past is returned to backfill some data from Datadog.
func (a *Adapter) LatestTimestamp(ctx context.Context, project, name string) (time.Time, error) {
	latest := time.Now().Add(-a.lookBackInterval)

	desc, err := a.getDescriptor(ctx, project, name)
	if err != nil {
		return latest, err
	}
	if desc == nil {
		log.Debugf(ctx, "No metric descriptor found for %s", name)
		return latest, nil
	}

	series, err := a.listTimeSeries(ctx, project, name)
	if err != nil {
		return latest, err
	}

	if len(series) == 0 {
		log.Debugf(ctx, "No timeseries found for %s", name)
		return latest, nil
	}
	if len(series) > 1 {
		log.Debugf(ctx, "Several timeseries found for %s: %v", name, series)
		return latest, fmt.Errorf("Found several time series with the same name: %v", series)
	}

	for _, point := range series[0].Points {
		ts, err := ptypes.Timestamp(point.Interval.EndTime)
		if err != nil {
			return latest, nil
		}
		if ts.After(latest) {
			latest = ts
		}
	}

	log.Debugf(ctx, "Latest point found for %s is %v", name, latest)
	return latest, nil
}

// CreateTimeseries writes time series data (new data points) for a given metric into Stackdriver.
// It also creates a metric descriptor if it does not exist.
func (a *Adapter) CreateTimeseries(ctx context.Context, project, name string, desc *metricpb.MetricDescriptor, series []*monitoringpb.TimeSeries) error {
	if err := a.setDescriptor(ctx, project, name, desc); err != nil {
		return err
	}

	for _, ts := range series {
		err := a.c.CreateTimeSeries(ctx, &monitoringpb.CreateTimeSeriesRequest{
			Name:       fmt.Sprintf("projects/%s", project),
			TimeSeries: []*monitoringpb.TimeSeries{ts},
		})
		if err != nil {
			return fmt.Errorf("CreateTimeSeries error: %s, timeseries: %v", err, ts)
		}
	}
	return nil
}
