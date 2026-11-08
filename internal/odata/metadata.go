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

package odata

import (
	"slices"
	"strconv"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/structure"
)

// Metadata is the metadata of a service in one language: its structure
// and the annotations of the kernel. Extensions of the metadata may change
// annotations, references and descriptions, never the structure.
type Metadata struct {
	Package     model.PackageID
	Service     string
	Language    string
	Namespace   string
	EntitySets  []MetaEntitySet
	EntityTypes []MetaEntityType
	// ComplexTypes are the structures of parameters and results of
	// actions.
	ComplexTypes  []MetaComplexType
	Actions       []MetaAction
	ActionImports []MetaActionImport
	Annotations   []Annotation
	References    []Reference
}

// MetaEntitySet is an entity set with the bindings of its navigations.
type MetaEntitySet struct {
	Name, EntityType, TechnicalName string
	Bindings                        []Binding
}

// Binding binds a navigation of an entity set to its target set.
type Binding struct{ Path, Target string }

// MetaEntityType is an entity type.
type MetaEntityType struct {
	Name, TechnicalName string
	Key                 []string
	Properties          []MetaProperty
	Navigations         []MetaNavigation
}

// MetaComplexType is a complex type.
type MetaComplexType struct {
	Name, TechnicalName string
	Properties          []MetaProperty
}

// MetaProperty is a property of a type or a parameter of an action.
type MetaProperty struct {
	Name, TechnicalName string
	// Type is the qualified type, for collections Collection(...).
	Type      string
	Nullable  bool
	MaxLength model.Optional[int64]
	Precision model.Optional[int64]
	Scale     model.Optional[int64]
}

// MetaNavigation is a navigation of an entity type.
type MetaNavigation struct {
	Name, Type     string
	Nullable       bool
	Partner        string
	ContainsTarget bool
	Constraints    []Constraint
}

// Constraint is a referential constraint of a navigation.
type Constraint struct{ Property, ReferencedProperty string }

// MetaAction is an action; bound actions name their type in BoundTo.
type MetaAction struct {
	Name, TechnicalName string
	BoundTo             string
	Parameters          []MetaProperty
	// ReturnType is empty for actions without result.
	ReturnType     string
	ReturnNullable bool
	// EntitySetPath is _it for bound actions that return their instance.
	EntitySetPath string
}

// MetaActionImport offers a static action in the entity container.
type MetaActionImport struct {
	Name, Action, EntitySet string
}

// Annotation applies a term to a target.
type Annotation struct {
	// Target is the path of the annotated element, such as
	// TRFIN.TRFIN_COMPANIES/NAME or TRFIN.Container/Companies.
	Target    string
	Term      string
	Qualifier string
	// Origin is the package of the extension that added or changed it;
	// empty for annotations of the kernel.
	Origin string
	Value  Value
}

// Reference refers to a vocabulary.
type Reference struct {
	URI, Namespace, Alias string
}

// Kinds of values of annotations.
const (
	KindString         = "string"
	KindBoolean        = "boolean"
	KindInteger        = "integer"
	KindDecimal        = "decimal"
	KindDate           = "date"
	KindDateTimeOffset = "dateTimeOffset"
	KindGuid           = "guid"
	KindEnumMember     = "enumMember"
	KindPath           = "path"
	KindPropertyPath   = "propertyPath"
	KindNavigationPath = "navigationPath"
	KindAnnotationPath = "annotationPath"
	KindNull           = "null"
	KindCollection     = "collection"
	KindRecord         = "record"
)

// ValueKinds lists the kinds of values.
var ValueKinds = []string{KindString, KindBoolean, KindInteger, KindDecimal, KindDate, KindDateTimeOffset, KindGuid, KindEnumMember, KindPath,
	KindPropertyPath, KindNavigationPath, KindAnnotationPath, KindNull, KindCollection, KindRecord}

// Value is the value of an annotation: a simple value in canonical text
// form, a collection of values or a record of members. Members whose
// names start with @ annotate the record.
type Value struct {
	Kind    string
	Text    string
	Type    string
	Items   []Value
	Members []Member
}

