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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/vm"
)

// ODataMetadataPoint is the extension point of TRBASE that the kernel runs
// whenever it builds the metadata of a service; its implementations
// annotate the metadata.
const ODataMetadataPoint = "/TRBASE/ODATA_METADATA"

// MetadataExtensionError reports an implementation of the extension point
// of the metadata that failed or broke its rules. Clients get 500, never
// the metadata without the implementation.
type MetadataExtensionError struct {
	Service        string
	Implementation string
	Package        model.PackageID
	Reason         string
}

func (e *MetadataExtensionError) Error() string {
	return fmt.Sprintf("the implementation %s of package %s of extension point %s failed for service %s: %s", e.Implementation, e.Package,
		ODataMetadataPoint, e.Service, e.Reason)
}

// metadataImplementations returns the active implementations of the
// extension point of the metadata for a service in their order, and the
// generation of the activation they come from. The point is activated
// only globally.
func (k *Kernel) metadataImplementations(ctx context.Context, g *generation, svc *model.Service) ([]*model.Implementation, int64, error) {
	rp := g.extensionRegistry().points[ODataMetadataPoint]
	if rp == nil || len(rp.impls) == 0 {
		return nil, 0, nil
	}
	n, err := k.repo.ImplementationGeneration(ctx, k.db)
	if err != nil {
		return nil, 0, err
	}
	act, err := k.cachedActivation(ctx)
	if err != nil {
		return nil, 0, err
	}
	values := map[string]string{"PACKAGE_ID": string(svc.Owner), "SERVICE_NAME": svc.Name}
	var out []*model.Implementation
	for _, ri := range rp.impls {
		if _, ok := rp.matches(ri, values); ok && act.active(ri.impl.Name, "") {
			out = append(out, ri.impl)
		}
	}
	return out, n, nil
}

// extendMetadata runs the implementations on the metadata in an execution
// without tenant and user in the language of the metadata, which only
// reads and has a short time limit. After every implementation the kernel
// checks what it changed.
func (k *Kernel) extendMetadata(ctx context.Context, g *generation, svc *model.Service, md *odata.Metadata, impls []*model.Implementation,
	timeout time.Duration) (*odata.Metadata, error) {
	if len(impls) == 0 {
		return md, nil
	}
	// The metadata as the implementations see it, so that lists without
	// entries compare equal.
	base, err := odata.DecodeMetadata(md.Encode())
	if err != nil {
		return nil, err
	}
	classes := make([]string, len(impls))
	for i, impl := range impls {
		classes[i] = impl.Class
	}
	fail := func(i int, reason string) error {
		return &MetadataExtensionError{Service: svc.Name, Implementation: impls[i].Name, Package: impls[i].Owner, Reason: reason}
	}
	current, done := base, 0
	opts := ExecOptions{Language: md.Language, ReadOnly: true, End: RollbackUnlessCommitted, Timeout: timeout,
		VM: vm.Options{Log: k.requestLog(model.ODataPath)}}
	err = k.executeWith(ctx, g, opts, func(ctx context.Context, m *vm.Machine) error {
		_, err := m.ChainJSON(ctx, ODataMetadataPoint, classes, "ADJUST", false, base.Encode(), func(i int, out json.RawMessage) error {
			done = i + 1
			next, err := odata.DecodeMetadata(out)
			if err != nil {
				return fail(i, err.Error())
			}
			if err := odata.CheckExtension(current, next, string(impls[i].Owner)); err != nil {
				return fail(i, err.Error())
			}
			current = next
			return nil
		})
		return err
	})
	var me *MetadataExtensionError
	switch {
	case errors.As(err, &me):
		return nil, me
	case err != nil:
		return nil, fail(min(done, len(impls)-1), strings.TrimSpace(err.Error()))
	}
	return current, nil
}
