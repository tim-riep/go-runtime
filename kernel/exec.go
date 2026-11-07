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

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/lang/build"
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/vm"
)

// Image loads the compiled code of all installed packages and links it
// with the classes of the kernel.
func (k *Kernel) Image(ctx context.Context) (*vm.Image, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	return k.loadImage(ctx, k.db)
}

func (k *Kernel) loadImage(ctx context.Context, q dialect.Querier) (*vm.Image, error) {
	s, err := k.repo.Load(ctx, q)
	if err != nil {
		return nil, err
	}
	gen, err := k.repo.Generation(ctx, q)
	if err != nil {
		return nil, err
	}
	programs, err := k.storedPrograms(ctx, q)
	if err != nil {
		return nil, err
	}
	kernel, err := build.KernelProgram()
	if err != nil {
		return nil, err
	}
	progs := []*bytecode.Program{kernel}
	for _, id := range installOrder(s, s.PackageIDs()) {
		if p := programs[id]; p != nil {
			progs = append(progs, p)
		}
	}
	return vm.Link(gen, progs...)
}
