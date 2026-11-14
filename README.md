# go-runtime

`go-runtime` is a kernel written in Go that manages installable software packages and their
database structures on a single SQLite or PostgreSQL database. Applications embed it as a
library; the `rt` command line tool is a thin layer on top.

A package brings domains (technical types), tables with primary and foreign keys, and
extensions that add fields to tables of other packages. The kernel validates packages,
resolves their dependencies, plans the DDL for the target dialect and executes it in one
transaction together with its own bookkeeping.

## Concepts

- **Package**: a directory with a manifest and schema files. Its ID consists of upper-case
  letters and digits and starts with a letter, e.g. `TRBASE`, `TRFIN`, `TROTTO`.
- **Kernel package**: a package embedded in the kernel binary (`kernelpkg/`), marked with
  `"kernelPackage": true` in its manifest and with the version of the kernel packages of the
  kernel. `rt init` installs it, a newer kernel upgrades it when it opens a database and
  compiles the packages that require it again; if that fails, the kernel does not start. It
  is never installed, upgraded or removed from outside, and only kernel packages have
  methods executed by kernel. Kernel packages are installed with the texts of the languages
  that are set up and get the texts of every language added later. `TRBASE` is the kernel
  package: base domains, data elements, the address structure, `/TRBASE/AUDIT_LOG` and the
  classes executed by kernel.
- **Ownership and namespace**: every object belongs to exactly one package, and its name
  starts with the namespace of that package: `/TRFIN/COMPANIES` belongs to `TRFIN`, even if
  `TROTTO` extends it. The full name is used everywhere, in package files for definitions and
  references alike.
- **Domain**: base type, length, decimals and optional fixed values. Usable by the owning
  package and by every package that requires it directly or transitively.
- **Data element**: a domain with field labels in every language. A field references either a
  domain or a data element; fields without data element have no labels.
- **Table**: fields, a primary key, foreign keys and the mandatory attributes
  `tenantSpecific` and `dataClass`.
- **Extension**: additional fields and foreign keys for a table of a required package. Their
  names carry the namespace of the extending package, e.g. the column `/TROTTO/OTTO_ID` in
  `/TRFIN/COMPANIES`. Extension fields are never key fields and must be nullable or have a
  default. Several packages can extend the same table without collisions. Fields and foreign
  keys declared by the table itself have no namespace.
- **Repository**: kernel tables that describe all installed objects, their owners and the
  physical layout. The repository, not database introspection, is the basis for every
  comparison. `Check` compares it with the actual schema and reports discrepancies.
- **Tenants**: the kernel table `/RT/TENANTS`. Tenant-specific tables get the field
  `/RT/TENANT` as first key field and a foreign key to the tenant table.
- **Languages**: the kernel table `/RT/LANGUAGES` lists the languages translatable texts may
  use. English always exists.
- **Asset repository**: a folder below `assets/` of a package with files (assets) and
  subfolders, such as images, stylesheets, fonts or print templates. It is identified by the
  package ID and its folder name, installed and removed with the package and read-only at
  runtime.
- **Business object**: a root table and dependent tables of a package as one object with
  operations, behavior and authorization; code changes its tables only with entity
  statements. A business object of the data class `configuration` or `runtimeConfiguration`
  maintains configuration.
- **Business object extension**: properties of fields, determinations, validations, actions
  and dependent tables that a package adds to a business object of a package it requires.
- **Structure**: named components without database table. A component has a data element, a
  domain, a structure or an array type. A structure is flat when all its components are
  elementary (data element or domain), otherwise deep. Every table is a flat structure as
  well: its fields without tenant field, keys and foreign keys.
- **Array type**: rows of a structure, table, data element or domain, optionally with a key.
- **Include**: an entry in the components of a structure or the fields of a table whose
  place is taken by all components of an included structure or table.
- **Structure extension**: additional components for a structure of a required package. They
  appear wherever the structure is used, including every table that includes it.
- **Extension point**: a place in the code of a package that other packages extend: a public
  interface, filter fields that select the implementations and whether one or all of them run.
- **Implementation**: a class of a package that implements the interface of an extension point
  of the package or of a required one, for the filter values it names. The kernel selects the
  active implementations when the code runs; the defining package does not know them.
- **Object type**: a type of objects that a package defines, whose content is a structure of
  the package. Packages that require it ship objects of the type, e.g. descriptions of
  pages; the kernel checks them like every other object and keeps them read-only.

## Package format

```
TRFIN/
  manifest.json
  schema/
    domains/*.json             one domain per file, named <NAME>.json
    data-elements/*.json       one data element per file, named <NAME>.json
    tables/*.json              one table per file, named <NAME>.json
    extensions/*.json          one extension per extended table
    structures/*.json          one structure per file, named <NAME>.json
    array-types/*.json         one array type per file, named <NAME>.json
    structure-extensions/*.json  one extension per extended structure
    http-services/*.json       one service node per file, any file name
    authorization-objects/*.json  one authorization object per file, named <NAME>.json
    roles/*.json               one role or composite role per file, named <NAME>.json
    extension-points/*.json    one extension point per file, named <NAME>.json
    extension-implementations/*.json  one implementation per file, named <NAME>.json
    business-objects/*.json    one business object per file, named <NAME>.json
    business-object-extensions/*.json  one extension per extended business object, named <NAME>.json
    services/*.json            one service over OData per file, named <NAME>.json
    object-types/*.json        one object type per file, named <NAME>.json
  src/
    <NAME>.class               a class, exception class or test class
    <NAME>.interface           an interface
    <NAME>.include             a members or statements include
  assets/
    <repository>/asset.json    every folder below assets/ is an asset repository
    <repository>/...           its files and folders
  transformations/
    <NAME>.xsl                 an XSLT stylesheet, the transformation /<PACKAGE>/<NAME>
    <NAME>.json                its labels and output, optional
  objects/
    <PACKAGE>/<TYPE>/<NAME>.json  an object of the type /<PACKAGE>/<TYPE>
```

`manifest.json`:

```json
{
  "id": "TROTTO",
  "version": "1.0.0",
  "description": "Connection of companies to the OTTO marketplace",
  "requires": [{ "id": "TRFIN", "version": ">=1.0.0" }]
}
```

A package with source files in `src/` states the version of the language in the manifest:
`"languageVersion": 1`. Grammar and canonical form are defined per language version.

Version constraints are comparisons separated by commas or spaces: `=1.2.3`, `>=1.0.0`,
`<2.0.0`, `^1.2.0`, `~1.2.0`. An omitted constraint accepts every version.

Domain, table and extension (abridged; see [examples](examples)):

```json
{ "name": "/TRFIN/COMPANY_STATUS", "type": "CHAR", "length": 10,
  "values": [{ "value": "ACTIVE" }, { "value": "c", "name": "CLOSED" }] }
```

A fixed value is addressed in code by its `name`, e.g. `/TRFIN/COMPANY_STATUS.CLOSED`.
Without a name, the value itself is the name if it consists of upper-case letters, digits
and underscores and starts with a letter. Names are unique within a domain.

```json
{
  "name": "/TRFIN/COMPANIES",
  "tenantSpecific": true,
  "dataClass": "configuration",
  "fields": [
    { "name": "COMPANY_ID", "dataElement": "/TRFIN/COMPANY" },
    { "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY", "notNull": true },
    { "name": "STATUS", "domain": "/TRFIN/COMPANY_STATUS", "notNull": true, "default": "ACTIVE" },
    { "name": "NOTE", "domain": "/TRBASE/TEXT", "default": null }
  ],
  "primaryKey": ["COMPANY_ID"],
  "foreignKeys": [
    { "name": "COUNTRY", "table": "/TRFIN/COUNTRIES", "fields": [{ "field": "COUNTRY", "references": "COUNTRY_CODE" }] }
  ]
}
```

```json
{ "table": "/TRFIN/COMPANIES", "fields": [{ "name": "/TROTTO/OTTO_ID", "dataElement": "/TROTTO/OTTO_PARTNER" }] }
```

Every file is validated against the JSON Schemas in [schemas](schemas). Errors name the
package, the file and the JSON Pointer inside the document.

Data element (`schema/data-elements/COUNTRY.json`). All four labels are required for every
language given; English is mandatory. Lengths in characters: `short` 10, `medium` 20, `long`
40, `heading` 40.

```json
{
  "name": "/TRBASE/COUNTRY",
  "domain": "/TRBASE/COUNTRY_CODE",
  "texts": {
    "en": { "short": "Country", "medium": "Country", "long": "Country key", "heading": "Country" },
    "de": { "short": "Land", "medium": "Land", "long": "Länderschlüssel", "heading": "Land" }
  }
}
```

### Base types

| Type | Meaning | PostgreSQL | SQLite (STRICT tables) |
|---|---|---|---|
| CHAR(n) | string with maximum length | VARCHAR(n) COLLATE "C" | TEXT, length check |
| STRING | string without limit | TEXT COLLATE "C" | TEXT |
| INT4, INT8 | integers | INTEGER, BIGINT | INTEGER, range check for INT4 |
| DEC(p,s) | exact decimal, p ≤ 18 | NUMERIC(p,s) | INTEGER holding value × 10^s, range check |
| DATE | calendar date | DATE | TEXT `YYYY-MM-DD`, format check |
| TIMESTAMP | point in time in UTC, microseconds | TIMESTAMPTZ | TEXT `YYYY-MM-DDTHH:MM:SS.ffffffZ`, format check |
| BOOL | truth value | BOOLEAN | INTEGER 0/1 |
| UUID | UUID | UUID | TEXT in lower case, format check |
| BLOB | binary data | BYTEA | BLOB |

Fixed values (CHAR, INT4, INT8) become CHECK constraints. Domains are expanded into column
types and checks instead of `CREATE DOMAIN`, so both dialects behave the same.

**DEC on SQLite** is stored as a scaled 64-bit integer (12.34 in `DEC(15,2)` is stored as
1234). It is exact, compares and sorts correctly in SQL, works in foreign keys and sums
correctly. The price is a maximum precision of 18 digits, which applies to both dialects so
packages stay portable. Values in text form would be exact too, but sort incorrectly and need
normalization (`1.0` vs `1.00`).

Structure (`schema/structures/ADDRESS.json`), a table including it, an array type and a
structure extension in another package. Every component has exactly one of `dataElement`,
`domain`, `structure` (a structure or a table) and `arrayType`; all may have `notNull` and
`description`, elementary components also `default`. An include is an entry `{ "include": ... }` at the
place where the components go, in `components` as in `fields`.

```json
{
  "name": "/TRBASE/ADDRESS",
  "texts": { "en": { "label": "Address" }, "de": { "label": "Adresse" } },
  "components": [
    { "name": "STREET", "dataElement": "/TRBASE/STREET" },
    { "name": "CITY", "dataElement": "/TRBASE/CITY" },
    { "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }
  ]
}
```

