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


// The role page in its own document, which the app /TRFRONTEND/MANAGE_ROLES
// shows in the content area of the shell. It uses the session of the shell
// and the theme and density chosen there.

import { Banner, FeedbackHost } from '@tim-riep/trui';
import { type Density, type ThemeSetting, TruiProvider } from '@tim-riep/trui/core';
import { createRoot } from 'react-dom/client';
import { readSession } from '../../shell/api.ts';
import { RolesApp } from './RolesApp.tsx';
import { roleTexts } from './texts.ts';

function stored(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

const theme = (['auto', 'light', 'dark'] as const).find((t) => t === stored('trfrontend.theme')) ?? 'auto';
const density = (['compact', 'comfortable'] as const).find((d) => d === stored('trfrontend.density')) ?? 'compact';

const root = document.getElementById('root');
if (root) {
  readSession().then(
    (s) => {
      const language = s?.language ?? 'en';
      const texts = roleTexts(language);
      createRoot(root).render(
        <TruiProvider theme={theme as ThemeSetting} density={density as Density} locale={language}>
          {s ? <RolesApp texts={texts} language={language} /> : <Banner tone="warning">{texts.signIn}</Banner>}
          <FeedbackHost />
        </TruiProvider>,
      );
    },
    () => createRoot(root).render(<TruiProvider>{roleTexts('en').signIn}</TruiProvider>),
  );
}
