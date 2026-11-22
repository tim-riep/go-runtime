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
// The remote apps of TRFINUI in real browsers against rt serve, loaded by
// the shell of TRFRONTEND with Module Federation: entering and posting a
// journal entry, an error at an item, clearing an invoice of a customer
// with a payment, the balance sheet with expand, collapse, export and the
// drill-down to a journal entry, the company data of a customer in the
// business partner app of TRBPUI, and axe on the pages.

import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { type Browser, chromium, firefox, type Page, webkit } from 'playwright';
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest';
import { invoices } from './data.ts';

const axe = readFileSync(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
const engines = { chromium, firefox, webkit };
const names = (process.env.TRFINUI_BROWSERS ?? 'chromium,firefox,webkit').split(',') as Array<keyof typeof engines>;

/** The violations of axe with impact serious or critical. */
async function violations(page: Page): Promise<string[]> {
  await page.waitForFunction(() => document.getAnimations().every((a) => a.playState !== 'running'));
  await page.addScriptTag({ content: axe });
  const result = await page.evaluate(async () => {
    type Node = { target: string[]; failureSummary?: string };
    const r = await (window as unknown as { axe: { run: (c: unknown) => Promise<{ violations: Array<{ id: string; impact: string; nodes: Node[] }> }> } }).axe.run(document);
    return r.violations.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.map((n) => `${n.target.join(' ')}: ${n.failureSummary ?? ''}`) }));
  });
  return result.filter((v) => v.impact === 'serious' || v.impact === 'critical').map((v) => `${v.id}: ${v.nodes.join(' | ')}`);
}

async function signIn(page: Page, user = 'anna') {
  await page.goto(`${inject('baseURL')}/trfrontend/`);
  await page.getByLabel('User').fill(user);
  await page.getByLabel('Password').fill('secret');
  await page.getByRole('button', { name: 'Sign in' }).click();
}

/** Opens an app by its tile in a space of the start page. */
async function openTile(page: Page, space: string, tile: string) {
  await page.getByRole('tab', { name: space }).click();
  await page.locator(`[data-tile-id="${tile}"]`).click();
}

/** Picks an option of a select of TRUI. */
async function choose(page: Page, combobox: string, option: RegExp) {
  await page.getByRole('combobox', { name: combobox, exact: true }).click();
  await page.getByRole('option', { name: option }).click();
}

/** Types into a field and leaves it, so that it takes the value. */
async function enter(page: Page, textbox: string, value: string) {
  const box = page.getByRole('textbox', { name: textbox, exact: true });
  await box.fill(value);
  await box.press('Tab');
}