// Member is a member of a record.
type Member struct {
	Name  string
	Value Value
}

// String, Bool, Int, PropertyPath and Record build values.
func String(s string) Value { return Value{Kind: KindString, Text: s} }

// Bool returns a boolean value.
func Bool(b bool) Value { return Value{Kind: KindBoolean, Text: strconv.FormatBool(b)} }

// Int returns an integer value.
func Int(n int64) Value { return Value{Kind: KindInteger, Text: strconv.FormatInt(n, 10)} }

// PropertyPath returns a path to a property.
func PropertyPath(p string) Value { return Value{Kind: KindPropertyPath, Text: p} }

// Collection returns a collection.
func Collection(items ...Value) Value {
	return Value{Kind: KindCollection, Items: append([]Value{}, items...)}
}

// Record returns a record of members given as name and value pairs.
func Record(members ...Member) Value { return Value{Kind: KindRecord, Members: members} }

// Vocabularies the kernel uses, with their aliases.
const (
	CoreNamespace         = "Org.OData.Core.V1"
	ValidationNamespace   = "Org.OData.Validation.V1"
	CapabilitiesNamespace = "Org.OData.Capabilities.V1"
	// FieldsNamespace is the vocabulary of the kernel; VocabularyPath is
	// the path of its document below /rt/odata, in VocabulariesPath.
	FieldsNamespace  = "RT.Fields"
	FieldsAlias      = "RT"
	VocabulariesPath = "$vocabularies/"
	VocabularyPath   = VocabulariesPath + FieldsNamespace
)

// ProtectedTerms are the annotations the kernel derives from business
// objects and enforces; extensions of the metadata may not add, change or
// remove them.
var ProtectedTerms = []string{"Core.Computed", "Core.Immutable", "Core.OptimisticConcurrency", "Validation.AllowedValues", FieldsAlias + ".Mandatory"}

// Protected reports whether a term belongs to the kernel.
func Protected(term string) bool {
	return slices.Contains(ProtectedTerms, term) || len(term) > len("Capabilities.") && term[:len("Capabilities.")] == "Capabilities."
}

// containerName is the name of the entity container.
const containerName = "Container"

// Qualified returns a name in the namespace of the metadata.
func (md *Metadata) Qualified(name string) string { return md.Namespace + "." + name }

// Container returns the qualified name of the entity container.
func (md *Metadata) Container() string { return md.Qualified(containerName) }

// metaBuilder derives the metadata of a model.
type metaBuilder struct {
	s       *model.Schema
	m       *Model
	md      *Metadata
	lang    string
	complex map[string]bool
}

// BuildMetadata derives the metadata of a service in a language with the
// annotations of the kernel.
func BuildMetadata(s *model.Schema, m *Model, lang string) *Metadata {
	md := &Metadata{Package: m.Service.Owner, Service: m.Service.Name, Language: lang, Namespace: m.Namespace,
		References: []Reference{
			{URI: "https://oasis-tcs.github.io/odata-vocabularies/vocabularies/Org.OData.Core.V1.xml", Namespace: CoreNamespace, Alias: "Core"},
			{URI: "https://oasis-tcs.github.io/odata-vocabularies/vocabularies/Org.OData.Validation.V1.xml", Namespace: ValidationNamespace, Alias: "Validation"},
			{URI: "https://oasis-tcs.github.io/odata-vocabularies/vocabularies/Org.OData.Capabilities.V1.xml", Namespace: CapabilitiesNamespace, Alias: "Capabilities"},
			{URI: "../../" + VocabularyPath + ".xml", Namespace: FieldsNamespace, Alias: FieldsAlias},
		}}
	b := &metaBuilder{s: s, m: m, md: md, lang: lang, complex: map[string]bool{}}
	b.annotate(md.Container(), "Core.Description", textIn(m.Service.Description, lang))
	for _, t := range m.Types() {
		b.entityType(t)
	}
	for _, set := range m.Sets {
		b.entitySet(set)
	}
	for _, a := range m.Imports {
		b.action(a)
		md.ActionImports = append(md.ActionImports, MetaActionImport{Name: a.Name, Action: md.Qualified(a.Name)})
	}
	return md
}

