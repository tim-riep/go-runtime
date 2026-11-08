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
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/tim-riep/go-runtime/model"
)

// The metadata in the structure /TRBASE/ODATA_METADATA, in the JSON format
// of rt run: absent values are null, lists never.
type (
	vmMetadata struct {
		PackageID     string          `json:"PACKAGE_ID"`
		ServiceName   string          `json:"SERVICE_NAME"`
		Language      string          `json:"LANGUAGE"`
		Namespace     string          `json:"NAMESPACE"`
		EntitySets    []vmEntitySet   `json:"ENTITY_SETS"`
		EntityTypes   []vmEntityType  `json:"ENTITY_TYPES"`
		ComplexTypes  []vmComplexType `json:"COMPLEX_TYPES"`
		Actions       []vmAction      `json:"ACTIONS"`
		ActionImports []vmImport      `json:"ACTION_IMPORTS"`
		Annotations   []vmAnnotation  `json:"ANNOTATIONS"`
		References    []vmReference   `json:"REFERENCES"`
	}
	vmEntitySet struct {
		Name          string      `json:"NAME"`
		EntityType    string      `json:"ENTITY_TYPE"`
		TechnicalName string      `json:"TECHNICAL_NAME"`
		Bindings      []vmBinding `json:"BINDINGS"`
	}
	vmBinding struct {
		Path   string `json:"PATH"`
		Target string `json:"TARGET"`
	}
	vmEntityType struct {
		Name          string         `json:"NAME"`
		TechnicalName string         `json:"TECHNICAL_NAME"`
		Key           []string       `json:"KEY"`
		Properties    []vmProperty   `json:"PROPERTIES"`
		Navigations   []vmNavigation `json:"NAVIGATIONS"`
	}
	vmComplexType struct {
		Name          string       `json:"NAME"`
		TechnicalName string       `json:"TECHNICAL_NAME"`
		Properties    []vmProperty `json:"PROPERTIES"`
	}
	vmProperty struct {
		Name          string  `json:"NAME"`
		TechnicalName *string `json:"TECHNICAL_NAME"`
		Type          string  `json:"TYPE"`
		Nullable      bool    `json:"NULLABLE"`
		MaxLength     *int64  `json:"MAX_LENGTH"`
		Precision     *int64  `json:"PRECISION"`
		Scale         *int64  `json:"SCALE"`
	}
	vmNavigation struct {
		Name           string         `json:"NAME"`
		Type           string         `json:"TYPE"`
		Nullable       bool           `json:"NULLABLE"`
		Partner        *string        `json:"PARTNER"`
		ContainsTarget bool           `json:"CONTAINS_TARGET"`
		Constraints    []vmConstraint `json:"CONSTRAINTS"`
	}
	vmConstraint struct {
		Property           string `json:"PROPERTY"`
		ReferencedProperty string `json:"REFERENCED_PROPERTY"`
	}
	vmAction struct {
		Name           string       `json:"NAME"`
		TechnicalName  string       `json:"TECHNICAL_NAME"`
		BoundTo        *string      `json:"BOUND_TO"`
		Parameters     []vmProperty `json:"PARAMETERS"`
		ReturnType     *string      `json:"RETURN_TYPE"`
		ReturnNullable bool         `json:"RETURN_NULLABLE"`
		EntitySetPath  *string      `json:"ENTITY_SET_PATH"`
	}
	vmImport struct {
		Name      string  `json:"NAME"`
		Action    string  `json:"ACTION"`
		EntitySet *string `json:"ENTITY_SET"`
	}
	vmAnnotation struct {
		Target    string    `json:"TARGET"`
		Term      string    `json:"TERM"`
		Qualifier *string   `json:"QUALIFIER"`
		Origin    *string   `json:"ORIGIN"`
		Value     []vmValue `json:"VALUE"`
	}
	vmValue struct {
		ID     int64   `json:"ID"`
		Parent *int64  `json:"PARENT"`
		Member *string `json:"MEMBER"`
		Kind   string  `json:"KIND"`
		Text   *string `json:"TEXT"`
		Type   *string `json:"TYPE"`
	}
	vmReference struct {
		URI       string  `json:"URI"`
		Namespace string  `json:"NAMESPACE"`
		Alias     *string `json:"ALIAS"`
	}
)

