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

// package worker contains the various workers that can be used concurrently to perform the various operations supported by the managed tokens library
package worker

import (
	"context"

	"go.opentelemetry.io/otel"
)

// Worker is a function, controlled by a context, that performs some operations based on the values passed through the
// channelGroup.GetServiceConfigChan() channel. The worker should report success or failure through the channelGroup.GetSuccessChan()
type Worker func(context.Context, channelGroup)

var tracer = otel.Tracer("worker")
