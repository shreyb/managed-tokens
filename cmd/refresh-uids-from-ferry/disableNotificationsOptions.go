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

// cmdUtils provides utilities that are meant to be used by multiple executables that use the
// managed tokens libraries
package main

// Here we define all the possible ways to disable notifications

type disableNotificationsOption uint

const (
	DISABLED_BY_CONFIGURATION disableNotificationsOption = iota
	DISABLED_BY_FLAG
)

func (d disableNotificationsOption) String() string {
	switch d {
	case DISABLED_BY_CONFIGURATION:
		return "configuration"
	case DISABLED_BY_FLAG:
		return "flag"
	default:
		return "unknown"
	}
}