/** The header of a journal entry of company 1000. */
async function header(page: Page, type: string) {
  await choose(page, 'Company', /^1000/);
  await choose(page, 'Document type', new RegExp(`^${type}\\b`));
  await enter(page, 'Posting date', '06/15/2026');
  await enter(page, 'Document date', '06/15/2026');
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

    it('enters and posts a journal entry and opens it', async () => {
      const context = await browser.newContext({ locale: 'en-US' });
      const page = await context.newPage();
      await signIn(page);
      await openTile(page, 'Posting', '/TRFINUI/POST_JOURNAL_ENTRY_TILE');
      await page.getByRole('grid', { name: 'Items' }).waitFor();
      expect(await violations(page)).toEqual([]);

      await header(page, 'SA');
      await enter(page, 'Header text', `Browser test ${name}`);
      await enter(page, 'Account of item 1', '110000');
      await enter(page, 'Amount of item 1', '250.00');
      // The running balance follows the amounts as exact decimals.
      await expect.poll(() => page.getByRole('status', { name: 'Running balance of item 2' }).textContent()).toBe('250.00');
      await page.getByText('Debit and credit differ by', { exact: false }).waitFor();
      await enter(page, 'Account of item 2', '400000');
      await enter(page, 'Amount of item 2', '250.00');
      await expect.poll(() => page.getByRole('status', { name: 'Running balance of item 2' }).textContent()).toBe('0.00');
      await page.getByText('Balanced', { exact: true }).waitFor();

      await page.getByRole('button', { name: 'Check', exact: true }).click();
      await page.getByText('The journal entry can be posted in period 6 of 2026.').waitFor();
      await page.getByRole('button', { name: 'Post', exact: true }).click();
      await page.getByText(/^Journal entry \d+ of 2026 posted\.$/).first().waitFor();
      expect(await violations(page)).toEqual([]);

      await page.getByRole('button', { name: 'Open journal entry' }).click();
      await page.getByRole('heading', { level: 1 }).filter({ hasText: /\d/ }).waitFor();
      await page.getByText(`Browser test ${name}`).first().waitFor();
      await context.close();
    });

    it('shows the error of an item at its cell and posts nothing', async () => {
      const context = await browser.newContext({ locale: 'en-US' });
      const page = await context.newPage();
      await signIn(page);
      await openTile(page, 'Posting', '/TRFINUI/POST_JOURNAL_ENTRY_TILE');
      await page.getByRole('grid', { name: 'Items' }).waitFor();
      await header(page, 'SA');
      await enter(page, 'Account of item 1', '110000');
      await enter(page, 'Amount of item 1', '80.00');
      await enter(page, 'Account of item 2', '999999');
      await enter(page, 'Amount of item 2', '80.00');

      const message = 'the account 999999 does not exist in the company 1000';
      await page.getByRole('button', { name: 'Check', exact: true }).click();
      const cell = page.getByRole('textbox', { name: 'Account of item 2', exact: true });
      await expect.poll(() => cell.getAttribute('aria-invalid')).toBe('true');
      await page.getByText(message).first().waitFor();
      expect(await page.getByRole('textbox', { name: 'Account of item 1', exact: true }).getAttribute('aria-invalid')).not.toBe('true');

      // POST fails with the same message at the same cell.
      await page.getByRole('button', { name: 'Post', exact: true }).click();
      await page.getByText(message).first().waitFor();
      await expect.poll(() => cell.getAttribute('aria-invalid')).toBe('true');
      expect(await page.getByText(/posted\.$/).count()).toBe(0);
      expect(await violations(page)).toEqual([]);
      await context.close();
    });

    it('clears an invoice of a customer with a payment', async () => {
      const context = await browser.newContext({ locale: 'en-US' });
      const page = await context.newPage();
      await signIn(page);
      await openTile(page, 'Posting', '/TRFINUI/CLEAR_OPEN_ITEMS_TILE');
      await choose(page, 'Company', /^1000/);
      await choose(page, 'Kind', /^Customer/);
      await enter(page, 'Partner', '0000000001');
      await choose(page, 'Document type', /^DZ\b/);
      await enter(page, 'Posting date', '11/20/2026');
      await page.getByRole('button', { name: 'Show open items' }).click();

      const amount = invoices[name] ?? '';
      const shown = Number(amount).toLocaleString('en-US', { minimumFractionDigits: 2 });
      const grid = page.getByRole('grid', { name: 'Open items' });
      const row = grid.getByRole('row').filter({ hasText: shown });
      await row.getByRole('checkbox').click();
      await page.getByRole('button', { name: 'Add items for the difference' }).click();
      await enter(page, 'Account of item 1', '110000');
      await page.getByText('Difference to clear: €0.00').waitFor();
      expect(await violations(page)).toEqual([]);
      await page.getByRole('button', { name: 'Clear', exact: true }).click();
      await page.getByText(/^Clearing entry \d+ of 2026 posted\.$/).first().waitFor();

      // The invoice is no longer open.
      await page.getByRole('button', { name: 'Show open items' }).click();
      await page.getByRole('grid', { name: 'Open items' }).waitFor();
      await expect.poll(() => page.getByRole('grid', { name: 'Open items' }).getByRole('row').filter({ hasText: shown }).count()).toBe(0);
      await context.close();
    });

    it('shows the balance sheet, collapses it, exports it and drills down to a journal entry', async () => {
      const context = await browser.newContext({ locale: 'en-US', acceptDownloads: true });
      const page = await context.newPage();
      await signIn(page);
      await openTile(page, 'Accounting', '/TRFINUI/BALANCE_SHEET_TILE');
      await choose(page, 'Company', /^1000/);
      await enter(page, 'Key date', '12/31/2026');
      await page.getByRole('button', { name: 'Run' }).click();
      const grid = page.getByRole('grid', { name: 'Balance sheet' });
      await grid.getByText('Fixed assets').waitFor();
      await page.getByText('Balanced', { exact: true }).waitFor();
      expect(await violations(page)).toEqual([]);

      await page.getByRole('button', { name: 'Collapse Assets' }).click();
      await expect.poll(() => grid.getByText('Fixed assets').count()).toBe(0);
      await page.getByRole('button', { name: 'Expand Assets' }).click();
      await grid.getByText('Fixed assets').waitFor();
      await page.getByRole('button', { name: 'Collapse all' }).click();
      await expect.poll(() => grid.getByText('Equity', { exact: true }).count()).toBe(0);
      await page.getByRole('button', { name: 'Expand all' }).click();

      const download = page.waitForEvent('download');
      await page.getByRole('button', { name: 'Export' }).click();
      const file = await download;
      expect(file.suggestedFilename()).toBe('balance-sheet-1000.xlsx');
      const path = await file.path();
      expect(readFileSync(path).subarray(0, 2).toString()).toBe('PK');

      // Account → line items → journal entry.
      await grid.getByText('110000 Bank').click();
      const items = page.getByRole('grid', { name: 'Line items' });
      await items.getByRole('row').nth(1).waitFor();
      expect(await violations(page)).toEqual([]);
      await items.getByRole('row').nth(1).click();
      await page.getByRole('heading', { level: 1 }).filter({ hasText: /\d/ }).waitFor();
      await context.close();
    });

    it('maintains the company data of a customer on the page of the business partner', async () => {
      const context = await browser.newContext({ locale: 'en-US' });
      const page = await context.newPage();
      await signIn(page, 'mara');
      await openTile(page, 'General ledger master data', '/TRBPUI/PARTNERS_TILE');
      await page.getByRole('grid', { name: 'Business partners' }).getByText('Birch Trading').click();
      await page.getByRole('heading', { name: '0000000002', level: 1 }).waitFor();
      // Not getByRole('region'): after it, axe crashes Firefox on this page.
      const customer = page.locator('section[aria-label="Customer data"]');
      const rows = customer.getByRole('grid', { name: 'Customer data' }).getByRole('row');
      await rows.filter({ hasText: '1000' }).waitFor();
      expect(await rows.filter({ hasText: '2000' }).count()).toBe(0);
      expect(await violations(page)).toEqual([]);

      // Company data for company 2000, created below the partner.
      await customer.getByRole('button', { name: 'Create' }).click();
      await page.getByRole('heading', { name: 'New entry', level: 1 }).waitFor();
      expect(await page.getByRole('textbox', { name: /^Business partner/ }).count()).toBe(0);
      await page.getByRole('textbox', { name: /^Company/ }).fill('2000');
      await page.getByRole('textbox', { name: /^Reconciliation acct\./ }).fill('140000');
      await page.getByRole('textbox', { name: /^Alternative account/ }).fill('161000');
      expect(await violations(page)).toEqual([]);
      await page.getByRole('button', { name: 'Save' }).click();
      await page.getByText('Created.').waitFor();
      await page.getByRole('heading', { name: '0000000002', level: 1 }).waitFor();
      await page.locator('output', { hasText: '/TRFIN/CUSTOMER' }).waitFor();

      // Blocked, changed on the page of the company data.
      await page.getByRole('button', { name: 'Edit' }).click();
      await page.getByRole('checkbox', { name: 'Blocked' }).check();
      await page.getByRole('button', { name: 'Save' }).click();
      await page.getByText('Saved.').waitFor();

      // Back on the partner: the new row in the table, opened by its path below the partner.
      await page.goBack();
      await page.goBack();
      const row = rows.filter({ hasText: '2000' });
      await row.waitFor();
      expect(await row.textContent()).toContain('Yes');
      await row.getByText('2000').click();
      await page.getByRole('heading', { name: '0000000002', level: 1 }).waitFor();
      await page.locator('output', { hasText: '2000' }).waitFor();
      await page.getByRole('button', { name: 'Delete' }).click();
      await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click();
      await page.getByText('Deleted.').waitFor();
      await rows.filter({ hasText: '1000' }).waitFor();
      expect(await rows.filter({ hasText: '2000' }).count()).toBe(0);
      await context.close();
    });
  });
}
