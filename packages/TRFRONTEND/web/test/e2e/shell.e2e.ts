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


// The shell in real browsers against rt serve: sign-in, start page with
// metric values, a list app with creating and changing an entity, the
// administration and the role page, sign-out, and axe on every page.

import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { type Browser, chromium, firefox, type Page, webkit } from 'playwright';
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest';

const axe = readFileSync(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
const engines = { chromium, firefox, webkit };
const names = (process.env.TRFRONTEND_BROWSERS ?? 'chromium,firefox,webkit').split(',') as Array<keyof typeof engines>;

/** The violations of axe with impact serious or critical. */
async function violations(page: Page): Promise<string[]> {
  // Colors are measured at rest: toasts fade in.
  await page.waitForFunction(() => document.getAnimations().every((a) => a.playState !== 'running'));
  await page.addScriptTag({ content: axe });
  const result = await page.evaluate(async () => {
    type Node = { target: string[]; failureSummary?: string };
    const r = await (window as unknown as { axe: { run: (c: unknown) => Promise<{ violations: Array<{ id: string; impact: string; nodes: Node[] }> }> } }).axe.run(document);
    return r.violations.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.map((n) => `${n.target.join(' ')}: ${n.failureSummary ?? ''}`) }));
  });
  return result.filter((v) => v.impact === 'serious' || v.impact === 'critical').map((v) => `${v.id}: ${v.nodes.join(' | ')}`);
}

async function signIn(page: Page, user: string) {
  await page.goto(`${inject('baseURL')}/trfrontend/`);
  await page.getByLabel('User').fill(user);
  await page.getByLabel('Password').fill('secret');
  await page.getByRole('button', { name: 'Sign in' }).click();
}

for (const name of names) {
  describe(name, () => {
    let browser: Browser;
    beforeAll(async () => {
      browser = await engines[name].launch();
    });
    afterAll(async () => {
      await browser.close();
    });

    it('signs in, opens the companies, creates one and signs out', async () => {
      const context = await browser.newContext({ locale: 'en-US' });
      const page = await context.newPage();
      await page.goto(`${inject('baseURL')}/trfrontend/`);
      await page.getByLabel('Password').waitFor();
      expect(await violations(page)).toEqual([]);
      await signIn(page, 'clara');

      await page.getByRole('tab', { name: 'Companies' }).waitFor();
      const metric = page.locator('[data-tile-id="/TRFINUI/ACTIVE_COMPANIES_TILE"]');
      await expect.poll(() => metric.textContent()).toMatch(/\d/);
      expect(await violations(page)).toEqual([]);

      await page.locator('[data-tile-id="/TRFINUI/COMPANIES_TILE"]').click();
      await page.getByRole('grid', { name: 'Companies' }).waitFor();
      expect(await violations(page)).toEqual([]);

      await page.getByRole('button', { name: 'Create' }).click();
      await page.getByRole('textbox', { name: /^Company/ }).fill(`E2E-${name}`);
      await page.getByRole('textbox', { name: /^Name/ }).fill('Browser test company');
      await page.getByRole('textbox', { name: /^Country/ }).fill('DE');
      await page.getByRole('button', { name: 'Save' }).click();
      await page.getByRole('heading', { name: 'Browser test company', level: 1 }).waitFor();
      expect(await violations(page)).toEqual([]);

      await page.getByRole('button', { name: 'Edit' }).click();
      await page.getByRole('textbox', { name: /^City/ }).fill('Hamburg');
      await page.getByRole('button', { name: 'Save' }).click();
      await page.getByText('Saved.').waitFor();
      expect(await page.locator('output', { hasText: 'Hamburg' }).count()).toBeGreaterThan(0);

      await page.getByRole('button', { name: 'Home' }).click();
      await page.getByRole('button', { name: /User menu of clara/ }).click();
      await page.getByRole('menuitem', { name: 'Sign out' }).click();
      await page.getByLabel('Password').waitFor();
      await context.close();
    });

    it('lets the administrator open the role page', async () => {
      const context = await browser.newContext({ locale: 'en-US' });
      const page = await context.newPage();
      await signIn(page, 'admin');
      await page.getByRole('tab', { name: 'Administration' }).waitFor();
      await page.locator('[data-tile-id="/TRFRONTEND/MANAGE_ROLES_TILE"]').click();
      const frame = page.frameLocator('iframe[title="Roles"]');
      await frame.getByRole('grid', { name: 'Roles' }).getByText('/TRFRONTEND/ADMINISTRATOR').waitFor();
      expect(await violations(page)).toEqual([]);
      await context.close();
    });
  });
}
