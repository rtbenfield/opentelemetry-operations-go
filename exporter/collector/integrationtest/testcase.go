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

package integrationtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sort"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace"
	distributionpb "google.golang.org/genproto/googleapis/api/distribution"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/collector"
)

var (
	// selfObsMetricsToNormalize is the set of self-observability metrics which may not record
	// the same value every time due to side effects. The values of these metrics get cleared
	// and are not checked in the fixture. Their labels and types are still checked.
	selfObsMetricsToNormalize = map[string]struct{}{
		"custom.googleapis.com/opencensus/grpc.io/client/roundtrip_latency":      {},
		"custom.googleapis.com/opencensus/grpc.io/client/sent_bytes_per_rpc":     {},
		"custom.googleapis.com/opencensus/grpc.io/client/received_bytes_per_rpc": {},
	}
)

const secondProjectEnv = "SECOND_PROJECT_ID"

type TestCase struct {
	// Configure will be called to modify the default configuration for this test case. Optional.
	Configure func(cfg *collector.Config)
	// Name of the test case
	Name string
	// OTLPInputFixturePath is the path to the JSON encoded OTLP
	// ExportMetricsServiceRequest input metrics fixture.
	OTLPInputFixturePath string
	// ExpectFixturePath is the path to the JSON encoded MetricExpectFixture
	// (see fixtures.proto) that contains request messages the exporter is expected to send.
	ExpectFixturePath string
	// Skip, if true, skips this test case
	Skip bool
	// ExpectErr sets whether the test is expected to fail
	ExpectErr bool
}

func (tc *TestCase) LoadOTLPTracesInput(
	t testing.TB,
	startTimestamp time.Time,
	endTimestamp time.Time,
) ptrace.Traces {
	bytes, err := ioutil.ReadFile(tc.OTLPInputFixturePath)
	require.NoError(t, err)
	traces, err := ptrace.NewJSONUnmarshaler().UnmarshalTraces(bytes)
	require.NoError(t, err)

	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		rs := traces.ResourceSpans().At(i)
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			sss := rs.ScopeSpans().At(j)
			for k := 0; k < sss.Spans().Len(); k++ {
				span := sss.Spans().At(k)
				if span.StartTimestamp() != 0 {
					span.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimestamp))
				}
				if span.EndTimestamp() != 0 {
					span.SetEndTimestamp(pcommon.NewTimestampFromTime(endTimestamp))
				}
			}
		}
	}
	return traces
}

func (tc *TestCase) LoadTraceExpectFixture(
	t testing.TB,
	startTimestamp time.Time,
	endTimestamp time.Time,
) *TraceExpectFixture {
	bytes, err := ioutil.ReadFile(tc.ExpectFixturePath)
	require.NoError(t, err)
	fixture := &TraceExpectFixture{}
	require.NoError(t, protojson.Unmarshal(bytes, fixture))

	for _, request := range fixture.BatchWriteSpansRequest {
		for _, span := range request.Spans {
			span.StartTime = timestamppb.New(startTimestamp)
			span.EndTime = timestamppb.New(endTimestamp)
		}
	}

	return fixture
}

func (tc *TestCase) SaveRecordedTraceFixtures(
	t testing.TB,
	fixture *TraceExpectFixture,
) {
	normalizeTraceFixture(t, fixture)

	jsonBytes, err := protojson.Marshal(fixture)
	require.NoError(t, err)
	formatted := bytes.Buffer{}
	require.NoError(t, json.Indent(&formatted, jsonBytes, "", "  "))
	formatted.WriteString("\n")
	require.NoError(t, ioutil.WriteFile(tc.ExpectFixturePath, formatted.Bytes(), 0640))
	t.Logf("Updated fixture %v", tc.ExpectFixturePath)
}

