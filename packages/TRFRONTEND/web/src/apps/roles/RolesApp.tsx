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


// The role page: all roles the user may see, a role with its authorizations,
// members, use and assignments, assigning and removing assignments, and
// creating, changing and deleting local roles. Cases the kernel wants
// confirmed (409 confirmation-required) are asked and sent again.

import { Banner, Button, DatePicker, Form, FormField, PageHeader, Select, SplitLayout, Table, Tag, Toolbar } from '@tim-riep/trui';
import { useMessageDialog, useToast } from '@tim-riep/trui/core';
import { useCallback, useEffect, useState } from 'react';
import { RequestError } from '../../shell/api.ts';
import { defined } from '../../shell/defined.ts';
import { format } from '../../shell/texts.ts';
import {
  type Assignment,
  assign,
  assignments,
  type AuthorizationObject,
  authorizationObjects,
  createRole,
  deleteRole,
  labelOf,
  type RoleDefinition,
  type RoleDetail,
  type RoleInfo,
  role as readRole,
  roles as readRoles,
  unassign,
  updateRole,
  type UserInfo,
  users as readUsers,
  valueText,
} from './api.ts';
import { RoleEditor } from './RoleEditor.tsx';
import type { RoleTextKey } from './texts.ts';

export interface RolesAppProps {
  texts: Record<RoleTextKey, string>;
  language: string;
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export function RolesApp({ texts, language }: RolesAppProps) {
  const [list, setList] = useState<RoleInfo[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [detail, setDetail] = useState<RoleDetail | null>(null);
  const [assigned, setAssigned] = useState<Assignment[]>([]);
  const [users, setUsers] = useState<UserInfo[]>([]);
  const [objects, setObjects] = useState<AuthorizationObject[]>([]);
  const [editing, setEditing] = useState<RoleDefinition | null | undefined>(undefined);
  const [editError, setEditError] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [user, setUser] = useState<string | null>(null);
  const [tenant, setTenant] = useState<string | null>(null);
  const [first, setFirst] = useState<string | null>(null);
  const [last, setLast] = useState<string | null>(null);
  const toast = useToast();
  const dialog = useMessageDialog();

  const loadList = useCallback(() => {
    readRoles().then(setList, (e: unknown) => setFailure(message(e)));
  }, []);

  const loadRole = useCallback((name: string) => {
    setDetail(null);
    Promise.all([readRole(name), assignments({ role: name })]).then(
      ([d, a]) => {
        setDetail(d);
        setAssigned(a);
      },
      (e: unknown) => setFailure(message(e)),
    );
  }, []);

  useEffect(() => {
    loadList();
    readUsers().then(setUsers, () => setUsers([]));
    authorizationObjects().then(setObjects, () => setObjects([]));
  }, [loadList]);

  useEffect(() => {
    if (selected) loadRole(selected);
  }, [selected, loadRole]);

  /** Runs a change and asks again when the kernel wants a confirmation. */
  async function confirmed(run: (confirm: boolean) => Promise<unknown>): Promise<boolean> {
    try {
      await run(false);
      return true;
    } catch (e) {
      if (e instanceof RequestError && e.code === 'confirmation-required') {
        const ok = await dialog.confirm({ title: texts.confirm, message: e.message, danger: true, confirmLabel: texts.confirm, cancelLabel: texts.cancel });
        if (!ok) return false;
        try {
          await run(true);
          return true;
        } catch (e2) {
          setFailure(message(e2));
          return false;
        }
      }
      setFailure(message(e));
      return false;
    }
  }

  async function save(r: RoleDefinition) {
    setEditError(null);
    try {
      if (editing === null) await createRole(r);
      else await updateRole(r);
      toast.show({ message: texts.saved, tone: 'success' });
      setEditing(undefined);
      loadList();
      setSelected(r.NAME);
      loadRole(r.NAME);
    } catch (e) {
      setEditError(message(e));
    }
  }

  async function remove(name: string) {
    if (await confirmed((c) => deleteRole(name, c))) {
      toast.show({ message: texts.deleted, tone: 'success' });
      setSelected(null);
      setDetail(null);
      loadList();
    }
  }

  async function addAssignment() {
    if (!detail || !user || !tenant) return;
    try {
      const r = await assign({ USER_NAME: user, ROLE_NAME: detail.NAME, TENANT: tenant, VALID_FROM: first, VALID_TO: last });
      toast.show({ message: r.OVERLAPS.length ? format(texts.overlaps, { count: r.OVERLAPS.length }) : texts.assigned, tone: r.OVERLAPS.length ? 'warning' : 'success' });
      loadRole(detail.NAME);
    } catch (e) {
      setFailure(message(e));
    }
  }

  async function removeAssignment(a: Assignment) {
    if (await confirmed((c) => unassign(a.ID, c))) {
      toast.show({ message: texts.deleted, tone: 'success' });
      if (detail) loadRole(detail.NAME);
    }
  }

  const local = detail !== null && detail.PACKAGE_ID === null;
  const tenants = users.find((u) => u.USER_NAME === user)?.TENANTS ?? [];

  const listPane = (
    <div className="trfrontend-list">
      <PageHeader title={texts.roles} headingLevel={1} actions={<Toolbar aria-label={texts.roles} items={[{ key: 'create', label: texts.create, variant: 'primary', overflow: 'never' }]} onAction={() => setEditing(null)} />} />
      <Table<RoleInfo>
        aria-label={texts.roles}
        columns={[
          { key: 'NAME', header: texts.name, sortable: true },
          { key: 'LABEL', header: texts.label, sortable: true },
          { key: 'PACKAGE_ID', header: texts.package, renderCell: (r) => r.PACKAGE_ID ?? <Tag>{texts.local}</Tag>, sortValue: (r) => r.PACKAGE_ID ?? '', sortable: true },
        ]}
        rows={list ?? []}
        loading={list === null}
        getRowKey={(r) => r.NAME}
        onRowPress={(r) => setSelected(r.NAME)}
        renderEmpty={() => texts.noRoles}
      />
    </div>
  );

  const detailPane = !detail ? (
    <p className="trfrontend-empty">{texts.select}</p>
  ) : (
    <div className="trfrontend-detail">
      <PageHeader
        title={labelOf(detail, language)}
        subtitle={detail.NAME}
        headingLevel={2}
        actions={
          local ? (
            <Toolbar
              aria-label={texts.role}
              items={[
                { key: 'edit', label: texts.edit, variant: 'primary', overflow: 'never' },
                { key: 'delete', label: texts.delete, overflow: 'never' },
              ]}
              onAction={(k) => (k === 'edit' ? setEditing(detail) : void remove(detail.NAME))}
            />
          ) : undefined
        }
      />
      {!local ? <Banner tone="information">{texts.packageRole}</Banner> : null}
      {detail.ALL ? <Banner tone="warning">{texts.all}</Banner> : null}
      {detail.COMPOSITE ? (
        <section aria-label={texts.members} className="trfrontend-section">
          <h3>{texts.members}</h3>
          <ul>
            {(detail.ROLES ?? []).map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        </section>
      ) : null}
      <section aria-label={texts.authorizations} className="trfrontend-section">
        <h3>{texts.authorizations}</h3>
        <Table<RoleDetail['EFFECT'][number]>
          aria-label={texts.authorizations}
          columns={[
            { key: 'OBJECT', header: texts.object },
            {
              key: 'FIELDS',
              header: texts.values,
              renderCell: (a) => a.FIELDS.map((f) => `${f.FIELD}: ${f.VALUES.map(valueText).join(', ')}`).join('; '),
            },
            { key: 'ROLE_NAME', header: texts.role, renderCell: (a) => a.ROLE_NAME ?? '' },
          ]}
          rows={detail.EFFECT}
          getRowKey={(a) => `${a.ROLE_NAME ?? ''}|${a.OBJECT}|${JSON.stringify(a.FIELDS)}`}
        />
      </section>
      {detail.USED_BY.length ? (
        <section aria-label={texts.usedBy} className="trfrontend-section">
          <h3>{texts.usedBy}</h3>
          <p>{detail.USED_BY.join(', ')}</p>
        </section>
      ) : null}
      <section aria-label={texts.assignments} className="trfrontend-section">
        <h3>{texts.assignments}</h3>
        <Table<Assignment>
          aria-label={texts.assignments}
          columns={[
            { key: 'USER_NAME', header: texts.user, sortable: true },
            { key: 'TENANT', header: texts.tenant, sortable: true },
            { key: 'VALID_FROM', header: texts.validFrom, renderCell: (a) => a.VALID_FROM ?? '' },
            { key: 'VALID_TO', header: texts.validTo, renderCell: (a) => a.VALID_TO ?? '' },
          ]}
          rows={assigned}
          getRowKey={(a) => a.ID}
          rowActions={() => [{ key: 'unassign', label: texts.unassign, danger: true }]}
          onRowAction={(_, a) => void removeAssignment(a)}
          renderEmpty={() => texts.noAssignments}
        />
        <Form
          columns={4}
          aria-label={texts.assign}
          onSubmit={(e) => {
            e.preventDefault();
            void addAssignment();
          }}
        >
          <FormField label={texts.user} required inputId="assign-user">
            <Select
              id="assign-user"
              required
              nullable
              items={users.map((u) => defined({ value: u.USER_NAME, label: u.NAME ?? u.USER_NAME, description: u.USER_NAME }))}
              value={user}
              onChange={(v) => {
                setUser(v);
                setTenant(users.find((u) => u.USER_NAME === v)?.DEFAULT_TENANT ?? null);
              }}
            />
          </FormField>
          <FormField label={texts.tenant} required inputId="assign-tenant">
            <Select id="assign-tenant" required nullable items={tenants.map((t) => ({ value: t, label: t }))} value={tenant} onChange={setTenant} />
          </FormField>
          <FormField label={texts.validFrom} inputId="assign-from">
            <DatePicker id="assign-from" nullable value={first} onChange={setFirst} />
          </FormField>
          <FormField label={texts.validTo} inputId="assign-to">
            <DatePicker id="assign-to" nullable value={last} onChange={setLast} />
          </FormField>
          <div>
            <Button variant="primary" type="submit" disabled={!user || !tenant}>
              {texts.assign}
            </Button>
          </div>
        </Form>
      </section>
    </div>
  );

  return (
    <>
      {failure ? (
        <Banner tone="error" announce="assertive" onClose={() => setFailure(null)}>
          {failure}
        </Banner>
      ) : null}
      <SplitLayout list={listPane} detail={detailPane} showDetail={selected !== null} onBack={() => setSelected(null)} listLabel={texts.roles} detailLabel={texts.role} />
      {editing !== undefined ? (
        <RoleEditor role={editing} roles={list ?? []} objects={objects} texts={texts} error={editError} onSave={(r) => void save(r)} onCancel={() => setEditing(undefined)} />
      ) : null}
    </>
  );
}
