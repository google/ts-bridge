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

	"github.com/google/ts-bridge/version"

	monitoring "cloud.google.com/go/monitoring/apiv3"
	"google.golang.org/api/iterator"
	option "google.golang.org/api/option"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// client wraps Stackdriver metric client, implementing MetricClient interface.
type client struct {
	sd *monitoring.MetricClient
}

// NewClient returns a new client.
func newClient(ctx context.Context) (*client, error) {
	opts := []option.ClientOption{
		option.WithUserAgent(version.UserAgent()),
	}
	sd, err := monitoring.NewMetricClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &client{sd}, nil
}

// Close closes the metric client.
func (c *client) Close() error {
	return c.sd.Close()
}

func (c *client) CreateMetricDescriptor(ctx context.Context, req *monitoringpb.CreateMetricDescriptorRequest) (*metricpb.MetricDescriptor, error) {
	return c.sd.CreateMetricDescriptor(ctx, req)
}

func (c *client) GetMetricDescriptor(ctx context.Context, req *monitoringpb.GetMetricDescriptorRequest) (*metricpb.MetricDescriptor, error) {
	return c.sd.GetMetricDescriptor(ctx, req)
}

func (c *client) DeleteMetricDescriptor(ctx context.Context, req *monitoringpb.DeleteMetricDescriptorRequest) error {
	return c.sd.DeleteMetricDescriptor(ctx, req)
}

func (c *client) CreateTimeSeries(ctx context.Context, req *monitoringpb.CreateTimeSeriesRequest) error {
	return c.sd.CreateTimeSeries(ctx, req)
}

func (c *client) ListTimeSeries(ctx context.Context, req *monitoringpb.ListTimeSeriesRequest) ([]*monitoringpb.TimeSeries, error) {
	it := c.sd.ListTimeSeries(ctx, req)
	var series []*monitoringpb.TimeSeries
	for {
		t, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		series = append(series, t)
	}
	return series, nil
}
