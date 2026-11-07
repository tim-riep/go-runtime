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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/tim-riep/go-runtime/lang/build"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/lang/testrun"
	"github.com/tim-riep/go-runtime/lang/types"
	"github.com/tim-riep/go-runtime/vm"
)

// sourceExts are the extensions of source files.
var sourceExts = []string{".class", ".interface", ".include"}

// dispatchLang runs the commands for source files, which need no
// database. It reports false for other commands.
func (c *cli) dispatchLang(ctx context.Context, cmd string, args []string) (int, bool) {
	switch cmd {
	case "fmt":
		return c.fmt(args), true
	case "build":
		if !c.need(args, 1, "build <package directory>") {
			return exitUsage, true
		}
		return c.build(args[0]), true
	case "test":
		if !c.need(args, 1, "test <package directory>") {
			return exitUsage, true
		}
		return c.test(ctx, args[0]), true
	case "run":
		if len(args) != 3 && len(args) != 4 {
			fmt.Fprintln(c.stderr, "usage: rt run <package directory> <class> <method> [arguments as JSON array]")
			return exitUsage, true
		}
		return c.run(ctx, args), true
	}
	return 0, false
}

// fmt formats source files in place, or with --check lists those that are
// not in canonical form.
func (c *cli) fmt(args []string) int {
	if len(args) == 0 {
		args = []string{"."}
	}
	var files []string
	for _, a := range args {
		st, err := os.Stat(a)
		if err != nil {
			return c.fail(err)
		}
		if !st.IsDir() {
			files = append(files, a)
			continue
		}
		err = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && slices.Contains(sourceExts, filepath.Ext(p)) {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return c.fail(err)
		}
	}
	code := exitOK
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return c.fail(err)
		}
		out, errs := syntax.Format(f, string(b))
		if len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(c.stderr, "%s:%d:%d: %s\n", f, e.Pos.Line, e.Pos.Col, e.Msg)
			}
			code = exitInvalid
			continue
		}
		if out == string(b) {
			continue
		}
		fmt.Fprintln(c.stdout, f)
		if c.check {
			if code == exitOK {
				code = exitCheck
			}
			continue
		}
		if err := os.WriteFile(f, []byte(out), 0o644); err != nil {
			return c.fail(err)
		}
	}
	return code
}

// compile builds a package directory and prints its diagnostics. It
// returns nil when the build failed.
func (c *cli) compile(dir string) (*build.Result, int) {
	r, err := build.Dir(dir)
	if err != nil {
		return nil, c.fail(err)
	}
	if c.asJSON {
		return r, exitOK
	}
	for _, d := range r.Diagnostics {
		w := c.stdout
		if d.Severity == types.Error {
			w = c.stderr
		}
		fmt.Fprintln(w, d)
	}
	if !r.OK() {
		fmt.Fprintf(c.stderr, "build of %s failed with %d error(s)\n", r.Package.Manifest.ID, len(r.Errors()))
		return nil, exitInvalid
	}
	return r, exitOK
}

// build compiles a package and reports the diagnostics.
func (c *cli) build(dir string) int {
	r, code := c.compile(dir)
	if c.asJSON && r != nil {
		diags := r.Diagnostics
		if diags == nil {
			diags = []types.Diagnostic{}
		}
		c.json(map[string]any{"package": r.Package.Manifest.ID, "ok": r.OK(), "diagnostics": diags})
		if !r.OK() {
			return exitInvalid
		}
		return exitOK
	}
	if r == nil {
		return code
	}
	n := 0
	for _, k := range r.Program.Classes {
		if !k.Kernel {
			n++
		}
	}
	fmt.Fprintf(c.stdout, "build of %s succeeded: %d classes and interfaces\n", r.Package.Manifest.ID, n)
	return exitOK
}

// run calls a public static method of a package and prints its result as
// JSON.
func (c *cli) run(ctx context.Context, args []string) int {
	r, code := c.compile(args[0])
	if r == nil {
		return code
	}
	p, err := r.Compile(false)
	if err != nil {
		return c.fail(err)
	}
	var list []json.RawMessage
	if len(args) == 4 {
		if err := json.Unmarshal([]byte(args[3]), &list); err != nil {
			fmt.Fprintf(c.stderr, "error: the arguments must be a JSON array: %v\n", err)
			return exitUsage
		}
	}
	m, err := vm.New(p, vm.Options{Log: logWriter{c.stderr}})
	if err != nil {
		return c.fail(err)
	}
	out, err := m.CallJSON(ctx, args[1], args[2], list)
	if err != nil {
		fmt.Fprintf(c.stderr, "error: %v\n", err)
		return exitError
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, out, "", "  ") == nil {
		out = pretty.Bytes()
	}
	fmt.Fprintln(c.stdout, string(out))
	return exitOK
}

// logWriter prints log statements of programs.
type logWriter struct{ w io.Writer }

func (l logWriter) Log(level, text string) { fmt.Fprintf(l.w, "%s: %s\n", level, text) }

// test runs the test classes of a package.
func (c *cli) test(ctx context.Context, dir string) int {
	r, code := c.compile(dir)
	if r == nil {
		return code
	}
	p, err := r.Compile(true)
	if err != nil {
		return c.fail(err)
	}
	results, err := testrun.Run(ctx, p, vm.Options{})
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		if results == nil {
			results = []testrun.Result{}
		}
		c.json(results)
	}
	failed := 0
	for _, t := range results {
		if !t.Passed {
			failed++
		}
		if c.asJSON {
			continue
		}
		if t.Passed {
			fmt.Fprintf(c.stdout, "PASS %s.%s (%s)\n", t.Class, t.Method, t.Duration.Round(time.Microsecond))
			continue
		}
		where := ""
		if t.File != "" {
			where = fmt.Sprintf(" at %s:%d", t.File, t.Line)
		}
		fmt.Fprintf(c.stdout, "FAIL %s.%s: %s%s\n", t.Class, t.Method, t.Message, where)
	}
	if !c.asJSON {
		fmt.Fprintf(c.stdout, "%d tests, %d passed, %d failed\n", len(results), len(results)-failed, failed)
	}
	if failed > 0 {
		return exitError
	}
	return exitOK
}
