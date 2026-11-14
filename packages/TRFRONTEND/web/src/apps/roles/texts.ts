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


const en = {
  roles: 'Roles',
  role: 'Role',
  name: 'Name',
  label: 'Label',
  description: 'Description',
  package: 'Package',
  local: 'Local',
  composite: 'Composite role',
  members: 'Member roles',
  incomplete: 'Authorizations that grant nothing',
  authorizations: 'Authorizations',
  object: 'Authorization object',
  field: 'Field',
  values: 'Values',
  valuesHelp: 'Separate values with commas; * matches the rest, A..F is a range.',
  usedBy: 'Used by',
  assignments: 'Assignments',
  user: 'User',
  tenant: 'Tenant',
  validFrom: 'Valid from',
  validTo: 'Valid to',
  assign: 'Assign',
  unassign: 'Remove',
  create: 'Create local role',
  edit: 'Edit',
  delete: 'Delete',
  save: 'Save',
  cancel: 'Cancel',
  addAuthorization: 'Add authorization',
  removeAuthorization: 'Remove',
  noRoles: 'There are no roles you may see.',
  noAssignments: 'The role is assigned to no one.',
  packageRole: 'Roles of packages cannot be changed here; their package defines them.',
  confirm: 'Confirm',
  saved: 'Saved.',
  deleted: 'Deleted.',
  assigned: 'Assigned.',
  overlaps: 'The new assignment overlaps {count} existing ones.',
  signIn: 'Sign in at the start page first.',
  select: 'Select a role.',
  all: 'All authorizations',
} as const;

export type RoleTextKey = keyof typeof en;

const de: Record<RoleTextKey, string> = {
  roles: 'Rollen',
  role: 'Rolle',
  name: 'Name',
  label: 'Bezeichnung',
  description: 'Beschreibung',
  package: 'Paket',
  local: 'Lokal',
  composite: 'Sammelrolle',
  members: 'Enthaltene Rollen',
  incomplete: 'Berechtigungen, die nichts erlauben',
  authorizations: 'Berechtigungen',
  object: 'Berechtigungsobjekt',
  field: 'Feld',
  values: 'Werte',
  valuesHelp: 'Werte durch Kommas trennen; * passt auf den Rest, A..F ist ein Bereich.',
  usedBy: 'Verwendet von',
  assignments: 'Zuordnungen',
  user: 'Benutzer',
  tenant: 'Mandant',
  validFrom: 'Gültig ab',
  validTo: 'Gültig bis',
  assign: 'Zuordnen',
  unassign: 'Entfernen',
  create: 'Lokale Rolle anlegen',
  edit: 'Bearbeiten',
  delete: 'Löschen',
  save: 'Sichern',
  cancel: 'Abbrechen',
  addAuthorization: 'Berechtigung hinzufügen',
  removeAuthorization: 'Entfernen',
  noRoles: 'Es gibt keine Rollen, die Sie sehen dürfen.',
  noAssignments: 'Die Rolle ist niemandem zugeordnet.',
  packageRole: 'Rollen von Paketen lassen sich hier nicht ändern; ihr Paket definiert sie.',
  confirm: 'Bestätigen',
  saved: 'Gesichert.',
  deleted: 'Gelöscht.',
  assigned: 'Zugeordnet.',
  overlaps: 'Die neue Zuordnung überschneidet sich mit {count} bestehenden.',
  signIn: 'Melden Sie sich zuerst auf der Startseite an.',
  select: 'Wählen Sie eine Rolle.',
  all: 'Alle Berechtigungen',
};

export function roleTexts(language: string | null | undefined): Record<RoleTextKey, string> {
  return (language ?? 'en').toLowerCase() === 'de' ? de : en;
}
