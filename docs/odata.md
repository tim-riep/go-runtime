# Services over OData

A package makes business objects usable from outside with a **service**: a file in
`schema/services/` selects entities of business objects and tables of configuration and
gives them public names. The kernel serves every service over OData Version 4.0 below
`/rt/odata/<package>/<service>/` and derives metadata, queries, changes and actions from
the business objects. A package writes no code for it.

## Defining a service

`TRFIN/schema/services/COMPANY_SERVICE.json`:

```json
{
  "name": "/TRFIN/COMPANY_SERVICE",
  "description": { "en": "Companies", "de": "Firmen" },
  "entitySets": {
    "Companies": {
      "businessObject": "/TRFIN/COMPANY",
      "entity": "/TRFIN/COMPANIES",
      "navigations": { "COUNTRY": "Country" }
    },
    "Contacts": { "businessObject": "/TRFIN/COMPANY", "entity": "/TRFIN/CONTACTS" },
    "Countries": { "businessObject": "/TRFIN/COUNTRY", "entity": "/TRFIN/COUNTRIES", "readOnly": true },
    "ExchangeRates": { "table": "/TRFIN/EXCHANGE_RATES" }
  }
}
```

- `name` carries the namespace of the package; the file is named after the part after it.
  `description` has a text per language, English is mandatory.
- Every entity set has a public name, an identifier of OData, and offers either an entity
  of a business object (`businessObject` and `entity`) or a table of configuration without
  business object (`table`), which is read only. Business objects and tables come from the
  own package or a package it requires. An entity is offered by at most one entity set.
- Per entity set: `readOnly`, `operations` (a subset of `create`, `update`, `delete` of the
  entity), `actions` (a subset of the actions of the entity that the package can name),
  `hiddenFields` and `navigations` (the public names of navigations along foreign keys).
  A service never offers more than the business object. Key fields, mandatory fields, the
  fields of the foreign key to the parent entity and the ETag field cannot be hidden.
- `extensions` tells which extensions of business objects and tables by other packages the
  service offers: `"all"` (the default), `"none"` or a list of package IDs. A service with a
  contract that must not change with the installation of other packages offers none.
- Tables of the data class `application` are offered only through business objects; tables
  of `configuration` and `runtimeConfiguration` without business object need
  `/TRBASE/CONFIGURATION` with the activity `READ`, the table as `BUSINESS_OBJECT` and its
  data class.

Installation checks a service against its business objects and tables as they are: an
upgrade of a business object that breaks an installed service fails with the name of the
service. Services hold no data; removing one is not destructive.

### Public names

Names without namespace stay as they are (`COMPANY_ID`, `BLOCK`). Names with namespace
become `<PACKAGE>_<NAME>`: `/TROTTO/OTTO_ID` is `TROTTO_OTTO_ID`, the action
`/TROTTO/SYNC` is `TROTTO_SYNC`. Package IDs have no underscore, so the first underscore
separates package and name again. Two names that map to the same public name in an entity
type, in the actions of a type or in the entity container fail the installation.

The schema of the metadata has the namespace of the package (`TRFIN`), entity types the
mapped name of their table (`TRFIN_COMPANIES`) and complex types that of their structure.

### Navigations

- Between the entities of a business object, when both have an entity set: the parent has a
  collection named after the entity set of the dependent entity (`Contacts`), the dependent
  entity a navigation back named after the entity set of the parent (`Companies`).
- Along a foreign key to another entity set of the service: a navigation to one entity,
  named after the target entity set unless `navigations` names it.
- Dependent entities of extensions of other packages are contained navigations of their
  parent (`Companies('C1')/TROTTO_COMPANY_CHANNELS`): the package of the service cannot name
  them, since it does not require the extending package.

## Serving and activation

The kernel owns the service node `/rt/odata` with the handler `/RT/ODATA_HANDLER`, which it
runs in Go. No package defines it: it cannot be uninstalled, it is active after `rt init`,
and it never allows anonymous access. `rt http configure /rt/odata` changes its settings for
all services; the page size and the time limit of extensions of the metadata are settings of
it as well:

```bash
rt http configure /rt/odata --max-page-size 100 --timeout 20 --metadata-timeout 3
rt http configure /rt/odata --reset
rt http show /rt/odata
```

A service is inactive after installation and answers 404. `rt service activate <name>` and
`rt service deactivate <name>` switch it; the activation is runtime configuration, kept by
upgrades and removed with the package. Package and service in the path ignore case; the
kernel writes URLs in lower case.