```json
{
  "name": "/TRFIN/COMPANIES",
  "fields": [
    { "name": "COMPANY_ID", "dataElement": "/TRFIN/COMPANY" },
    { "include": "/TRBASE/ADDRESS" },
    { "name": "STATUS", "domain": "/TRFIN/COMPANY_STATUS", "notNull": true, "default": "ACTIVE" }
  ],
  "primaryKey": ["COMPANY_ID"],
  "foreignKeys": [
    { "name": "COUNTRY", "table": "/TRFIN/COUNTRIES", "fields": [{ "field": "COUNTRY", "references": "COUNTRY_CODE" }] }
  ]
}
```

```json
{
  "name": "/TRFIN/COMPANY_ARRAY",
  "texts": { "en": { "label": "Companies" } },
  "rowType": { "structure": "/TRFIN/COMPANIES" },
  "key": { "fields": ["COMPANY_ID"], "unique": true }
}
```

```json
{ "structure": "/TRBASE/ADDRESS", "components": [{ "name": "/TROTTO/PICKUP_POINT", "dataElement": "/TROTTO/PICKUP_POINT" }] }
```

## Rules

- Names of objects of a package are `/<PACKAGE-ID>/<NAME>`, where `<NAME>` consists of
  upper-case letters, digits and underscores and starts with a letter. Table, domain, data
  element and business object names are unique in the database; tables, structures and array types
  share one set of names, because a component of kind `structure` may name either. Names are always quoted in SQL.
- The loader checks namespaces on import, right after the JSON Schema check and before any
  object is built: definitions must carry the namespace of the package, references that of
  the package, of a package listed in its manifest, or the kernel's. All violations are
  reported together as `rterr.NamespaceViolation`, with file, JSON Pointer, the name found and
  the expected namespaces. Files that belong to no known object type are rejected as well.
  Every object type registers with the loader for this check.
- Names defined by packages are limited to 63 bytes and rejected when longer, never shortened.
- Generated names: `<TABLE>#PK`, `<TABLE>#FK#<KEY>`, `<TABLE>#CK#<FIELD>`. A generated name
  longer than 63 bytes keeps its first 52 bytes followed by `~` and 10 hex digits of the
  SHA-256 of the full name. The repository stores the generated names; `rt check` compares
  them. `#` and `~` cannot occur in object names.
- The kernel owns the namespace `/RT/`; the package ID `RT` is reserved.
- A foreign key references the complete primary key of its target, which must belong to the
  same package or a required one. Field types must match (same base type, CHAR not longer
  than the target, DEC with equal precision and scale).
- Kernel domains and the tenant table are available to every package. Other kernel tables
  can neither be referenced nor extended.

### Asset repositories

`assets/templates/asset.json` of `TRFIN`:

```json
{
  "description": { "en": "Finance print templates", "de": "Druckvorlagen Finanzen" },
  "folders": { "invoice": { "description": { "en": "Invoices", "de": "Rechnungen" } } },
  "files": {
    "invoice/invoice.xsl": {
      "languages": { "en": "invoice/invoice_en.xsl", "de": "invoice/invoice_de.xsl" }
    },
    "data/rates.dat": { "mimeType": "text/csv" }
  }
}
```

- The repository ID is the folder name: up to 32 lower-case letters, digits, `_` and `-`,
  starting with a letter. `web` of `TRBASE` and `web` of `TRFIN` are separate repositories.
  Repositories and assets are always addressed by package ID, repository ID and path.
- `description` (the label) is mandatory and needs English. `folders` adds descriptions and
  folders that may be missing on disk. `files` sets the media type, a description and
  language variants per path. Descriptions of folders and files may be `""`, which differs
  from no description.
- A path with `languages` is language-dependent. Its variant files are reachable only through
  that path; a file at the path itself is the language-independent version. Reading with a
  language takes the requested language, then English, then the language-independent version;
  without a language the language-independent version, then English. Variants in languages
  that are not set up are rejected like texts.
- Paths: segments of ASCII letters, digits, `.`, `_` and `-` that do not start with `.`, at
  most 100 bytes per segment and 255 bytes per path, no leading, trailing or double slashes,
  and no two paths that differ only in case.
- Rejected: files directly below `assets/`, folders without `asset.json`, entries in
  `asset.json` without file, and symbolic links anywhere in the package.
- The media type comes from a fixed table of extensions; unknown extensions give
  `application/octet-stream`. Files may have at most 16 MiB by default
  (`Kernel.SetMaxAssetSize`, `--max-asset-size`, `RT_MAX_ASSET_SIZE`).
- Contents are stored in the database in chunks of 256 KiB, addressed by their SHA-256 and
  stored once. Reading streams chunk by chunk. Upgrades compare hashes: new assets are added,
  changed ones replaced, removed ones dropped, and contents no longer referenced are deleted
  in the same transaction. Asset changes are listed one by one in the plan and are never
  destructive. `rt check` verifies every content against its hash.

Reading at runtime:

```go
r, err := k.OpenAsset(ctx, "TRFIN", "templates", "invoice/invoice.xsl", model.Some("de"))
defer r.Close()
info := r.Info() // MimeType, Size, ETag(), ModifiedAt, Language
io.Copy(w, r)    // r is an io.ReadSeeker

fsys, err := k.AssetFS(ctx, "TRFIN", "templates", model.Some("de")) // io/fs.FS for one language
tmpl, err := template.ParseFS(fsys, "mail/*.html")
```