// annotate adds an annotation with a text unless the text is empty.
func (b *metaBuilder) annotate(target, term, text string) {
	if text != "" {
		b.md.Annotations = append(b.md.Annotations, Annotation{Target: target, Term: term, Value: String(text)})
	}
}

func (b *metaBuilder) add(target, term string, v Value) {
	b.md.Annotations = append(b.md.Annotations, Annotation{Target: target, Term: term, Value: v})
}

// textIn returns the text of a language, English otherwise.
func textIn(texts map[string]string, lang string) string {
	if t, ok := texts[lang]; ok {
		return t
	}
	return texts[model.DefaultLanguage]
}

func labelsIn(de *model.DataElement, lang string) (model.DataElementText, bool) {
	if de == nil {
		return model.DataElementText{}, false
	}
	if t, ok := de.Texts[lang]; ok {
		return t, true
	}
	t, ok := de.Texts[model.DefaultLanguage]
	return t, ok
}

// EdmType returns the type of OData of a domain with its facets.
func EdmType(d *model.Domain) MetaProperty {
	p := MetaProperty{}
	switch d.Type {
	case model.Char:
		p.Type, p.MaxLength = "Edm.String", model.Some(int64(d.Length))
	case model.String:
		p.Type = "Edm.String"
	case model.Int4:
		p.Type = "Edm.Int32"
	case model.Int8:
		p.Type = "Edm.Int64"
	case model.Dec:
		p.Type, p.Precision, p.Scale = "Edm.Decimal", model.Some(int64(d.Length)), model.Some(int64(d.Decimals))
	case model.Date:
		p.Type = "Edm.Date"
	case model.Timestamp:
		p.Type, p.Precision = "Edm.DateTimeOffset", model.Some(int64(6))
	case model.Bool:
		p.Type = "Edm.Boolean"
	case model.UUID:
		p.Type = "Edm.Guid"
	case model.Blob:
		p.Type = "Edm.Binary"
	}
	return p
}

func (b *metaBuilder) entityType(t *EntityType) {
	et := MetaEntityType{Name: t.Name, TechnicalName: t.Table.Name}
	for _, k := range t.Key {
		et.Key = append(et.Key, k.Name)
	}
	qualified := b.md.Qualified(t.Name)
	for _, p := range t.Properties {
		mp := EdmType(p.Domain)
		mp.Name, mp.TechnicalName, mp.Nullable = p.Name, p.Field.Name, p.Nullable
		et.Properties = append(et.Properties, mp)
		b.property(qualified+"/"+p.Name, p, t)
	}
	for _, n := range t.Navigations {
		mn := MetaNavigation{Name: n.Name, Type: b.md.Qualified(n.Type.Name), Nullable: n.Nullable, Partner: n.Partner, ContainsTarget: n.Contained}
		if n.Collection {
			mn.Type = "Collection(" + mn.Type + ")"
		}
		for _, c := range n.Constraints {
			src, dst := t.FieldProperty(c.Field), n.Type.FieldProperty(c.References)
			if src != nil && dst != nil {
				mn.Constraints = append(mn.Constraints, Constraint{Property: src.Name, ReferencedProperty: dst.Name})
			}
		}
		et.Navigations = append(et.Navigations, mn)
	}
	b.md.EntityTypes = append(b.md.EntityTypes, et)
	if t.BusinessObject != nil {
		b.annotate(qualified, "Core.Description", textIn(t.BusinessObject.Description, b.lang))
		b.add(qualified, FieldsAlias+".DataClass", String(string(t.BusinessObject.DataClass)))
	} else {
		b.annotate(qualified, "Core.Description", t.Table.Description.Or(""))
		b.add(qualified, FieldsAlias+".DataClass", String(string(t.Table.DataClass)))
	}
	for _, a := range t.Actions {
		b.action(a)
	}
}

