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

// The metadata of a service as the generic pages use it, read from the
// metadata document in CSDL JSON.

/** A value of an annotation: a string, number, boolean, path, record or collection. */
export type AnnotationValue =
  | string
  | number
  | boolean
  | null
  | { $Path: string }
  | { $PropertyPath: string }
  | { $NavigationPropertyPath: string }
  | AnnotationValue[]
  | { [member: string]: AnnotationValue };

/** Annotations of one target by term without the leading @, e.g. Core.Description. */
export type Annotations = Record<string, AnnotationValue>;

export interface Property {
  name: string;
  /** The qualified type, e.g. Edm.String, Edm.Decimal or TRFIN.TRFIN_ADDRESS. */
  type: string;
  collection: boolean;
  nullable: boolean;
  maxLength: number | null;
  precision: number | null;
  scale: number | null;
  annotations: Annotations;
}

export interface Navigation {
  name: string;
  /** The qualified name of the target type. */
  type: string;
  collection: boolean;
  nullable: boolean;
  containsTarget: boolean;
  /** Property of the source → property of the target. */
  constraints: Record<string, string>;
}

export interface EntityType {
  /** Qualified name, e.g. TRFIN.TRFIN_COMPANIES. */
  name: string;
  key: string[];
  properties: Property[];
  navigations: Navigation[];
  annotations: Annotations;
}

export interface Parameter {
  name: string;
  type: string;
  nullable: boolean;
}

export interface Action {
  /** Qualified name, e.g. TRFIN.BLOCK. */
  name: string;
  /** The qualified entity type of bound actions, null for action imports. */
  boundTo: string | null;
  parameters: Parameter[];
  returnType: string | null;
  annotations: Annotations;
}

export interface EntitySet {
  name: string;
  type: EntityType;
  /** Navigation property → entity set of the target. */
  bindings: Record<string, string>;
  annotations: Annotations;
}

export interface ComplexType {
  name: string;
  properties: Property[];
}

export interface ServiceModel {
  namespace: string;
  entitySets: Record<string, EntitySet>;
  entityTypes: Record<string, EntityType>;
  complexTypes: Record<string, ComplexType>;
  actions: Action[];
  /** Action imports by name with their action. */
  imports: Record<string, Action>;
  /** Aliases of referenced vocabularies → their namespace. */
  aliases: Record<string, string>;
}

type Json = Record<string, unknown>;

function asObject(v: unknown): Json {
  return v !== null && typeof v === 'object' && !Array.isArray(v) ? (v as Json) : {};
}

/** Annotations of a CSDL JSON element: members that start with @. */
function inlineAnnotations(element: Json): Annotations {
  const out: Annotations = {};
  for (const [k, v] of Object.entries(element)) {
    if (k.startsWith('@')) {
      out[k.slice(1)] = v as AnnotationValue;
    }
  }
  return out;
}

function property(name: string, p: Json): Property {
  return {
    name,
    type: typeof p.$Type === 'string' ? p.$Type : 'Edm.String',
    collection: p.$Collection === true,
    nullable: p.$Nullable === true,
    maxLength: typeof p.$MaxLength === 'number' ? p.$MaxLength : null,
    precision: typeof p.$Precision === 'number' ? p.$Precision : null,
    scale: typeof p.$Scale === 'number' ? p.$Scale : null,
    annotations: inlineAnnotations(p),
  };
}