Every request needs `/TRBASE/HTTP_SERVICE` with `EXECUTE` for the package `RT` and the path
`/rt/odata`, so that operators release or lock OData as a whole, and
`/TRBASE/ODATA_SERVICE` with `EXECUTE` for the package and the name of the service. Without
it the request answers 403. The authorizations of the business object apply after that.

`GET /rt/odata/` lists the active services the user may start:

```json
{ "value": [ { "package": "TRFIN", "name": "/TRFIN/COMPANY_SERVICE",
               "description": "Firmen", "url": "/rt/odata/trfin/company_service/" } ] }
```

An incompatible version of a service is a new service with a name of its own, such as
`/TRFIN/COMPANY_SERVICE_V2`; the path has no version.

## Metadata

`$metadata` answers in the XML format of CSDL 4.0, with `$format=json` or
`Accept: application/json` in the JSON format of CSDL 4.01. The texts follow the language of
the request; the answer has `Content-Language`, `Vary: Accept, Accept-Language` and an ETag,
and `If-None-Match` answers 304. The documents are cached per generation, service, language
and activation of the extensions of the metadata.

| Type       | OData                                   |
| ---------- | --------------------------------------- |
| CHAR(n)    | `Edm.String` with `MaxLength`           |
| STRING     | `Edm.String`                            |
| INT4       | `Edm.Int32`                             |
| INT8       | `Edm.Int64`                             |
| DEC(p,s)   | `Edm.Decimal` with `Precision` and `Scale` |
| DATE       | `Edm.Date`                              |
| TIMESTAMP  | `Edm.DateTimeOffset`, always in UTC     |
| BOOL       | `Edm.Boolean`                           |
| UUID       | `Edm.Guid`                              |
| BLOB       | `Edm.Binary`, base64url; not filterable or sortable |

`Nullable` follows `notNull`; keys are never nullable. The kernel annotates with the
vocabularies Core, Validation and Capabilities of OASIS and with its own vocabulary
[RT.Fields](odata-vocabulary.md):

- `Core.Description` and `Core.LongDescription` from data elements and texts, the four
  labels of data elements in `RT.ShortLabel`, `RT.MediumLabel`, `RT.LongLabel` and
  `RT.Heading`, and the data class in `RT.DataClass`;
- `Core.Computed` for read-only, administrative and ETag fields and keys from UUIDs,
  `Core.Immutable` for fields read-only on update, external keys and the foreign key to the
  parent, `RT.Mandatory` for mandatory fields, `Core.OptimisticConcurrency` with the ETag
  field;
- `Validation.AllowedValues` from fixed values with their descriptions;
- the restrictions of insert, update, delete, filter, sort, count, expand and search, and
  `RT.ValueList` along foreign keys to entity sets of the service.

`rt service metadata <name> [--lang de] [--format xml|json] [--without-extensions]` prints
the documents as clients get them.

### Extensions of the metadata

Packages annotate the metadata of services without changing them through the extension
point `/TRBASE/ODATA_METADATA` of TRBASE: use `multiple`, filter fields `PACKAGE_ID` and
`SERVICE_NAME` with the default `*`, interface `/TRBASE/ODATA_METADATA_EXTENSION` with

```
method ADJUST(metadata: /TRBASE/ODATA_METADATA) returns /TRBASE/ODATA_METADATA
```

The implementations run whenever the kernel builds metadata, chained in their order: the
result of one is the argument of the next. `/TRBASE/ODATA_METADATA` holds the entity sets,
entity types, complex types, actions and action imports with technical and public names,
the annotations of the kernel and the references to vocabularies. An annotation has a
target such as `TRFIN.TRFIN_COMPANIES/NAME`, a term such as `Core.Description`, an optional
qualifier and a value as a flat tree of nodes `/TRBASE/ODATA_VALUE`: `ID`, `PARENT` (none
for the root), `MEMBER` for members of records, `KIND` (`STRING`, `BOOLEAN`, `INTEGER`,
`DECIMAL`, `DATE`, `DATE_TIME_OFFSET`, `GUID`, `ENUM_MEMBER`, `PATH`, `PROPERTY_PATH`,
`NAVIGATION_PATH`, `ANNOTATION_PATH`, `NULL`, `COLLECTION`, `RECORD`), `TEXT` in canonical
form and `TYPE` of records. `/TRBASE/ODATA_ANNOTATIONS` builds annotations with simple
values:

