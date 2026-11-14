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


// Icons of apps and tiles are named like the icons of TRUI (kebab-case).

import type { IconDefinition } from '@tim-riep/trui/core';
import { allIcons } from '@tim-riep/trui/icons';

let byName: Map<string, IconDefinition> | null = null;

/** The icon of a name, or undefined for null and unknown names. */
export function iconOf(name: string | null | undefined): IconDefinition | undefined {
  if (!name) {
    return undefined;
  }
  byName ??= new Map(allIcons.map((i) => [i.name, i]));
  return byName.get(name);
}
