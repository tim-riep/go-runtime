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

// The rows of the balance sheet as a tree: every row has its LEVEL, and the
// rows below a node follow it with a greater level. TRUI 0.2 has no tree
// table, so the page shows the visible rows indented in a Table.

export interface StatementRow {
  LEVEL: number;
  NODE: string;
  PARENT_NODE: string | null;
  NODE_KIND: string;
  SIDE: string;
  ACCOUNT: string | null;
  LABEL: string | null;
  AMOUNT: string;
}

/** The key of a row: nodes by node, accounts by node and account. */
export function rowKey(row: StatementRow): string {
  return row.ACCOUNT === null ? `node:${row.NODE}` : `account:${row.NODE}:${row.ACCOUNT}`;
}

/** Whether the row at index i has rows below it. */
export function hasChildren(rows: readonly StatementRow[], i: number): boolean {
  const row = rows[i];
  const next = rows[i + 1];
  return row !== undefined && row.ACCOUNT === null && next !== undefined && next.LEVEL > row.LEVEL;
}

/** The keys of all nodes with rows below them. */
export function parentKeys(rows: readonly StatementRow[]): Set<string> {
  const out = new Set<string>();
  rows.forEach((r, i) => {
    if (hasChildren(rows, i)) out.add(rowKey(r));
  });
  return out;
}

/** The rows that show when the nodes with the keys in collapsed are collapsed. */
export function visibleRows(rows: readonly StatementRow[], collapsed: ReadonlySet<string>): StatementRow[] {
  const out: StatementRow[] = [];
  let hideBelow: number | null = null;
  for (const row of rows) {
    if (hideBelow !== null) {
      if (row.LEVEL > hideBelow) continue;
      hideBelow = null;
    }
    out.push(row);
    if (row.ACCOUNT === null && collapsed.has(rowKey(row))) {
      hideBelow = row.LEVEL;
    }
  }
  return out;
}
