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

// Package testrun runs the test methods of the test classes of a program.
// Every test method runs on a new object in a new machine, so tests do not
// influence each other.
package testrun

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/vm"
)

// Result is the outcome of one test method.
type Result struct {
	Class    string        `json:"class"`
	Method   string        `json:"method"`
	Passed   bool          `json:"passed"`
	Message  string        `json:"message,omitempty"`
	File     string        `json:"file,omitempty"`
	Line     int           `json:"line,omitempty"`
	Duration time.Duration `json:"duration"`
	Log      []vm.LogEntry `json:"log,omitempty"`
}

// Run runs all test methods of the test classes of a package in an image,
// in the order of classes and methods.
func Run(ctx context.Context, image *vm.Image, pkg string, opts vm.Options) ([]Result, error) {
	return RunEach(ctx, image, pkg, opts, nil)
}

// Wrap prepares the execution of one test method, such as its database
// access, and calls run with the options for its machine.
type Wrap func(ctx context.Context, opts vm.Options, run func(vm.Options) error) error

// RunEach is Run with a wrap around every test method; nil runs them
// directly.
func RunEach(ctx context.Context, image *vm.Image, pkg string, opts vm.Options, wrap Wrap) ([]Result, error) {
	if wrap == nil {
		wrap = func(_ context.Context, o vm.Options, run func(vm.Options) error) error { return run(o) }
	}
	pr := image.Program(pkg)
	if pr == nil {
		return nil, errors.New("testrun: the image has no package " + pkg)
	}
	if !pr.P.Tests {
		return nil, errors.New("testrun: the program was compiled without test classes")
	}
	var out []Result
	for _, c := range pr.P.Classes {
		if !c.Test {
			continue
		}
		for _, m := range c.Methods {
			if !m.Test || bytecode.RefSlot(m.Func) != 0 {
				continue
			}
			rec := &vm.LogRecorder{}
			o := opts
			o.Log = rec
			r := Result{Class: c.Name, Method: m.Name}
			start := time.Now()
			err := wrap(ctx, o, func(o vm.Options) error {
				mach, err := vm.New(image, o)
				if err != nil {
					return err
				}
				err = mach.CallMethod(ctx, c.Name, m.Name)
				return errors.Join(err, mach.ReleaseHandles())
			})
			r.Duration = time.Since(start)
			r.Log = rec.Entries
			var e *vm.Exception
			switch {
			case err == nil:
				r.Passed = true
			case errors.As(err, &e):
				r.Message = fmt.Sprintf("%s: %s", e.Class, e.Text)
				if len(e.Trace) > 0 {
					r.File, r.Line = e.Trace[0].File, e.Trace[0].Line
				}
			default:
				r.Message = err.Error()
			}
			out = append(out, r)
		}
	}
	return out, nil
}
