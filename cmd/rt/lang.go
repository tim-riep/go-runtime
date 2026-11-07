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
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/tim-riep/go-runtime/lang/syntax"
)

// sourceExts are the extensions of source files.
var sourceExts = []string{".class", ".interface", ".include"}

// dispatchLang runs the commands for source files, which need no
// database. It reports false for other commands.
func (c *cli) dispatchLang(ctx context.Context, cmd string, args []string) (int, bool) {
	switch cmd {
	case "fmt":
		return c.fmt(args), true
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
