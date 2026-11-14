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
  listPending: 'The list of this app is not available yet.',
  checked: 'Selected',
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
  listPending: 'Die Liste dieser App ist noch nicht verfügbar.',
  checked: 'Ausgewählt',
};

const packages: Record<string, Record<TextKey, string>> = { en, de };

/** The texts of a language, English for others. */
export function textsOf(language: string | null | undefined): Record<TextKey, string> {
  return packages[(language ?? 'en').toLowerCase()] ?? en;
}
