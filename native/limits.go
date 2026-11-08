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

package native

// ArchiveLimits restrict what native methods unpack from zip and gzip
// data, measured while unpacking, not by the sizes an archive claims.
type ArchiveLimits struct {
	Total   int64 // bytes unpacked in all
	Entry   int64 // bytes of one entry
	Entries int   // number of entries
	// Ratio limits unpacked to packed bytes once RatioFrom bytes are
	// unpacked.
	Ratio     int64
	RatioFrom int64
}

// WorkbookLimits restrict workbooks in xlsx format.
type WorkbookLimits struct {
	Archive ArchiveLimits // the parts of the file
	Sheets  int
	Rows    int
	Columns int
	Cells   int64
}

// Limits restrict the formats that native methods read.
type Limits struct {
	Archive  ArchiveLimits
	Workbook WorkbookLimits
}

// DefaultLimits are the limits unless configured otherwise.
var DefaultLimits = Limits{
	Archive: ArchiveLimits{Total: 256 << 20, Entry: 64 << 20, Entries: 10_000, Ratio: 1000, RatioFrom: 1 << 20},
	Workbook: WorkbookLimits{Archive: ArchiveLimits{Total: 256 << 20, Entry: 64 << 20, Entries: 10_000, Ratio: 1000, RatioFrom: 1 << 20},
		Sheets: 256, Rows: 1_048_576, Columns: 16_384, Cells: 10_000_000},
}

// OrDefault returns the limits with zero fields taken from DefaultLimits.
func (l Limits) OrDefault() Limits {
	fill := func(a *ArchiveLimits, d ArchiveLimits) {
		if a.Total <= 0 {
			a.Total = d.Total
		}
		if a.Entry <= 0 {
			a.Entry = d.Entry
		}
		if a.Entries <= 0 {
			a.Entries = d.Entries
		}
		if a.Ratio <= 0 {
			a.Ratio = d.Ratio
		}
		if a.RatioFrom <= 0 {
			a.RatioFrom = d.RatioFrom
		}
	}
	d := DefaultLimits
	fill(&l.Archive, d.Archive)
	fill(&l.Workbook.Archive, d.Workbook.Archive)
	if l.Workbook.Sheets <= 0 {
		l.Workbook.Sheets = d.Workbook.Sheets
	}
	if l.Workbook.Rows <= 0 {
		l.Workbook.Rows = d.Workbook.Rows
	}
	if l.Workbook.Columns <= 0 {
		l.Workbook.Columns = d.Workbook.Columns
	}
	if l.Workbook.Cells <= 0 {
		l.Workbook.Cells = d.Workbook.Cells
	}
	return l
}