// property adds the annotations of a property.
func (b *metaBuilder) property(target string, p *Property, t *EntityType) {
	if l, ok := labelsIn(p.DataElement, b.lang); ok {
		b.annotate(target, "Core.Description", l.Medium)
		b.annotate(target, "Core.LongDescription", l.Description.Or(l.Long))
		b.annotate(target, FieldsAlias+".ShortLabel", l.Short)
		b.annotate(target, FieldsAlias+".MediumLabel", l.Medium)
		b.annotate(target, FieldsAlias+".LongLabel", l.Long)
		b.annotate(target, FieldsAlias+".Heading", l.Heading)
	} else {
		b.annotate(target, "Core.Description", p.Field.Description.Or(""))
	}
	if p.Computed {
		b.add(target, "Core.Computed", Bool(true))
	}
	if p.Immutable {
		b.add(target, "Core.Immutable", Bool(true))
	}
	if p.Mandatory {
		b.add(target, FieldsAlias+".Mandatory", Bool(true))
	}
	if vs := p.Domain.Values; len(vs) > 0 {
		var items []Value
		for _, v := range vs {
			members := []Member{{Name: "Value", Value: simple(p.Domain, v.Value)}}
			if d, ok := v.Description.Get(); ok {
				members = append(members, Member{Name: "@Core.Description", Value: String(d)})
			}
			items = append(items, Record(members...))
		}
		b.add(target, "Validation.AllowedValues", Collection(items...))
	}
	for _, n := range t.Navigations {
		if n.Contained || n.Collection || len(n.Constraints) == 0 || n.Constraints[0].Field != p.Field.Name || n.Target == nil {
			continue
		}
		var params []Value
		for _, c := range n.Constraints {
			src, dst := t.FieldProperty(c.Field), n.Type.FieldProperty(c.References)
			if src != nil && dst != nil {
				params = append(params, Record(Member{Name: "LocalProperty", Value: PropertyPath(src.Name)},
					Member{Name: "ValueListProperty", Value: String(dst.Name)}))
			}
		}
		b.add(target, FieldsAlias+".ValueList", Record(Member{Name: "EntitySet", Value: String(n.Target.Name)},
			Member{Name: "Parameters", Value: Collection(params...)}))
	}
}

// simple returns a fixed value as value of the type of a domain.
func simple(d *model.Domain, text string) Value {
	switch d.Type {
	case model.Int4, model.Int8:
		return Value{Kind: KindInteger, Text: text}
	}
	return String(text)
}

func (b *metaBuilder) entitySet(set *Set) {
	t := set.Type
	es := MetaEntitySet{Name: set.Name, EntityType: b.md.Qualified(t.Name), TechnicalName: t.Table.Name}
	for _, n := range t.Navigations {
		if n.Target != nil {
			es.Bindings = append(es.Bindings, Binding{Path: n.Name, Target: n.Target.Name})
		}
	}
	b.md.EntitySets = append(b.md.EntitySets, es)
	target := b.md.Container() + "/" + set.Name
	if t.ETag != nil {
		b.add(target, "Core.OptimisticConcurrency", Collection(PropertyPath(t.ETag.Name)))
	}
	b.restrictions(target, t)
	var nav []Value
	for _, n := range t.Navigations {
		if n.Contained {
			nav = append(nav, Record(Member{Name: "NavigationProperty", Value: Value{Kind: KindNavigationPath, Text: n.Name}},
				Member{Name: "InsertRestrictions", Value: Record(Member{Name: "Insertable", Value: Bool(n.Type.Allows(model.OperationCreate))})},
				Member{Name: "UpdateRestrictions", Value: Record(Member{Name: "Updatable", Value: Bool(n.Type.Allows(model.OperationUpdate))})},
				Member{Name: "DeleteRestrictions", Value: Record(Member{Name: "Deletable", Value: Bool(n.Type.Allows(model.OperationDelete))})}))
		}
	}
	if len(nav) > 0 {
		b.add(target, "Capabilities.NavigationRestrictions", Record(Member{Name: "RestrictedProperties", Value: Collection(nav...)}))
	}
}

