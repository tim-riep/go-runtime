# Vocabulary RT.Fields

The kernel annotates the metadata of services with the vocabulary `RT.Fields` (alias `RT`)
besides Core, Validation and Capabilities of OASIS. Generic user interfaces read it to show
and check entities without code of their own. The kernel serves it at
`/rt/odata/$vocabularies/RT.Fields.xml` and `.json`; metadata documents refer to it with the
relative URI `../../$vocabularies/RT.Fields.xml`.

| Term | Type | Applies to | Meaning |
| ---- | ---- | ---------- | ------- |
| `ShortLabel` | `Edm.String` | properties | short label of the data element, at most 10 characters |
| `MediumLabel` | `Edm.String` | properties | medium label, at most 20 characters |
| `LongLabel` | `Edm.String` | properties | long label, at most 40 characters |
| `Heading` | `Edm.String` | properties | column heading, at most 40 characters |
| `Mandatory` | `Core.Tag` | properties | the field needs a value when the instance is saved: not null, and for text not `""` |
| `ValueList` | `RT.Fields.ValueListType` | properties | an entity set of the service that gives the values along a foreign key |
| `DataClass` | `Edm.String` | entity types | `application`, `configuration` or `runtimeConfiguration` |
| `Origin` | `Edm.String` | | the package whose behavior or extension reported a message |

`ValueListType` has `EntitySet`, the public name of the entity set, and `Parameters`, a
collection of `ValueListParameter` with `LocalProperty`, the path of the property of the
annotated entity, and `ValueListProperty`, the name of the property of the entity set:

```json
"TRFIN.TRFIN_COMPANIES/COUNTRY": {
  "@RT.ValueList": {
    "EntitySet": "Countries",
    "Parameters": [{ "LocalProperty": { "$PropertyPath": "COUNTRY" }, "ValueListProperty": "COUNTRY_CODE" }]
  }
}
```

The labels come in the language of the request, in English when the data element has no
text in it. `RT.Mandatory` is derived from the business object and enforced by the save
sequence; extensions of the metadata cannot change it. `RT.Origin` appears in the
`details` of errors and in `@Core.Messages`.