`StatAsset`, `ListAssets`, `AssetExists` and `AssetRepositories` complete the API. Code
reads asset repositories with `/TRBASE/ASSET_REPOSITORY`; see the
[language reference](docs/language.md#asset-repositories). Missing
assets return `rterr.AssetNotFound`, which matches `fs.ErrNotExist`.

### null and the empty string

`null` means "no value"; `""` is a value. The kernel keeps them apart in package files,
validation, repository, database, Go API, JSON and CLI and never converts one into the other.

- A field with `notNull: false` accepts `null` and `""` as different values; `notNull: true`
  rejects `null` but accepts `""`.
- Domains of type CHAR and STRING may set `"allowEmpty": false` (default `true`); the kernel
  then adds a check that rejects `""`. Fixed values may be `""`, never `null`.
- Defaults have three cases, in domains and fields alike: no `default` attribute (no
  default), `"default": null` (the explicit default null) and `"default": ""` or another value.
  A field without own default takes the default of its domain. The DDL writes no DEFAULT,
  `DEFAULT NULL` or `DEFAULT '…'` accordingly, and the repository stores the kind. A default
  `null` on a `notNull` field is invalid.
- CHAR(n) is `VARCHAR(n)` in PostgreSQL and a length check in SQLite: values are neither
  padded nor trimmed, trailing spaces are kept.
- Text columns compare in byte order on both databases: PostgreSQL columns have the
  collation `"C"`, as SQLite and Go compare. Comparisons, `order by`, indexes and
  authorization filters therefore give the same result everywhere. Kernel version 14
  changes the columns of existing databases.
- In Go, values that may be absent are `model.Optional[T]`; defaults are `model.Default`. JSON
  writes `null` for absent values, `""` for the empty string, and leaves out a default only if
  there is none. The CLI shows `<null>` and quoted strings.

### Data classes

| Class | Meaning |
|---|---|
| `configuration` | Defines the behavior of the application; set up once, rarely changed. |
| `application` | Master and transactional data of the running business. |
| `runtimeConfiguration` | Configuration maintained during operation, e.g. exchange rates. |
| `system` | Reserved for the kernel's native tables. |

Extensions inherit the class of their table. Changing a class on upgrade is not destructive
and needs no statements; the plan marks it with `[DATA CLASS]`. Databases created by the first
kernel version get the class `application` for all package tables when they are migrated, as
a provisional value until each package is applied again.

### Languages and texts

Language codes are ISO 639-1 codes in lower case. Texts of data elements and other objects are
stored in text tables per language. Installing a package with texts in a language that is not
set up fails with `rterr.MissingLanguages`, which names the missing languages. A language with
texts can only be removed with confirmation, which removes its texts as well.

### Structures and array types

- Component names are unique after all includes and extensions are resolved. A collision
  names both origins, e.g. `component CITY of /X/SITES collides with component CITY from
  include /TRBASE/ADDRESS`. `/RT/TENANT` is reserved and cannot be a component.
- Structures, tables and array types must not contain themselves, through any mix of
  includes, nested components, array components and row types. The error shows the full
  chain, e.g. `/X/A -> include /X/B -> component C /X/C -> row type /X/A`.
- Referenced objects belong to the own package, a required one or the kernel.
- Structures may include flat and deep structures and tables; tables include only flat ones.
  A structure extension that would make a structure deep is rejected when a table includes
  that structure.
- An included table brings only its fields, including the fields of its table extensions:
  not its tenant field, primary key, foreign keys or data class. Its key fields come without
  their NOT NULL, which follows from the key. The including table may use included columns
  in its primary key and foreign keys, and foreign keys may reference included key columns.
- Components of structure extensions follow the components of the structure, ordered by
  package. Structure extensions follow the rules of table extensions: names in the namespace
  of the extending package, nullable or with a default, no includes.
- `default` exists only for elementary components. A structure or array component with
  `notNull` is never null: new values start with an empty structure or array.
- Array keys consist of elementary top-level components of a structure or table row type.
- A change of a structure changes the columns of every table that includes it, in every
  package; the plan shows the structure, e.g. `add-column /TRFIN/COMPANIES.REGION (CHAR(80))
  via /TRBASE/ADDRESS`. A removed or narrowed component is destructive. A change that makes an
  object of another package invalid is rejected and names that object. Uninstalling a package
  removes its structures, array types and structure extensions with their columns, after
  confirmation.
- The repository stores structures, array types, extensions and includes, and for every
  column the include it comes from (`INCLUDE_NAME`) and the declaring structure or table
  (`SOURCE_NAME`) with origin `INCLUDE`. `rt check` compares the stored columns with the
  resolved includes.

`kernel.Structure` (and `rt show`) returns the resolved definition from package
[structure](structure) with its uses: components in their final order with type, labels,
effective default and origin (declaring object, include chain, extending package); nested
structures and array types are resolved as well. Fields name the include and the source of
included columns.

### Authorization objects

An authorization object names the fields whose values grants will give, and with the field
`ACTIVITY` the activities that can be granted (`schema/authorization-objects/COMPANY.json` of
`TRFIN`):

```json
{
  "name": "/TRFIN/COMPANY",
  "texts": {
    "en": { "label": "Company master data", "description": "Reading and changing companies by company and country" },
    "de": { "label": "Stammdaten Firma" }
  },
  "fields": [
    { "name": "COMPANY_ID", "dataElement": "/TRFIN/COMPANY" },
    { "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" },
    { "name": "ACTIVITY" }
  ],
  "activities": ["READ", "CREATE", "UPDATE", "DELETE", "EXPORT"]
}
```

- One to ten fields in a fixed order, with names of upper-case letters, digits and
  underscores, unique in the object. Each has a data element or a domain of the own or a
  required package, of the types `CHAR(n)`, `INT4`, `INT8`, `BOOL` or `UUID`: grants will
  give single values, ranges and patterns, which other types do not support well. Fixed
  values of the domain stay part of the field.
- `ACTIVITY` is the field of the kernel. It has neither data element nor domain; its type is
  the data element and domain `/RT/ACTIVITY`, `CHAR(16)` with the codes below as fixed
  values. With it, `activities` lists the activities that can be granted, at least one and
  none twice; without it, `activities` is not allowed. A field of its own named `ACTIVITY` is
  an error.
- A change of a data element or domain, also in another package, that gives a field another
  type than these is rejected and names the object and field.
- Upgrades may add fields and activities and change texts and order. Removing an object, a
  field or an activity and changing the data element, domain or type of a field are
  destructive, so grants of later versions stay valid; a longer `CHAR` is not destructive.
- Data elements and domains list the fields of authorization objects that use them.

| Activity | Meaning |
|---|---|
| `READ` | Display and read data |
| `CREATE` | Create new data |
| `UPDATE` | Change existing data |
| `DELETE` | Delete data |
| `EXECUTE` | Start a processing, evaluation or action |
| `APPROVE` | Approve or release in business terms |
| `REJECT` | Reject in business terms |
| `CANCEL` | Cancel or reverse a posted or completed transaction |
| `IMPORT` | Take over data from files or other systems |
| `EXPORT` | Download data or output it as a file |
| `PRINT` | Output and print |
| `SEND` | Transmit to third parties, e.g. by e-mail or interface |
| `ADMINISTER` | Maintain settings and administrative functions |

The list is closed; packages cannot add activities. Every activity has a label and an
explanation in English and German (`model.Activities`), and the kernel adds new ones at the
end without changing a code. A placeholder for all activities belongs to the grants of a
later version, not to the codes.

`kernel.AuthorizationObject`, `AuthorizationObjects` and `Activities` return the resolved
definitions from package [reflector](reflector): per field the data element with its labels
in all languages set up, the domain, type, length and fixed values, and for `ACTIVITY` the
allowed activities with their texts. `rt show authorization-object:/TRFIN/COMPANY`,
`rt authorization-objects list [package]` and `rt activities list` show them, with `--json`
in the form of the reflector. Code reads them with `/TRBASE/REFLECTOR`.

### Roles

A role bundles authorizations on authorization objects (`schema/roles/COMPANY_CLERK.json` of
`TRFIN`):

```json
{
  "name": "/TRFIN/COMPANY_CLERK",
  "texts": { "en": { "label": "Company clerk" }, "de": { "label": "Sachbearbeitung Firma" } },
  "authorizations": [
    {
      "object": "/TRFIN/COMPANY",
      "fields": {
        "COMPANY_ID": [{ "pattern": "*" }],
        "COUNTRY": [{ "value": "DE" }, { "value": "AT" }, { "from": "PL", "to": "SK" }],
        "ACTIVITY": [{ "value": "READ" }, { "value": "UPDATE" }]
      }
    }
  ]
}
```

- An authorization names every field of its object; a missing field is an error, not a
  grant. Each field lists values (`{"value": v}`), ranges with both bounds included
  (`{"from": a, "to": b}`) and patterns: `*` for all values or a prefix followed by `*`
  (`DE*`). The entries of a field combine with or, the fields of an authorization with and,
  and several authorizations on the same object, in one role or several, with or.
- Values fit the type and length of the field: strings for `CHAR` and `UUID`, numbers for
  `INT4` and `INT8`, `true` or `false` for `BOOL`; with fixed values only those. Ranges and
  prefixes need `CHAR` or an integer type; ranges of `CHAR` compare code points, prefixes of
  integers their decimal digits. `ACTIVITY` takes allowed activities of the object or `*`,
  which includes activities the object allows later.
- `""` is a value where the domain allows it, and `*` includes it; a prefix never matches
  `""`. `null` is never granted.
- A composite role has `"roles"` instead of `"authorizations"`: single roles only, no
  composite roles and not `/RT/ALL_AUTHORIZATIONS`.
- Package roles reference objects and roles of the own and required packages. They are
  installed, upgraded and uninstalled with their package and must fit their objects then.
  When an upgrade of another package adds a field to an object or removes an activity,
  authorizations that no longer fit grant nothing until they are maintained; the plan lists
  them as `incomplete-authorization` before the commit. An operation that removes a role
  that is assigned is rejected.
- Local roles have the same format in the namespace `/LOCAL/` (the package ID `LOCAL` is
  reserved) and are maintained at runtime: `kernel.Roles().Create`, `Update`, `Import`,
  `Export`, `Delete`, and the `rt role` commands. They may use every installed object and
  role. Deleting one that is assigned or in a composite role needs a confirmation, which
  removes it from both. Uninstalling a package removes its roles from local composite roles,
  the authorizations on its objects from local roles and the assignments of its roles.

Roles are assigned to users per tenant, with an optional first and last day of validity
(`kernel.Roles().Assign`, `rt role assign`). A user gets roles only in tenants it works in;
when it loses the tenant, or the tenant is deleted, its assignments there go too. The same
role may be assigned several times, also with overlapping periods.

`/RT/ALL_AUTHORIZATIONS` is the role of the kernel for the first setup and emergencies. It
grants every authorization object with all values and every allowed activity, decided when
authorizations are evaluated: objects, fields and activities installed later are included,
nothing is stored and nothing can become incomplete. It is part of the kernel tables with
texts in English and German, written by `rt init` with the other kernel objects, and cannot
be changed or deleted, neither by commands, the library, imports nor packages, which cannot
use the namespace `/RT/`. The kernel checks it when it starts, after `rt init`, after every
operation on packages and when languages change; a missing or changed role, e.g. after
direct access to the database, is restored with its assignments kept and recorded as
`all-authorizations-restored`.

- It is assigned only with `rt role assign` (`RoleOptions.AllAuthorizations` in the library),
  never by packages, imports or composite roles.
- Every assignment, removal, restoration and sign-in of a user who has it is recorded in the
  history and written to the log at level warn with `audit=true`, whatever level the log is
  set to.
- Removing the last effective assignment, locking the last user who has it or taking the
  tenant of that user needs `--yes`, since nobody could administer everything afterwards.
  A lock after failed logins happens anyway and is audited.
- `rt check` lists the users who have it and warns when no active user has it today.
- `rt init --admin <user> --tenant <tenant>` creates a first user with it, with the password
  on standard input; `RT_ADMIN_USER`, `RT_ADMIN_TENANT` and `RT_ADMIN_PASSWORD` do the same,
  flags win. On a terminal of a database without users, `rt init` asks. A missing tenant is
  created.

TRBASE brings the authorization objects `/TRBASE/USER_ADMIN` (`USER_NAME`),
`/TRBASE/ROLE_ADMIN` (`ROLE_NAME`, also `IMPORT` and `EXPORT`) and
`/TRBASE/ROLE_ASSIGNMENT` (`ROLE_NAME`, `TENANT`) for the administration. Applications that
let their users administer call the library on behalf of an actor:
`k.Users().As(kernel.Actor{User: "rita", Tenant: "ACME"})` and `k.Roles().As(...)` check
every function with the actor's authorizations in the tenant first: creating needs
`CREATE`, changes, locks, passwords and tenants `UPDATE`, reading `READ`, deleting
`DELETE`; `List` and `Assignments` return only what the actor may read. A failed check
returns `*rterr.NotAuthorized`, which names the object and the activity but not the
authorizations; the history records the actor as who made the change. Without actor, as
on the command line, nothing is checked.

Every change of local roles and assignments, also those that operations on packages,
users and tenants make, is recorded in `/RT/ROLE_HISTORY` with who made it and the old and
new state; entries are never changed (`rt role history`).

`kernel.EffectiveAuthorizations(user, tenant, day)` returns the effective authorizations from
package [authorization](authorization): the assignments valid on the day, composite roles
resolved, grouped by object with their origin, compiled for fast checks, and the
authorizations that are not effective. Locked users have none. Results are cached per user,
tenant and day until `/RT/AUTH_GENERATION` changes, which every relevant change increases in
its transaction; every call reads the counter, so that kernels on the same database see the
changes of each other at once.

### Authorization checks

Code checks authorizations with `check authorization` and `authorized`, also as filter in
`where` conditions (see the [language guide](docs/language.md)); native methods with
`native.Context.Authorize`; the kernel checks service nodes and, with an actor, the
administration. An execution reads the effective authorizations of its user once, every
check after that runs in memory (`go test -bench Check ./authorization ./kernel`: about
0.1 µs per check). An execution without user fails every check; `rt run --user anna`
runs as a user, who must work in the tenant, and an unhandled `/RT/NOT_AUTHORIZED` ends
`rt run` with exit code 7 and shows the values of the check.

The kernel keeps the **last failed check** of every user and tenant in
`/RT/AUTH_LAST_FAILED`: the object, the values (`null` for `any`), where the check was made
(class, method and line of code, or the function of the kernel) and the grants of the user
on the object at that time. It is written after the execution ended, so a rollback keeps
it. Failed `authorized` expressions and filters are expected and not kept, and neither are
checks without user.

```bash
rt user last-failed-check anna --tenant ACME
```

An **authorization trace** records the checks of a user, in one tenant or in all, for a
time (default 30 minutes) and up to a number of entries (default 10 000): every check with
its result and place, every filter with the SQL condition the kernel made and the number of
rows the statement read or changed. A trace that is off costs nothing: whether one runs is
part of the cached effective authorizations.

```bash
rt trace authorizations start anna --tenant ACME --minutes 10
rt trace authorizations show anna
rt trace authorizations stop anna
```

Test methods set their own authorizations with `/TRBASE/TEST_AUTHORIZATIONS`, whatever
roles their user has; every test method starts without authorizations, and outside of
`rt test` the methods fail:

```
/TRBASE/TEST_AUTHORIZATIONS.GRANT("/TRFIN/COMPANY", ["COMPANY_ID=*", "COUNTRY=D*", "ACTIVITY=UPDATE"])
/TRBASE/TEST_AUTHORIZATIONS.GRANT_ROLE("/TRFIN/COMPANY_CLERK")
/TRBASE/TEST_AUTHORIZATIONS.GRANT_ALL()
/TRBASE/TEST_AUTHORIZATIONS.CLEAR()
```

The examples check as well: `/TRFIN/COMPANY_API` lists the companies the user may read,
`/TRFIN/COMPANY_INTAKE` creates only companies the user may create, and
`/TROTTO/COMPANY_SYNC` needs `EXECUTE` on `/TROTTO/SYNC` for the country.

### Extension points

An extension point lets other packages run code at a place of the own code, typed, filtered
and switchable per tenant. `TRFIN` defines `/TRFIN/COMPANY_CHECKS`
(`schema/extension-points/COMPANY_CHECKS.json`) on the interface of the same name:

```json
{
  "name": "/TRFIN/COMPANY_CHECKS",
  "description": { "en": "Checks when a company is created", "de": "Prüfungen beim Anlegen einer Company" },
  "interface": "/TRFIN/COMPANY_CHECKS",
  "use": "multiple",
  "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }],
  "fallback": null
}
```

`TROTTO` implements it for Germany and Austria
(`schema/extension-implementations/COMPANY_CHECKS.json`):

```json
{
  "name": "/TROTTO/COMPANY_CHECKS",
  "description": { "en": "Companies wait for the OTTO sync" },
  "extensionPoint": "/TRFIN/COMPANY_CHECKS",
  "class": "/TROTTO/COMPANY_CHECKS",
  "filters": { "COUNTRY": [{ "value": "DE" }, { "value": "AT" }] },
  "order": 100
}
```

and the behavior class `/TRFIN/COMPANY_BEHAVIOR` of the business object `/TRFIN/COMPANY`
runs it with `extension point /TRFIN/COMPANY_CHECKS filters COUNTRY = ...` when it adjusts
and validates new companies; see the [language reference](docs/language.md#extension-points).

- **Extension point**: a name in the namespace of the package, unique among extension points,
  a description per language (English mandatory, at most 60 characters) and a `public`
  interface of the own package. `use` is `single`, exactly one implementation runs, or
  `multiple`, all matching active implementations run in their order. Up to five `filters`
  with a data element or a domain of the types of fields of authorization objects (`CHAR(n)`,
  `INT4`, `INT8`, `BOOL`, `UUID`). `fallback` names a class of the own package that implements
  the interface and runs when no implementation matches; `null` or no attribute is none.
  Without fallback, no matching implementation is a runtime error for `single`
  (`/RT/NO_IMPLEMENTATION`) and runs nothing for `multiple`. `commitAllowed` (default `false`)
  lets implementations commit or roll back the default session. `activation` is `tenant`
  (the default: implementations are switched globally and per tenant) or `global` for points
  the kernel runs without tenant, such as `/TRBASE/ODATA_METADATA`, whose implementations are
  switched only globally.
- **Implementation**: a name in the namespace of the implementing package, a description, the
  extension point, which belongs to the package or to a package it requires, a `public` class
  of the package with a public constructor without parameters that raises no checked
  exception, and per filter field values, ranges and patterns with the rules of roles: `*` for
  all values, every field given. `order` (default 0) orders the implementations of a
  `multiple` extension point; equal numbers are ordered by package ID and name. A package may
  bring several implementations of one extension point and implement its own ones. Extension
  points and implementations are names of their own: they may be called like the interface
  and the class, as in the example.
- **Checks on installation**: the interface exists, is an interface and public; the classes
  implement it with the constructor described; the values fit the fields; the methods suit
  the use (see the language reference). Two active implementations of a `single` extension
  point that match a common filter value are rejected, globally and in every tenant with
  settings of its own; the message names both and the values in which they overlap. Should two
  still match when the code runs, it raises `/RT/IMPLEMENTATION_CONFLICT` with both names.
- **No recompilation of the definer**: the kernel selects the implementations from a registry
  that every generation of code builds. Installing or removing an implementation compiles only
  its own package and starts a new generation. A change of the interface compiles every
  implementing package again through the requirements, and fails when a class no longer fits.
- **Upgrades**: new methods in the interface and changed texts are allowed; implementations then
  have to implement the new methods. The interface has no default methods: a package that wants
  to add methods later offers a `public open` class implementing the interface, which
  implementations extend, as `/TRFIN/COMPANY_CHECKS_BASE`. A new filter field needs a `default`
  (entries as in implementations) for implementations that do not name it. Removing an
  extension point or a filter field, changing the type of a field, switching between `single`
  and `multiple`, another interface and forbidding commits are destructive and need
  `AllowDestructive`; as always, a change that breaks an implementation of another package is
  rejected in any case.

**Activation.** Implementations are active after their installation.
`rt implementation deactivate|activate <name> [--tenant T]` (`k.Implementations().Deactivate`,
`Activate`) switch them globally or for a tenant, whose setting takes precedence over the global
one; `rt implementation reset <name> [--tenant T]` removes a setting. Activating an
implementation of a `single` extension point that overlaps with another active one is rejected.
The settings are runtime configuration in `/RT/IMPLEMENTATION_STATES` and the tenant-specific
`/RT/IMPLEMENTATION_TENANT_STATES`: upgrades keep them, removing the implementation or its
package and deleting the tenant remove them. Every change increases
`/RT/IMPLEMENTATION_GENERATION`; an execution reads it once, when it first runs an extension
point, so a change reaches every kernel on the database with its next execution, without a
restart. Every change, also installations and removals of implementations, is recorded in
`/RT/IMPLEMENTATION_HISTORY` (`rt implementation history [name]`).

**Description and trace.** `kernel.ExtensionPoint` and `ExtensionPoints` (from package
[reflector](reflector), plus the activation and the code that runs an extension point),
`rt extension-point list [package]` and `rt extension-point show <name>` describe extension
points: interface, use, filters, fallback and the implementations with package, class, filter
values, order and activation globally and per tenant; `--json` gives the form of the reflector.
Code reads them with `/TRBASE/REFLECTOR`. `rt trace extension-points start|show|stop <user>
[--tenant T]` records for a time which implementations the extension points that a user runs
select and why: selected, inactive globally or in the tenant, a filter field that does not
match, or the fallback class.

**Extension points and `include ... if found`.** Both stay. Extension points are the
recommended way for business extensions: they are typed against an interface, filtered,
switchable per tenant and installed without compiling the defining package again.
`include ... if found` remains for code that belongs into the context of the including package
itself, such as members or statements that use its private parts.

### Business objects

A business object combines a root table and dependent tables of the own package, its
entities, into one object with fixed behavior. Code changes its tables only with entity
statements (see [docs/language.md](docs/language.md)), which work in a transaction buffer
and go through a fixed save sequence. `TRFIN` defines `/TRFIN/COMPANY`
(`schema/business-objects/COMPANY.json`):

```json
{
  "name": "/TRFIN/COMPANY",
  "description": { "en": "Company", "de": "Firma" },
  "root": "/TRFIN/COMPANIES",
  "behaviorClass": "/TRFIN/COMPANY_BEHAVIOR",
  "entities": {
    "/TRFIN/COMPANIES": {
      "operations": ["create", "update", "delete"],
      "numbering": "external",
      "fields": { "NAME": { "mandatory": true }, "STATUS": { "readOnly": true } },
      "administrative": { "createdBy": "CREATED_BY", "createdAt": "CREATED_AT", "changedBy": "CHANGED_BY", "changedAt": "CHANGED_AT" },
      "etag": "CHANGED_AT",
      "determinations": [
        { "name": "SET_INITIAL_STATUS", "when": "modify", "on": ["create"] },
        { "name": "ADJUST", "when": "save", "on": ["create"] }
      ],
      "validations": [{ "name": "CHECK_COUNTRY", "on": ["create", "update"], "fields": ["COUNTRY"] }],
      "actions": [{ "name": "BLOCK", "parameter": "/TRFIN/BLOCK_REASON", "result": "self" }],
      "authorization": {
        "object": "/TRFIN/COMPANY",
        "fields": { "COMPANY_ID": "COMPANY_ID", "COUNTRY": "COUNTRY" },
        "activities": { "read": "READ", "create": "CREATE", "update": "UPDATE", "delete": "DELETE", "BLOCK": "UPDATE" }
      }
    },
    "/TRFIN/CONTACTS": { "parent": "/TRFIN/COMPANIES", "foreignKey": "COMPANY", "operations": ["create", "update", "delete"], "numbering": "uuid" },
    "/TRFIN/BANK_ACCOUNTS": { "parent": "/TRFIN/COMPANIES", "foreignKey": "COMPANY", "operations": ["create", "update", "delete"] }
  }
}
```

- Every entity is a table of the own package in at most one business object, all with the
  same tenant flag and the data class of the business object. Dependent entities name
  their `parent` and a `foreignKey` of their table that covers the key of the parent; they
  may not have fields `LINK` or `PARENT_LINK`, which link new instances in a statement.
- `numbering` is `external` (the caller gives the key, the default) or `uuid` (the kernel
  gives the single key field of type `UUID`).
- `fields` sets `readOnly`, `readOnlyOnUpdate` and `mandatory`; `administrative` names the
  fields the kernel fills when it saves (`createdAt`/`changedAt` TIMESTAMP, `createdBy`/
  `changedBy` text) and `etag` a TIMESTAMP, UUID or INT8 field for optimistic locking.
- `dataClass` is `configuration` or `runtimeConfiguration`; without it the business object
  holds application data. The behavior class is optional for a business object without
  determinations, validations and actions, the usual case for configuration.
- Determinations (`when`: `modify` or `save`) and validations name methods of the behavior
  class and trigger on operations, for `update` optionally only on some `fields`. Actions
  are `instance` (the default) or static, with an optional `parameter` structure, a `result`
  (`self` or a structure) and `modifies` (default `true`).
- `authorization` maps every field of the authorization object except `ACTIVITY` to a field
  of the entity and every operation, `read` and every action to an activity. The root of a
  business object of application data needs one. A dependent entity without one takes that
  of the nearest parent entity with one: `read` with its activity for read, everything else
  with its activity for update.
- A business object of configuration without an authorization is checked with
  `/TRBASE/CONFIGURATION` of TRBASE: its field `BUSINESS_OBJECT` gets the name of the
  business object, `DATA_CLASS` its data class, `ACTIVITY` is `READ`, `CREATE`, `UPDATE` or
  `DELETE` for the operation, and `UPDATE` for actions.
- `extensible` lists the kinds of extensions other packages may make (`fieldProperties`,
  `determinations`, `validations`, `actions`, `entities`), for the business object and
  narrower per entity. Without it everything is allowed; `[]` allows nothing.
- In an upgrade new entities, actions, determinations and validations are allowed, and
  removed ones or stricter field properties as far as the code compiles again. A new root, a
  new parent, foreign key or numbering of an entity and removing the business object are
  destructive. A table that becomes an entity fails the upgrade with every place of code
  that still writes it.

Configuration is maintained the same way: `TRFIN` keeps its countries in the business object
`/TRFIN/COUNTRY` of the data class `configuration` without behavior class and authorization,
and the test package [testdata/businessobjects/TRSET](testdata/businessobjects/TRSET) has
partners with their accounts. Code writes no table of a business object directly.

#### Extensions of business objects

A package extends a business object of a package it requires in
`schema/business-object-extensions/<NAME>.json`. `TROTTO` extends `/TRFIN/COMPANY`:

```json
{
  "businessObject": "/TRFIN/COMPANY",
  "behaviorClass": "/TROTTO/COMPANY_BEHAVIOR",
  "entities": {
    "/TRFIN/COMPANIES": {
      "fields": { "/TROTTO/OTTO_ID": { "readOnly": true } },
      "determinations": [{ "name": "/TROTTO/RESERVE_ID", "when": "save", "on": ["create"] }],
      "validations": [{ "name": "/TROTTO/CHECK_MARKET", "on": ["create", "update"], "fields": ["COUNTRY"] }],
      "actions": [{ "name": "/TROTTO/SYNC", "result": "self", "activity": "EXECUTE",
        "authorization": { "object": "/TROTTO/MARKETPLACE", "fields": { "COUNTRY": "COUNTRY" } } }]
    },
    "/TROTTO/COMPANY_CHANNELS": { "parent": "/TRFIN/COMPANIES", "foreignKey": "COMPANY",
      "operations": ["create", "update", "delete"], "numbering": "uuid" }
  }
}
```

- An extension sets field properties only for fields of its own package, from extensions of
  tables or of structures they include. It cannot change operations, numbering,
  administrative fields, ETag or authorization of the business object, nor replace or switch
  off behavior of other packages.
- Determinations, validations and actions carry the namespace of the extending package; the
  methods of its behavior class have their names without it (`RESERVE_ID`). Code runs an
  action of an extension with its full name: `execute /TRFIN/COMPANIES./TROTTO/SYNC`.
- An action on an entity of the business object takes an `activity` of its authorization or
  of an `authorization` object of the extending package with fields of the entity.
- Dependent entities of the extending package follow the rules of entities, have the data
  class of the business object and take the authorization of their parent unless they have
  one. Deleting an instance deletes them with it.
- Code uses actions, entities and fields of an extension only when its package requires the
  extending package. In local mode the behavior class of an extension reads the whole
  business object but changes only fields of its own package and its own entities.
- Determinations of the business object run first, then those of the extensions by the
  level of the extending package (a package that requires another extending package comes
  after it) and package ID. Their changes trigger the others; each runs at most once per
  instance and save, in at most ten rounds. Validations run in the same order, all of them.
  Failures and messages carry the package in `ORIGIN`.
- The kernel finds the extensions in a registry per generation, so installing or removing
  one compiles the package of the business object only when the layout of its rows changes.
  A change of the business object that breaks an installed extension is rejected with a
  message that names it. A field that becomes `mandatory` leaves existing rows as they are
  and is checked when their instance is next saved.

`kernel.BusinessObject` and `BusinessObjects` (from package [reflector](reflector), plus the
code that works on it), `rt business-object list [package] [--data-class C]` and `rt
business-object show <name> [--base]` describe business objects with their installed
extensions: entities as a tree with key, parent key, operations, numbering, fields with
type, texts and properties, behavior and authorization, each with the package it comes from;
`--base` leaves the extensions out and `--json` gives the form of the reflector.

### Object types and objects

A package defines an object type in `schema/object-types/<NAME>.json`:

```json
{ "name": "/TRDEF/VIEW", "description": { "en": "View" },
  "content": "/TRDEF/VIEW", "behavior": "/TRDEF/VIEW_BEHAVIOR" }
```

`content` is a structure of the package, flat or deep with nested structures and array
types. A package that requires the defining package directly, or the defining package
itself, ships objects in `objects/<DEFINING PACKAGE>/<TYPE WITHOUT NAMESPACE>/<NAME>.json`;
the name carries the namespace of the shipping package:

```json
{ "name": "/TRUSE/NOTES",
  "content": { "ENTITY": "/TRDEF/NOTES", "TEXTS": [{ "LANGUAGE": "en", "TITLE": "Notes" }],
               "COLUMNS": [{ "FIELD": "TITLE" }, { "FIELD": "BODY", "WIDTH": 20 }] } }
```

- The content has the format of `/TRBASE/JSON`: components by name, extension fields by their
  full name, `DEC` as string, fixed values by their name, `null` and `""` apart. Data
  elements, domains, fixed values, `notNull` and defaults apply as everywhere. The kernel
  stores the canonical form: every component in order, a missing optional component as
  `null`, a missing component with `notNull` with its default. Its SHA-256 is the hash.
- **References** are components with a data element of TRBASE that has the meaning of a
  reference: `/TRBASE/REF_PACKAGE`, `REF_BUSINESS_OBJECT`, `REF_ENTITY`, `REF_FIELD`,
  `REF_ACTION`, `REF_SERVICE`, `REF_ENTITY_SET`, `REF_ROLE`, `REF_HTTP_NODE`,
  `REF_ASSET_REPOSITORY` and `REF_OBJECT`. A field, an action, an entity set and an asset
  repository belong to the nearest entity (or business object), service or package in the
  same structure or one that encloses it; the type needs such a component in scope. A data
  element of the package with the domain `/TRBASE/REF_OBJECT` and `"objectType"` refers to
  objects of that type. References take part in the namespace check of the import, must name
  existing objects of the package or of packages it requires, and keep them from being
  removed by an upgrade; `rt show` lists the objects that use an object.
- **Texts** are arrays whose rows include `/TRBASE/TEXT_LANGUAGE` and have the unique key
  `LANGUAGE`. English is mandatory and other languages must be set up, as for all texts.
- Types of the repository and of the same installation are known. An object whose type, or
  whose folder, is unknown is rejected, so is an object of a type whose package is not
  required.
- When an upgrade changes the content structure, every installed object of the type must
  fit the new structure, or the upgrade fails with the name of the object and its package.
- Objects are not tenant-specific and read-only at runtime. Code reads them with
  `/TRBASE/OBJECTS` (see the [language](docs/language.md#objects-of-package-defined-types)).
- The **behavior class** implements `/TRBASE/OBJECT_TYPE_BEHAVIOR`. In the transaction of an
  installation, upgrade or removal, after the repository holds the new objects, the kernel
  calls `CHECK` and then `CHANGED` once per type with all changed objects of the type
  (`CREATED`, `CHANGED`, `DELETED` with the old and new hash), types in the dependency order
  of their packages. Both run in an execution that only reads, without tenant and user, for
  at most `Config.ObjectBehaviorTimeout` (5 seconds). `CHECK` returns messages: an error, or
  an exception of either method, rejects the operation; warnings appear in its plan as
  `object-message`. A dry run does not call them. Combining several objects that refer to
  the same target is up to the defining package.
- `rt object-type list|show|schema`, `rt object list <type> [package]` and `rt object show
  <name>` describe types and objects; `rt object-type schema` writes a JSON Schema of the
  object files for editors. `rt show` lists the objects that refer to an object.

### Tenants

For a table with `tenantSpecific: true` the kernel

- adds `/RT/TENANT` as first field and first key field,
- adds a foreign key from it to `/RT/TENANTS`,
- prepends the tenant field to every foreign key to another tenant-specific table, so a row
  can never reference data of another tenant.

Foreign keys from tenant-specific to shared tables run without the tenant field. Foreign keys
from shared to tenant-specific tables are rejected. Extensions inherit the flag of their
table; changing the flag of an existing table is destructive.

The tenant key is `CHAR(16)` of upper-case letters, digits and underscores: short, because it
is part of every key of every tenant-specific table, readable in logs and on the command line,
and stable across databases. A tenant with data cannot be deleted through a plain `DELETE`.
`Tenants().Delete` with confirmation removes its rows from all tenant-specific tables in
dependency order first.

## Operations

All operations share one path: read the repository, compute the target schema, validate it as
a whole, diff, let the dialect plan the statements, and run them in one transaction with the
repository update and a log entry. On error everything is rolled back; the failure is logged
afterwards. With `DryRun` only the plan is returned.

- **Install**: packages given together are installed in dependency order in one transaction.
- **Upgrade**: compares the new version with the repository. Allowed without further options:
  new tables, new fields that are nullable or have a default, longer CHAR, new foreign keys,
  new fixed values, relaxing NOT NULL, changed defaults. Everything that removes or restricts
  (fields, tables, foreign keys, fixed values, shorter or different types, key changes, NOT
  NULL on existing fields, tenant flag) needs `AllowDestructive` and is marked
  `[DESTRUCTIVE]` in the plan. Changes that break another installed package are rejected
  regardless.
- **Uninstall**: only if no installed package requires the package; needs `Confirm`. Removes
  its tables, domains and its extension fields in tables of other packages.
- **Check**: compares repository and database (tables, columns, types, nullability, defaults,
  keys, foreign keys, constraint names) and validates the repository, including all business objects and their extensions.

### Packages from GitHub

Packages come from directories, ZIP files or GitHub repositories, one package per repository
or several in directories of one:

```bash
rt install github.com/tim-riep/trfin@v1.2.0
rt install github.com/tim-riep/trotto@^1.0
rt install https://github.com/tim-riep/erp-packages/tree/v2.0.0/packages/TRFIN
rt upgrade TRFIN
rt outdated
```

- **Addresses** have the canonical form `github.com/<owner>/<repo>[/<dir>][@<spec>]`; URLs
  `https://github.com/<owner>/<repo>` and `…/tree/<ref>/<dir>` become that form. The spec is
  a version (the tag `v1.2.0` or `1.2.0`), a constraint in the syntax of manifests (`^1.0`,
  `>=1.2.0 <2.0.0`, highest matching tag, pre-releases only when the constraint names one), a
  full commit hash or, with `--allow-branch`, a branch. Without spec the highest stable version
  counts. A branch with `/` in its name is written after `@`.
- **Tags of packages in directories**: a repository that releases several packages tags
  each with the ID of the package in lower case as prefix, e.g. `trfrontend-v1.2.0` for
  `github.com/tim-riep/go-runtime/packages/TRFRONTEND`. For an address whose directory ends
  with a package ID only such tags count, as long as the repository has one; otherwise its
  tags without prefix do. `@1.2.0` and `@trfrontend-v1.2.0` name the same version.
- **Archives**: the asset `<ID>-<version>.zip` of the release of the tag comes first, the way
  for packages with a build step; with several package assets the last segment of the
  directory or `--package <ID>` chooses. Otherwise the archive of the commit counts, with
  the package at the root or in the directory; of a repository only the package entries
  (`manifest.json`, `schema/`, `src/`, `assets/`, `transformations/`, `objects/`) are taken, not
  README.md and the like. The manifest decides the ID, and its version must be that of the
  tag. Then the usual path follows: validation, namespace check, dependencies, compiling,
  plan, one transaction.
- **Dependencies**: a requirement may name a source, `{ "id": "TRFIN", "version": "^1.0",
  "source": "github.com/tim-riep/trfin" }`. A required package that is missing, or installed
  but too old, comes from its source at the highest version that every known constraint of
  the set and of the installed packages allows; a later constraint that excludes it is a
  conflict. Without source the package must be installed. Upgrades of installed packages need
  `--upgrade-dependencies`; the plan shows them first. Kernel packages never come from a
  source. Everything is resolved and downloaded into the cache before the plan is shown, and
  installed in one transaction in dependency order.
- **Confirmation**: with an address `rt install` shows the plan with all packages, sources,
  commits and hashes and asks; `--yes` confirms, `--dry-run` only shows it.
- **Origins** are kept in `/RT/PACKAGE_ORIGINS`: address, requested spec, the constraint of
  later upgrades (the requested constraint, else `^<version>`), tag, commit, asset, SHA-256 of
  the archive, time and who; `rt show <ID>` shows them. Packages from directories and ZIP files
  are local. `rt upgrade <ID>` chooses the highest version that the constraint and the
  requirements of the installed packages allow, `rt upgrade <ID>@<spec>` another one;
  `rt outdated` lists the installed, the highest matching and the latest version.
- **Integrity**: `--expect-sha256 <hash>` checks the archive of a single address. The commit
  and hash of every tag seen are kept in `/RT/SOURCE_TAGS`, also after uninstalling; a tag
  that points elsewhere later is rejected unless `--allow-retag`. `rt export lock -o rt.lock`
  writes the origins of all packages, `rt install --lock rt.lock` installs exactly these
  commits and archives elsewhere; local packages of a lock must be installed already.
- **Cache**: archives are kept by SHA-256 in `$RT_CACHE_DIR` (default the user cache
  directory, `go-runtime/`), API answers with their ETag beside them. A lock installs from the
  cache without network.
- **Access**: public repositories need nothing. `RT_GITHUB_TOKEN`, else `GITHUB_TOKEN`, gives a
  token for private repositories and higher rate limits; it goes only to the API, never to
  download hosts, and is never stored, logged or shown. `RT_GITHUB_URL` names a GitHub
  Enterprise Server (API at `<url>/api/v3`). `RT_SOURCE_HOSTS` lists the hosts requests and
  redirects may reach (default github.com, api.github.com, codeload.github.com and the
  download hosts), `RT_SOURCE_ALLOW` the owners or `owner/repo` that packages may come from,
  also as dependencies. Only HTTPS; requests time out after 30 seconds, downloads after 5
  minutes; 502, 503, 504 and network errors are retried three times; a reached rate limit is
  reported with the time of its reset.
- **Archives are data**: entries outside the package, absolute paths, links and other
  special files are rejected, and so are archives above 64 MiB, more than 256 MiB of files,
  files above 32 MiB (at least the asset limit) or more than 20 000 entries. Nothing of an
  archive is executed; its source files are compiled like any others.

### Dialect notes

- SQLite: every connection enforces foreign keys. Because `PRAGMA foreign_keys` has no effect
  inside a transaction, the kernel switches it off on a dedicated connection before a schema
  transaction and runs `PRAGMA foreign_key_check` before commit. Changes that `ALTER TABLE`
  cannot express rebuild the table (create, copy, drop, rename), keeping the fields of all
  extending packages.
- PostgreSQL: DDL is transactional, operations are serialized with an advisory lock, foreign
  keys are `DEFERRABLE INITIALLY IMMEDIATE`. The kernel uses the first schema of the
  `search_path`.
- Sessions of code: on PostgreSQL every session has its own connection with a lock timeout
  (default 2 seconds), every statement runs in a savepoint and cursors are fetched in
  portions. On SQLite sessions begin their transactions deferred and read in parallel,
  but only one session of an execution writes at a time: a second writing session gets
  `/RT/SESSION_ERROR` at once instead of waiting for itself. `/TROTTO/COMPANY_SYNC`, which
  updates companies in the default session and writes the audit log in another one, thus
  works on PostgreSQL and on SQLite only when no company needs an update.

## Source language

Packages contain code in the go-runtime language in `src/`: classes, interfaces,
exception and test classes, and includes. The [language reference](docs/language.md)
describes syntax, types and rules; its examples are compiled by the tests. The example
package [TRORDER](examples/TRORDER) shows classes, an interface, schema types (a
structure, a table used as structure, an array type with a unique key and a domain with
fixed values as enum), an exception class, a test class and both kinds of includes.

```
public class /TRORDER/ORDER_RATING implements /TRORDER/RATER

  public constant LIMIT: DEC(15,2) = 1000.00
  private attribute HISTORY: array of /TRORDER/ORDER_INFO

  public method RATE(order: /TRORDER/ORDER_INFO) returns /TRORDER/RATING raises /TRORDER/INVALID_ORDER
    var note = order.NOTE default ""
    HISTORY.APPEND(order)
    if order.AMOUNT > LIMIT
      return /TRORDER/RATING.HIGH
    end
    return /TRORDER/RATING.NORMAL
  end

end
```

The pipeline needs no database: `lang/syntax` (lexer, parser, canonical printer),
`lang/types` (checker with null checks along the control flow, checked exceptions and
includes), `lang/compile` (layouts and typed stack instructions), `lang/bytecode` (the
program and its binary encoding with format version), `vm` (the machine with limits) and
`lang/build`, which reads a package directory and runs all of them.

### Code in the kernel

- Installing and upgrading a package compile its code after the schema check and before
  the commit; dry runs and `rt plan` compile too. A compile error or a file that is not
  in canonical form stops the operation, and the error names package, file, line, column
  and code. Sources and bytecode are stored in the kernel tables `/RT/SOURCES` and
  `/RT/BYTECODE`, which are the only source of later compilations.
- Code uses types, classes, interfaces and includes of the packages it requires, and
  `include ... if found` of any package. Compiled programs refer to classes and methods
  of other packages by direct indexes and offsets, so the kernel compiles a package again
  in the same transaction when a package it requires, a package with an include it names
  or an object it uses changes. The plan shows these steps as `compile-package` with the
  reason, such as `compile-package TROTTO (requires TRFIN)`. Implementations of extension
  points need no compilation of the package that defines the extension point.
- `/RT/CODE_USES` records every reference of code with package, file and line;
  `rt show /TRFIN/COMPANIES` lists them, and `rt show class:/TRFIN/COMPANY_SERVICE` shows a
  class. `/RT/CODE_INCLUDES` keeps the includes, also those not found.
- Every change of code starts a new generation in `/RT/CODE_GENERATION`. An execution
  uses the generation that was current when it started until its end; the next one
  loads the new code. A kernel with another bytecode format compiles all packages again
  when it opens the database.
- Code reads and writes tables with `select`, `insert`, `update` and `delete`, always
  restricted to the tenant of the execution, and controls transactions over several
  sessions; see the [language reference](docs/language.md#database-access). Tables of
  business objects change only with entity statements. The example packages show it:
  `TRFIN` reads companies in `/TRFIN/COMPANY_SERVICE` and changes them through the business
  object `/TRFIN/COMPANY`, and `TROTTO` syncs them in `/TROTTO/COMPANY_SYNC`, writes `/TRBASE/AUDIT_LOG` in a session
  of its own and checks new companies of `TRFIN` through the extension point
  `/TRFIN/COMPANY_CHECKS`.

- Kernel packages contain methods executed by kernel: Go functions registered in the
  package `native` with the signature they implement, which the compiler checks against
  the declaration. Values cross the boundary as Go types (`model.Optional` for optional
  values, `dec.Dec` for `DEC`, generated structs from `rt gen natives` for structures);
  declared exceptions are raised with `native.Raise`, other errors and panics become
  `/RT/NATIVE_ERROR`. Native methods run under the time limit of the execution, declare
  capabilities such as `clock` that packages must list in their manifest, and register
  effects outside that run after the commit of their session; failed effects are kept in
  `/RT/EFFECT_FAILURES`. `rt natives list` shows the registered methods. TRBASE brings
  XSLT, assets, clock, UUID and hash methods; see the
  [language reference](docs/language.md#methods-executed-by-kernel).
- Transformations are XSLT stylesheets of packages, compiled with
  [go-xslt](https://github.com/tim-riep/go-xslt) on installation and kept compiled per
  generation. `transform value using /TRFIN/INVOICE_HTML parameters LANG = "de"` runs one on
  XML or on a value in a canonical XML format, and `into T` reads the result back; see the
  [language reference](docs/language.md#transformations). `TRFIN` renders invoices with
  `/TRFIN/INVOICE_HTML`, imports orders with `/TRFIN/ORDER_IMPORT` into the table
  `/TRFIN/ORDERS` and renders a print template of its asset repository with
  `/TRBASE/XSLT_UTILITIES` and `/TRBASE/XSLT_STYLESHEET`.
- TRBASE reads and writes zip archives (`/TRBASE/ZIP_ARCHIVE`), gzip data (`/TRBASE/GZIP`)
  and workbooks in xlsx format, cell by cell with `/TRBASE/XLSX_WORKBOOK` and as whole
  arrays with the native transformation `/TRBASE/XLSX`, which matches columns by the
  headings of the data elements in all languages and reports all values that do not fit
  at once. Limits against archives that unpack to far more than they claim are measured
  while unpacking and set with `kernel.Config.Formats`; see the
  [language reference](docs/language.md#xlsx). `TRFIN` exports its companies with
  `/TRFIN/COMPANY_FILES` as workbook in a zip archive with an invoice and imports them
  again, from code and through the nodes `/trfin/companies/export` and
  `/trfin/companies/import`:

  ```bash
  rt run TRFIN /TRFIN/COMPANY_FILES EXPORT '["2026-11-08"]' --tenant ACME --user anna | jq -r . | base64 -d > companies.zip
  rt run TRFIN /TRFIN/COMPANY_FILES IMPORT "[\"$(base64 < companies.zip | tr -d '\n')\"]" --tenant ACME --user anna
  ```

```go
out, err := k.Run(ctx, kernel.ExecOptions{Tenant: "ACME"}, "/TROTTO/COMPANY_SYNC", "SYNC",
	[]json.RawMessage{json.RawMessage(`"DE"`)})
results, err := k.Test(ctx, "TRFIN", nil, kernel.ExecOptions{})
```

## HTTP services

A package offers endpoints over HTTP with service nodes. Every installed package has a root
node `/<package ID in lower case>`; a file in `schema/http-services` adds a node below it:

```json
{
  "path": "companies",
  "texts": { "en": { "label": "Companies" }, "de": { "label": "Firmen" } },
  "handlers": [{ "class": "/TRFIN/COMPANY_API" }, { "class": "/TRFIN/COMPANY_INTAKE" }],
  "settings": { "maxBodySize": 65536 }
}
```

- The path is relative to the root node of the package; `""` is the root node itself.
  Segments consist of `A-Z a-z 0-9 . _ ~ -`, start with a letter or digit and are
  case-sensitive. Missing nodes in between exist without handlers. A request goes to the
  deepest node whose path is a prefix of the request path, segment by segment; the rest is
  the rest path of the request.
- Handlers are classes of the own or a required package that implement
  `/TRBASE/HTTP_HANDLER` and have a public constructor without parameters. The kernel
  creates a new object for every request and calls `HANDLE` of one handler after the other
  until one returns `true`. `parameters` gives a handler entry name-value pairs that the
  handler reads with `HANDLER_PARAMETER`. Installation and upgrades check the references
  and record them as uses of the classes.
- Settings that a node does not set come from its parent; the root nodes have the defaults
  of the kernel:

  | Setting           | Default                     | Meaning                                         |
  | ----------------- | --------------------------- | ----------------------------------------------- |
  | `authentication`  | `["basic", "token"]`        | accepted ways of signing in                     |
  | `anonymous`       | `false`                     | requests without credentials are allowed        |
  | `anonymousTenant` | `null`                      | tenant of anonymous requests; `null` runs without tenant |
  | `tenantSources`   | `["header", "query", "user"]` | `X-Tenant`, `?tenant=`, default tenant of the user |
  | `languageSources` | `["query", "header", "user"]` | `?lang=`, `Accept-Language`, language of the user |
  | `maxBodySize`     | 1 MiB                       | larger request bodies are answered with 413     |
  | `maxResponseSize` | 16 MiB                      | larger responses fail with 500                  |
  | `timeoutSeconds`  | 30                          | longer requests are answered with 503           |
  | `budget`          | 10 000 000                  | instructions per request                        |
  | `authorization`   | none                        | an additional check, see below; `null` removes the inherited one |

Nodes are not reachable after installation. `rt http activate <path>` activates a node and
the nodes below it that were never activated; `rt http deactivate <path>` deactivates one.
A node is reachable when it and all nodes above it are active; other nodes answer 404 as if
they did not exist. The activation is runtime configuration: upgrades keep it, new nodes
start inactive, and uninstalling a package removes it. Activations, installations and
upgrades take effect with the next request, without a restart.

### Users and signing in

Users have a password (stored as Argon2id hash), a status, a default language, a default
tenant and the tenants they may work in. `basic` signs in with user name and password,
`token` with `Authorization: Bearer <token>` and an API token of the user, which is shown
only when it is created and stored as SHA-256 hash. After 5 failed logins in a row
(`kernel.Config.MaxFailedLogins`) a user is locked until `rt user unlock`. Every failure
answers 401 with `WWW-Authenticate` and does not tell whether the user exists.

`session` signs in browsers with the cookie of a session of the kernel node `/rt/session`,
which works without the dialog of the browser:

- `POST /rt/session` with `{"user": "...", "password": "..."}` as `application/json`, and
  optionally `"tenant"` and `"language"`, signs in. Failed attempts count as for `basic`. The
  answer sets the cookie `rt_session` with `HttpOnly`, `Secure`, `SameSite=Strict` and
  `Path=/`; the database keeps only a hash of the session ID.
- `GET /rt/session` describes the session: `user`, `name` (`null` until users have names),
  `tenants` of the user, the current `tenant` and `language`, the `languages` set up,
  `csrfToken` and `expiresAt`. Without a valid session it answers 401 `session-expired`.
- `PATCH /rt/session` with `{"tenant": ..., "language": ...}` changes tenant and language of
  the session, a tenant only among those of the user; `DELETE /rt/session` signs out.
- A request of a session with a method other than GET, HEAD and OPTIONS needs the header
  `X-CSRF-Token` with `csrfToken`, otherwise it is answered with 403 `csrf-token-invalid`.
  `basic` and `token` need none.
- A session ends after 30 minutes without use and 12 hours after the sign-in (`rt serve
  --session-idle`, `--session-lifetime`, `kernel.Config.Sessions`). Locking a user, a new
  password and taking a tenant from the user end the sessions of the user; `rt session list
  [user]` lists them and `rt session revoke <user>` ends them. `rt serve
  --insecure-session-cookie` leaves out `Secure` for development without TLS.

`session` is part of the defaults of all nodes, including `/rt/odata`.

The tenant of a request comes from the first source of the node that gives one; a tenant the
user is not assigned to is answered with 403, a request without tenant with 400. Anonymous
requests run in `anonymousTenant` only. The language is the first one of the sources that is
set up, else English. For a browser session the source `user` gives the tenant and language
of the session before the defaults of the user.

### Authorizations of requests

Before the first handler runs, a request with sign-in to a node without anonymous access
needs the authorization object `/TRBASE/HTTP_SERVICE` of TRBASE in the tenant of the
request, with the package of the node, its full path and the activity `EXECUTE`:

```json
{ "object": "/TRBASE/HTTP_SERVICE",
  "fields": { "PACKAGE_ID": [{ "value": "TRFIN" }], "SERVICE_PATH": [{ "pattern": "/trfin*" }],
              "ACTIVITY": [{ "value": "EXECUTE" }] } }
```

A node may add a check of its own with the setting `authorization`, which its subtree
inherits; `"authorization": null` removes the inherited check for a node and its subtree.
Every field of the object needs a value of its type, or `{"any": true}`; the object belongs
to the package, a required one or the kernel, and the activity must be one of the object:

```json
"settings": {
  "authorization": { "object": "/TRFIN/COMPANY",
                     "fields": { "COMPANY_ID": { "any": true }, "COUNTRY": { "any": true }, "ACTIVITY": "READ" } }
}
```

A failed check answers 403 `forbidden` without saying which authorization is missing.
Nodes with anonymous access check nothing. The code of the handlers checks further with
`check authorization` and `authorized`, and an unhandled `/RT/NOT_AUTHORIZED` answers 403
as well and rolls the request back.

### Requests

A request runs as one execution with the tenant, language and user of the request and the
time limit and budget of the node. Its changes are committed when a handler returned `true`
and the status is below 400; a status of 400 or more, an exception that no handler catches,
and requests that no handler takes are rolled back. Errors are answered with JSON in the
language of the request:

```json
{ "code": "not-found", "message": "There is no service at this path." }
```

The codes are `not-found`, `method-not-handled`, `unauthorized`, `forbidden`,
`bad-request`, `body-too-large`, `internal-error` (details only in the log) and
`unavailable` (time limit, with `Retry-After`). Every request writes an access log line
with user, tenant, node, handler, method, status, duration and size.

TRBASE ships two handlers and two nodes:

- `/TRBASE/PING_HANDLER` on `/trbase/ping` (anonymous) answers `{"status":"ok"}`.
- `/TRBASE/ASSET_HANDLER` serves the files of the asset repository that its parameters
  `package` and `repository` name, at the rest path of the request, with `Content-Type`,
  `Content-Length`, `ETag`, `Last-Modified` and `Cache-Control: no-cache`. It answers
  `HEAD`, and 304 for `If-None-Match` and `If-Modified-Since`, and gives assets in the
  language of the request. The package of the node may serve its own repositories and those
  of the packages it requires. `/trbase/web` serves the repository `web` of TRBASE.

### Serving

```bash
rt user create anna --tenant ACME < password.txt
rt token create anna --label cli
rt http activate /trbase
rt http activate /trfin
rt serve --addr :8080
curl localhost:8080/trbase/ping
curl -u anna localhost:8080/trfin/companies
curl -H "Authorization: Bearer rt_..." -H "X-Tenant: ACME" localhost:8080/trfin/companies
curl -u anna -o companies.zip localhost:8080/trfin/companies/export
curl -u anna -o companies.xlsx "localhost:8080/trfin/companies/export?file=companies.xlsx"
curl -u anna --data-binary @companies.zip localhost:8080/trfin/companies/import
```

`rt serve` serves until SIGINT or SIGTERM. Then it accepts no new connections and gives
running requests `--shutdown-timeout` (default 30s) to finish; they end with the code they
started with, also when an upgrade committed new code in the meantime. `--tls-cert` and
`--tls-key` serve HTTPS; `--read-timeout` (30s) and `--write-timeout` (60s) limit reading a
request and writing a response. In Go, `k.HTTPHandler()` is an `http.Handler` and
[server](server) runs it with the same options.

## Services over OData

A package offers entities of business objects and tables of configuration over OData with a
service in `schema/services/`. The kernel serves every service below
`/rt/odata/<package>/<service>/` with the node `/rt/odata` of its own and derives metadata,
queries with `$filter`, `$orderby`, `$top`, `$skip`, `$count`, `$select` and `$expand`,
changes through the transaction buffer and actions from the business objects, with the
authorizations of the business objects in SQL. Services are inactive after installation;
`rt service activate` switches them on. Packages annotate the metadata through the extension
point `/TRBASE/ODATA_METADATA`. [docs/odata.md](docs/odata.md) describes services, the
supported parts of OData and the use in a spreadsheet; [docs/odata-vocabulary.md](docs/odata-vocabulary.md)
the vocabulary `RT.Fields` of the kernel.

## User interface

The package `TRFRONTEND` in [packages/TRFRONTEND](packages/TRFRONTEND) brings the user
interface to the browser, built with the components of
[TRUI](https://github.com/tim-riep/trui):

- a **shell** at `/trfrontend/` with sign-in over browser sessions (`/rt/session`), a header
  with search over the apps, a switch between spaces and a user menu for language, tenant,
  theme, density and signing out;
- a **start page** of the spaces of the roles of the user: pages and their sections as groups
  of tiles, metric tiles that count entities of a service with the authorizations of the user;
- **generic list and detail pages** for apps of the kind `LIST`, driven by the metadata of
  the service and the UI annotations of packages;
- **apps** of the kinds `PAGE` (a page of a service node in the content area), `REMOTE` (a
  module loaded with Module Federation that shares React and TRUI with the shell) and `URL`;
- **administration**: apps for local apps, tiles, app collections, pages, spaces and role
  contents, and a role page for local roles and role assignments, for the role
  `/TRFRONTEND/ADMINISTRATOR`.

Packages contribute through the object types of TRFRONTEND: `/TRFRONTEND/APP`, `TILE`,
`APP_COLLECTION`, `PAGE`, `SPACE`, `ROLE_CONTENT` (which collections and spaces a role gets)
and `UI_ANNOTATIONS` (columns, filters, sections and field groups of an entity, which other
packages extend). [docs/frontend-vocabulary.md](docs/frontend-vocabulary.md) describes the
annotations in the metadata. Administrators add local entries under `/LOCAL/` in the
administration apps or copy shipped ones.

From an empty database to the shell with the example companies, in the directory of the
repository (Node 22 builds the user interface):

```bash
export RT_DATABASE=sqlite:rt.db
rt init --admin admin --tenant ACME
rt language add de
make trfrontend
rt install packages/TRFRONTEND/dist/TRFRONTEND examples/TRFIN examples/TROTTO examples/TRFINUI examples/TROTTOUI --yes
rt http activate /trui
rt http activate /trfrontend
rt service activate /TRFIN/COMPANY_SERVICE
rt service activate /TRFRONTEND/CONFIGURATION_SERVICE
rt user create clara --tenant ACME
rt role assign clara /TRFRONTEND/USER --tenant ACME
rt role assign clara /TRFIN/COMPANY_CLERK --tenant ACME
rt role assign clara /TRFIN/COMPANY_ADMIN --tenant ACME
rt role assign clara /TRFIN/ODATA_USER --tenant ACME
rt role assign admin /TRFRONTEND/ADMINISTRATOR --tenant ACME
rt run TRFIN /TRFIN/COMPANY_SERVICE ADD_COUNTRY '["DE", "Germany"]' --tenant ACME --user admin
rt serve --insecure-session-cookie
```

`rt init` and `rt user create` read the passwords from standard input; the examples have
German texts, hence `rt language add de`. `/TRFIN/COMPANY_CLERK` gives clara the start page
of companies, `/TRFIN/COMPANY_ADMIN` lets her create them. `rt install` of TRFRONTEND brings
TRUI from GitHub along, since the manifest names its
source. Instead of `make trfrontend`, a release of TRFRONTEND installs directly:
`rt install github.com/tim-riep/go-runtime/packages/TRFRONTEND@1.0.1`. Then open
`http://localhost:8080/trfrontend/` and sign in as `clara` or `admin`.
`--insecure-session-cookie` lets the session cookie travel without TLS and is meant for
development only.

## Library

```go
k, err := kernel.Open(ctx, "sqlite:app.db") // or "postgres://user@host/db"
if err != nil { ... }
defer k.Close()

if _, err := k.Init(ctx, kernel.Options{}); err != nil { ... }

fin, _ := kernel.LoadDir("examples/TRFIN") // TRBASE is a kernel package, installed by Init
p, err := k.Install(ctx, []*model.Package{fin}, kernel.Options{})
fmt.Print(p.Text())

_, err = k.Upgrade(ctx, newVersion, kernel.Options{DryRun: true})
var dc *rterr.DestructiveChange
if errors.As(err, &dc) { ... }

tenant, err := k.Tenants().Create(ctx, "ACME", model.Some("Acme Corp"))
discrepancies, err := k.Check(ctx)
```

Packages from sources go through `kernel.Config.Sources` (or `SetSources`) with
`source/github` or another implementation of `source.Source`: `ResolveSources` resolves a
`SourceRequest` with its dependencies, `ApplyResolution` installs it (with `DryRun` for the
plan), `UpgradeSource`, `Outdated`, `PackageOrigin`, `ExportLock`, `ParseLock` and
`ResolveLock` cover upgrades, listings and locks. `kernel.LoadZip` reads a ZIP file.

All errors are typed (package [rterr](rterr)) and can be inspected with `errors.As`.
Several problems found at once are returned as `rterr.List`. `kernel.LoadFS` reads a package
from any `fs.FS`. `Dialect().Encode` and `Decode` convert Go values to and from the stored
representation.

## Command line

```bash
go install github.com/tim-riep/go-runtime/cmd/rt@latest
```

Each release `v<version>` of the repository also carries `rt` built for Linux, macOS and
Windows; a tag `v<version>` releases it with `.github/workflows/rt-release.yml`.

```bash
export RT_DATABASE=sqlite:app.db
rt init
rt language add de
rt install examples/TRFIN examples/TROTTO
rt plan examples/upgrade/TROTTO
rt upgrade examples/upgrade/TROTTO
rt list
rt show /TRFIN/COMPANIES
rt check
rt install testdata/businessobjects/TRSET testdata/businessobjects/TRSETX
rt business-object list --data-class configuration
rt business-object show /TRSET/PARTNER
rt service list
rt service show /TRFIN/COMPANY_SERVICE
rt service activate /TRFIN/COMPANY_SERVICE
rt service metadata /TRFIN/COMPANY_SERVICE --lang de --format json
rt http configure /rt/odata --max-page-size 100
rt install testdata/objects/TRDEF testdata/objects/TRUSE
rt object-type list
rt object-type schema /TRDEF/VIEW > view.schema.json
rt object list /TRDEF/VIEW
rt object show /TRUSE/COMPANIES
rt show data-element:/TRBASE/COUNTRY
rt show /TRBASE/ADDRESS
rt show /TRFIN/COMPANY_ARRAY --json
rt show /TRFIN/COMPANIES_ARCHIVE
rt asset repos
rt asset list TRFIN templates invoice
rt asset show TRFIN templates invoice/invoice.xsl
rt asset get TRFIN templates invoice/invoice.xsl --lang de -o invoice.xsl
rt tenant create ACME --description "Acme Corp"
rt tenant delete ACME --yes
rt uninstall TROTTO --yes
rt log
```

```bash
rt fmt examples/TRORDER
rt fmt --check .
rt build examples/TRORDER
rt disasm examples/TRORDER /TRORDER/ORDER_RATING
rt install examples/TRORDER
rt install TRFIN-1.0.0.zip
rt tenant create ACME
rt run TROTTO /TROTTO/COMPANY_SYNC SYNC '["DE"]' --tenant ACME --user anna
rt run TRFIN /TRFIN/INVOICE_RENDERER RENDER '[{"INVOICE_ID":"R-1","COMPANY_ID":"C1","ISSUED_ON":"2026-11-07","NOTE":null,"REFERENCE":null,"TOTAL":"10.00","ITEMS":[]}]' --tenant ACME
rt test TRORDER
rt test examples/TRORDER
rt natives list
rt user create anna --tenant ACME
rt user tenant add anna BETA
rt user list
rt token create anna --label cli --expires 2027-01-31
rt token list anna
rt show authorization-object:/TRFIN/COMPANY
rt authorization-objects list TRFIN
rt activities list --lang de
rt user last-failed-check anna --tenant ACME
rt trace authorizations start anna --tenant ACME
rt init --admin root --tenant ACME < password.txt
rt role list --incomplete
rt role show /TRFIN/COMPANY_CLERK
rt role import operations.json
rt role export /LOCAL/OPERATIONS -o operations.json
rt role assign anna /LOCAL/OPERATIONS --tenant ACME --from 2027-01-01
rt user roles anna
rt user authorizations anna --tenant ACME --date 2027-01-15
rt role history --user anna
rt extension-point list TRFIN
rt extension-point show /TRFIN/COMPANY_CHECKS
rt business-object list TRFIN
rt business-object show /TRFIN/COMPANY
rt implementation deactivate /TROTTO/COMPANY_CHECKS --tenant ACME
rt implementation history /TROTTO/COMPANY_CHECKS
rt trace extension-points start anna --tenant ACME
rt http list TRFIN
rt http show /trfin/companies
rt http activate /trfin
rt serve --addr :8443 --tls-cert cert.pem --tls-key key.pem
rt effects list
rt gen natives testdata/lang/native/TRTEST --go-package nativetest -o types_gen.go
```

`fmt`, `build` and `disasm` work on package directories and need no database; `build`
and `disasm` look up the packages a package requires next to it, in a directory named
after their ID. `disasm` lists the bytecode of all functions, of a class or of one
method (`/TRORDER/ORDER_RATING.RATE`), with the source line before its instructions and
comments that name the called methods, classes, constants, types, transformations and
database statements. `run`
calls a public static method of an installed package for a tenant. `test` runs the test
classes of an installed package, or of a directory compiled against the installed
packages; every test method runs in a tenant of its own and leaves no rows.

Flags may appear anywhere: `--db`, `--dry-run`, `--allow-destructive`, `--yes`, `--json`,
`--description`, `--name`, `--lang`, `--tenant`, `-o`, `--max-asset-size`, `--max-asset-read`, `--label`,
`--expires`, `--package`, `--incomplete`, `--user`, `--from`, `--to`, `--date`, `--admin`, `--addr`, `--tls-cert`, `--tls-key`, `--read-timeout`, `--write-timeout`,
`--shutdown-timeout`. Exit codes: 0 success, 2 usage, 3 validation or dependency error,
4 destructive change or confirmation required, 5 check found discrepancies, 6 execution
failed and was rolled back, 1 other errors.

## Development

```bash
go test ./...
```

```bash
RT_TEST_POSTGRES_DSN=postgres://localhost/go_runtime_test go test ./...
```

SQLite tests always run. PostgreSQL tests run when `RT_TEST_POSTGRES_DSN` is set; each test
works in its own schema that is dropped afterwards. The generated DDL per dialect is compared
with golden files in `testdata/golden`; after an intended change, update them with:

```bash
go test ./internal/planner ./kernel ./lang/syntax -update
```

The user interface of TRFRONTEND has tests of its own in `packages/TRFRONTEND/web`, which need
Node 22: `npm test` runs the unit tests and the components in Chromium, Firefox and WebKit,
`TRUI_PACKAGE=<TRUI directory or ZIP> npm run test:e2e` builds `rt`, sets up a database as in
[User interface](#user-interface) and drives the shell in the three browsers with checks of
axe. A tag `trfrontend-v<version>` releases the package with
`.github/workflows/trfrontend-release.yml`.

## Not included yet

Stylesheets that include, import or read other stylesheets and documents, native methods
with network or file access, outgoing HTTP connections, responses streamed from code,
further ways of signing in, messages with texts per language, a
language server and a debugger, views, indexes beyond keys,
data migrations with custom logic, joins and aggregates other than `count` in code,
display properties of fields, translation packages
for objects of other packages, export, delivery or protection of configuration by data class, field and operation control per
instance, extensions of drafts and of services, assets uploaded at runtime or per tenant, extending asset repositories of
other packages, references to assets from other objects, compression of stored assets and image processing,
includes in extensions, derived roles with organizational levels, suggested values per application, groups of users and
external role sources, extension points of kernel packages for events of the kernel such as
sign-in or installation, implementations in local objects without package, implementations
called asynchronously after the commit, drafts of business
objects, `$batch`, `$search`, `$apply`, lambda operators, delta queries, media and OData
Version 2 in services, business objects in
`/TRBASE/REFLECTOR` and data migrations of business object tables that bypass their
behavior.

## License

Apache License 2.0, see [LICENSE](LICENSE).
