// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmdUtils

import (
	"context"
	"sync"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// scheddCache is a cache where the schedds corresponding to each collector are stored.  It is a container for a map[string]*scheddCacheEntry,
// where the key is the collector host, and a mutex to control access to this map
type scheddCache struct {
	cache map[string]*scheddCacheEntry
	mu    *sync.Mutex
}

// scheddCacheEntry is an entry that contains a *scheddCollection and a *sync.Once to ensure that it is populated exactly once
type scheddCacheEntry struct {
	*scheddCollection
	once *sync.Once
	err  error // error encountered while populating the cache entry
}

// populateFromCollector queries the condor collector for the schedds and stores them in scheddCacheEntry
func (s *scheddCacheEntry) populateFromCollector(ctx context.Context, collectorHost, constraint string) error {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "cmdUtils.populateFromCollector")
	span.SetAttributes(
		attribute.KeyValue{Key: "collectorHost", Value: attribute.StringValue(collectorHost)},
		attribute.KeyValue{Key: "constraint", Value: attribute.StringValue(constraint)},
	)
	defer span.End()

	schedds, err := getScheddsFromCondor(ctx, collectorHost, constraint)
	if err != nil {
		msg := "Could not populate schedd Cache Entry from condor"
		span.SetStatus(codes.Error, msg)
		span.RecordError(err)
		log.Error(msg)
		return err
	}
	s.scheddCollection.storeSchedds(schedds)
	span.SetStatus(codes.Ok, "Schedds populated from collector")
	return nil
}
