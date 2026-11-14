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


// What the generic pages show, derived from the metadata of a service: the
// annotations of TRFRONTEND (Frontend.Header, ListPage, DetailPage) for
// the layout and those of the kernel (Core, Validation, Capabilities,
// RT.Fields) for labels, fixed values, value helps, mandatory and read-only
// fields. Without annotations of TRFRONTEND a page shows the key and all
// fields.

import { type AnnotationValue, type Annotations, annotation, type EntitySet, literal, type Property, type ServiceModel } from '../odata/index.ts';

export interface FixedValue {
  value: string;
  label: string;
}

export interface ValueHelp {
  /** The entity set the values come from. */
  set: string;
  /** The property of that set whose value the field takes. */
  property: string;
  /** The text properties of that set that a search looks in: the property first. */
  search: string[];
}

/** A field of an entity with what the pages need to show and edit it. */
export interface Field {
  name: string;
  property: Property;
  label: string;
  /** The label of columns. */
  heading: string;
  key: boolean;
  mandatory: boolean;
  /** Never written by the client, e.g. computed by the server. */
  computed: boolean;
  /** Written only when the entity is created. */
  immutable: boolean;
  fixedValues: FixedValue[] | null;
  valueHelp: ValueHelp | null;
}

export interface Column {
  field: Field;
  /** Width in rem, or null. */
  width: number | null;
  importance: 'HIGH' | 'MEDIUM' | 'LOW';
}

export interface ListView {
  set: EntitySet;
  typeName: string;
  typeNamePlural: string;
  columns: Column[];
  filterFields: Field[];
  sort: Array<{ field: string; descending: boolean }>;
  /** Qualified names of bound actions. */
  actions: string[];
  /** A second, read-only entity set shown in the same list, e.g. shipped entries. */
  mergeSet: EntitySet | null;
  insertable: boolean;
  deletable: boolean;
}

export interface Group {
  id: string;
  title: string | null;
  fields: Field[];
}

export interface Section {
  id: string;
  title: string;
  groups: Group[];
  /** A table of dependent entities through this navigation property. */
  table: string | null;
}

export interface DetailView {
  set: EntitySet;
  typeName: string;
  titleField: string | null;
  subtitleField: string | null;
  descriptionField: string | null;
  sections: Section[];
  actions: string[];
  updatable: boolean;
  deletable: boolean;
  /** Properties whose values the ETag covers, read but never written. */
  concurrency: string[];
}

type Record_ = { [member: string]: AnnotationValue };

function record(v: AnnotationValue | undefined): Record_ {
  return v !== null && typeof v === 'object' && !Array.isArray(v) ? (v as Record_) : {};
}

function list(v: AnnotationValue | undefined): AnnotationValue[] {
  return Array.isArray(v) ? v : [];
}

function str(v: AnnotationValue | undefined): string | null {
  return typeof v === 'string' ? v : null;
}

/** The property a path value names, e.g. {"$PropertyPath": "NAME"}. */
export function path(v: AnnotationValue | undefined): string | null {
  const r = record(v);
  for (const k of ['$PropertyPath', '$Path', '$NavigationPropertyPath']) {
    if (typeof r[k] === 'string') {
      return r[k] as string;
    }
  }
  return null;
}

function flag(m: ServiceModel, a: Annotations, term: string): boolean {
  return annotation(m, a, term) === true;
}

function restriction(m: ServiceModel, set: EntitySet, term: string, member: string): boolean {
  const r = annotation(m, set.annotations, term);
  if (r === undefined) {
    return true;
  }
  return record(r)[member] !== false;
}

