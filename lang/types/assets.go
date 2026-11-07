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

package types

import (
	"slices"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// AssetRepositoryClass is the class of the kernel package TRBASE that
// opens asset repositories.
const AssetRepositoryClass = "/TRBASE/ASSET_REPOSITORY"

// assetRepository checks OPEN(pkg, repository) with two string literals:
// the repository exists and the package may read it, which the kernel
// checks again when the code runs. The repository is recorded as used, so
// an upgrade that removes it compiles the package again and fails. Other
// arguments are checked only when the code runs.
func (c *checker) assetRepository(x *syntax.CallExpr) {
	if len(x.Args) != 2 {
		return
	}
	pkg, ok1 := literal(x.Args[0].Value)
	id, ok2 := literal(x.Args[1].Value)
	if !ok1 || !ok2 {
		return
	}
	pos := x.Args[0].Value.Start()
	owner := model.PackageID(pkg)
	own := c.cfg.Package
	switch {
	case owner != own && !c.cfg.KernelPackage && !slices.Contains(c.cfg.Visible, owner):
		c.errorf(pos, CodeAsset, "%s may not read the asset repositories of package %s: %s does not require %s", own, pkg, own, pkg)
	case c.cfg.Schema.AssetRepositories[model.AssetRepositoryKey{Package: owner, ID: id}] == nil:
		c.errorf(x.Args[1].Value.Start(), CodeAsset, "package %s has no asset repository %s", pkg, id)
	default:
		c.use(pos, "asset-repository", pkg, id)
	}
}

// literal returns the text of a string literal without placeholders.
func literal(e syntax.Expr) (string, bool) {
	s, ok := e.(*syntax.StringExpr)
	if !ok {
		return "", false
	}
	text := ""
	for _, p := range s.Parts {
		if p.Expr != nil {
			return "", false
		}
		text += p.Text
	}
	return text, true
}

// ReflectorClass is the class of the kernel package TRBASE that describes
// objects of the schema.
const ReflectorClass = "/TRBASE/REFLECTOR"

// authorizationObject checks AUTHORIZATION_OBJECT(name) with a string
// literal that names an object of the own package, of a required one or of
// the kernel: the object exists, and it is recorded as used, so an upgrade
// that removes it compiles the package again and fails. Objects of other
// packages are not in the schema the package compiles against; they are
// looked up when the code runs.
func (c *checker) authorizationObject(x *syntax.CallExpr) {
	if len(x.Args) != 1 {
		return
	}
	name, ok := literal(x.Args[0].Value)
	if !ok {
		return
	}
	pos := x.Args[0].Value.Start()
	pkg, _, ok := model.SplitName(name)
	if !ok || pkg != c.cfg.Package && pkg != model.Kernel && !slices.Contains(c.cfg.Visible, pkg) {
		return
	}
	if c.cfg.Schema.AuthorizationObjects[name] == nil {
		c.errorf(pos, CodeAuthorization, "authorization object %s does not exist", name)
		return
	}
	c.use(pos, "authorization-object", name, "")
}
