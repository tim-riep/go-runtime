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

package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/tim-riep/go-runtime/internal/xslt"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/vm"
)

// sheets holds the compiled stylesheets of one generation. Each is
// compiled when it is used first and then shared by all executions.
type sheets struct {
	schema *model.Schema
	mu     sync.Mutex
	byName map[string]*sheet
}

type sheet struct {
	once sync.Once
	s    *xslt.Stylesheet
	err  error
}

func newSheets(s *model.Schema) *sheets { return &sheets{schema: s, byName: map[string]*sheet{}} }

func (c *sheets) get(name string) (*xslt.Stylesheet, error) {
	t := c.schema.Transformations[name]
	if t == nil {
		return nil, fmt.Errorf("kernel: the transformation %s does not exist", name)
	}
	c.mu.Lock()
	e := c.byName[name]
	if e == nil {
		e = &sheet{}
		c.byName[name] = e
	}
	c.mu.Unlock()
	e.once.Do(func() { e.s, e.err = xslt.Compile(t.Text) })
	return e.s, e.err
}

// Transform implements vm.Transformer for an execution.
func (x *execution) Transform(ctx context.Context, name string, source []byte, params map[string]string) ([]byte, error) {
	s, err := x.sheets.get(name)
	if err == nil {
		var out []byte
		if out, err = s.Transform(ctx, string(source), params, x.opts.MaxNativeInput); err == nil {
			return out, nil
		}
	}
	var xe *xslt.Error
	if errors.As(err, &xe) {
		return nil, &vm.TransformError{Line: xe.Line, Text: xe.Text}
	}
	return nil, err
}
