// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"time"

	mexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/instrument"
	"go.opentelemetry.io/otel/sdk/metric/controller/basic"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

type observedFloat struct {
	mu sync.RWMutex
	f  float64
}

func (of *observedFloat) set(v float64) {
	of.mu.Lock()
	defer of.mu.Unlock()
	of.f = v
}

func (of *observedFloat) get() float64 {
	of.mu.RLock()
	defer of.mu.RUnlock()
	return of.f
}

func newObservedFloat(v float64) *observedFloat {
	return &observedFloat{
		f: v,
	}
}

func main() {
	// Initialization. In order to pass the credentials to the exporter,
	// prepare credential file following the instruction described in this doc.
	// https://pkg.go.dev/golang.org/x/oauth2/google?tab=doc#FindDefaultCredentials
	opts := []mexporter.Option{}

	// NOTE: In current implementation of exporter, this resource is ignored because
	// the function to handle the common resource just ignore the passed resource and
	// it returned hard coded "global" resource.
	// This should be fixed in #29.
	resOpt := basic.WithResource(resource.NewWithAttributes(
		semconv.SchemaURL,
		attribute.String("instance_id", "abc123"),
		attribute.String("application", "example-app"),
	))
	pusher, err := mexporter.InstallNewPipeline(opts, resOpt)
	if err != nil {
		log.Fatalf("Failed to establish pipeline: %v", err)
	}
	ctx := context.Background()
	defer pusher.Stop(ctx)

	// Start meter
	meter := pusher.Meter("cloudmonitoring/example")

	// Register counter value
	counter, err := meter.SyncInt64().Counter("counter-a")
	if err != nil {
		log.Fatalf("Failed to create counter: %v", err)
	}
	clabels := []attribute.KeyValue{attribute.Key("key").String("value")}
	counter.Add(ctx, 100, clabels...)

	histogram, err := meter.SyncFloat64().Histogram("histogram-b")
	if err != nil {
		log.Fatalf("Failed to create histogram: %v", err)
	}

	// Register observer value
	olabels := []attribute.KeyValue{
		attribute.String("foo", "Tokyo"),
		attribute.String("bar", "Sushi"),
	}
	of := newObservedFloat(12.34)

	gaugeObserver, err := meter.AsyncFloat64().Gauge("observer-a")
	if err != nil {
		log.Panicf("failed to initialize instrument: %v", err)
	}
	_ = meter.RegisterCallback([]instrument.Asynchronous{gaugeObserver}, func(ctx context.Context) {
		v := of.get()
		gaugeObserver.Observe(ctx, v, olabels...)
	})

	// Add measurement once an every 10 second.
	timer := time.NewTicker(10 * time.Second)
	for range timer.C {
		rand.Seed(time.Now().UnixNano())

		r := rand.Int63n(100)
		cv := 100 + r
		counter.Add(ctx, cv, clabels...)

		r2 := rand.Int63n(100)
		hv := float64(r2) / 20.0
		histogram.Record(ctx, hv, clabels...)
		ov := 12.34 + hv
		of.set(ov)
		log.Printf("Most recent data: counter %v, observer %v; histogram %v", cv, ov, hv)
	}
}
