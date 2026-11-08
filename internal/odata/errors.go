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

package odata

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Codes of errors of services. They are stable: clients may rely on them.
const (
	CodeNotFound           = "NOT_FOUND"
	CodeForbidden          = "NOT_AUTHORIZED"
	CodeNotImplemented     = "NOT_IMPLEMENTED"
	CodeInvalidRequest     = "INVALID_REQUEST"
	CodeUnsupportedVersion = "UNSUPPORTED_VERSION"
	CodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	CodeNotAcceptable      = "NOT_ACCEPTABLE"
	CodeInternal           = "INTERNAL_ERROR"
	CodeUnavailable        = "UNAVAILABLE"
	CodeTooLarge           = "BODY_TOO_LARGE"
	CodePreconditionFailed = "ETAG_MISMATCH"
	CodePreconditionNeeded = "ETAG_REQUIRED"
	CodeAlreadyExists      = "ALREADY_EXISTS"
	CodeValidation         = "VALIDATION"
	CodeMandatory          = "MANDATORY"
	CodeParentNotFound     = "PARENT_NOT_FOUND"
	CodeBehavior           = "BEHAVIOR"
	CodeUnknownProperty    = "UNKNOWN_PROPERTY"
	CodeReadOnlyProperty   = "READ_ONLY_PROPERTY"
	CodeInvalidValue       = "INVALID_VALUE"
)

// Error is an error in the format of OData. Its message is either a text
// given as it is, such as a message of a behavior class, or the text of
// its code in the language of the request.
type Error struct {
	Status  int
	Code    string
	Args    []any
	Text    string
	Target  string
	Details []*Error
	// Origin is the package of the behavior that reported it, if any.
	Origin string
}

// Errorf returns an error with the text of its code and the arguments.
func Errorf(status int, code string, args ...any) *Error {
	return &Error{Status: status, Code: code, Args: args}
}

func (e *Error) Error() string { return e.Message("en") }

// Message returns the message in a language.
func (e *Error) Message(lang string) string {
	if e.Text != "" {
		return e.Text
	}
	return Text(lang, e.Code, e.Args...)
}

// At returns the error with a target.
func (e *Error) At(target string) *Error {
	e.Target = target
	return e
}

// errorDoc is the body of an error response.
type errorDoc struct {
	Code    string     `json:"code"`
	Message string     `json:"message"`
	Target  string     `json:"target,omitempty"`
	Details []errorDoc `json:"details,omitempty"`
	Origin  string     `json:"@RT.Origin,omitempty"`
}

func (e *Error) doc(lang string) errorDoc {
	d := errorDoc{Code: e.Code, Message: e.Message(lang), Target: e.Target, Origin: e.Origin}
	for _, x := range e.Details {
		d.Details = append(d.Details, x.doc(lang))
	}
	return d
}

// Write writes the error as response in the format of OData.
func (e *Error) Write(w http.ResponseWriter, lang string) int {
	body, _ := json.Marshal(map[string]errorDoc{"error": e.doc(lang)})
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("OData-Version", "4.0")
	h.Set("Content-Language", lang)
	if e.Status == http.StatusServiceUnavailable {
		h.Set("Retry-After", "1")
	}
	w.WriteHeader(e.Status)
	n, _ := w.Write(body)
	return n
}

// texts are the texts of the codes by language.
var texts = map[string]map[string]string{
	CodeNotFound: {
		"en": "%s does not exist.",
		"de": "%s gibt es nicht.",
	},
	CodeForbidden: {
		"en": "You may not use %s.",
		"de": "Sie dürfen %s nicht nutzen.",
	},
	CodeNotImplemented: {
		"en": "The service does not support %s.",
		"de": "Der Dienst unterstützt %s nicht.",
	},
	CodeInvalidRequest: {
		"en": "The request is invalid: %s",
		"de": "Die Anfrage ist ungültig: %s",
	},
	CodeUnsupportedVersion: {
		"en": "The service speaks OData 4.0; the client accepts at most %s.",
		"de": "Der Dienst spricht OData 4.0; der Client akzeptiert höchstens %s.",
	},
	CodeMethodNotAllowed: {
		"en": "%s does not allow the method %s.",
		"de": "%s erlaubt die Methode %s nicht.",
	},
	CodeNotAcceptable: {
		"en": "The service cannot answer in the format %s.",
		"de": "Der Dienst kann nicht im Format %s antworten.",
	},
	CodeInternal: {
		"en": "The service failed. The error is logged.",
		"de": "Der Dienst ist fehlgeschlagen. Der Fehler ist protokolliert.",
	},
	CodeUnavailable: {
		"en": "The service did not finish in time. Try again later.",
		"de": "Der Dienst wurde nicht rechtzeitig fertig. Versuchen Sie es später noch einmal.",
	},
	CodeTooLarge: {
		"en": "The body of the request is too large.",
		"de": "Der Inhalt der Anfrage ist zu groß.",
	},
	CodePreconditionFailed: {
		"en": "The entity was changed in the meantime; read it again.",
		"de": "Die Entität wurde inzwischen geändert; lesen Sie sie erneut.",
	},
	CodePreconditionNeeded: {
		"en": "Send the ETag of the entity in the header If-Match.",
		"de": "Senden Sie das ETag der Entität im Header If-Match.",
	},
	CodeAlreadyExists: {
		"en": "An entity with this key exists already.",
		"de": "Eine Entität mit diesem Schlüssel gibt es schon.",
	},
	CodeValidation: {
		"en": "The entity is not valid.",
		"de": "Die Entität ist nicht gültig.",
	},
	CodeMandatory: {
		"en": "%s needs a value.",
		"de": "%s braucht einen Wert.",
	},
	CodeParentNotFound: {
		"en": "The parent entity does not exist.",
		"de": "Die übergeordnete Entität gibt es nicht.",
	},
	CodeBehavior: {
		"en": "The behavior of the entity failed. The error is logged.",
		"de": "Das Verhalten der Entität ist fehlgeschlagen. Der Fehler ist protokolliert.",
	},
	CodeUnknownProperty: {
		"en": "%s has no property %s.",
		"de": "%s hat keine Eigenschaft %s.",
	},
	CodeReadOnlyProperty: {
		"en": "The property %s cannot be changed.",
		"de": "Die Eigenschaft %s kann nicht geändert werden.",
	},
	CodeInvalidValue: {
		"en": "The value of %s is invalid: %s",
		"de": "Der Wert von %s ist ungültig: %s",
	},
}

// Text returns the text of a code in a language, English otherwise.
func Text(lang, code string, args ...any) string {
	t, ok := texts[code][lang]
	if !ok {
		t = texts[code]["en"]
	}
	if t == "" {
		return code
	}
	return fmt.Sprintf(t, args...)
}
