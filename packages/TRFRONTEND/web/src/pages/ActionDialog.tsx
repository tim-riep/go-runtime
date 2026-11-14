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


import { Button, Dialog, Form, FormField } from '@tim-riep/trui';
import { useState } from 'react';
import type { Action, ODataClient, ServiceModel, Value } from '../odata/index.ts';
import type { TextKey } from '../shell/texts.ts';
import { FieldEditor } from './Field.tsx';
import type { Field } from './view.ts';
import { actionLabel } from './view.ts';

/** The fields of the parameters of an action. */
export function parameterFields(action: Action): Field[] {
  return action.parameters.map((p) => ({
    name: p.name,
    property: { name: p.name, type: p.type, collection: false, nullable: p.nullable, maxLength: null, precision: null, scale: null, annotations: {} },
    label: p.name,
    heading: p.name,
    key: false,
    mandatory: !p.nullable,
    computed: false,
    immutable: false,
    fixedValues: null,
    valueHelp: null,
  }));
}

export interface ActionDialogProps {
  model: ServiceModel;
  action: Action;
  client: ODataClient;
  texts: Record<TextKey, string>;
  onRun: (params: Record<string, Value>) => void;
  onCancel: () => void;
}

/** Asks for the parameters of an action before it runs. */
export function ActionDialog({ model, action, client, texts, onRun, onCancel }: ActionDialogProps) {
  const fields = parameterFields(action);
  const [values, setValues] = useState<Record<string, Value>>(() => Object.fromEntries(fields.map((f) => [f.name, f.property.nullable ? null : ''])));
  const formId = `action-${action.name}`;
  return (
    <Dialog
      open
      onClose={onCancel}
      title={actionLabel(model, action.name)}
      footer={
        <>
          <Button variant="primary" type="submit" form={formId}>
            {texts.run}
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
          onRun(values);
        }}
      >
        {fields.map((f) => (
          <FormField key={f.name} label={f.label} required={f.mandatory} inputId={`${formId}-${f.name}`}>
            <FieldEditor
              field={f}
              id={`${formId}-${f.name}`}
              value={values[f.name]}
              onChange={(v) => setValues((old) => ({ ...old, [f.name]: v }))}
              readOnly={false}
              client={client}
              error={null}
            />
          </FormField>
        ))}
      </Form>
    </Dialog>
  );
}
