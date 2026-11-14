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


// The texts of the shell in English and German; other languages fall back
// to English. Texts of apps, tiles and spaces come from the start page
// service in the language of the session.

const en = {
  title: 'go-runtime',
  signInTitle: 'Sign in',
  user: 'User',
  password: 'Password',
  signIn: 'Sign in',
  signInFailed: 'The user or the password is wrong, or the user is locked.',
  sessionExpired: 'Your session expired. Please sign in again.',
  search: 'Search apps',
  home: 'Start page',
  language: 'Language',
  tenant: 'Tenant',
  theme: 'Theme',
  themeAuto: 'Like the system',
  themeLight: 'Light',
  themeDark: 'Dark',
  density: 'Density',
  densityCompact: 'Compact',
  densityComfortable: 'Comfortable',
  signOut: 'Sign out',
  spaces: 'Spaces',
  noSpaces: 'There are no spaces for your roles yet. Ask an administrator to assign a role with a start page.',
  loadFailed: 'The start page could not be loaded.',
  retry: 'Try again',
  appUnknown: 'This app does not exist or you may not start it.',
  remoteFailed: 'The app could not be loaded.',
  opened: 'The app opened in a new window.',
  checked: 'Selected',
  create: 'Create',
  edit: 'Edit',
  save: 'Save',
  cancel: 'Cancel',
  delete: 'Delete',
  go: 'Go',
  clearFilter: 'Clear',
  filters: 'Filters',
  actions: 'Actions',
  run: 'Run',
  yes: 'Yes',
  no: 'No',
  shipped: 'Shipped',
  local: 'Local',
  noEntries: 'There are no entries.',
  saved: 'Saved.',
  created: 'Created.',
  deleted: 'Deleted.',
  actionDone: 'Done.',
  confirmDelete: 'Delete {count} entries? This cannot be undone.',
  confirmDeleteOne: 'Delete this entry? This cannot be undone.',
  conflict: 'Someone else changed this entry since you read it. Reload it and make your changes again.',
  reload: 'Reload',
  notFound: 'This entry does not exist or you may not read it.',
  failed: 'The request failed.',
  truncated: 'Only the first {count} entries are shown; narrow the filter.',
  newEntry: 'New entry',
  readOnlyShipped: 'Shipped entries cannot be changed. Copy one to change it locally.',
} as const;

export type TextKey = keyof typeof en;

const de: Record<TextKey, string> = {
  title: 'go-runtime',
  signInTitle: 'Anmelden',
  user: 'Benutzer',
  password: 'Passwort',
  signIn: 'Anmelden',
  signInFailed: 'Benutzer oder Passwort sind falsch, oder der Benutzer ist gesperrt.',
  sessionExpired: 'Ihre Sitzung ist abgelaufen. Bitte melden Sie sich erneut an.',
  search: 'Apps suchen',
  home: 'Startseite',
  language: 'Sprache',
  tenant: 'Mandant',
  theme: 'Darstellung',
  themeAuto: 'Wie das System',
  themeLight: 'Hell',
  themeDark: 'Dunkel',
  density: 'Dichte',
  densityCompact: 'Kompakt',
  densityComfortable: 'Großzügig',
  signOut: 'Abmelden',
  spaces: 'Bereiche',
  noSpaces: 'Für Ihre Rollen gibt es noch keine Bereiche. Bitten Sie einen Administrator um eine Rolle mit Startseite.',
  loadFailed: 'Die Startseite konnte nicht geladen werden.',
  retry: 'Erneut versuchen',
  appUnknown: 'Diese App gibt es nicht, oder Sie dürfen sie nicht starten.',
  remoteFailed: 'Die App konnte nicht geladen werden.',
  opened: 'Die App wurde in einem neuen Fenster geöffnet.',
  checked: 'Ausgewählt',
  create: 'Anlegen',
  edit: 'Bearbeiten',
  save: 'Sichern',
  cancel: 'Abbrechen',
  delete: 'Löschen',
  go: 'Start',
  clearFilter: 'Zurücksetzen',
  filters: 'Filter',
  actions: 'Aktionen',
  run: 'Ausführen',
  yes: 'Ja',
  no: 'Nein',
  shipped: 'Ausgeliefert',
  local: 'Lokal',
  noEntries: 'Es gibt keine Einträge.',
  saved: 'Gesichert.',
  created: 'Angelegt.',
  deleted: 'Gelöscht.',
  actionDone: 'Erledigt.',
  confirmDelete: '{count} Einträge löschen? Das lässt sich nicht rückgängig machen.',
  confirmDeleteOne: 'Diesen Eintrag löschen? Das lässt sich nicht rückgängig machen.',
  conflict: 'Jemand anderes hat diesen Eintrag geändert, seit Sie ihn gelesen haben. Laden Sie ihn neu und ändern Sie ihn erneut.',
  reload: 'Neu laden',
  notFound: 'Diesen Eintrag gibt es nicht, oder Sie dürfen ihn nicht lesen.',
  failed: 'Die Anfrage ist gescheitert.',
  truncated: 'Nur die ersten {count} Einträge werden gezeigt; schränken Sie den Filter ein.',
  newEntry: 'Neuer Eintrag',
  readOnlyShipped: 'Ausgelieferte Einträge lassen sich nicht ändern. Kopieren Sie einen, um ihn lokal zu ändern.',
};

const packages: Record<string, Record<TextKey, string>> = { en, de };

/** The texts of a language, English for others. */
export function textsOf(language: string | null | undefined): Record<TextKey, string> {
  return packages[(language ?? 'en').toLowerCase()] ?? en;
}

/** A text with {name} replaced by its parameter. */
export function format(text: string, params: Record<string, string | number>): string {
  return text.replace(/\{(\w+)\}/g, (all, name: string) => (name in params ? String(params[name]) : all));
}
