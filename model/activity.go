// Copyright 2026 The go-runtime Authors
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

package model

// ActivityDomain is the domain of the field ACTIVITY of authorization
// objects; its fixed values are the codes of the activities. The data
// element of the field has the same name.
const ActivityDomain = KernelNamespace + "ACTIVITY"

// ActivityField is the name of the field of authorization objects that
// takes the activities; the kernel gives it its type.
const ActivityField = "ACTIVITY"

// Activity is something a user may do with the data an authorization
// object protects. Codes never change; the kernel adds new activities at
// the end, together with a fixed value of ActivityDomain.
type Activity struct {
	Code  string                  `json:"code"`
	Texts map[string]ActivityText `json:"texts"` // by language
}

// ActivityText is the label and explanation of an activity in a language.
type ActivityText struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Activities are the activities of the kernel in their order.
var Activities = []Activity{
	{"READ", map[string]ActivityText{
		"en": {"Read", "Display and read data"},
		"de": {"Lesen", "Daten anzeigen und lesen"}}},
	{"CREATE", map[string]ActivityText{
		"en": {"Create", "Create new data"},
		"de": {"Anlegen", "Neue Daten anlegen"}}},
	{"UPDATE", map[string]ActivityText{
		"en": {"Update", "Change existing data"},
		"de": {"Ändern", "Bestehende Daten ändern"}}},
	{"DELETE", map[string]ActivityText{
		"en": {"Delete", "Delete data"},
		"de": {"Löschen", "Daten löschen"}}},
	{"EXECUTE", map[string]ActivityText{
		"en": {"Execute", "Start a processing, evaluation or action"},
		"de": {"Ausführen", "Eine Verarbeitung, Auswertung oder Aktion starten"}}},
	{"APPROVE", map[string]ActivityText{
		"en": {"Approve", "Approve or release in business terms"},
		"de": {"Genehmigen", "Fachlich genehmigen oder freigeben"}}},
	{"REJECT", map[string]ActivityText{
		"en": {"Reject", "Reject in business terms"},
		"de": {"Ablehnen", "Fachlich ablehnen"}}},
	{"CANCEL", map[string]ActivityText{
		"en": {"Cancel", "Cancel or reverse a posted or completed transaction"},
		"de": {"Stornieren", "Einen gebuchten oder abgeschlossenen Vorgang stornieren oder zurücknehmen"}}},
	{"IMPORT", map[string]ActivityText{
		"en": {"Import", "Take over data from files or other systems"},
		"de": {"Importieren", "Daten aus Dateien oder Fremdsystemen übernehmen"}}},
	{"EXPORT", map[string]ActivityText{
		"en": {"Export", "Download data or output it as a file"},
		"de": {"Exportieren", "Daten herunterladen oder als Datei ausgeben"}}},
	{"PRINT", map[string]ActivityText{
		"en": {"Print", "Output and print"},
		"de": {"Drucken", "Ausgeben und drucken"}}},
	{"SEND", map[string]ActivityText{
		"en": {"Send", "Transmit to third parties, e.g. by e-mail or interface"},
		"de": {"Senden", "An Dritte übermitteln, z. B. per E-Mail oder Schnittstelle"}}},
	{"ADMINISTER", map[string]ActivityText{
		"en": {"Administer", "Maintain settings and administrative functions"},
		"de": {"Verwalten", "Einstellungen und Verwaltungsfunktionen pflegen"}}},
}

// ActivityByCode returns the activity with a code.
func ActivityByCode(code string) (*Activity, bool) {
	for i := range Activities {
		if Activities[i].Code == code {
			return &Activities[i], true
		}
	}
	return nil, false
}