```
public class /TROTTO/ODATA_METADATA implements /TRBASE/ODATA_METADATA_EXTENSION

  public method ADJUST(metadata: /TRBASE/ODATA_METADATA) returns /TRBASE/ODATA_METADATA
    var vocabulary: /TRBASE/ODATA_REFERENCE
    vocabulary.URI = "urn:trotto:vocabularies:TROTTO.Marketplace"
    vocabulary.NAMESPACE = "TROTTO.Marketplace"
    vocabulary.ALIAS = "Marketplace"
    metadata.REFERENCES.APPEND(vocabulary)
    metadata.ANNOTATIONS.APPEND(/TRBASE/ODATA_ANNOTATIONS.TAG(metadata.NAMESPACE + ".TRFIN_COMPANIES/TROTTO_OTTO_ID", "Marketplace.PartnerId"))
    return metadata
  end

end
```

Rules, checked after every implementation:

- Implementations add, change and remove annotations, add references and change
  descriptions. The structure stays as it is: entity sets, types, properties, keys,
  navigations, actions, their types and public names.
- The annotations the kernel derives from the business object and enforces — `Core.Computed`,
  `Core.Immutable`, `Core.OptimisticConcurrency`, all `Capabilities`, `Validation.AllowedValues`
  and `RT.Mandatory` — are the kernel's: implementations neither remove nor change nor add
  them. A stricter annotation would promise clients a behavior the service does not have.
- Every annotation has a target of the metadata and a term of a referenced vocabulary.
- The implementations run without tenant and user in the language of the request, in an
  execution that only reads: changing rows or business objects, `commit` and `rollback`
  raise `/RT/SESSION_ERROR`. They have the time limit `--metadata-timeout`.

A failure answers `$metadata` with 500 and logs the implementation, its package and the
reason; `rt service metadata` shows it. There is no fallback to the metadata without the
implementation, so that clients never see changing metadata. The extension point is
activated only globally (`"activation": "global"` in its definition), since metadata are the
same for all tenants: `rt implementation activate|deactivate` without `--tenant`. Installing,
removing, activating and deactivating an implementation builds the documents again with a
new ETag. `rt extension-point show /TRBASE/ODATA_METADATA` lists the implementations with
their filters.

## Reading

`GET` on an entity set, an entity by key (`Companies('C1')`, `Accounts(COMPANY_ID='C1',ACCOUNT_NO='1')`,
`Contacts(1b4e28ba-2fa1-11d2-883f-0016d3cca427)`), a navigation and `$count`, with

| Option | Support |
| ------ | ------- |
| `$filter` | `eq`, `ne`, `gt`, `ge`, `lt`, `le`, `and`, `or`, `not`, parentheses, `in`, `null`, `contains`, `startswith`, `endswith`, `tolower`, `toupper` |
| `$orderby` | properties with `asc` or `desc`; null last ascending, first descending |
| `$top`, `$skip` | numbers from 0 |
| `$count` | `true` adds `@odata.count`; `/$count` answers the number as text |
| `$select` | properties or `*` |
| `$expand` | navigations one level deep, with `$select` inside |
| `$skiptoken` | the offset of the next page in `@odata.nextLink` |

Every statement holds the tenant of the request and the authorization of the business object
for reading, in SQL: the object of the entity, of its nearest parent with an authorization
through `EXISTS`, or `/TRBASE/CONFIGURATION` for configuration. Counts and pages therefore
hold only rows the user may read, and an entity the user may not read answers 404. The
kernel reads the database: the transaction buffer is empty in every request.

`$filter` compares a property with a literal of its type or another property; literals are
bound as parameters. Comparisons follow the two-valued logic of OData: `NOTE eq null` and
`NOTE ne null` test for null, `NOTE eq ''` for the empty string, and `NOTE ne 'x'` is true for
null. `tolower` and `toupper` change only the letters A to Z on both databases. Arithmetic,
other functions, paths along navigations, `any` and `all` answer 501 with the part that is
not supported; syntax errors answer 400 with the position.

Pages have at most the size of the node, 200 by default, or less with
`Prefer: odata.maxpagesize`. `$top` larger than a page gets `@odata.nextLink` like a query
without `$top`. Expanded collections have at most one page per entity; more answer 400.

Responses carry `@odata.context` and `@odata.etag`. `Edm.Int64` and `Edm.Decimal` are JSON
numbers with their exact text, strings with `IEEE754Compatible=true` in `$format` or
`Accept`. `odata.metadata=none` leaves out the annotations of control; `full` answers 406.
`If-None-Match` with the ETag of an entity answers 304.

## Changing

Every changing request is an execution with entity statements on the transaction buffer and
the save sequence at its end. If anything fails, nothing is written.