/** The field of a property with its labels and rules. */
export function fieldOf(m: ServiceModel, set: EntitySet, p: Property): Field {
  const a = p.annotations;
  const label = str(annotation(m, a, 'RT.Fields.MediumLabel')) ?? str(annotation(m, a, 'Org.OData.Core.V1.Description')) ?? p.name;
  const heading = str(annotation(m, a, 'RT.Fields.Heading')) ?? label;
  let fixedValues: FixedValue[] | null = null;
  const allowed = annotation(m, a, 'Org.OData.Validation.V1.AllowedValues');
  if (Array.isArray(allowed)) {
    fixedValues = allowed.map((x) => {
      const r = record(x);
      const value = String(r.Value ?? '');
      const inline: Annotations = {};
      for (const [k, v] of Object.entries(r)) {
        if (k.startsWith('@')) inline[k.slice(1)] = v;
      }
      return { value, label: str(annotation(m, inline, 'Org.OData.Core.V1.Description')) ?? value };
    });
  }
  let valueHelp: ValueHelp | null = null;
  const vl = record(annotation(m, a, 'RT.Fields.ValueList'));
  if (typeof vl.EntitySet === 'string') {
    const param = list(vl.Parameters)
      .map(record)
      .find((x) => path(x.LocalProperty) === p.name);
    const property = str(param?.ValueListProperty) ?? p.name;
    const texts = (m.entitySets[vl.EntitySet]?.type.properties ?? []).filter((x) => x.type === 'Edm.String').map((x) => x.name);
    valueHelp = { set: vl.EntitySet, property, search: [property, ...texts.filter((x) => x !== property)] };
  }
  return {
    name: p.name,
    property: p,
    label,
    heading,
    key: set.type.key.includes(p.name),
    mandatory: flag(m, a, 'RT.Fields.Mandatory') || (!p.nullable && !flag(m, a, 'Org.OData.Core.V1.Computed')),
    computed: flag(m, a, 'Org.OData.Core.V1.Computed'),
    immutable: flag(m, a, 'Org.OData.Core.V1.Immutable'),
    fixedValues,
    valueHelp,
  };
}

/** The fields of the primitive properties of an entity set by name. */
export function fieldsOf(m: ServiceModel, set: EntitySet): Map<string, Field> {
  const out = new Map<string, Field>();
  for (const p of set.type.properties) {
    if (p.type.startsWith('Edm.') && !p.collection) {
      out.set(p.name, fieldOf(m, set, p));
    }
  }
  return out;
}

function pick(fields: Map<string, Field>, names: Array<string | null>): Field[] {
  return names.filter((n): n is string => n !== null && fields.has(n)).map((n) => fields.get(n) as Field);
}

function header(m: ServiceModel, set: EntitySet) {
  const h = record(annotation(m, set.type.annotations, 'TRFRONTEND.UI.Header'));
  const description = str(annotation(m, set.type.annotations, 'Org.OData.Core.V1.Description'));
  const typeName = str(h.TypeName) ?? description ?? set.name;
  return {
    typeName,
    typeNamePlural: str(h.TypeNamePlural) ?? set.name,
    title: path(h.Title),
    subtitle: path(h.Subtitle),
    description: path(h.Description),
  };
}

/** The list page of an entity set. */
export function listView(m: ServiceModel, setName: string): ListView {
  const set = m.entitySets[setName];
  if (!set) {
    throw new Error(`the service has no entity set ${setName}`);
  }
  const fields = fieldsOf(m, set);
  const lp = annotation(m, set.type.annotations, 'TRFRONTEND.UI.ListPage');
  const h = header(m, set);
  const base = {
    set,
    typeName: h.typeName,
    typeNamePlural: h.typeNamePlural,
    insertable: restriction(m, set, 'Org.OData.Capabilities.V1.InsertRestrictions', 'Insertable'),
    deletable: restriction(m, set, 'Org.OData.Capabilities.V1.DeleteRestrictions', 'Deletable'),
  };
  if (lp === undefined) {
    const columns = [...fields.values()].map((field) => ({ field, width: null, importance: field.key ? 'HIGH' : 'MEDIUM' }) as Column);
    return { ...base, columns, filterFields: pick(fields, set.type.key), sort: set.type.key.map((k) => ({ field: k, descending: false })), actions: [], mergeSet: null };
  }
  const r = record(lp);
  const columns: Column[] = [];
  for (const c of list(r.Columns).map(record)) {
    const field = fields.get(path(c.Property) ?? '');
    if (field) {
      const imp = str(c.Importance);
      columns.push({ field, width: typeof c.Width === 'number' ? c.Width : null, importance: imp === 'HIGH' || imp === 'LOW' ? imp : 'MEDIUM' });
    }
  }
  const merge = str(r.MergeSet);
  return {
    ...base,
    columns,
    filterFields: pick(fields, list(r.FilterFields).map(path)),
    sort: list(r.SortOrder)
      .map(record)
      .map((s) => ({ field: path(s.Property) ?? '', descending: s.Descending === true }))
      .filter((s) => fields.has(s.field)),
    actions: list(r.Actions).filter((a): a is string => typeof a === 'string'),
    mergeSet: merge ? (m.entitySets[merge] ?? null) : null,
  };
}

