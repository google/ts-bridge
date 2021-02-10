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
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/golang/protobuf/proto"
	"github.com/google/ts-bridge/datastore"
	"github.com/google/ts-bridge/mocks"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())
	// Save the emulator's quit channel.
	quit := datastore.Emulator(ctx, true)
	code := m.Run()
	cancel()
	// Wait for channel close before exiting the test suite
	<-quit
	os.Exit(code)
}

func mustUnmarshalText(s string, pb proto.Message) {
	if err := proto.UnmarshalText(s, pb); err != nil {
		panic(err)
	}
}

func unmarshalTimeSeries(textprotos []string) []*monitoringpb.TimeSeries {
	ts := make([]*monitoringpb.TimeSeries, 0)
	for _, textproto := range textprotos {
		pb := &monitoringpb.TimeSeries{}
		mustUnmarshalText(textproto, pb)
		ts = append(ts, pb)
	}
	return ts
}

func TestGetDescriptor(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name string
		desc *metricpb.MetricDescriptor
		err  error
		want *metricpb.MetricDescriptor
	}{
		{"metric found", &metricpb.MetricDescriptor{Name: "projects/foo/metricDescriptors/bar"}, nil, &metricpb.MetricDescriptor{Name: "projects/foo/metricDescriptors/bar"}},
		{"empty response", nil, nil, nil},
		{"gprc not found code", nil, status.Error(codes.NotFound, "Not found"), nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()
			mock := mocks.NewMockMetricClient(mockCtrl)
			mock.EXPECT().GetMetricDescriptor(gomock.Any(), gomock.Any()).Return(tt.desc, tt.err)
			a := &Adapter{mock, time.Hour}

			got, err := a.getDescriptor(ctx, "foo", "bar")
			if !proto.Equal(got, tt.want) {
				t.Errorf("getDescriptor() = %v, want %v", got, tt.want)
			}
			if err != nil {
				t.Errorf("getDescriptor() unexpected error: %v", err)
			}
		})
	}
}

func TestSetDescriptor(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name        string
		desc        *metricpb.MetricDescriptor
		descErr     error
		deleteCalls int
		deleteError error
		createCalls int
		createError error
		wantError   string
	}{
		{"no descriptor exist", nil, nil, 0, nil, 1, nil, ""},
		{"different descriptor exists",
			&metricpb.MetricDescriptor{ValueType: metricpb.MetricDescriptor_INT64, Type: "bar", Name: "projects/foo/metricDescriptors/bar", Description: "another metric"},
			nil, 1, nil, 1, nil, ""},
		{"similar descriptor exists",
			&metricpb.MetricDescriptor{ValueType: metricpb.MetricDescriptor_DOUBLE, Type: "bar2", Name: "projects/foo/metricDescriptors/bar", Description: "my metric old"},
			nil, 0, nil, 0, nil, ""},
		{"error getting descriptor",
			&metricpb.MetricDescriptor{}, fmt.Errorf("error1"), 0, nil, 0, nil, "error1"},
		{"error deleting descriptor", &metricpb.MetricDescriptor{}, nil, 1, fmt.Errorf("error2"), 0, nil, "error2"},
		{"error creating descriptor", &metricpb.MetricDescriptor{}, nil, 1, nil, 1, fmt.Errorf("error3"), "error3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			mock := mocks.NewMockMetricClient(mockCtrl)
			mock.EXPECT().GetMetricDescriptor(gomock.Any(), gomock.Any()).Return(tt.desc, tt.descErr)
			mock.EXPECT().DeleteMetricDescriptor(gomock.Any(), gomock.Any()).Times(tt.deleteCalls).Return(tt.deleteError)
			mock.EXPECT().CreateMetricDescriptor(gomock.Any(), gomock.Any()).Times(tt.createCalls).Return(&metricpb.MetricDescriptor{}, tt.createError)
			a := &Adapter{mock, time.Hour}

			err := a.setDescriptor(ctx, "foo", "bar", &metricpb.MetricDescriptor{ValueType: metricpb.MetricDescriptor_DOUBLE, Type: "bar", Description: "my metric"})
			if tt.wantError == "" && err != nil {
				t.Errorf("setDescriptor() unexpected error: %v", err)
			}
			if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Errorf("expected error from setDescriptor() to contain %v; got %v", tt.wantError, err)
			}
		})
	}
}

