// GENERATED FILE — DO NOT EDIT.
//
// The worked examples on /docs/api, rendered from
// api/internal/httpapi/openapi_examples.go. Each one is EXECUTED against a
// live server by api/internal/httpapi/openapi_examples_test.go and asserted
// on its status and body, so a copy-pasteable example cannot be wrong
// without the api suite going red. The `curl` strings here are generated
// from the same structured request the test runs.
//
// Regenerate:  cd api && go run ./cmd/openapi-gen

export type ApiExample = {
  id: string;
  title: string;
  why: string;
  note: string;
  method: string;
  path: string;
  curl: string;
  expectStatus: number;
  expectContains: string;
  needsKey: boolean;
  needsFixture: boolean;
};

export const API_EXAMPLES: { examples: ApiExample[] } = {
  "examples": [
    {
      "curl": "curl -sS 'https://api.jomform.com/v1/rest/keys' \\\n  -H \"Authorization: Bearer $JOMFORM_API_KEY\"",
      "expectContains": "\"scopes\"",
      "expectStatus": 200,
      "id": "auth",
      "method": "GET",
      "needsFixture": false,
      "needsKey": true,
      "note": "This lists your own keys — it never returns a secret. The raw key is shown once, when you create it.",
      "path": "/v1/rest/keys",
      "title": "1. Authenticate",
      "why": "Every /v1/rest route takes the key in an Authorization header. There is no query-string key and no cookie path for a script."
    },
    {
      "curl": "curl -sS 'https://api.jomform.com/v1/rest/keys'",
      "expectContains": "\"error\"",
      "expectStatus": 401,
      "id": "no-key",
      "method": "GET",
      "needsFixture": false,
      "needsKey": false,
      "note": "The body is {\"error\":\"authentication required\"} — the API's own wording, not a fixed error catalogue.",
      "path": "/v1/rest/keys",
      "title": "2. What a missing credential gets",
      "why": "An unauthenticated call is refused with 401 before any account is resolved, so a misconfigured client fails loudly instead of reading someone else's data."
    },
    {
      "curl": "curl -sS -X POST 'https://api.jomform.com/v1/rest/forms' \\\n  -H \"Authorization: Bearer $JOMFORM_API_KEY\" \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"title\":\"Docs example form\"}'",
      "expectContains": "\"slug\"",
      "expectStatus": 201,
      "id": "create-form",
      "method": "POST",
      "needsFixture": false,
      "needsKey": true,
      "note": "Leave slug out and it is generated from the title and made unique in the workspace. The response carries mode (payment by default) and the draft's url.",
      "path": "/v1/rest/forms",
      "title": "3. Create a form",
      "why": "A new form is created as a draft. Nothing is live until you publish it."
    },
    {
      "curl": "curl -sS -X POST 'https://api.jomform.com/v1/rest/forms/{{form_id}}/publish' \\\n  -H \"Authorization: Bearer $JOMFORM_API_KEY\"",
      "expectContains": "\"status\"",
      "expectStatus": 200,
      "id": "publish-form",
      "method": "POST",
      "needsFixture": true,
      "needsKey": true,
      "note": "A payment form with no products is refused here with 409 and requires_ack: true, because it could never take money; send {\"acknowledge\":true} to publish it anyway.",
      "path": "/v1/rest/forms/{{form_id}}/publish",
      "title": "4. Publish it",
      "why": "Publishing is what makes the form live at its URL."
    },
    {
      "curl": "curl -sS 'https://api.jomform.com/v1/rest/forms' \\\n  -H \"Authorization: Bearer $JOMFORM_API_KEY\"",
      "expectContains": "\"mode\"",
      "expectStatus": 200,
      "id": "read-forms",
      "method": "GET",
      "needsFixture": true,
      "needsKey": true,
      "note": "mode is payment or response, so a response form is never mistaken for an order form.",
      "path": "/v1/rest/forms",
      "title": "5. Read your forms back",
      "why": "The list carries everything needed to pick a form id: slug, title, status, mode and public URL."
    },
    {
      "curl": "curl -sS 'https://api.jomform.com/v1/rest/sales?page=1\u0026limit=15' \\\n  -H \"Authorization: Bearer $JOMFORM_API_KEY\"",
      "expectContains": "\"items\"",
      "expectStatus": 200,
      "id": "list-sales",
      "method": "GET",
      "needsFixture": true,
      "needsKey": true,
      "note": "Without page this route answers with a plain array instead — the same rows, the older shape the dashboard reads.",
      "path": "/v1/rest/sales?page=1\u0026limit=15",
      "title": "6. Read the sales this form produced",
      "why": "The last step of the end-to-end flow: what the form actually recorded."
    }
  ]
}
;