- `POST` on an entity set or along a navigation to dependent entities creates an instance.
  Dependent instances in the body (`"Contacts": [ ... ]`, also along contained navigations)
  are created in the same statement, also with keys from UUIDs. The answer is 201 with the
  entity, `Location` and `ETag`, or 204 for `Prefer: return=minimal`.
- `PATCH` changes exactly the properties of the body: `null` sets null, `""` the empty
  string, a missing property stays. `PUT` sets every field the client may change and null
  for those it leaves out. Both answer 204, or 200 with the entity for
  `Prefer: return=representation`.
- `DELETE` removes the instance with its dependent instances.
- Entities with an ETag need `If-Match` for `PATCH`, `PUT` and `DELETE`: 428 without, 412
  when it differs; `*` accepts every version. Changes between reading and saving fail with
  412 as well.

The body is read strictly: unknown properties, read-only, administrative and ETag fields,
keys and fields read-only on update in `PATCH` and `PUT`, and null for fields that are not
null answer 400 with the property as `target`. `Edm.Int64` and `Edm.Decimal` come as number
or string and never pass through floating point.

## Actions

Instance actions of a business object and of its extensions are bound actions:
`POST Companies('C1')/TRFIN.BLOCK` with the components of the parameter structure as members
of the body, such as `{"TEXT": "audit"}`. Static actions are action imports at the root of
the service: `POST STATS`. An action with result `self` answers with the entity, one with a
structure with a complex value, one without result with 204. Actions of instances check
`If-Match` when it is sent.

## Errors and messages

Errors have the format of OData:

```json
{ "error": { "code": "VALIDATION", "message": "the name BAD is not allowed", "target": "NAME",
             "details": [ { "code": "VALIDATION", "message": "the name BAD is not allowed", "target": "NAME" } ] } }
```

| Status | Code | When |
| ------ | ---- | ---- |
| 400 | `INVALID_REQUEST`, `UNKNOWN_PROPERTY`, `READ_ONLY_PROPERTY`, `INVALID_VALUE`, `VALIDATION`, `PARENT_NOT_FOUND`, `BEHAVIOR` | invalid requests and failed checks |
| 403 | `NOT_AUTHORIZED` | missing authorization for a change or an action, or to start the service |
| 404 | `NOT_FOUND` | unknown entities and entities the user may not read |
| 405 | `METHOD_NOT_ALLOWED` | operations the entity set does not offer |
| 406 | `NOT_ACCEPTABLE` | formats other than JSON, `odata.metadata=full` |
| 409 | `ALREADY_EXISTS` | a key that exists |
| 412, 428 | `ETAG_MISMATCH`, `ETAG_REQUIRED` | ETags |
| 413 | `BODY_TOO_LARGE` | bodies larger than the node allows |
| 500 | `INTERNAL_ERROR` | everything else; details are only in the log |
| 501 | `NOT_IMPLEMENTED` | parts of OData the services do not support |

The messages of the behavior become `details` with the property as `target`, below the path
of the entity in a deep insert (`Contacts(2)/NAME`), and with `@RT.Origin` for messages of
extensions of other packages. Warnings and informations of successful changes come back in
`@Core.Messages` of the entity or value. Texts of the kernel follow the language of the
request; SQL and stack traces never appear in answers.

## Conformance

The services aim at the minimal conformance level of OData 4.0 for reading, and support the
parts listed above for changing. `OData-MaxVersion` below 4.0 answers 400; answers carry
`OData-Version: 4.0`, and 4.01 for metadata in JSON.

Not supported: `$batch`, drafts, `$search`, `$apply` and aggregation, lambda operators,
delta queries, functions, `$levels` and options other than `$select` in `$expand`, paths to
single properties and `$value`, `$ref` and `@odata.bind`, media entities and streams, OData
Version 2, XML and Atom payloads, sessions with cookies and CSRF tokens.

The tests check the metadata documents in JSON against the JSON Schema of CSDL 4.01 of
OASIS (`testdata/odata/oasis`).

## Using a service in a spreadsheet

Spreadsheet applications import OData feeds:

1. Activate the service and give the user the roles to start it, for TRFIN
   `/TRFIN/ODATA_USER` and a role for the companies such as `/TRFIN/COMPANY_CLERK`. The user
   needs a default tenant, since a spreadsheet sends no `X-Tenant` header.
2. In the spreadsheet, choose the import of data from an OData feed and enter the URL of the
   service, such as `https://host/rt/odata/trfin/company_service/`.
3. Sign in with basic authentication, user and password, or with an API token as password.
4. Select the entity sets, such as `Companies`, and load them. Refreshing reads them again;
   the service pages large sets with `@odata.nextLink`, which the import follows.
