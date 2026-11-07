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

package repo

import (
	"context"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// TEffectFailures holds effects that failed after the commit of their
// session.
const TEffectFailures = model.KernelNamespace + "EFFECT_FAILURES"

// EffectFailure is an effect that failed after a commit.
type EffectFailure struct {
	ID       string                 `json:"id"`
	FailedAt time.Time              `json:"failedAt"`
	Tenant   model.Optional[string] `json:"tenant"`
	Kind     string                 `json:"kind"`
	Payload  string                 `json:"payload"`
	Error    string                 `json:"error"`
	Attempts int                    `json:"attempts"`
}

// InsertEffectFailure records a failed effect.
func (r *Repo) InsertEffectFailure(ctx context.Context, q dialect.Querier, e EffectFailure) error {
	return r.insert(ctx, q, TEffectFailures, row{"EFFECT_ID": e.ID, "FAILED_AT": e.FailedAt, "TENANT": val(e.Tenant), "KIND": e.Kind,
		"PAYLOAD": e.Payload, "ERROR_TEXT": e.Error, "ATTEMPTS": int64(e.Attempts)})
}

// EffectFailures returns the failed effects, oldest first.
func (r *Repo) EffectFailures(ctx context.Context, q dialect.Querier) ([]EffectFailure, error) {
	rows, err := r.selectRows(ctx, q, TEffectFailures, []string{"EFFECT_ID", "FAILED_AT", "TENANT", "KIND", "PAYLOAD", "ERROR_TEXT", "ATTEMPTS"}, "FAILED_AT", "EFFECT_ID")
	if err != nil {
		return nil, err
	}
	out := make([]EffectFailure, 0, len(rows))
	for _, e := range rows {
		out = append(out, EffectFailure{ID: str(e["EFFECT_ID"]), FailedAt: tm(e["FAILED_AT"]), Tenant: opt(e["TENANT"]), Kind: str(e["KIND"]),
			Payload: str(e["PAYLOAD"]), Error: str(e["ERROR_TEXT"]), Attempts: num(e["ATTEMPTS"])})
	}
	return out, nil
}

// DeleteEffectFailure removes a failed effect.
func (r *Repo) DeleteEffectFailure(ctx context.Context, q dialect.Querier, id string) error {
	_, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(TEffectFailures)+" WHERE "+r.d.QuoteIdent("EFFECT_ID")+" = "+r.d.Placeholder(1), id)
	return err
}
