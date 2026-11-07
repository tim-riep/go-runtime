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

package pkgload

import (
	"io/fs"
	"path"
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// sourceDoc is a parsed source file.
type sourceDoc struct {
	file *syntax.File
	text string
	refs []NameRef
}

// sourceTypes maps the extensions of source files to their kinds and the
// declarations they must contain.
var sourceTypes = []struct {
	kind model.SourceKind
	file syntax.FileKind
	what string
}{
	{model.SourceClass, syntax.ClassFile, "a class"},
	{model.SourceInterface, syntax.InterfaceFile, "an interface"},
	{model.SourceInclude, syntax.IncludeFile, "an include"},
}

func init() {
	for _, st := range sourceTypes {
		ext := "." + string(st.kind)
		register(ObjectType{
			Kind:     string(st.kind),
			Document: func(p string) bool { return path.Dir(p) == SourceDir && strings.HasSuffix(p, ext) },
			Parse: func(file string, data []byte) (any, []*rterr.ValidationError) {
				return parseSource(file, data, st.file, st.what)
			},
			FileName: func(doc any) (string, bool) {
				_, local, ok := model.SplitName(doc.(*sourceDoc).file.Name().Name)
				return local + ext, ok
			},
			Definitions: func(doc any) []NameRef {
				n := doc.(*sourceDoc).file.Name()
				return []NameRef{{Name: n.Name, Line: n.Pos.Line, Column: n.Pos.Col}}
			},
			References: func(doc any) []NameRef { return doc.(*sourceDoc).refs },
			Build: func(l *loader, file string, doc any, pkg *model.Package) {
				d := doc.(*sourceDoc)
				pkg.Sources = append(pkg.Sources, model.Source{Name: d.file.Name().Name, Kind: st.kind, File: file, Text: d.text})
			},
		})
	}
}

// parseSource parses a source file and collects every global name it
// refers to for the namespace check.
func parseSource(file string, data []byte, kind syntax.FileKind, what string) (any, []*rterr.ValidationError) {
	text := string(data)
	f, errs := syntax.Parse(file, text)
	var out []*rterr.ValidationError
	for _, e := range errs {
		out = append(out, &rterr.ValidationError{Code: rterr.CodeSyntax, Location: rterr.Location{File: file, Line: e.Pos.Line, Column: e.Pos.Col}, Message: e.Msg})
	}
	if len(out) > 0 {
		return nil, out
	}
	if f.Kind != kind {
		return nil, []*rterr.ValidationError{{Code: rterr.CodeSource, Location: rterr.Location{File: file}, Message: "the file must contain " + what}}
	}
	d := &sourceDoc{file: f, text: text}
	def := f.Name().Pos
	toks, _ := syntax.Lex(text)
	for _, t := range toks {
		if t.Kind == syntax.GLOBAL && t.Pos != def {
			d.refs = append(d.refs, NameRef{Name: t.Text, Line: t.Pos.Line, Column: t.Pos.Col})
		}
		if t.Kind == syntax.STRING {
			d.refs = append(d.refs, placeholderRefs(t)...)
		}
	}
	return d, nil
}

// placeholderRefs returns the global names in the placeholders of a string
// literal.
func placeholderRefs(t syntax.Token) []NameRef {
	var out []NameRef
	x, _ := syntax.ParseExpr(t.Text)
	syntax.Inspect(x, func(e syntax.Expr) {
		if g, ok := e.(*syntax.GlobalExpr); ok {
			out = append(out, NameRef{Name: g.Name.Name, Line: t.Pos.Line, Column: t.Pos.Col + g.Name.Pos.Col - 1})
		}
	})
	return out
}

// readSource reads and parses a source file.
func (l *loader) readSource(t *ObjectType, file string) (any, bool) {
	data, err := fs.ReadFile(l.fsys, file)
	if err != nil {
		l.fail(rterr.CodeFile, l.loc(file), "cannot read file: %v", err)
		return nil, false
	}
	doc, errs := t.Parse(file, data)
	for _, e := range errs {
		e.Location.Package = l.id
		l.errs = append(l.errs, e)
	}
	return doc, len(errs) == 0
}

// checkLanguageVersion requires a supported language version when the
// package has source files.
func (l *loader) checkLanguageVersion(v int, docs []loadedDoc) {
	for _, d := range docs {
		if d.typ.Parse == nil {
			continue
		}
		switch {
		case v == 0:
			l.fail(rterr.CodeSource, l.loc(ManifestFile), "languageVersion is required because the package has source files in src/")
		case v > syntax.LanguageVersion:
			l.fail(rterr.CodeSource, l.loc(ManifestFile).At("/languageVersion"), "language version %d is not supported; the newest is %d", v, syntax.LanguageVersion)
		}
		return
	}
}