func normalizeTraceFixture(t testing.TB, fixture *TraceExpectFixture) {
	for _, req := range fixture.BatchWriteSpansRequest {
		for _, span := range req.Spans {
			if span.GetStartTime() != nil {
				span.StartTime = &timestamppb.Timestamp{}
			}
			if span.GetEndTime() != nil {
				span.EndTime = &timestamppb.Timestamp{}
			}
		}
	}
}

func (tc *TestCase) CreateTraceConfig() collector.Config {
	cfg := collector.DefaultConfig()
	cfg.ProjectID = "fake-project"

	if tc.Configure != nil {
		tc.Configure(&cfg)
	}

	return cfg
}

func (tc *TestCase) LoadOTLPLogsInput(
	t testing.TB,
	timestamp time.Time,
) plog.Logs {
	bytes, err := ioutil.ReadFile(tc.OTLPInputFixturePath)
	require.NoError(t, err)
	logs, err := plog.NewJSONUnmarshaler().UnmarshalLogs(bytes)
	require.NoError(t, err)

	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		rl := logs.ResourceLogs().At(i)
		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sls := rl.ScopeLogs().At(j)
			for k := 0; k < sls.LogRecords().Len(); k++ {
				log := sls.LogRecords().At(k)
				if log.Timestamp() != 0 {
					log.SetTimestamp(pcommon.NewTimestampFromTime(timestamp))
				}
			}
		}
	}
	return logs
}

func (tc *TestCase) CreateLogConfig() collector.Config {
	cfg := collector.DefaultConfig()
	cfg.ProjectID = "fake-project"

	if tc.Configure != nil {
		tc.Configure(&cfg)
	}

	return cfg
}

func (tc *TestCase) LoadLogExpectFixture(
	t testing.TB,
	timestamp time.Time,
) *LogExpectFixture {
	bytes, err := ioutil.ReadFile(tc.ExpectFixturePath)
	require.NoError(t, err)
	fixture := &LogExpectFixture{}
	require.NoError(t, protojson.Unmarshal(bytes, fixture))

	for _, request := range fixture.WriteLogEntriesRequests {
		for _, entry := range request.Entries {
			entry.Timestamp = timestamppb.New(timestamp)
		}
	}

	return fixture
}

func (tc *TestCase) SaveRecordedLogFixtures(
	t testing.TB,
	fixture *LogExpectFixture,
) {
	normalizeLogFixture(t, fixture)

	jsonBytes, err := protojson.Marshal(fixture)
	require.NoError(t, err)
	formatted := bytes.Buffer{}
	require.NoError(t, json.Indent(&formatted, jsonBytes, "", "  "))
	formatted.WriteString("\n")
	require.NoError(t, ioutil.WriteFile(tc.ExpectFixturePath, formatted.Bytes(), 0640))
	t.Logf("Updated fixture %v", tc.ExpectFixturePath)
}

// Normalizes timestamps which create noise in the fixture because they can
// vary each test run
func normalizeLogFixture(t testing.TB, fixture *LogExpectFixture) {
	for _, req := range fixture.WriteLogEntriesRequests {
		for _, entry := range req.Entries {
			// Normalize timestamps if they are set
			if entry.GetTimestamp() != nil {
				entry.Timestamp = &timestamppb.Timestamp{}
			}
		}
	}
}

