/*
COPYRIGHT 2024 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by "libovsdb.modelgen"
// DO NOT EDIT.

package ovsmodel

const ManagerTable = "Manager"

type (
	ManagerConnectionMode = string
)

var (
	ManagerConnectionModeInBand    ManagerConnectionMode = "in-band"
	ManagerConnectionModeOutOfBand ManagerConnectionMode = "out-of-band"
)

// Manager defines an object in Manager table
type Manager struct {
	UUID            string                 `ovsdb:"_uuid"`
	ConnectionMode  *ManagerConnectionMode `ovsdb:"connection_mode"`
	ExternalIDs     map[string]string      `ovsdb:"external_ids"`
	InactivityProbe *int                   `ovsdb:"inactivity_probe"`
	IsConnected     bool                   `ovsdb:"is_connected"`
	MaxBackoff      *int                   `ovsdb:"max_backoff"`
	OtherConfig     map[string]string      `ovsdb:"other_config"`
	Status          map[string]string      `ovsdb:"status"`
	Target          string                 `ovsdb:"target"`
}
