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
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

const allRole = model.AllAuthorizationsRole

// TestAllAuthorizationsRoleDefined checks the role in a new database: a
// role of the kernel with texts in the languages set up, which nothing can
// change.
func TestAllAuthorizationsRoleDefined(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		r := s.Roles[allRole]
		if r == nil || r.Owner != model.Kernel || r.Composite || len(r.Authorizations) != 0 || r.Texts["en"].Label != "All authorizations" ||
			!r.Texts["en"].Description.Valid() || len(r.Texts) != 1 {
			t.Fatalf("role after init: %+v", r)
		}
		if _, err := k.Languages().Add(ctx, "de", ""); err != nil {
			t.Fatal(err)
		}
		if s, _ := k.Schema(ctx); s.Roles[allRole].Texts["de"].Label != "Alle Berechtigungen" {
			t.Errorf("German text: %+v", s.Roles[allRole].Texts)
		}

		roles := k.Roles()
		var ia *rterr.InvalidArgument
		role := `{ "name": "/RT/ALL_AUTHORIZATIONS", "texts": { "en": { "label": "Mine" } }, "authorizations": [] }`
		if _, err := roles.Import(ctx, []byte(role), "all.json", kernel.RoleOptions{}); !errors.As(err, &ia) {
			t.Errorf("import with the name: %v", err)
		}
		if err := roles.Update(ctx, &model.Role{Name: allRole, Texts: map[string]model.Label{"en": {Label: "x"}}}, kernel.RoleOptions{}); !errors.As(err, &ia) {
			t.Errorf("update: %v", err)
		}
		if err := roles.Delete(ctx, allRole, kernel.RoleOptions{Confirm: true}); !errors.As(err, &ia) || !strings.Contains(err.Error(), "role of the kernel") {
			t.Errorf("delete: %v", err)
		}
		if err := roles.Create(ctx, &model.Role{Name: "/LOCAL/WITH_ALL", Texts: map[string]model.Label{"en": {Label: "x"}}, Composite: true, Roles: []string{allRole}},
			kernel.RoleOptions{}); err == nil || !strings.Contains(err.Error(), "is no member of composite roles") {
			t.Errorf("local composite role with it: %v", err)
		}

		// A package cannot define roles in the namespace of the kernel.
		dir := t.TempDir()
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRSNEAK", "version": "1.0.0" }`))
		testutil.WriteFile(t, dir, "schema/roles/ALL_AUTHORIZATIONS.json", []byte(role))
		_, err = kernel.LoadDir(dir)
		var nv *rterr.NamespaceViolation
		if !errors.As(err, &nv) || nv.Name != allRole {
			t.Errorf("package with a role of the kernel: %v", err)
		}
		d, err := roles.Get(ctx, allRole)
		// It covers the objects of TRBASE, installed with the kernel.
		if err != nil || !d.All || len(d.Authorizations) != 3 || d.Authorizations[0].Authorization.Object != "/TRBASE/ROLE_ADMIN" || !d.Authorizations[0].AllActivities {
			t.Errorf("covered objects: %+v %v", d, err)
		}
	})
}

// auditRecorder keeps the records that reach a handler whose level is
// error, as the audit log does.
type auditRecorder struct {
	records *[]slog.Record
}

func (h auditRecorder) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelError }
func (h auditRecorder) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, r)
	return nil
}
func (h auditRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h auditRecorder) WithGroup(string) slog.Handler      { return h }

// recordAudit makes the default logger keep its records at level error,
// and returns the messages of the audit records it got.
func recordAudit(t *testing.T) func() []string {
	var records []slog.Record
	old := slog.Default()
	slog.SetDefault(slog.New(auditRecorder{&records}))
	t.Cleanup(func() { slog.SetDefault(old) })
	return func() []string {
		var out []string
		for _, r := range records {
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "audit" {
					out = append(out, r.Message)
				}
				return true
			})
		}
		records = nil
		return out
	}
}

