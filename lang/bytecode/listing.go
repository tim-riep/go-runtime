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

package bytecode

import (
	"fmt"
	"strings"
)

// Source returns the lines of a source file of a program, nil if unknown.
type Source func(path string) []string

// Listing lists the instructions of a function of p like Disassemble,
// with comments that name what operands refer to: functions, classes,
// string constants, types, transformations and database statements. A
// line of the source file precedes the instructions it compiled to. The
// exception handlers follow the code. imports returns the program in a
// slot of p's references, nil if not known; source may be nil.
func Listing(p *Program, f *Func, imports func(slot int) *Program, source Source) string {
	in := func(ref Ref) *Program {
		if RefSlot(ref) == 0 {
			return p
		}
		if imports == nil {
			return nil
		}
		return imports(RefSlot(ref))
	}
	fn := func(ref Ref) string {
		if q := in(ref); q != nil && RefIndex(ref) < len(q.Funcs) {
			return q.Funcs[RefIndex(ref)].Name
		}
		return fmt.Sprintf("function %d:%d", RefSlot(ref), RefIndex(ref))
	}
	class := func(ref Ref) string {
		if ref < 0 {
			return "any exception"
		}
		if q := in(ref); q != nil && RefIndex(ref) < len(q.Classes) {
			return q.Classes[RefIndex(ref)].Name
		}
		return fmt.Sprintf("class %d:%d", RefSlot(ref), RefIndex(ref))
	}
	typ := func(t int) string {
		if t >= 0 && t < len(p.Types) && p.Types[t].Name != "" {
			return p.Types[t].Name
		}
		return ""
	}
	note := func(op Op, a []int32) string {
		switch op {
		case CALL, MAKE_FN:
			return fn(int(a[0]))
		case CALL_IFACE, NEW_OBJ, STATICS:
			return class(int(a[0]))
		case CONST_STR:
			if int(a[0]) < len(p.Strings) {
				s := p.Strings[a[0]]
				if len(s) > 60 {
					s = s[:57] + "..."
				}
				return fmt.Sprintf("%q", s)
			}
		case ZERO, NEW_ARRAY, NEW_MAP, ARRAY_LIT, MAP_LIT, CHECK, COPY, EQ_VALUE:
			return typ(int(a[0]))
		case NEW_STRUCT:
			if int(a[0]) < len(p.Layouts) {
				return p.Layouts[a[0]].Name
			}
		case XFORM:
			if int(a[0]) < len(p.Transforms) {
				t := p.Transforms[a[0]]
				return t.Name + " (" + t.Format + ")"
			}
		case AUTH_CHECK:
			if int(a[0]) < len(p.AuthChecks) {
				return p.AuthChecks[a[0]].Object
			}
		case EXT_GET, EXT_CALL, EXT_INFO:
			if int(a[0]) < len(p.Extensions) {
				return "extension point " + p.Extensions[a[0]].Name
			}
		case DB_QUERY, DB_CURSOR:
			if int(a[0]) < len(p.Queries) {
				q := p.Queries[a[0]]
				return q.Kind.String() + " " + q.Table
			}
		case LOG:
			if a[0] >= 0 && int(a[0]) < len(logLevels) {
				return logLevels[a[0]]
			}
		}
		return ""
	}

	var b strings.Builder
	lines := map[int]Line{}
	for _, l := range f.Lines {
		lines[l.PC] = l
	}
	last := Line{File: -1}
	for pc := 0; pc < len(f.Code); {
		if l, ok := lines[pc]; ok && (l.Line != last.Line || l.File != last.File) {
			last = l
			path := ""
			if l.File >= 0 && l.File < len(p.Files) {
				path = p.Files[l.File].Path
			}
			text := ""
			if source != nil {
				if src := source(path); l.Line >= 1 && l.Line <= len(src) {
					text = "  " + strings.TrimSpace(src[l.Line-1])
				}
			}
			fmt.Fprintf(&b, "     ; %s:%d%s\n", path, l.Line, text)
		}
		op := Op(f.Code[pc])
		n := op.Operands()
		end := min(pc+1+n, len(f.Code))
		args := f.Code[pc+1 : end]
		var ins strings.Builder
		fmt.Fprintf(&ins, "%4d %s", pc, op)
		for _, a := range args {
			fmt.Fprintf(&ins, " %d", a)
		}
		if c := note(op, args); c != "" && len(args) == n {
			fmt.Fprintf(&b, "%-32s ; %s\n", ins.String(), c)
		} else {
			b.WriteString(ins.String() + "\n")
		}
		pc = end
	}
	for _, h := range f.Handlers {
		fmt.Fprintf(&b, "     catch %d..%d -> %d: %s\n", h.Start, h.End, h.Target, class(h.Class))
	}
	return b.String()
}

var logLevels = []string{"debug", "info", "warning", "error"}
