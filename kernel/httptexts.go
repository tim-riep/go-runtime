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

package kernel

import "github.com/tim-riep/go-runtime/internal/httpsvc"

// httpTexts are the texts of the answers of the kernel to requests of
// http services, by code and language. They move to messages once
// messages are an object type.
var httpTexts = map[string]map[string]string{
	httpsvc.CodeNotFound: {
		"en": "There is no service at this path.",
		"de": "Unter diesem Pfad gibt es keinen Dienst.",
	},
	httpsvc.CodeUnauthorized: {
		"en": "Sign in to use this service.",
		"de": "Melden Sie sich an, um diesen Dienst zu nutzen.",
	},
	httpsvc.CodeForbidden: {
		"en": "You may not use this service in this tenant.",
		"de": "Sie dürfen diesen Dienst in diesem Mandanten nicht nutzen.",
	},
	httpsvc.CodeBadRequest: {
		"en": "The request names no tenant, and the user has no default tenant.",
		"de": "Die Anfrage nennt keinen Mandanten, und der Benutzer hat keinen Standardmandanten.",
	},
	httpsvc.CodeTooLarge: {
		"en": "The body of the request is too large.",
		"de": "Der Inhalt der Anfrage ist zu groß.",
	},
	httpsvc.CodeInternal: {
		"en": "The service failed. The error is logged.",
		"de": "Der Dienst ist fehlgeschlagen. Der Fehler ist protokolliert.",
	},
	httpsvc.CodeUnavailable: {
		"en": "The service did not finish in time. Try again later.",
		"de": "Der Dienst wurde nicht rechtzeitig fertig. Versuchen Sie es später noch einmal.",
	},
	httpsvc.CodeNotHandled: {
		"en": "No handler of the service handled the request.",
		"de": "Kein Handler des Dienstes hat die Anfrage bearbeitet.",
	},
}

// httpText returns the text of a code in a language, English otherwise.
func httpText(code, lang string) string {
	if t, ok := httpTexts[code][lang]; ok {
		return t
	}
	return httpTexts[code]["en"]
}
