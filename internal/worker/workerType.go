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

package worker

// WorkerType is a type that represents the kind of worker being referenced.  Its main use is to set configuration values that are
// worker-specific, like retry counts, timeouts, etc.
type WorkerType uint8

const (
	GetKerberosTicketsWorkerType WorkerType = iota
	StoreAndGetTokenWorkerType
	PingAggregatorWorkerType
	PushTokensWorkerType
	invalidWorkerType
)

func (wt WorkerType) String() string {
	switch wt {
	case GetKerberosTicketsWorkerType:
		return "GetKerberosTicketsWorker"
	case StoreAndGetTokenWorkerType:
		return "StoreAndGetTokenWorker"
	case PingAggregatorWorkerType:
		return "PingAggregatorWorker"
	case PushTokensWorkerType:
		return "PushTokensWorker"
	default:
		return "UnknownWorkerType"
	}
}

func isValidWorkerType(w WorkerType) bool {
	return w < invalidWorkerType
}