// opt returns nil for "", which marks an absent value in Metadata.
func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func optInt(o model.Optional[int64]) *int64 {
	if v, ok := o.Get(); ok {
		return &v
	}
	return nil
}

func fromInt(p *int64) model.Optional[int64] {
	if p == nil {
		return model.None[int64]()
	}
	return model.Some(*p)
}

func vmProperties(ps []MetaProperty) []vmProperty {
	out := []vmProperty{}
	for _, p := range ps {
		out = append(out, vmProperty{Name: p.Name, TechnicalName: opt(p.TechnicalName), Type: p.Type, Nullable: p.Nullable, MaxLength: optInt(p.MaxLength),
			Precision: optInt(p.Precision), Scale: optInt(p.Scale)})
	}
	return out
}

func metaProperties(ps []vmProperty) []MetaProperty {
	out := []MetaProperty{}
	for _, p := range ps {
		out = append(out, MetaProperty{Name: p.Name, TechnicalName: deref(p.TechnicalName), Type: p.Type, Nullable: p.Nullable,
			MaxLength: fromInt(p.MaxLength), Precision: fromInt(p.Precision), Scale: fromInt(p.Scale)})
	}
	return out
}

// Encode returns the metadata as value of /TRBASE/ODATA_METADATA in the
// JSON format of rt run. Values of annotations become flat trees of nodes.
func (md *Metadata) Encode() json.RawMessage {
	v := vmMetadata{PackageID: string(md.Package), ServiceName: md.Service, Language: md.Language, Namespace: md.Namespace,
		EntitySets: []vmEntitySet{}, EntityTypes: []vmEntityType{}, ComplexTypes: []vmComplexType{}, Actions: []vmAction{},
		ActionImports: []vmImport{}, Annotations: []vmAnnotation{}, References: []vmReference{}}
	for _, s := range md.EntitySets {
		es := vmEntitySet{Name: s.Name, EntityType: s.EntityType, TechnicalName: s.TechnicalName, Bindings: []vmBinding{}}
		for _, b := range s.Bindings {
			es.Bindings = append(es.Bindings, vmBinding{Path: b.Path, Target: b.Target})
		}
		v.EntitySets = append(v.EntitySets, es)
	}
	for _, t := range md.EntityTypes {
		et := vmEntityType{Name: t.Name, TechnicalName: t.TechnicalName, Key: append([]string{}, t.Key...), Properties: vmProperties(t.Properties),
			Navigations: []vmNavigation{}}
		for _, n := range t.Navigations {
			nav := vmNavigation{Name: n.Name, Type: n.Type, Nullable: n.Nullable, Partner: opt(n.Partner), ContainsTarget: n.ContainsTarget,
				Constraints: []vmConstraint{}}
			for _, c := range n.Constraints {
				nav.Constraints = append(nav.Constraints, vmConstraint{Property: c.Property, ReferencedProperty: c.ReferencedProperty})
			}
			et.Navigations = append(et.Navigations, nav)
		}
		v.EntityTypes = append(v.EntityTypes, et)
	}
	for _, t := range md.ComplexTypes {
		v.ComplexTypes = append(v.ComplexTypes, vmComplexType{Name: t.Name, TechnicalName: t.TechnicalName, Properties: vmProperties(t.Properties)})
	}
	for _, a := range md.Actions {
		v.Actions = append(v.Actions, vmAction{Name: a.Name, TechnicalName: a.TechnicalName, BoundTo: opt(a.BoundTo), Parameters: vmProperties(a.Parameters),
			ReturnType: opt(a.ReturnType), ReturnNullable: a.ReturnNullable, EntitySetPath: opt(a.EntitySetPath)})
	}
	for _, a := range md.ActionImports {
		v.ActionImports = append(v.ActionImports, vmImport{Name: a.Name, Action: a.Action, EntitySet: opt(a.EntitySet)})
	}
	for _, a := range md.Annotations {
		va := vmAnnotation{Target: a.Target, Term: a.Term, Qualifier: opt(a.Qualifier), Origin: opt(a.Origin), Value: []vmValue{}}
		flatten(a.Value, nil, nil, &va.Value)
		v.Annotations = append(v.Annotations, va)
	}
	for _, r := range md.References {
		v.References = append(v.References, vmReference{URI: r.URI, Namespace: r.Namespace, Alias: opt(r.Alias)})
	}
	out, _ := json.Marshal(v)
	return out
}

