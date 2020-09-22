// Copyright 2020 Google LLC
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

// Package storage provides generic interfaces for pluggable storage engines and associated
// abstract storage implementation logic
package storage

import (
	"context"
	"time"
)

//go:generate mockgen -destination=../mocks/mock_storage_manager.go -package=mocks github.com/google/ts-bridge/storage Manager

// Manager interface implemented by associated storage manager, e.g. Datastore, BoltDB, etc.
type Manager interface {
	NewMetricRecord(ctx context.Context, name, query string) (MetricRecord, error)
	CleanupRecords(ctx context.Context, keep []string) error
	Close() error
}

//go:generate mockgen -destination=../mocks/mock_metric_record.go -package=mocks github.com/google/ts-bridge/storage MetricRecord

// MetricRecord is an interface implemented by StoredMetricRecord.
type MetricRecord interface {
	UpdateError(ctx context.Context, e error) error
	UpdateSuccess(ctx context.Context, points int, msg string) error
	GetLastUpdate() time.Time
	GetCounterStartTime() time.Time
	SetCounterStartTime(ctx context.Context, start time.Time) error
}
