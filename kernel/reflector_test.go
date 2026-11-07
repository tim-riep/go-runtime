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

package kernel_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// TestReflector reads authorization objects and activities with their
// texts in the languages set up.
func TestReflector(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		o, err := k.AuthorizationObject(ctx, "/TRFIN/COMPANY")
		if err != nil {
			t.Fatal(err)
		}
		if o.Package != "TRFIN" || o.Texts["de"].Label != "Stammdaten Firma" || o.Texts["en"].Description != model.Some("Reading and changing companies by company and country") ||
			len(o.Fields) != 3 || len(o.Activities) != 5 || o.Activities[4].Code != "EXPORT" || o.Activities[4].Texts["de"].Label != "Exportieren" {
			t.Fatalf("object: %+v", o)
		}
		country := o.Fields[1]
		if country.Activity || country.DataElement != model.Some("/TRBASE/COUNTRY") || country.Domain != "/TRBASE/COUNTRY_CODE" ||
			country.Type != model.Char || country.Length != model.Some(2) || country.Labels["de"].Medium == "" || len(country.Values) != 0 {
			t.Errorf("COUNTRY: %+v", country)
		}
		act := o.Fields[2]
		if !act.Activity || act.DataElement != model.Some(model.ActivityDomain) || act.Labels["de"].Medium != "Aktivität" ||
			act.Length != model.Some(16) || len(act.Values) != 5 || act.Values[0].Value != "READ" || act.Values[0].Texts["de"].Label != "Lesen" {
			t.Errorf("ACTIVITY: %+v", act)
		}
		status, err := k.AuthorizationObject(ctx, "/TRFIN/STATUS_SCOPE")
		if err != nil {
			t.Fatal(err)
		}
		f := status.Fields[0]
		// The object has no German text; a domain field has no labels and
		// fixed values without texts per language.
		if _, ok := status.Texts["de"]; ok || status.Texts["en"].Description.Valid() || len(status.Activities) != 0 ||
			f.DataElement.Valid() || len(f.Labels) != 0 || len(f.Values) != 3 || f.Values[1].Name != model.Some("BLOCKED") || f.Values[1].Texts != nil {
			t.Errorf("STATUS_SCOPE: %+v", status)
		}
		var nf *rterr.NotFound
		if _, err := k.AuthorizationObject(ctx, "/TRFIN/NOPE"); !errors.As(err, &nf) {
			t.Errorf("unknown object: %v", err)
		}
		list, err := k.AuthorizationObjects(ctx, "TRFIN")
		if err != nil || len(list) != 2 || list[0].Name != "/TRFIN/COMPANY" || list[1].Name != "/TRFIN/STATUS_SCOPE" {
			t.Errorf("objects of TRFIN: %v %v", list, err)
		}
		if all, err := k.AuthorizationObjects(ctx, ""); err != nil || len(all) != 3 || all[2].Name != "/TROTTO/SYNC" {
			t.Errorf("all objects: %v %v", all, err)
		}
		acts, err := k.Activities(ctx)
		if err != nil || len(acts) != 13 || acts[12].Code != "ADMINISTER" || acts[12].Texts["de"].Description == "" {
			t.Errorf("activities: %v %v", acts, err)
		}
		b, err := json.MarshalIndent(o, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		testutil.Golden(t, "reflector-TRFIN-COMPANY.json", string(b)+"\n")
	})
}

// TestReflectorLanguages gives texts only in the languages set up.
func TestReflectorLanguages(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		acts, err := k.Activities(ctx)
		if err != nil || len(acts) != 13 || len(acts[0].Texts) != 1 || acts[0].Texts["en"].Label != "Read" {
			t.Errorf("activities in English only: %v %v", acts, err)
		}
		var ni *rterr.NotInstalled
		if _, err := k.AuthorizationObjects(ctx, "TRFIN"); !errors.As(err, &ni) {
			t.Errorf("objects of a package not installed: %v", err)
		}
	})
}