// Load OTLP metric fixture, test expectation fixtures and modify them so they're suitable for
// testing. Currently, this just updates the timestamps.
func (tc *TestCase) LoadOTLPMetricsInput(
	t testing.TB,
	startTime time.Time,
	endTime time.Time,
) pmetric.Metrics {
	bytes, err := ioutil.ReadFile(tc.OTLPInputFixturePath)
	require.NoError(t, err)
	metrics, err := pmetric.NewJSONUnmarshaler().UnmarshalMetrics(bytes)
	require.NoError(t, err)

	// Interface with common fields that pdata metric points have
	type point interface {
		StartTimestamp() pcommon.Timestamp
		Timestamp() pcommon.Timestamp
		SetStartTimestamp(pcommon.Timestamp)
		SetTimestamp(pcommon.Timestamp)
	}
	updatePoint := func(p point) {
		if p.StartTimestamp() != 0 {
			p.SetStartTimestamp(pcommon.NewTimestampFromTime(startTime))
		}
		if p.Timestamp() != 0 {
			p.SetTimestamp(pcommon.NewTimestampFromTime(endTime))
		}
	}

	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		rm := metrics.ResourceMetrics().At(i)
		for i := 0; i < rm.ScopeMetrics().Len(); i++ {
			sms := rm.ScopeMetrics().At(i)
			for i := 0; i < sms.Metrics().Len(); i++ {
				m := sms.Metrics().At(i)

				switch m.DataType() {
				case pmetric.MetricDataTypeGauge:
					for i := 0; i < m.Gauge().DataPoints().Len(); i++ {
						updatePoint(m.Gauge().DataPoints().At(i))
					}
				case pmetric.MetricDataTypeSum:
					for i := 0; i < m.Sum().DataPoints().Len(); i++ {
						updatePoint(m.Sum().DataPoints().At(i))
					}
				case pmetric.MetricDataTypeHistogram:
					for i := 0; i < m.Histogram().DataPoints().Len(); i++ {
						updatePoint(m.Histogram().DataPoints().At(i))
					}
				case pmetric.MetricDataTypeSummary:
					for i := 0; i < m.Summary().DataPoints().Len(); i++ {
						updatePoint(m.Summary().DataPoints().At(i))
					}
				case pmetric.MetricDataTypeExponentialHistogram:
					for i := 0; i < m.ExponentialHistogram().DataPoints().Len(); i++ {
						updatePoint(m.ExponentialHistogram().DataPoints().At(i))
					}
				}
			}
		}
	}

	return metrics
}

func (tc *TestCase) LoadMetricExpectFixture(
	t testing.TB,
	startTime time.Time,
	endTime time.Time,
) *MetricExpectFixture {
	bytes, err := ioutil.ReadFile(tc.ExpectFixturePath)
	require.NoError(t, err)
	fixture := &MetricExpectFixture{}
	require.NoError(t, protojson.Unmarshal(bytes, fixture))
	tc.updateMetricExpectFixture(t, startTime, endTime, fixture)

	return fixture
}

func (tc *TestCase) updateMetricExpectFixture(
	t testing.TB,
	startTime time.Time,
	endTime time.Time,
	fixture *MetricExpectFixture,
) {
	reqs := append(
		fixture.GetCreateTimeSeriesRequests(),
		fixture.GetCreateServiceTimeSeriesRequests()...,
	)
	for _, req := range reqs {
		for _, ts := range req.GetTimeSeries() {
			for _, p := range ts.GetPoints() {
				if p.GetInterval().GetStartTime() != nil {
					p.GetInterval().StartTime = timestamppb.New(startTime)
				}
				if p.GetInterval().GetEndTime() != nil {
					p.GetInterval().EndTime = timestamppb.New(endTime)
				}
			}
		}

	}
}

func (tc *TestCase) SaveRecordedMetricFixtures(
	t testing.TB,
	fixture *MetricExpectFixture,
) {
	normalizeMetricFixture(t, fixture)

	jsonBytes, err := protojson.Marshal(fixture)
	require.NoError(t, err)
	formatted := bytes.Buffer{}
	require.NoError(t, json.Indent(&formatted, jsonBytes, "", "  "))
	formatted.WriteString("\n")
	require.NoError(t, ioutil.WriteFile(tc.ExpectFixturePath, formatted.Bytes(), 0640))
	t.Logf("Updated fixture %v", tc.ExpectFixturePath)
}

// Normalizes timestamps which create noise in the fixture because they can
// vary each test run
func normalizeMetricFixture(t testing.TB, fixture *MetricExpectFixture) {
	normalizeTimeSeriesReqs(t, fixture.CreateTimeSeriesRequests...)
	normalizeTimeSeriesReqs(t, fixture.CreateServiceTimeSeriesRequests...)
	normalizeMetricDescriptorReqs(t, fixture.CreateMetricDescriptorRequests...)
	normalizeSelfObs(t, fixture.SelfObservabilityMetrics)
}