// TestAllAuthorizationsRoleRestored changes the role directly in the
// database: the kernel restores it when it starts and after operations,
// keeps its assignments and records it.
func TestAllAuthorizationsRoleRestored(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			audits := recordAudit(t)
			k, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			withRoleKernel(t, k)
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: allRole, Tenant: "ACME"}, kernel.RoleOptions{AllAuthorizations: true}); err != nil {
				t.Fatal(err)
			}
			if got := audits(); len(got) != 1 || got[0] != "all authorizations assigned" {
				t.Errorf("audit of the assignment: %v", got)
			}
			k.Close()
			exec := func(sql string) {
				t.Helper()
				k, err := kernel.Open(ctx, db.DSN)
				if err != nil {
					t.Fatal(err)
				}
				defer k.Close()
				if _, err := k.DB().ExecContext(ctx, sql); err != nil {
					t.Fatalf("%s: %v", sql, err)
				}
			}
			for _, damage := range []string{
				`DELETE FROM "/RT/ROLE_TEXTS" WHERE "ROLE_NAME" = '/RT/ALL_AUTHORIZATIONS'`,
				`UPDATE "/RT/ROLE_TEXTS" SET "LABEL" = 'Nothing' WHERE "ROLE_NAME" = '/RT/ALL_AUTHORIZATIONS' AND "LANGUAGE" = 'en'`,
				`INSERT INTO "/RT/ROLE_AUTHORIZATIONS" ("ROLE_NAME", "POSITION", "OBJECT_NAME") VALUES ('/RT/ALL_AUTHORIZATIONS', 1, '/TRFIN/COMPANY')`,
			} {
				// Opening a kernel restores the role, the first time already
				// in exec, so damage it between two kernels.
				exec(damage)
			}
			// Delete all its rows with a connection of its own, then start.
			raw, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			audits()
			for _, sql := range []string{
				`DELETE FROM "/RT/ROLE_TEXTS" WHERE "ROLE_NAME" = '/RT/ALL_AUTHORIZATIONS'`,
				`DELETE FROM "/RT/ROLES" WHERE "ROLE_NAME" = '/RT/ALL_AUTHORIZATIONS'`,
			} {
				if _, err := raw.DB().ExecContext(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			raw.Close()
			k, err = kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer k.Close()
			if got := audits(); len(got) != 1 || got[0] != "role with all authorizations restored to its defined form" {
				t.Errorf("audit of the restoration: %v", got)
			}
			s, err := k.Schema(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if r := s.Roles[allRole]; r == nil || r.Texts["en"].Label != "All authorizations" || r.Texts["de"].Label != "Alle Berechtigungen" || len(r.Authorizations) != 0 {
				t.Errorf("restored role: %+v", r)
			}
			if list, _ := k.Roles().Assignments(ctx, "anna", "ACME"); len(list) != 1 || list[0].Role != allRole {
				t.Errorf("assignments after the restoration: %+v", list)
			}
			history, _ := k.Roles().History(ctx, allRole, "")
			n := 0
			for _, h := range history {
				if h.Kind == kernel.ChangeAllRestored {
					n++
				}
			}
			// One for each of the three damages and one for the deletion.
			if n != 4 || history[len(history)-1].OldState.Valid() {
				t.Errorf("%d restorations in the history: %+v", n, history[len(history)-1])
			}

			// An operation restores it too.
			if _, err := k.DB().ExecContext(ctx, `DELETE FROM "/RT/ROLE_TEXTS" WHERE "ROLE_NAME" = '/RT/ALL_AUTHORIZATIONS' AND "LANGUAGE" = 'de'`); err != nil {
				t.Fatal(err)
			}
			if _, err := k.Uninstall(ctx, "TROTTO", kernel.Options{Confirm: true}); err != nil {
				t.Fatal(err)
			}
			if s, _ := k.Schema(ctx); s.Roles[allRole].Texts["de"].Label != "Alle Berechtigungen" {
				t.Errorf("after an uninstall: %+v", s.Roles[allRole].Texts)
			}
		})
	}
}
