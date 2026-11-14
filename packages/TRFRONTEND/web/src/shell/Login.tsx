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


import { Banner, Button, Form, FormField, Page, PageHeader, TextField } from '@tim-riep/trui';
import { type FormEvent, useState } from 'react';
import { type Session, signIn } from './api.ts';
import type { TextKey } from './texts.ts';

export interface LoginProps {
  texts: Record<TextKey, string>;
  /** Why the sign-in is shown again, e.g. the session expired. */
  notice: string | null;
  onSignedIn: (s: Session) => void;
}

/** The sign-in with user and password over /rt/session. */
export function Login({ texts, notice, onSignedIn }: LoginProps) {
  const [user, setUser] = useState('');
  const [password, setPassword] = useState('');
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setFailed(false);
    try {
      onSignedIn(await signIn(user, password));
    } catch {
      setFailed(true);
      setPassword('');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Page header={<PageHeader title={texts.signInTitle} headingLevel={1} />}>
      <main className="trfrontend-login">
        {notice && !failed ? <Banner tone="information">{notice}</Banner> : null}
        {failed ? (
          <Banner tone="error" announce="assertive">
            {texts.signInFailed}
          </Banner>
        ) : null}
        <Form columns={1} labelPosition="top" onSubmit={submit} aria-label={texts.signInTitle}>
          <FormField label={texts.user} required inputId="trfrontend-user">
            <TextField id="trfrontend-user" name="username" autoComplete="username" required value={user} onChange={setUser} />
          </FormField>
          <FormField label={texts.password} required inputId="trfrontend-password">
            <TextField
              id="trfrontend-password"
              name="password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={setPassword}
            />
          </FormField>
          <div>
            <Button variant="primary" type="submit" loading={busy}>
              {texts.signIn}
            </Button>
          </div>
        </Form>
      </main>
    </Page>
  );
}
