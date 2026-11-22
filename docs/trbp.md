# TRBP: business partners

TRBP keeps the business partners of a tenant: persons and organizations with the roles they
take, such as customer or supplier. It requires TRBASE 1.11.0 or later. Other packages, such
as a general ledger, extend the partner and ship roles of their own.

## Business objects

- **`/TRBP/PARTNER`** (application data): the root `/TRBP/PARTNERS` with the key `PARTNER`
  (`CHAR(10)`), the grouping, the category `PERSON` or `ORGANIZATION`, `NAME1` and `NAME2`
  for organizations, `FIRST_NAME` and `LAST_NAME` for persons, a search term, `BLOCKED`,
  administrative fields and an ETag. Grouping and category are fixed once the partner exists.
  The dependent entity `/TRBP/PARTNER_ROLES` (key `PARTNER`, `ROLE`) holds the roles with
  `VALID_FROM` and `VALID_TO`, `null` for no limit, and a copy of the grouping of the partner.
- **`/TRBP/GROUPING`** (configuration): `/TRBP/GROUPINGS` with the key `GROUPING`
  (`CHAR(4)`), the `INTERVAL` of the number range and whether numbers are `EXTERNAL`, and
  `/TRBP/GROUPING_TEXTS` per language. It is checked with `/TRBASE/CONFIGURATION`.

Validations: a person has a last name and no `NAME1`/`NAME2`, an organization a `NAME1` and
no first or last name; a role exists as an object of `/TRBP/PARTNER_ROLE`, allows the
category of the partner and does not end before it starts.

## Numbers

Partners have late numbering. A partner of an internal grouping is created without number
and gets the next number of the interval of its grouping from the number range object
`/TRBP/PARTNER` (10 digits, no subobject, not per fiscal year, buffered in blocks of 10)
when it is saved; the result of the save and the `Location` of an OData `POST` carry it. A
partner of an external grouping needs a number from the caller in the external interval of
the grouping; the validation rejects other numbers at the field `PARTNER`, as it rejects a
number given for an internal grouping. Set up the intervals per tenant:

```bash
rt number-range interval add /TRBP/PARTNER 01 --tenant ACME --from 1 --to 4999999999
rt number-range interval add /TRBP/PARTNER XX --tenant ACME --from 5000000000 --to 9999999999 --external
```

## Partner roles

A partner role is an object of the type `/TRBP/PARTNER_ROLE` with texts per language and the
categories of partners that may take it. TRBP ships `/TRBP/GENERAL`; a package that requires
TRBP ships its own in `objects/TRBP/PARTNER_ROLE/<NAME>.json`:

```json
{
  "name": "/TRDEMOSHOP/CUSTOMER",
  "content": {
    "TEXTS": [{ "LANGUAGE": "en", "LABEL": "Customer" }],
    "CATEGORIES": [{ "CATEGORY": "PERSON" }, { "CATEGORY": "ORGANIZATION" }]
  }
}
```

## Authorizations and roles

The authorization object `/TRBP/PARTNER` has the fields `GROUPING`, `ROLE` and `ACTIVITY`
(`READ`, `CREATE`, `UPDATE`, `DELETE`). The partner is checked with its grouping and any
role, a role of a partner with its role and the grouping of the partner. Reading over OData
and with `read business object` filters the same way.

| Role | Grants |
|---|---|
| `/TRBP/PARTNER_ADMIN` | all activities on partners of all groupings and roles, reading groupings |
| `/TRBP/DISPLAY` | reading partners, roles and groupings |
| `/TRBP/CONFIGURATION` | maintaining groupings and the intervals of `/TRBP/PARTNER` |
| `/TRBP/ODATA_USER` | starting the service `/TRBP/PARTNER_SERVICE` below `/rt/odata` |

## Service

`/TRBP/PARTNER_SERVICE` offers `Partners`, `PartnerRoles`, `Groupings`, `GroupingTexts` and
the partner roles as `Roles`, read only. Activate it with
`rt service activate /TRBP/PARTNER_SERVICE`.

## Tests

`go test ./kernel -run Partner` installs TRBP from `packages/TRBP` with the test package
`testdata/partners/TRBPTEST` against SQLite and, with `RT_TEST_POSTGRES_DSN`, PostgreSQL.
