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

package vm

import (
	"fmt"

	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/native"
)

// Image is a set of linked programs: the kernel program and the programs
// of packages, one generation of the compiled code. Images are immutable
// and shared by executions.
type Image struct {
	Generation int64
	progs      map[string]*Prog
	order      []*Prog
	classes    map[string]*LinkedClass
	rt         map[string]*LinkedClass // kernel classes
	textField  bytecode.Field
	natives    *native.Registry
}

// Prog is a linked program.
type Prog struct {
	P       *bytecode.Program
	imports []*Prog // slot n is imports[n-1]
	Classes []*LinkedClass
	Funcs   []*LinkedFunc
}

// LinkedClass is a linked class or interface.
type LinkedClass struct {
	Name    string
	Def     *bytecode.Class
	Prog    *Prog
	Base    *LinkedClass
	VTable  []*LinkedFunc
	ITabs   map[*LinkedClass][]*LinkedFunc
	Supers  map[*LinkedClass]bool
	Layout  *bytecode.Layout
	Statics *bytecode.Layout
	Ctor    *LinkedFunc
	Init    *LinkedFunc
	Methods map[string]*LinkedFunc
	static  map[string]bool // static methods
	public  map[string]bool
	test    map[string]bool
}

// LinkedFunc is a linked function.
type LinkedFunc struct {
	Def    *bytecode.Func
	Prog   *Prog
	native *nativeFunc
}

func (p *Prog) at(slot int) *Prog {
	if slot == 0 {
		return p
	}
	return p.imports[slot-1]
}

func (p *Prog) class(r bytecode.Ref) *LinkedClass {
	return p.at(bytecode.RefSlot(r)).Classes[bytecode.RefIndex(r)]
}

func (p *Prog) fn(r bytecode.Ref) *LinkedFunc {
	return p.at(bytecode.RefSlot(r)).Funcs[bytecode.RefIndex(r)]
}

// Link links programs. The kernel program and every imported program must
// be among them.
func Link(generation int64, programs ...*bytecode.Program) (*Image, error) {
	return LinkWith(generation, native.Default, programs...)
}

// LinkWith is Link with the native methods of a registry.
func LinkWith(generation int64, natives *native.Registry, programs ...*bytecode.Program) (*Image, error) {
	image := &Image{Generation: generation, progs: map[string]*Prog{}, classes: map[string]*LinkedClass{}, rt: map[string]*LinkedClass{}}
	for _, p := range programs {
		if _, dup := image.progs[p.Package]; dup {
			return nil, fmt.Errorf("vm: package %s is linked twice", p.Package)
		}
		pr := &Prog{P: p}
		image.progs[p.Package] = pr
		image.order = append(image.order, pr)
		for i := range p.Funcs {
			pr.Funcs = append(pr.Funcs, &LinkedFunc{Def: &p.Funcs[i], Prog: pr})
		}
		for i := range p.Classes {
			def := &p.Classes[i]
			c := &LinkedClass{Name: def.Name, Def: def, Prog: pr, ITabs: map[*LinkedClass][]*LinkedFunc{}, Supers: map[*LinkedClass]bool{}, Methods: map[string]*LinkedFunc{},
				static: map[string]bool{}, public: map[string]bool{}, test: map[string]bool{}}
			if def.Layout >= 0 {
				c.Layout = &p.Layouts[def.Layout]
			}
			if def.Statics >= 0 {
				c.Statics = &p.Layouts[def.Statics]
			}
			if def.Ctor >= 0 {
				c.Ctor = pr.Funcs[def.Ctor]
			}
			if def.Init >= 0 {
				c.Init = pr.Funcs[def.Init]
			}
			pr.Classes = append(pr.Classes, c)
			if _, dup := image.classes[c.Name]; dup {
				return nil, fmt.Errorf("vm: class %s exists twice", c.Name)
			}
			image.classes[c.Name] = c
			if def.Kernel {
				image.rt[c.Name] = c
			}
		}
	}
	for _, pr := range image.order {
		for _, name := range pr.P.Imports {
			imp, ok := image.progs[name]
			if !ok {
				return nil, fmt.Errorf("vm: package %s imports %s, which is not linked", pr.P.Package, name)
			}
			pr.imports = append(pr.imports, imp)
		}
	}
	valid := func(pr *Prog, r bytecode.Ref, classes bool) bool {
		s, i := bytecode.RefSlot(r), bytecode.RefIndex(r)
		if r < 0 || s > len(pr.imports) {
			return false
		}
		t := pr.at(s)
		if classes {
			return i < len(t.Classes)
		}
		return i < len(t.Funcs)
	}
	for _, pr := range image.order {
		for _, c := range pr.Classes {
			def := c.Def
			if def.Base >= 0 {
				if !valid(pr, def.Base, true) {
					return nil, fmt.Errorf("vm: %s: invalid base class", c.Name)
				}
				c.Base = pr.class(def.Base)
			}
			for _, r := range def.VTable {
				if !valid(pr, r, false) {
					return nil, fmt.Errorf("vm: %s: invalid method table", c.Name)
				}
				c.VTable = append(c.VTable, pr.fn(r))
			}
			for _, r := range def.Supers {
				if !valid(pr, r, true) {
					return nil, fmt.Errorf("vm: %s: invalid super class", c.Name)
				}
				c.Supers[pr.class(r)] = true
			}
			for _, it := range def.ITables {
				if !valid(pr, it.Interface, true) {
					return nil, fmt.Errorf("vm: %s: invalid interface table", c.Name)
				}
				var fs []*LinkedFunc
				for _, r := range it.Funcs {
					if !valid(pr, r, false) {
						return nil, fmt.Errorf("vm: %s: invalid interface table", c.Name)
					}
					fs = append(fs, pr.fn(r))
				}
				c.ITabs[pr.class(it.Interface)] = fs
			}
			for _, m := range def.Methods {
				if !valid(pr, m.Func, false) {
					return nil, fmt.Errorf("vm: %s: invalid method %s", c.Name, m.Name)
				}
				c.Methods[m.Name] = pr.fn(m.Func)
				c.static[m.Name], c.public[m.Name], c.test[m.Name] = m.Static, m.Public, m.Test
			}
		}
	}
	rte := image.rt["/RT/RUNTIME_ERROR"]
	if rte == nil {
		return nil, fmt.Errorf("vm: the kernel program is not linked")
	}
	image.textField = rte.Layout.Fields[rte.Layout.Field("TEXT")]
	if err := image.bindNatives(natives); err != nil {
		return nil, err
	}
	image.natives = natives
	return image, nil
}

// Class returns a class by name, or nil.
func (image *Image) Class(name string) *LinkedClass { return image.classes[name] }

// Program returns the linked program of a package, or nil.
func (image *Image) Program(pkg string) *Prog { return image.progs[pkg] }

// Programs returns the linked programs in the order they were given.
func (image *Image) Programs() []*Prog { return image.order }

// MethodInfo describes a method of a class for callers.
type MethodInfo struct {
	Func   *LinkedFunc
	Static bool
	Public bool
	Test   bool
}

// Method returns a method of a class.
func (c *LinkedClass) Method(name string) (MethodInfo, bool) {
	f, ok := c.Methods[name]
	return MethodInfo{Func: f, Static: c.static[name], Public: c.public[name], Test: c.test[name]}, ok
}