/** The detail page of an entity set. */
export function detailView(m: ServiceModel, setName: string): DetailView {
  const set = m.entitySets[setName];
  if (!set) {
    throw new Error(`the service has no entity set ${setName}`);
  }
  const fields = fieldsOf(m, set);
  const h = header(m, set);
  const dp = annotation(m, set.type.annotations, 'TRFRONTEND.UI.DetailPage');
  let sections: Section[];
  let actions: string[] = [];
  if (dp === undefined) {
    const keys = pick(fields, set.type.key);
    const rest = [...fields.values()].filter((f) => !f.key);
    sections = [
      {
        id: 'GENERAL',
        title: h.typeName,
        groups: [{ id: 'MAIN', title: null, fields: [...keys, ...rest] }],
        table: null,
      },
    ];
  } else {
    const r = record(dp);
    sections = list(r.Sections)
      .map(record)
      .map((s) => ({
        id: str(s.Id) ?? '',
        title: str(s.Title) ?? str(s.Id) ?? '',
        groups: list(s.Groups)
          .map(record)
          .map((g) => ({ id: str(g.Id) ?? '', title: str(g.Title), fields: pick(fields, list(g.Fields).map(path)) })),
        table: path(s.Table),
      }));
    actions = list(r.Actions).filter((a): a is string => typeof a === 'string');
  }
  const oc = annotation(m, set.annotations, 'Org.OData.Core.V1.OptimisticConcurrency');
  return {
    set,
    typeName: h.typeName,
    titleField: h.title,
    subtitleField: h.subtitle,
    descriptionField: h.description,
    sections,
    actions,
    updatable: restriction(m, set, 'Org.OData.Capabilities.V1.UpdateRestrictions', 'Updatable'),
    deletable: restriction(m, set, 'Org.OData.Capabilities.V1.DeleteRestrictions', 'Deletable'),
    concurrency: list(oc).map(path).filter((x): x is string => x !== null),
  };
}

/** The label of an action, from Core.Description or its name. */
/**
 * The filter of a search in a value help: any of its text properties
 * contains the query, regardless of case.
 */
export function valueHelpFilter(help: ValueHelp, query: string): string {
  const q = literal(query.toLowerCase(), 'Edm.String');
  return help.search.map((p) => `contains(tolower(${p}),${q})`).join(' or ');
}

/**
 * The detail view of a list for an entry of its second set, e.g. a shipped
 * entry shown with the view of the local ones: tables through navigations
 * that the type of that set lacks are left out.
 */
export function mergedDetailView(view: DetailView, set: EntitySet): DetailView {
  const navigations = new Set(set.type.navigations.map((n) => n.name));
  return { ...view, sections: view.sections.filter((s) => s.table === null || navigations.has(s.table)) };
}

export function actionLabel(m: ServiceModel, name: string): string {
  const a = m.actions.find((x) => x.name === name);
  const d = a ? str(annotation(m, a.annotations, 'Org.OData.Core.V1.Description')) : null;
  return d ?? name.slice(name.lastIndexOf('.') + 1);
}
