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

// Layout of the pages. Remote apps bring no style sheet of their own: TRUI
// styles the components through the shell, and these few rules are inline.

import type { CSSProperties } from 'react';

export const pageStyle: CSSProperties = { display: 'grid', gap: '1rem', padding: '0 1rem 1rem', alignContent: 'start' };
export const fieldsStyle: CSSProperties = { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(14rem, 1fr))', gap: '0.75rem 1rem', alignItems: 'start' };
export const sectionStyle: CSSProperties = { display: 'grid', gap: '0.75rem' };
export const figuresStyle: CSSProperties = { display: 'flex', flexWrap: 'wrap', gap: '0.5rem 2rem', margin: 0 };
export const figureStyle: CSSProperties = { display: 'grid', gap: '0.125rem' };
export const figureNameStyle: CSSProperties = { fontSize: '0.875em', color: 'var(--trui-color-text-secondary, inherit)' };
export const figureValueStyle: CSSProperties = { margin: 0, fontVariantNumeric: 'tabular-nums', fontWeight: 600 };
export const buttonRowStyle: CSSProperties = { display: 'flex', flexWrap: 'wrap', gap: '0.5rem', alignItems: 'center' };