func normalizeTimeSeriesReqs(t testing.TB, reqs ...*monitoringpb.CreateTimeSeriesRequest) {
	for _, req := range reqs {
		for _, ts := range req.TimeSeries {
			for _, p := range ts.Points {
				// Normalize timestamps if they are set
				if p.GetInterval().GetStartTime() != nil {
					p.GetInterval().StartTime = &timestamppb.Timestamp{}
				}
				if p.GetInterval().GetEndTime() != nil {
					p.GetInterval().EndTime = &timestamppb.Timestamp{}
				}
			}

			// clear project ID from monitored resource
			delete(ts.GetResource().GetLabels(), "project_id")
		}
	}
}

func normalizeMetricDescriptorReqs(t testing.TB, reqs ...*monitoringpb.CreateMetricDescriptorRequest) {
	for _, req := range reqs {
		if req.MetricDescriptor == nil {
			continue
		}
		md := req.MetricDescriptor
		sort.Slice(md.Labels, func(i, j int) bool {
			return md.Labels[i].Key < md.Labels[j].Key
		})
	}
}

func normalizeSelfObs(t testing.TB, selfObs *SelfObservabilityMetric) {
	for _, req := range selfObs.CreateTimeSeriesRequests {
		normalizeTimeSeriesReqs(t, req)
		tss := req.TimeSeries
		for _, ts := range tss {
			if _, ok := selfObsMetricsToNormalize[ts.Metric.Type]; ok {
				// zero out the specific value type
				switch value := ts.Points[0].Value.Value.(type) {
				case *monitoringpb.TypedValue_Int64Value:
					value.Int64Value = 0
				case *monitoringpb.TypedValue_DoubleValue:
					value.DoubleValue = 0
				case *monitoringpb.TypedValue_DistributionValue:
					// Only preserve the bucket options and zeroed out counts
					for i := range value.DistributionValue.BucketCounts {
						value.DistributionValue.BucketCounts[i] = 0
					}
					value.DistributionValue = &distributionpb.Distribution{
						BucketOptions: value.DistributionValue.BucketOptions,
						BucketCounts:  value.DistributionValue.BucketCounts,
					}
				default:
					t.Logf("Do not know how to normalize typed value type %T", value)
				}
			}
		}
		// sort time series by (type, labelset)
		sort.Slice(tss, func(i, j int) bool {
			iMetric := tss[i].Metric
			jMetric := tss[j].Metric
			if iMetric.Type == jMetric.Type {
				// Doesn't need to sorted correctly, just consistently
				return fmt.Sprint(iMetric.Labels) < fmt.Sprint(jMetric.Labels)
			}
			return iMetric.Type < jMetric.Type
		})
	}

	normalizeMetricDescriptorReqs(t, selfObs.CreateMetricDescriptorRequests...)
	// sort descriptors by type
	sort.Slice(selfObs.CreateMetricDescriptorRequests, func(i, j int) bool {
		return selfObs.CreateMetricDescriptorRequests[i].MetricDescriptor.Type <
			selfObs.CreateMetricDescriptorRequests[j].MetricDescriptor.Type
	})
}

func (tc *TestCase) SkipIfNeeded(t testing.TB) {
	if tc.Skip {
		t.Skip("Test case is marked to skip in internal/integrationtest/testcases.go")
	}
}

func (tc *TestCase) CreateMetricConfig() collector.Config {
	cfg := collector.DefaultConfig()
	cfg.ProjectID = "fakeprojectid"
	// Set a big buffer to capture all CMD requests without dropping
	cfg.MetricConfig.CreateMetricDescriptorBufferSize = 500
	cfg.MetricConfig.InstrumentationLibraryLabels = false

	if tc.Configure != nil {
		tc.Configure(&cfg)
	}

	return cfg
}
