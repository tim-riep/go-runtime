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


// The administration of roles at /trfrontend/api over the role functions of
// the kernel, in the format of /TRBASE/JSON.

import { request } from '../../shell/api.ts';

export interface RoleInfo {
  NAME: string;
  PACKAGE_ID: string | null;
  COMPOSITE: boolean;
  LABEL: string | null;
  INCOMPLETE: number;
}

export interface RoleValue {
  KIND: 'VALUE' | 'RANGE' | 'PATTERN';
  LOW: string | null;
  HIGH: string | null;
}

export interface RoleField {
  FIELD: string;
  VALUES: RoleValue[];
}

export interface RoleAuthorization {
  OBJECT: string;
  FIELDS: RoleField[];
  ROLE_NAME: string | null;
  COMPOSITE: string | null;
}

export interface RoleText {
  LANGUAGE: string;
  LABEL: string;
  DESCRIPTION: string | null;
}

export interface RoleDefinition {
  NAME: string;
  COMPOSITE: boolean;
  TEXTS: RoleText[] | null;
  ROLES: string[] | null;
  AUTHORIZATIONS: RoleAuthorization[] | null;
}

export interface RoleDetail extends RoleDefinition {
  PACKAGE_ID: string | null;
  ALL: boolean;
  EFFECT: RoleAuthorization[];
  USED_BY: string[];
  ASSIGNMENTS: number;
}

export interface Assignment {
  ID: string;
  USER_NAME: string;
  ROLE_NAME: string;
  TENANT: string;
  VALID_FROM: string | null;
  VALID_TO: string | null;
  CREATED_AT: string | null;
  CREATED_BY: string | null;
}

export interface UserInfo {
  USER_NAME: string;
  NAME: string | null;
  STATUS: string;
  LANGUAGE: string | null;
  DEFAULT_TENANT: string | null;
  TENANTS: string[] | null;
}

export interface AuthorizationField {
  NAME: string;
  IS_ACTIVITY: boolean;
  LABEL: string | null;
}

export interface AuthorizationObject {
  NAME: string;
  PACKAGE_ID: string | null;
  LABEL: string | null;
  FIELDS: AuthorizationField[];
}

const base = '/trfrontend/api/';
const q = (params: Record<string, string | null | undefined>) => {
  const s = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== null && v !== undefined) s.set(k, v);
  const t = s.toString();
  return t ? `?${t}` : '';
};

export const roles = () => request<RoleInfo[]>('GET', `${base}roles`);
export const role = (name: string) => request<RoleDetail>('GET', `${base}role${q({ name })}`);
export const createRole = (r: RoleDefinition) => request<unknown>('POST', `${base}role`, r);
export const updateRole = (r: RoleDefinition) => request<unknown>('PUT', `${base}role`, r);
export const deleteRole = (name: string, confirm: boolean) => request<null>('DELETE', `${base}role${q({ name, confirm: confirm ? 'true' : null })}`);
export const assignments = (filter: { user?: string; role?: string; tenant?: string }) => request<Assignment[]>('GET', `${base}assignments${q(filter)}`);
export const assign = (a: { USER_NAME: string; ROLE_NAME: string; TENANT: string; VALID_FROM: string | null; VALID_TO: string | null }) =>
  request<{ ASSIGNMENT: Assignment; OVERLAPS: Assignment[] }>('POST', `${base}assignments`, a);
export const unassign = (id: string, confirm: boolean) => request<null>('DELETE', `${base}assignment${q({ id, confirm: confirm ? 'true' : null })}`);
export const users = () => request<UserInfo[]>('GET', `${base}users`);
export const authorizationObjects = () => request<AuthorizationObject[]>('GET', `${base}authorization-objects`);

/** The label of a role in a language, else English, else its name. */
export function labelOf(r: RoleDefinition, language: string): string {
  const texts = r.TEXTS ?? [];
  return (texts.find((t) => t.LANGUAGE === language) ?? texts.find((t) => t.LANGUAGE === 'en'))?.LABEL ?? r.NAME;
}

/** A value of an authorization as text: a value, LOW–HIGH or a pattern. */
export function valueText(v: RoleValue): string {
  return v.KIND === 'RANGE' ? `${v.LOW ?? ''}–${v.HIGH ?? ''}` : (v.LOW ?? '');
}

/**
 * Values typed as text, separated by commas: a*b patterns, from-to ranges
 * with two dots (A..F), other values as they are.
 */
export function parseValues(text: string): RoleValue[] {
  return text
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '')
    .map((s) => {
      if (s.includes('*')) return { KIND: 'PATTERN', LOW: s, HIGH: null };
      const range = s.split('..');
      if (range.length === 2) return { KIND: 'RANGE', LOW: range[0]?.trim() ?? '', HIGH: range[1]?.trim() ?? '' };
      return { KIND: 'VALUE', LOW: s, HIGH: null };
    });
}

/** Values as text for parseValues. */
export function formatValues(values: RoleValue[]): string {
  return values.map((v) => (v.KIND === 'RANGE' ? `${v.LOW ?? ''}..${v.HIGH ?? ''}` : (v.LOW ?? ''))).join(', ');
}