/** Reads a metadata document in CSDL JSON. */
export function parseMetadata(doc: unknown): ServiceModel {
  const root = asObject(doc);
  const aliases: Record<string, string> = {};
  for (const ref of Object.values(asObject(root.$Reference))) {
    for (const inc of (asObject(ref).$Include as Json[] | undefined) ?? []) {
      if (typeof inc.$Alias === 'string' && typeof inc.$Namespace === 'string') {
        aliases[inc.$Alias] = inc.$Namespace;
      }
    }
  }
  const namespace = Object.keys(root).find((k) => !k.startsWith('$')) ?? '';
  const schema = asObject(root[namespace]);
  const model: ServiceModel = { namespace, entitySets: {}, entityTypes: {}, complexTypes: {}, actions: [], imports: {}, aliases };
  let container: Json = {};
  for (const [name, raw] of Object.entries(schema)) {
    const el = asObject(raw);
    const qualified = `${namespace}.${name}`;
    switch (el.$Kind) {
      case 'EntityType': {
        const t: EntityType = { name: qualified, key: (el.$Key as string[] | undefined) ?? [], properties: [], navigations: [], annotations: {} };
        for (const [pn, pv] of Object.entries(el)) {
          if (pn.startsWith('$') || pn.startsWith('@')) {
            continue;
          }
          const p = asObject(pv);
          if (p.$Kind === 'NavigationProperty') {
            t.navigations.push({
              name: pn,
              type: String(p.$Type),
              collection: p.$Collection === true,
              nullable: p.$Nullable === true,
              containsTarget: p.$ContainsTarget === true,
              constraints: (asObject(p.$ReferentialConstraint) as Record<string, string>) ?? {},
            });
          } else {
            t.properties.push(property(pn, p));
          }
        }
        model.entityTypes[qualified] = t;
        break;
      }
      case 'ComplexType': {
        const ct: ComplexType = { name: qualified, properties: [] };
        for (const [pn, pv] of Object.entries(el)) {
          if (!pn.startsWith('$') && !pn.startsWith('@')) {
            ct.properties.push(property(pn, asObject(pv)));
          }
        }
        model.complexTypes[qualified] = ct;
        break;
      }
      case 'EntityContainer':
        container = el;
        break;
    }
    if (Array.isArray(raw)) {
      // Actions in JSON are arrays of overloads without $Kind on the array.
      const first = asObject(raw[0]);
      if (first.$Kind === 'Action' && !model.actions.some((a) => a.name === qualified)) {
        for (const overload of raw as Json[]) {
          const params = (overload.$Parameter as Json[] | undefined) ?? [];
          const bound = overload.$IsBound === true;
          const ret = asObject(overload.$ReturnType);
          model.actions.push({
            name: qualified,
            boundTo: bound && params[0] ? String(params[0].$Type) : null,
            parameters: params.slice(bound ? 1 : 0).map((p) => ({
              name: String(p.$Name),
              type: typeof p.$Type === 'string' ? p.$Type : 'Edm.String',
              nullable: p.$Nullable === true,
            })),
            returnType: typeof ret.$Type === 'string' ? ret.$Type : null,
            annotations: {},
          });
        }
      }
    }
  }
  for (const [name, raw] of Object.entries(container)) {
    if (name.startsWith('$') || name.startsWith('@')) {
      continue;
    }
    const el = asObject(raw);
    if (typeof el.$Action === 'string') {
      const action = model.actions.find((a) => a.name === el.$Action && a.boundTo === null);
      if (action) {
        model.imports[name] = action;
      }
      continue;
    }
    const type = model.entityTypes[String(el.$Type)];
    if (el.$Collection === true && type) {
      model.entitySets[name] = {
        name,
        type,
        bindings: (asObject(el.$NavigationPropertyBinding) as Record<string, string>) ?? {},
        annotations: inlineAnnotations(el),
      };
    }
  }
  // External annotations by target.
  for (const [target, terms] of Object.entries(asObject(schema.$Annotations))) {
    const annotations = inlineAnnotations(asObject(terms));
    const [path, member] = target.split('/');
    if (path === `${namespace}.Container` || path === `${namespace}.${String(container.$Kind ?? '')}`) {
      const set = member ? model.entitySets[member] : undefined;
      if (set) {
        Object.assign(set.annotations, annotations);
      }
      continue;
    }
    const type = path ? model.entityTypes[path] : undefined;
    if (type) {
      if (!member) {
        Object.assign(type.annotations, annotations);
        continue;
      }
      const p = type.properties.find((x) => x.name === member);
      if (p) {
        Object.assign(p.annotations, annotations);
      }
      continue;
    }
    const actions = model.actions.filter((a) => a.name === path);
    for (const a of actions) {
      Object.assign(a.annotations, annotations);
    }
  }
  return model;
}

/** The value of a term on annotations, with the alias or the full namespace. */
export function annotation(model: ServiceModel, annotations: Annotations, term: string): AnnotationValue | undefined {
  if (term in annotations) {
    return annotations[term];
  }
  const dot = term.lastIndexOf('.');
  const prefix = term.slice(0, dot);
  for (const [alias, ns] of Object.entries(model.aliases)) {
    if (ns === prefix && `${alias}${term.slice(dot)}` in annotations) {
      return annotations[`${alias}${term.slice(dot)}`];
    }
    if (alias === prefix && `${ns}${term.slice(dot)}` in annotations) {
      return annotations[`${ns}${term.slice(dot)}`];
    }
  }
  return undefined;
}

/** The entity set whose entities have a type. */
export function setOfType(model: ServiceModel, type: string): EntitySet | undefined {
  return Object.values(model.entitySets).find((s) => s.type.name === type);
}
