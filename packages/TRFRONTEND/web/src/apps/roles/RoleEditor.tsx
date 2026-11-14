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


import { Button, Checkbox, Dialog, Form, FormField, MultiSelect, Select, TextField } from '@tim-riep/trui';
import { useState } from 'react';
import { defined } from '../../shell/defined.ts';
import { type AuthorizationObject, formatValues, parseValues, type RoleAuthorization, type RoleDefinition, type RoleInfo } from './api.ts';
import type { RoleTextKey } from './texts.ts';

export interface RoleEditorProps {
  /** The role to change, or null to create one. */
  role: RoleDefinition | null;
  roles: RoleInfo[];
  objects: AuthorizationObject[];
  texts: Record<RoleTextKey, string>;
  error: string | null;
  onSave: (r: RoleDefinition) => void;
  onCancel: () => void;
}

interface EditedAuthorization {
  object: string;
  /** Values as text by field. */
  fields: Record<string, string>;
}

function edited(a: RoleAuthorization): EditedAuthorization {
  return { object: a.OBJECT, fields: Object.fromEntries(a.FIELDS.map((f) => [f.FIELD, formatValues(f.VALUES)])) };
}

/** Creates or changes a local role: English texts, members or authorizations. */
export function RoleEditor({ role, roles, objects, texts, error, onSave, onCancel }: RoleEditorProps) {
  const english = role?.TEXTS?.find((t) => t.LANGUAGE === 'en');
  const [name, setName] = useState(role?.NAME ?? '/LOCAL/');
  const [label, setLabel] = useState(english?.LABEL ?? '');
  const [description, setDescription] = useState<string | null>(english?.DESCRIPTION ?? null);
  const [composite, setComposite] = useState(role?.COMPOSITE ?? false);
  const [members, setMembers] = useState<string[]>(role?.ROLES ?? []);
  const [auths, setAuths] = useState<EditedAuthorization[]>(() => (role?.AUTHORIZATIONS ?? []).map(edited));

  const save = () => {
    const others = (role?.TEXTS ?? []).filter((t) => t.LANGUAGE !== 'en');
    onSave({
      NAME: name,
      COMPOSITE: composite,
      TEXTS: [{ LANGUAGE: 'en', LABEL: label, DESCRIPTION: description }, ...others],
      ROLES: composite ? members : [],
      AUTHORIZATIONS: composite
        ? []
        : auths
            .filter((a) => a.object !== '')
            .map((a) => ({
              OBJECT: a.object,
              FIELDS: Object.entries(a.fields)
                .map(([f, v]) => ({ FIELD: f, VALUES: parseValues(v) }))
                .filter((f) => f.VALUES.length > 0),
              ROLE_NAME: null,
              COMPOSITE: null,
            })),
    });
  };

  const objectItems = objects.map((o) => defined({ value: o.NAME, label: o.LABEL ?? o.NAME, description: o.NAME }));
  const formId = 'role-editor';
  return (
    <Dialog
      open
      size="l"
      onClose={onCancel}
      title={role ? role.NAME : texts.create}
      footer={
        <>
          <Button variant="primary" type="submit" form={formId}>
            {texts.save}
          </Button>
          <Button variant="secondary" onClick={onCancel}>
            {texts.cancel}
          </Button>
        </>
      }
    >
      <Form
        id={formId}
        columns={1}
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        {error ? <p role="alert">{error}</p> : null}
        <FormField label={texts.name} required inputId="role-name">
          <TextField id="role-name" value={name} onChange={setName} readOnly={role !== null} required />
        </FormField>
        <FormField label={texts.label} required inputId="role-label">
          <TextField id="role-label" value={label} onChange={setLabel} required />
        </FormField>
        <FormField label={texts.description} inputId="role-description">
          <TextField id="role-description" nullable value={description} onChange={setDescription} />
        </FormField>
        <Checkbox checked={composite} onChange={setComposite}>
          {texts.composite}
        </Checkbox>
        {composite ? (
          <FormField label={texts.members} inputId="role-members">
            <MultiSelect
              id="role-members"
              items={roles.filter((r) => !r.COMPOSITE && r.NAME !== name).map((r) => defined({ value: r.NAME, label: r.LABEL ?? r.NAME, description: r.NAME }))}
              value={members}
              onChange={setMembers}
            />
          </FormField>
        ) : (
          <fieldset className="trfrontend-authorizations">
            <legend>{texts.authorizations}</legend>
            {auths.map((a, i) => {
              const object = objects.find((o) => o.NAME === a.object);
              return (
                <div key={i} className="trfrontend-authorization">
                  <FormField label={texts.object} inputId={`auth-${i}`}>
                    <Select
                      id={`auth-${i}`}
                      items={objectItems}
                      value={a.object}
                      onChange={(v) => setAuths((old) => old.map((x, j) => (j === i ? { object: v, fields: {} } : x)))}
                    />
                  </FormField>
                  {(object?.FIELDS ?? []).map((f) => (
                    <FormField key={f.NAME} label={f.LABEL ?? f.NAME} help={texts.valuesHelp} inputId={`auth-${i}-${f.NAME}`}>
                      <TextField
                        id={`auth-${i}-${f.NAME}`}
                        value={a.fields[f.NAME] ?? ''}
                        onChange={(v) => setAuths((old) => old.map((x, j) => (j === i ? { ...x, fields: { ...x.fields, [f.NAME]: v } } : x)))}
                      />
                    </FormField>
                  ))}
                  <Button variant="text" onClick={() => setAuths((old) => old.filter((_, j) => j !== i))}>
                    {texts.removeAuthorization}
                  </Button>
                </div>
              );
            })}
            <Button variant="secondary" onClick={() => setAuths((old) => [...old, { object: '', fields: {} }])}>
              {texts.addAuthorization}
            </Button>
          </fieldset>
        )}
      </Form>
    </Dialog>
  );
}
