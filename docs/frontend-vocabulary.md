# UI annotations and the vocabulary TRFRONTEND.UI

The generic list and detail pages of the package `TRFRONTEND` show the entities of a service
only from its metadata. Packages describe how with objects of the type
`/TRFRONTEND/UI_ANNOTATIONS`; `TRFRONTEND` combines those of all packages for an entity and
writes them into the metadata of every service that offers it, with the vocabulary
`TRFRONTEND.UI` (alias `Frontend`). Labels, fixed values, mandatory fields, read-only fields
and value helps stay those of the kernel and of `RT.Fields`; UI annotations do not repeat
them.

## UI annotations of a package

A package that requires `TRFRONTEND` and the package of the business object ships
`objects/TRFRONTEND/UI_ANNOTATIONS/<NAME>.json`, e.g. `examples/TRFINUI`:

```json
{
  "name": "/TRFINUI/COMPANIES_UI",
  "content": {
    "ENTITY": "/TRFIN/COMPANIES",
    "HEADER": { "TITLE_FIELD": "NAME", "SUBTITLE_FIELD": "COMPANY_ID",
                "TEXTS": [{ "LANGUAGE": "en", "TYPE_NAME": "Company", "TYPE_NAME_PLURAL": "Companies" }] },
    "LIST": {
      "COLUMNS": [{ "FIELD": "COMPANY_ID", "WIDTH": 8 }, { "FIELD": "NAME" }, { "FIELD": "STATUS", "IMPORTANCE": "LOW" }],
      "FILTER_FIELDS": [{ "FIELD": "NAME" }, { "FIELD": "STATUS" }],
      "SORT": [{ "FIELD": "NAME" }],
      "ACTIONS": [{ "ACTION": "BLOCK" }]
    },
    "DETAIL": {
      "SECTIONS": [
        { "ID": "GENERAL", "TEXTS": [{ "LANGUAGE": "en", "TITLE": "General" }],
          "GROUPS": [{ "ID": "MAIN", "FIELDS": [{ "FIELD": "COMPANY_ID" }, { "FIELD": "NAME" }] }] },
        { "ID": "CONTACTS", "TEXTS": [{ "LANGUAGE": "en", "TITLE": "Contacts" }], "TABLE": { "ENTITY": "/TRFIN/CONTACTS" } }
      ],
      "ACTIONS": [{ "ACTION": "BLOCK" }]
    }
  }
}
```

- `ENTITY` is an entity of a business object. Fields and actions belong to it; the kernel
  checks that they exist, and an upgrade that removes one is rejected with the name of the
  object.
- `HEADER`: the fields of title, subtitle and description and the names of the type per
  language.
- `LIST`: columns with optional width in rem and importance (`HIGH`, `MEDIUM`, `LOW`),
  filter fields, the default order and actions in the toolbar. `MERGE_SET` names an entity
  set of the service with the same properties whose entities the list shows as well.
- `DETAIL`: sections with groups of fields, or with `TABLE` the dependent entities of an
  entity as table along the navigation of the service; actions of the page.

## Extensions by other packages

The objects of all packages for an entity are combined in the dependency order of their
packages; the first is the base. A later package adds:

- columns, filter fields and fields of groups with `"POSITION": { "AFTER": "NAME" }` or
  `"BEFORE"`, or at the end without position;
- sections with a `POSITION` relative to the ID of a section, groups to a section with the
  same ID, and fields to an existing group with `GROUP_ADDITIONS`;
- actions at the end; a header, sort order or merged set only where the base has none.

Sections and groups that a package adds carry its namespace in their ID, e.g.
`/TROTTOUI/MARKETPLACE`. `examples/TROTTOUI` shows the partner ID of `TROTTO` this way in the
list and the detail page of the companies of `TRFIN`.

## Terms

All terms apply to entity types. Properties are public names of the service, actions their
qualified names; fields, sections and actions that the service does not offer are left out.

| Term | Type | Members |
|---|---|---|
| `Frontend.Header` | `HeaderType` | `TypeName`, `TypeNamePlural` (strings in the language of the metadata), `Title`, `Subtitle`, `Description` (property paths) |
| `Frontend.ListPage` | `ListPageType` | `Columns` (collection of `Property`, `Width`, `Importance`), `FilterFields` (property paths), `SortOrder` (collection of `Property`, `Descending`), `Actions` (strings), `MergeSet` (string) |
| `Frontend.DetailPage` | `DetailPageType` | `Sections` (collection of `Id`, `Title`, `Groups` with `Id`, `Title`, `Fields`, and `Table`, a navigation property path), `Actions` (strings) |

The vocabulary in CSDL JSON is served at `/trfrontend/vocabularies/TRFRONTEND.UI.json`.