func TestLatestTimestampSimple(t *testing.T) {
	ctx := context.Background()

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mock := mocks.NewMockMetricClient(mockCtrl)
	mock.EXPECT().GetMetricDescriptor(gomock.Any(), gomock.Any()).Return(
		&metricpb.MetricDescriptor{Name: "projects/foo/metricDescriptors/bar"}, nil)

	latest := time.Now().Add(-13 * time.Minute).Truncate(time.Second)
	points := fmt.Sprintf(`points <interval: <end_time: <seconds: %d> start_time: <seconds: %d>>>
                           points <interval: <end_time: <seconds: %d> start_time: <seconds: %d>>>`,
		latest.Unix(), latest.Add(-2*time.Minute).Unix(),
		latest.Add(-10*time.Minute).Unix(), latest.Add(-12*time.Minute).Unix())
	mock.EXPECT().ListTimeSeries(gomock.Any(), gomock.Any()).Return(unmarshalTimeSeries([]string{points}), nil)
	a := &Adapter{mock, time.Hour}

	got, err := a.LatestTimestamp(ctx, "foo", "bar")
	if err != nil {
		t.Errorf("LatestTimestamp() unexpected error: %v", err)
	}
	if !got.Equal(latest) {
		t.Errorf("LatestTimestamp() expected %v; got %v", latest, got)
	}
}
func TestLatestTimestampBasedOnLookbackInterval(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name            string
		getDescResponse *metricpb.MetricDescriptor
		listTSResponse  []string // textproto of monitoringpb.TimeSeries
	}{
		{"metric not found", nil, nil},
		{"no time series", &metricpb.MetricDescriptor{Name: "projects/foo/metricDescriptors/bar"}, nil},
		{"no recent points", &metricpb.MetricDescriptor{Name: "projects/foo/metricDescriptors/bar"}, []string{
			fmt.Sprintf(`points <interval: <end_time: <seconds: %d> start_time: <seconds: %d>>>
                        points <interval: <end_time: <seconds: %d> start_time: <seconds: %d>>>`,
				time.Now().Add(-45*time.Minute).Unix(), time.Now().Add(-46*time.Minute).Unix(),
				time.Now().Add(-47*time.Minute).Unix(), time.Now().Add(-48*time.Minute).Unix()),
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()
			mock := mocks.NewMockMetricClient(mockCtrl)
			mock.EXPECT().GetMetricDescriptor(gomock.Any(), gomock.Any()).Return(tt.getDescResponse, nil)
			mock.EXPECT().ListTimeSeries(gomock.Any(), gomock.Any()).AnyTimes().Return(unmarshalTimeSeries(tt.listTSResponse), nil)

			a := &Adapter{mock, 30 * time.Minute}
			got, err := a.LatestTimestamp(ctx, "foo", "bar")
			if err != nil {
				t.Errorf("LatestTimestamp() unexpected error: %v", err)
			}
			// Check that the latest point is ~30 minutes in the past (since lookBackInterval is 30min)
			if got.After(time.Now().Add(-29 * time.Minute)) {
				t.Errorf("LatestTimestamp() too recent: %v", got)
			}
			if got.Before(time.Now().Add(-31 * time.Minute)) {
				t.Errorf("LatestTimestamp() too old: %v", got)
			}
		})
	}
}

func TestLatestTimestampErrors(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name           string
		getDescError   error
		listTSError    error
		listTSResponse []string // textproto of monitoringpb.TimeSeries
		wantErr        string
	}{
		{"error getting descriptor", fmt.Errorf("some error"), nil, nil, "some error"},
		{"error listing time series", nil, fmt.Errorf("another error"), nil, "another error"},
		{"multiple time series returned", nil, nil, []string{
			`metric: <type: "bar" labels <key: "label" value: "one">>`,
			`metric: <type: "bar" labels <key: "label" value: "two">>`,
		}, "Found several time series with the same name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()
			mock := mocks.NewMockMetricClient(mockCtrl)
			mock.EXPECT().GetMetricDescriptor(gomock.Any(), gomock.Any()).Return(
				&metricpb.MetricDescriptor{Name: "projects/foo/metricDescriptors/bar"}, tt.getDescError)
			mock.EXPECT().ListTimeSeries(gomock.Any(), gomock.Any()).AnyTimes().Return(unmarshalTimeSeries(tt.listTSResponse), tt.listTSError)

			a := &Adapter{mock, 30 * time.Minute}
			_, err := a.LatestTimestamp(ctx, "foo", "bar")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("LatestTimestamp() expected error to contain '%s'; got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCreateTimeseriesErrors(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name            string
		getDescError    error
		createDescError error
		createTSError   error
		wantErr         string
	}{
		{"error getting descriptor", fmt.Errorf("some error"), nil, nil, "some error"},
		{"error creating descriptor", nil, fmt.Errorf("another error"), nil, "another error"},
		{"error creating time series", nil, nil, fmt.Errorf("cool error"), "cool error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()
			mock := mocks.NewMockMetricClient(mockCtrl)
			mock.EXPECT().GetMetricDescriptor(gomock.Any(), gomock.Any()).Return(nil, tt.getDescError)
			mock.EXPECT().CreateMetricDescriptor(gomock.Any(), gomock.Any()).AnyTimes().Return(nil, tt.createDescError)
			mock.EXPECT().CreateTimeSeries(gomock.Any(), gomock.Any()).AnyTimes().Return(tt.createTSError)

			a := &Adapter{mock, time.Hour}
			err := a.CreateTimeseries(ctx, "foo", "bar", &metricpb.MetricDescriptor{ValueType: metricpb.MetricDescriptor_DOUBLE}, []*monitoringpb.TimeSeries{&monitoringpb.TimeSeries{}})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("LatestTimestamp() expected error to contain '%s'; got %v", tt.wantErr, err)
			}
		})
	}
}