// restrictions adds the capabilities of an entity set.
func (b *metaBuilder) restrictions(target string, t *EntityType) {
	b.add(target, "Capabilities.InsertRestrictions", Record(Member{Name: "Insertable", Value: Bool(t.Allows(model.OperationCreate))}))
	b.add(target, "Capabilities.UpdateRestrictions", Record(Member{Name: "Updatable", Value: Bool(t.Allows(model.OperationUpdate))}))
	b.add(target, "Capabilities.DeleteRestrictions", Record(Member{Name: "Deletable", Value: Bool(t.Allows(model.OperationDelete))}))
	var blobs []Value
	for _, p := range t.Properties {
		if p.Domain.Type == model.Blob {
			blobs = append(blobs, PropertyPath(p.Name))
		}
	}
	b.add(target, "Capabilities.FilterRestrictions", Record(Member{Name: "Filterable", Value: Bool(true)},
		Member{Name: "NonFilterableProperties", Value: Collection(blobs...)}))
	b.add(target, "Capabilities.SortRestrictions", Record(Member{Name: "Sortable", Value: Bool(true)},
		Member{Name: "NonSortableProperties", Value: Collection(blobs...)}))
	b.add(target, "Capabilities.TopSupported", Bool(true))
	b.add(target, "Capabilities.SkipSupported", Bool(true))
	b.add(target, "Capabilities.CountRestrictions", Record(Member{Name: "Countable", Value: Bool(true)}))
	b.add(target, "Capabilities.ExpandRestrictions", Record(Member{Name: "Expandable", Value: Bool(true)}, Member{Name: "MaxLevels", Value: Int(1)}))
	b.add(target, "Capabilities.SearchRestrictions", Record(Member{Name: "Searchable", Value: Bool(false)}))
	b.add(target, "Capabilities.DeepInsertSupport", Record(Member{Name: "Supported", Value: Bool(t.Allows(model.OperationCreate))},
		Member{Name: "ContentIDSupported", Value: Bool(false)}))
}

// action adds an action with the complex types of its parameter and
// result.
func (b *metaBuilder) action(a *Action) {
	ma := MetaAction{Name: a.Name, TechnicalName: a.Action.Name}
	if a.Type != nil {
		ma.BoundTo = b.md.Qualified(a.Type.Name)
		ma.Parameters = append(ma.Parameters, MetaProperty{Name: "_it", Type: ma.BoundTo})
	}
	if p, ok := a.Action.Parameter.Get(); ok {
		if def, err := structure.Resolve(b.s, []string{b.lang}, p); err == nil {
			ma.Parameters = append(ma.Parameters, b.components(def)...)
		}
	}
	switch r := a.Action.Result.Or(""); r {
	case "":
	case model.ResultSelf:
		ma.ReturnType = ma.BoundTo
		if a.Type != nil {
			ma.EntitySetPath = "_it"
		}
	default:
		if def, err := structure.Resolve(b.s, []string{b.lang}, r); err == nil {
			ma.ReturnType = b.complexType(def)
		}
	}
	b.md.Actions = append(b.md.Actions, ma)
}

// components returns the properties of the components of a structure.
func (b *metaBuilder) components(def *structure.Definition) []MetaProperty {
	var out []MetaProperty
	for _, c := range def.Components {
		out = append(out, b.component(c))
	}
	return out
}

func (b *metaBuilder) component(c structure.Component) MetaProperty {
	var p MetaProperty
	switch {
	case c.Structure != nil:
		p.Type = b.complexType(c.Structure)
	case c.ArrayType != nil:
		row := b.component(c.ArrayType.Array.Row)
		p = row
		p.Type = "Collection(" + row.Type + ")"
	default:
		p = EdmType(&model.Domain{Type: c.Type, Length: c.Length, Decimals: c.Decimals})
	}
	p.Name, p.TechnicalName, p.Nullable = model.PublicName(c.Name), c.Name, !c.NotNull
	return p
}

// complexType adds the complex type of a structure once and returns its
// qualified name.
func (b *metaBuilder) complexType(def *structure.Definition) string {
	name := model.PublicName(def.Name)
	if !b.complex[name] {
		b.complex[name] = true
		ct := MetaComplexType{Name: name, TechnicalName: def.Name}
		ct.Properties = b.components(def)
		b.md.ComplexTypes = append(b.md.ComplexTypes, ct)
	}
	return b.md.Qualified(name)
}