// kindName returns the name of the fixed value of /TRBASE/ODATA_VALUE_KIND
// of a kind, which code and the JSON format of rt run use: dateTimeOffset
// becomes DATE_TIME_OFFSET.
func kindName(kind string) string {
	var b strings.Builder
	for i, r := range kind {
		if r >= 'A' && r <= 'Z' && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

// kindOf returns the kind of the name of a fixed value.
func kindOf(name string) (string, bool) {
	for _, k := range ValueKinds {
		if kindName(k) == name {
			return k, true
		}
	}
	return "", false
}

// flatten appends the nodes of a value: the node itself, then its items
// or members.
func flatten(v Value, parent *int64, member *string, out *[]vmValue) {
	id := int64(len(*out) + 1)
	n := vmValue{ID: id, Parent: parent, Member: member, Kind: kindName(v.Kind), Type: opt(v.Type)}
	if v.Kind != KindCollection && v.Kind != KindRecord && v.Kind != KindNull {
		text := v.Text
		n.Text = &text
	}
	*out = append(*out, n)
	for _, it := range v.Items {
		flatten(it, &id, nil, out)
	}
	for _, m := range v.Members {
		name := m.Name
		flatten(m.Value, &id, &name, out)
	}
}

// DecodeMetadata reads metadata returned by an extension. It checks the
// trees of values of annotations.
func DecodeMetadata(raw json.RawMessage) (*Metadata, error) {
	var v vmMetadata
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	md := &Metadata{Package: model.PackageID(v.PackageID), Service: v.ServiceName, Language: v.Language, Namespace: v.Namespace}
	for _, s := range v.EntitySets {
		es := MetaEntitySet{Name: s.Name, EntityType: s.EntityType, TechnicalName: s.TechnicalName}
		for _, b := range s.Bindings {
			es.Bindings = append(es.Bindings, Binding{Path: b.Path, Target: b.Target})
		}
		md.EntitySets = append(md.EntitySets, es)
	}
	for _, t := range v.EntityTypes {
		et := MetaEntityType{Name: t.Name, TechnicalName: t.TechnicalName, Key: t.Key, Properties: metaProperties(t.Properties)}
		for _, n := range t.Navigations {
			nav := MetaNavigation{Name: n.Name, Type: n.Type, Nullable: n.Nullable, Partner: deref(n.Partner), ContainsTarget: n.ContainsTarget}
			for _, c := range n.Constraints {
				nav.Constraints = append(nav.Constraints, Constraint{Property: c.Property, ReferencedProperty: c.ReferencedProperty})
			}
			et.Navigations = append(et.Navigations, nav)
		}
		md.EntityTypes = append(md.EntityTypes, et)
	}
	for _, t := range v.ComplexTypes {
		md.ComplexTypes = append(md.ComplexTypes, MetaComplexType{Name: t.Name, TechnicalName: t.TechnicalName, Properties: metaProperties(t.Properties)})
	}
	for _, a := range v.Actions {
		md.Actions = append(md.Actions, MetaAction{Name: a.Name, TechnicalName: a.TechnicalName, BoundTo: deref(a.BoundTo), Parameters: metaProperties(a.Parameters),
			ReturnType: deref(a.ReturnType), ReturnNullable: a.ReturnNullable, EntitySetPath: deref(a.EntitySetPath)})
	}
	for _, a := range v.ActionImports {
		md.ActionImports = append(md.ActionImports, MetaActionImport{Name: a.Name, Action: a.Action, EntitySet: deref(a.EntitySet)})
	}
	for i, a := range v.Annotations {
		val, err := unflatten(a.Value)
		if err != nil {
			return nil, fmt.Errorf("the value of annotation %d (%s of %s): %w", i+1, a.Term, a.Target, err)
		}
		md.Annotations = append(md.Annotations, Annotation{Target: a.Target, Term: a.Term, Qualifier: deref(a.Qualifier), Origin: deref(a.Origin), Value: val})
	}
	for _, r := range v.References {
		md.References = append(md.References, Reference{URI: r.URI, Namespace: r.Namespace, Alias: deref(r.Alias)})
	}
	return md, nil
}

// unflatten builds a value from its nodes: exactly one root; items of
// collections have no member, members of records have one; simple values
// have a text and no children.
func unflatten(nodes []vmValue) (Value, error) {
	byID := map[int64]*vmValue{}
	children := map[int64][]*vmValue{}
	var root *vmValue
	for i := range nodes {
		n := &nodes[i]
		if _, ok := byID[n.ID]; ok {
			return Value{}, fmt.Errorf("the node %d appears twice", n.ID)
		}
		byID[n.ID] = n
		kind, ok := kindOf(n.Kind)
		if !ok {
			return Value{}, fmt.Errorf("the node %d has the unknown kind %q", n.ID, n.Kind)
		}
		n.Kind = kind
		if n.Parent == nil {
			if root != nil {
				return Value{}, fmt.Errorf("the value has the roots %d and %d", root.ID, n.ID)
			}
			root = n
		}
	}
	if root == nil {
		return Value{}, fmt.Errorf("the value has no root")
	}
	for i := range nodes {
		n := &nodes[i]
		if n.Parent == nil {
			continue
		}
		p, ok := byID[*n.Parent]
		if !ok {
			return Value{}, fmt.Errorf("the parent %d of node %d does not exist", *n.Parent, n.ID)
		}
		switch {
		case p.Kind == KindCollection && n.Member != nil:
			return Value{}, fmt.Errorf("the item %d of a collection has a member name", n.ID)
		case p.Kind == KindRecord && n.Member == nil:
			return Value{}, fmt.Errorf("the member %d of a record has no name", n.ID)
		case p.Kind != KindCollection && p.Kind != KindRecord:
			return Value{}, fmt.Errorf("the node %d is a %s and has no children", p.ID, p.Kind)
		}
		children[p.ID] = append(children[p.ID], n)
	}
	seen := map[int64]bool{}
	var build func(n *vmValue) (Value, error)
	build = func(n *vmValue) (Value, error) {
		if seen[n.ID] {
			return Value{}, fmt.Errorf("the node %d is its own ancestor", n.ID)
		}
		seen[n.ID] = true
		v := Value{Kind: n.Kind, Type: deref(n.Type)}
		switch n.Kind {
		case KindCollection, KindRecord, KindNull:
			if n.Text != nil {
				return Value{}, fmt.Errorf("the %s %d has a text", n.Kind, n.ID)
			}
		default:
			if n.Text == nil {
				return Value{}, fmt.Errorf("the %s %d has no text", n.Kind, n.ID)
			}
			v.Text = *n.Text
		}
		for _, c := range children[n.ID] {
			cv, err := build(c)
			if err != nil {
				return Value{}, err
			}
			if n.Kind == KindCollection {
				v.Items = append(v.Items, cv)
			} else {
				v.Members = append(v.Members, Member{Name: *c.Member, Value: cv})
			}
		}
		return v, nil
	}
	v, err := build(root)
	if err != nil {
		return Value{}, err
	}
	if len(seen) != len(nodes) {
		return Value{}, fmt.Errorf("%d nodes are not part of the value", len(nodes)-len(seen))
	}
	return v, nil
}

// structureOf returns everything of metadata that extensions may not
// change.
func structureOf(md *Metadata) Metadata {
	return Metadata{Package: md.Package, Service: md.Service, Language: md.Language, Namespace: md.Namespace, EntitySets: md.EntitySets,
		EntityTypes: md.EntityTypes, ComplexTypes: md.ComplexTypes, Actions: md.Actions, ActionImports: md.ActionImports}
}

// annotationKey identifies an annotation.
func annotationKey(a Annotation) string { return a.Target + "\x00" + a.Term + "\x00" + a.Qualifier }

// CheckExtension checks the metadata an extension returned against what
// it got: the structure is the same, references were only added,
// protected annotations of the kernel are unchanged and none were added,
// and every annotation has a target of the metadata and a term of a
// referenced vocabulary. It marks annotations the extension added or
// changed with origin.
func CheckExtension(before, after *Metadata, origin string) error {
	if !reflect.DeepEqual(structureOf(before), structureOf(after)) {
		return fmt.Errorf("it changed the structure of the metadata: %s", structureDiff(before, after))
	}
	refs := map[string]Reference{}
	for _, r := range after.References {
		refs[r.Namespace] = r
	}
	for _, r := range before.References {
		if refs[r.Namespace] != r {
			return fmt.Errorf("it changed or removed the reference to %s; extensions only add references", r.Namespace)
		}
	}
	old := map[string]Annotation{}
	for _, a := range before.Annotations {
		old[annotationKey(a)] = a
	}
	targets := after.targets()
	kept := map[string]bool{}
	for i := range after.Annotations {
		a := &after.Annotations[i]
		key := annotationKey(*a)
		prev, had := old[key]
		kept[key] = true
		if had && reflect.DeepEqual(prev.Value, a.Value) {
			a.Origin = prev.Origin
			continue
		}
		if Protected(a.Term) {
			if had {
				return fmt.Errorf("it changed %s of %s, which the kernel derives from the business object", a.Term, a.Target)
			}
			return fmt.Errorf("it added %s to %s, which only the kernel derives from the business object", a.Term, a.Target)
		}
		if !targets[a.Target] {
			return fmt.Errorf("the annotation %s has the target %s, which the metadata do not have", a.Term, a.Target)
		}
		alias, _, ok := strings.Cut(a.Term, ".")
		if !ok || !after.referenced(a.Term[:strings.LastIndex(a.Term, ".")]) && !after.referenced(alias) {
			return fmt.Errorf("the term %s belongs to no referenced vocabulary; add a reference with its namespace and alias", a.Term)
		}
		a.Origin = origin
	}
	for _, a := range before.Annotations {
		if !kept[annotationKey(a)] && Protected(a.Term) {
			return fmt.Errorf("it removed %s of %s, which the kernel derives from the business object and the service enforces", a.Term, a.Target)
		}
	}
	return nil
}

// referenced reports whether a namespace or alias is referenced.
func (md *Metadata) referenced(name string) bool {
	for _, r := range md.References {
		if r.Namespace == name || r.Alias == name {
			return true
		}
	}
	return false
}

// targets returns the paths extensions may annotate: the container, its
// entity sets and action imports, the types with their properties and
// navigations, and the actions.
func (md *Metadata) targets() map[string]bool {
	out := map[string]bool{md.Container(): true}
	for _, s := range md.EntitySets {
		out[md.Container()+"/"+s.Name] = true
	}
	for _, a := range md.ActionImports {
		out[md.Container()+"/"+a.Name] = true
	}
	for _, t := range md.EntityTypes {
		out[md.Qualified(t.Name)] = true
		for _, p := range t.Properties {
			out[md.Qualified(t.Name)+"/"+p.Name] = true
		}
		for _, n := range t.Navigations {
			out[md.Qualified(t.Name)+"/"+n.Name] = true
		}
	}
	for _, t := range md.ComplexTypes {
		out[md.Qualified(t.Name)] = true
		for _, p := range t.Properties {
			out[md.Qualified(t.Name)+"/"+p.Name] = true
		}
	}
	for _, a := range md.Actions {
		out[md.Qualified(a.Name)] = true
	}
	return out
}

// structureDiff names the first part of the structure that differs.
func structureDiff(a, b *Metadata) string {
	sa, sb := structureOf(a), structureOf(b)
	va, vb := reflect.ValueOf(sa), reflect.ValueOf(sb)
	for i := 0; i < va.NumField(); i++ {
		if !reflect.DeepEqual(va.Field(i).Interface(), vb.Field(i).Interface()) {
			return strings.ToLower(va.Type().Field(i).Name) + " differ"
		}
	}
	return "unknown"
}
