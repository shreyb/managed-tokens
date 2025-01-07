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

package db

import (
	"log/slog"
	"os"
)

var (
	debugEnabled             = false
	debugLogger  DebugLogger = &stdLogger{slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))}
)

func init() {
	if _, ok := os.LookupEnv("MANAGED_TOKENS_DEBUG"); ok {
		debugEnabled = true
	}
}

// DebugLogger is an interface for logging debug messages
type DebugLogger interface {
	Debug(...any)
}

// SetDebugLogger sets the debug logger for the db package.  The debug logger must
// satisfy the DebugLogger interface
func SetDebugLogger(logger DebugLogger) {
	debugEnabled = true
	debugLogger = logger
}

type stdLogger struct {
	l *slog.Logger
}

func (s *stdLogger) Debug(args ...any) {
	if len(args) == 0 {
		return
	}
	val, ok := args[0].(string)
	if !ok {
		return
	}
	s.l.Debug(val, args[1:]...)
}
