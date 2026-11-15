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

package model

import (
	"maps"
	"slices"

	"github.com/tim-riep/go-runtime/rterr"
)

// NumberRange is a number range object of a package
// (schema/number-ranges/*.json): numbers of a fixed length with leading
// zeros, given from intervals that each tenant maintains, optionally per
// value of a subobject and per fiscal year.
type NumberRange struct {
	Name  string           `json:"name"`
	Owner PackageID        `json:"owner"`
	File  string           `json:"file"`
	Texts map[string]Label `json:"texts"`
	// Length is the number of digits, 1 to MaxNumberLength.
	Length int `json:"length"`
	// Subobject divides the intervals by the values of a field, e.g. a
	// company; none when absent.
	Subobject     Optional[NumberRangeSubobject] `json:"subobject"`
	PerFiscalYear bool                           `json:"perFiscalYear"`
	Buffering     NumberRangeBuffering           `json:"buffering"`
	// BlockSize is the number of numbers a process reserves at once with
	// buffering; 0 without.
	BlockSize int `json:"blockSize,omitempty"`
	// WarningPercent is the share of an interval after which the kernel
	// logs a warning.
	WarningPercent int            `json:"warningPercent"`
	Src            rterr.Location `json:"-"`
}

// NumberRangeSubobject is the field that divides the intervals of a
// number range object.
type NumberRangeSubobject struct {
	Name        string           `json:"name"`
	DataElement Optional[string] `json:"dataElement"`
	Domain      Optional[string] `json:"domain"`
}

// NumberRangeBuffering tells how a number range gives numbers.
type NumberRangeBuffering string

const (
	// BufferingNone gives numbers without gaps in the transaction of the
	// execution; a rollback returns them.
	BufferingNone NumberRangeBuffering = "none"
	// BufferingBuffered gives numbers from blocks a process reserves; gaps
	// are allowed.
	BufferingBuffered NumberRangeBuffering = "buffered"
)

// Limits of number ranges.
const (
	MaxNumberLength       = 20
	MaxNumberRangeBlock   = 10000
	DefaultWarningPercent = 90
)

// NumberRangeNames returns the names of all number range objects in sorted
// order.
func (s *Schema) NumberRangeNames() []string {
	return slices.Sorted(maps.Keys(s.NumberRanges))
}

// Languages returns the languages of the texts in sorted order.
func (n *NumberRange) Languages() []string { return slices.Sorted(maps.Keys(n.Texts)) }

func cloneNumberRange(n *NumberRange) *NumberRange {
	c := *n
	c.Texts = maps.Clone(n.Texts)
	return &c
}
