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

package main

import (
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"

	"github.com/fermitools/managed-tokens/internal/tracing"
)

func logErrorWithTracing(l *logrus.Entry, span trace.Span, e error, keyValues ...tracing.KeyValueForLog) {
	for _, kv := range keyValues {
		l = l.WithField(kv.Key, kv.Value)
	}
	l.Error(e)
	tracing.LogErrorWithTrace(span, e, keyValues...)
}

func logSuccessWithTracing(l *logrus.Entry, span trace.Span, msg string, keyValues ...tracing.KeyValueForLog) {
	for _, kv := range keyValues {
		l = l.WithField(kv.Key, kv.Value)
	}
	l.Info(msg)
	tracing.LogSuccessWithTrace(span, msg, keyValues...)
}