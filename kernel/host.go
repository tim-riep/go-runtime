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
	"errors"
	"io/fs"
	"time"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// The execution is the host of native methods.

func (x *execution) Tenant() string { return x.tenant }

func (x *execution) Language() string {
	if x.opts.Language == "" {
		return model.DefaultLanguage
	}
	return x.opts.Language
}

func (x *execution) Now() time.Time { return x.k.now().UTC() }

func (x *execution) AfterCommit(session int, e native.Effect) error {
	return errors.New("kernel: effects are not supported yet")
}

func (x *execution) Assets(pkg model.PackageID, repo string, lang model.Optional[string]) (fs.FS, error) {
	return nil, errors.New("kernel: assets are not supported yet")
}

// SetClock replaces the clock of the kernel, for tests of code with the
// capability clock.
func (k *Kernel) SetClock(now func() time.Time) { k.now = now }
