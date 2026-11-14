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
	"fmt"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/native"
)

// notAuthorizedTexts are the texts of /RT/NOT_AUTHORIZED by language.
var notAuthorizedTexts = map[string][2]string{
	"en": {"no authorization for %s", "no authorization for %s: the execution has no user; rt run takes one with --user"},
	"de": {"keine Berechtigung für %s", "keine Berechtigung für %s: die Ausführung hat keinen Benutzer; rt run nimmt einen mit --user"},
}

// notAuthorizedText returns the text of a failed check in a language,
// else in English.
func notAuthorizedText(lang, object string, withUser bool) string {
	t, ok := notAuthorizedTexts[lang]
	if !ok {
		t = notAuthorizedTexts["en"]
	}
	i := 0
	if !withUser {
		i = 1
	}
	return fmt.Sprintf(t[i], object)
}

// authorizations returns the effective authorizations of the user of the
// execution in its tenant today, loaded once per execution; nil without
// user.
func (x *execution) authorizations() (*authorization.Effective, error) {
	if x.authLoaded {
		return x.auth, nil
	}
	user, ok := x.opts.User.Get()
	if ok {
		e, err := x.k.EffectiveAuthorizations(x.ctx, user, x.tenant, x.k.now())
		if err != nil {
			return nil, err
		}
		x.auth = e
	}
	x.authLoaded = true
	return x.auth, nil
}

// CheckAuthorization checks an authorization of the user of the execution.
func (x *execution) CheckAuthorization(c authorization.Check, at native.CheckLocation, mode native.CheckMode) (bool, error) {
	e, err := x.authorizations()
	if err != nil {
		return false, err
	}
	ok := e.Check(c)
	// Only failed statements and traces are recorded, so that a check
	// stays in memory.
	failed := !ok && (mode == native.CheckStatement || mode == native.CheckEntity)
	if user, has := x.opts.User.Get(); has && (failed || e.Trace != nil) {
		r := checkRecord{user: user, tenant: x.tenant, at: x.k.timestamp(), mode: mode, check: c, allowed: ok, site: at}
		if failed {
			x.failed = &r
		}
		if e.Tracing(r.at) {
			x.traced = append(x.traced, r)
		}
	}
	if !ok && mode == native.CheckStatement {
		return false, x.notAuthorized(c)
	}
	return ok, nil
}

// notAuthorized returns the error of a failed check in the language of
// the execution.
func (x *execution) notAuthorized(c authorization.Check) *native.NotAuthorized {
	return &native.NotAuthorized{Check: c, Text: notAuthorizedText(x.Language(), c.Object, x.opts.User.Valid())}
}
