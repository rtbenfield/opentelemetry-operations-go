// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Script to record test expectation fixtures and save them to disk.

package main

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/collector/internal/integrationtest"
)

type FakeTesting struct {
	testing.TB
}

func (t *FakeTesting) Logf(format string, args ...interface{}) {
	log.Printf(format, args...)
}
func (t *FakeTesting) Errorf(format string, args ...interface{}) {
	panic(fmt.Errorf(format, args...))
}
func (t *FakeTesting) FailNow() {
	t.Errorf("FailNow()")
}
func (t *FakeTesting) Helper()      {}
func (t *FakeTesting) Name() string { return "record fixtures" }

func main() {
	ctx := context.Background()
	endTime := time.Now()
	startTime := endTime.Add(-time.Second)
	t := &FakeTesting{}

	recordLogs(ctx, t, endTime)
	recordMetrics(ctx, t, startTime, endTime)
	recordTraces(ctx, t, startTime, endTime)
}

func recordTraces(ctx context.Context, t *FakeTesting, startTime, endTime time.Time) {
	testServer, err := integrationtest.NewTracesTestServer()
	if err != nil {
		panic(err)
	}
	go testServer.Serve()
	defer testServer.Shutdown()

	for _, test := range integrationtest.TracesTestCases {
		if test.Skip {
			continue
		}

		func() {
			traces := test.LoadOTLPTracesInput(t, startTime, endTime)
			testServerExporter := testServer.NewExporter(ctx, t, test.CreateTraceConfig())

			require.NoError(t, testServerExporter.PushTraces(ctx, traces), "failed to export logs to local test server")
			require.NoError(t, testServerExporter.Shutdown(ctx))

			require.NoError(t, err)
			fixture := &integrationtest.TraceExpectFixture{
				BatchWriteSpansRequest: testServer.CreateBatchWriteSpansRequests(),
			}
			test.SaveRecordedTraceFixtures(t, fixture)
		}()
	}
}

func recordLogs(ctx context.Context, t *FakeTesting, timestamp time.Time) {
	testServer, err := integrationtest.NewLoggingTestServer()
	if err != nil {
		panic(err)
	}
	go testServer.Serve()
	defer testServer.Shutdown()

	for _, test := range integrationtest.LogsTestCases {
		if test.Skip {
			continue
		}
		func() {
			logs := test.LoadOTLPLogsInput(t, timestamp)
			testServerExporter := testServer.NewExporter(ctx, t, test.CreateLogConfig())

			require.NoError(t, testServerExporter.PushLogs(ctx, logs), "failed to export logs to local test server")
			require.NoError(t, testServerExporter.Shutdown(ctx))

			require.NoError(t, err)
			fixture := &integrationtest.LogExpectFixture{
				WriteLogEntriesRequests: testServer.CreateWriteLogEntriesRequests(),
			}
			test.SaveRecordedLogFixtures(t, fixture)
		}()
	}
}

func recordMetrics(ctx context.Context, t *FakeTesting, startTime, endTime time.Time) {
	testServer, err := integrationtest.NewMetricTestServer()
	if err != nil {
		panic(err)
	}
	go testServer.Serve()
	defer testServer.Shutdown()

	for _, test := range integrationtest.MetricsTestCases {
		if test.Skip {
			continue
		}
		func() {
			metrics := test.LoadOTLPMetricsInput(t, startTime, endTime)
			testServerExporter := testServer.NewExporter(ctx, t, test.CreateMetricConfig())
			inMemoryOCExporter, err := integrationtest.NewInMemoryOCViewExporter()
			require.NoError(t, err)
			defer inMemoryOCExporter.Shutdown(ctx)

			err = testServerExporter.PushMetrics(ctx, metrics)
			if !test.ExpectErr {
				require.NoError(t, err, "failed to export metrics to local test server")
			} else {
				require.Error(t, err, "didn't record expected error")
			}
			require.NoError(t, testServerExporter.Shutdown(ctx))

			selfObsMetrics, err := inMemoryOCExporter.Proto(ctx)
			require.NoError(t, err)
			fixture := &integrationtest.MetricExpectFixture{
				CreateMetricDescriptorRequests:  testServer.CreateMetricDescriptorRequests(),
				CreateTimeSeriesRequests:        testServer.CreateTimeSeriesRequests(),
				CreateServiceTimeSeriesRequests: testServer.CreateServiceTimeSeriesRequests(),
				SelfObservabilityMetrics:        selfObsMetrics,
			}
			test.SaveRecordedMetricFixtures(t, fixture)
		}()
	}
}
