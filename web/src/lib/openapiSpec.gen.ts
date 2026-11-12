// GENERATED FILE — DO NOT EDIT.
//
// The JomForm REST API contract (OpenAPI 3.1), rendered from
// api/internal/httpapi/openapi_routes.go — the same registry the API's own
// route table is pinned to by api/internal/httpapi/openapi_parity_test.go.
//
// The /docs/api page imports THIS module and renders the endpoint reference
// from it, so the page cannot describe an endpoint the API does not have.
//
// Regenerate:  cd api && go run ./cmd/openapi-gen
// A stale copy fails the api test suite (TestGeneratedArtifactsAreInSync).

export type OpenApiOperation = {
  operationId?: string;
  summary?: string;
  description?: string;
  tags?: string[];
  parameters?: { name: string; in: string; required?: boolean; description?: string; schema?: unknown }[];
  requestBody?: unknown;
  responses?: Record<string, unknown>;
  "x-jomform-access"?: string;
  "x-jomform-rate-limit"?: { scope?: string; policy?: string };
  // Any further OpenAPI field — deliberately open, see OpenApiSpec.
  [k: string]: unknown;
};

// The document's own keys, typed only as far as a consumer needs them. The
// generated object is a full OpenAPI document, so everything beyond these
// fields is intentionally left loose: a strict shape here would only mean a
// type fight every time the generator emits one more standard field.
export type OpenApiSpec = {
  openapi: string;
  info: { title: string; version: string; description?: string; license?: { name: string } };
  servers: { url: string; description?: string }[];
  tags: { name: string }[];
  paths: Record<string, Record<string, OpenApiOperation>>;
  components: { schemas: Record<string, unknown> };
  "x-jomform-scope"?: string;
  "x-jomform-cors"?: string;
  "x-jomform-versioning"?: string;
  // Any further OpenAPI field (webhooks, externalDocs, …).
  [k: string]: unknown;
};

export const OPENAPI_SPEC: OpenApiSpec = {
  "components": {
    "schemas": {
      "BulkResult": {
        "properties": {
          "deleted": {
            "type": "integer"
          },
          "edge": {
            "description": "Present when the action left a publicly served status and cached public artefacts (share card, QR PNG) had to go: what the CDN said.",
            "enum": [
              "purged",
              "not_configured",
              "out_of_zone",
              "failed"
            ],
            "type": "string"
          },
          "edge_max_age_s": {
            "description": "Present with any edge value other than purged: the freshness window that now bounds how long the old copies can still be served.",
            "type": "integer"
          },
          "needs_products": {
            "items": {
              "type": "string"
            },
            "type": "array"
          },
          "updated": {
            "type": "integer"
          }
        },
        "type": "object"
      },
      "Count": {
        "properties": {
          "count": {
            "type": "integer"
          }
        },
        "required": [
          "count"
        ],
        "type": "object"
      },
      "Deleted": {
        "properties": {
          "deleted": {
            "type": "boolean"
          }
        },
        "type": "object"
      },
      "Error": {
        "description": "Every failure body is `{\"error\": \"...\"}`. A few routes add fields: publish adds `code`, `requires_ack` and (for `no_product_selected`) `block_id` when a payment form cannot take money.",
        "properties": {
          "code": {
            "type": "string"
          },
          "error": {
            "type": "string"
          },
          "requires_ack": {
            "type": "boolean"
          }
        },
        "required": [
          "error"
        ],
        "type": "object"
      },
      "Event": {
        "properties": {
          "created_at": {
            "type": "string"
          },
          "id": {
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "properties": {
            "additionalProperties": true,
            "type": "object"
          }
        },
        "type": "object"
      },
      "EventList": {
        "items": {
          "$ref": "#/components/schemas/Event"
        },
        "type": "array"
      },
      "Form": {
        "properties": {
          "config": {
            "additionalProperties": true,
            "type": "object"
          },
          "id": {
            "type": "string"
          },
          "is_private": {
            "type": "boolean"
          },
          "mode": {
            "enum": [
              "payment",
              "response"
            ],
            "type": "string"
          },
          "slug": {
            "type": "string"
          },
          "status": {
            "enum": [
              "draft",
              "private",
              "published",
              "unlisted"
            ],
            "type": "string"
          },
          "theme": {
            "additionalProperties": true,
            "type": "object"
          },
          "title": {
            "type": "string"
          },
          "updated_at": {
            "type": "string"
          },
          "url": {
            "type": "string"
          }
        },
        "required": [
          "id",
          "slug",
          "title",
          "status",
          "mode"
        ],
        "type": "object"
      },
      "FormList": {
        "items": {
          "$ref": "#/components/schemas/Form"
        },
        "type": "array"
      },
      "FormStats": {
        "additionalProperties": true,
        "description": "One row per form: the form's own mode, its entry count (responses for a response form, sales for a payment form) and its money figures.",
        "type": "object"
      },
      "FormStatsList": {
        "items": {
          "$ref": "#/components/schemas/FormStats"
        },
        "type": "array"
      },
      "FormStatus": {
        "properties": {
          "edge": {
            "description": "Present when this transition left a publicly served status (published or unlisted) and the form's cached public artefacts — the share card /og/{slug}.png and the QR PNG /qr/{slug}.png — had to be dropped: what the CDN said. Absent when nothing was invalidated.",
            "enum": [
              "purged",
              "not_configured",
              "out_of_zone",
              "failed"
            ],
            "type": "string"
          },
          "edge_error": {
            "description": "Present with edge=failed: what the CDN answered.",
            "type": "string"
          },
          "edge_max_age_s": {
            "description": "Present with any edge value other than purged: the cache freshness window (the same one both artefacts declare) that then bounds how long the old copies can still be served.",
            "type": "integer"
          },
          "edge_urls": {
            "description": "The artefact URL prefixes the purge named: both paths on each public host.",
            "items": {
              "type": "string"
            },
            "type": "array"
          },
          "status": {
            "type": "string"
          }
        },
        "type": "object"
      },
      "FormWrite": {
        "description": "Form fields to create or change. `config` is the builder document (blocks); sending it replaces the stored config.",
        "properties": {
          "config": {
            "additionalProperties": true,
            "type": "object"
          },
          "slug": {
            "type": "string"
          },
          "theme": {
            "additionalProperties": true,
            "type": "object"
          },
          "title": {
            "type": "string"
          }
        },
        "type": "object"
      },
      "Key": {
        "properties": {
          "id": {
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "scopes": {
            "description": "readonly may read; readwrite may also write. A legacy admin scope exists on old keys and behaves as a superset; it is not offered to new keys.",
            "items": {
              "enum": [
                "readonly",
                "readwrite"
              ],
              "type": "string"
            },
            "type": "array"
          }
        },
        "required": [
          "id",
          "name",
          "scopes"
        ],
        "type": "object"
      },
      "KeyCreated": {
        "properties": {
          "id": {
            "type": "string"
          },
          "key": {
            "description": "The raw key. Returned in this response only — it is never readable again.",
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "scopes": {
            "items": {
              "type": "string"
            },
            "type": "array"
          }
        },
        "required": [
          "id",
          "key"
        ],
        "type": "object"
      },
      "KeyList": {
        "items": {
          "$ref": "#/components/schemas/Key"
        },
        "type": "array"
      },
      "Media": {
        "properties": {
          "form_id": {
            "type": "string"
          },
          "id": {
            "type": "string"
          },
          "key": {
            "type": "string"
          },
          "mime": {
            "type": "string"
          },
          "scan_reason": {
            "type": "string"
          },
          "scan_status": {
            "description": "Only `ok` is served for a code asset.",
            "type": "string"
          },
          "size": {
            "type": "integer"
          },
          "sizes": {
            "additionalProperties": true,
            "type": "object"
          },
          "source": {
            "type": "string"
          },
          "url": {
            "type": "string"
          }
        },
        "required": [
          "id",
          "url"
        ],
        "type": "object"
      },
      "MediaList": {
        "items": {
          "$ref": "#/components/schemas/Media"
        },
        "type": "array"
      },
      "Paged": {
        "properties": {
          "items": {
            "items": {
              "type": "object"
            },
            "type": "array"
          },
          "limit": {
            "type": "integer"
          },
          "page": {
            "type": "integer"
          },
          "total": {
            "type": "integer"
          }
        },
        "required": [
          "items",
          "page",
          "limit",
          "total"
        ],
        "type": "object"
      },
      "Product": {
        "properties": {
          "active": {
            "type": "boolean"
          },
          "billing_period": {
            "enum": [
              "month",
              "year",
              ""
            ],
            "type": "string"
          },
          "created_at": {
            "type": "string"
          },
          "description": {
            "type": "string"
          },
          "id": {
            "type": "string"
          },
          "images": {
            "items": {
              "type": "string"
            },
            "type": "array"
          },
          "name": {
            "type": "string"
          },
          "price_sen": {
            "description": "Price in sen (1/100).",
            "type": "integer"
          },
          "schedule_mode": {
            "enum": [
              "from_payment",
              "catch_up",
              "skip_missed"
            ],
            "type": "string"
          },
          "stock": {
            "description": "null means unlimited.",
            "type": [
              "integer",
              "null"
            ]
          },
          "subscription_charges": {
            "description": "null means charge for as long as the subscription runs.",
            "type": [
              "integer",
              "null"
            ]
          },
          "trial_days": {
            "type": "integer"
          },
          "type": {
            "enum": [
              "one_time",
              "subscription"
            ],
            "type": "string"
          },
          "updated_at": {
            "type": "string"
          },
          "variations": {
            "items": {
              "$ref": "#/components/schemas/Variation"
            },
            "type": "array"
          }
        },
        "required": [
          "id",
          "name",
          "price_sen",
          "type"
        ],
        "type": "object"
      },
      "ProductList": {
        "items": {
          "$ref": "#/components/schemas/Product"
        },
        "type": "array"
      },
      "ProductWrite": {
        "description": "Product fields to create or change. On PUT, omitted fields are unchanged.",
        "properties": {
          "active": {
            "type": "boolean"
          },
          "billing_period": {
            "enum": [
              "month",
              "year"
            ],
            "type": "string"
          },
          "description": {
            "type": "string"
          },
          "images": {
            "items": {
              "format": "uri",
              "type": "string"
            },
            "type": "array"
          },
          "name": {
            "type": "string"
          },
          "price_sen": {
            "minimum": 0,
            "type": "integer"
          },
          "schedule_mode": {
            "enum": [
              "from_payment",
              "catch_up",
              "skip_missed"
            ],
            "type": "string"
          },
          "stock": {
            "type": [
              "integer",
              "null"
            ]
          },
          "subscription_charges": {
            "type": [
              "integer",
              "null"
            ]
          },
          "trial_days": {
            "minimum": 0,
            "type": "integer"
          },
          "type": {
            "enum": [
              "one_time",
              "subscription"
            ],
            "type": "string"
          },
          "variations": {
            "items": {
              "$ref": "#/components/schemas/Variation"
            },
            "type": "array"
          }
        },
        "type": "object"
      },
      "Subscription": {
        "properties": {
          "amount_sen": {
            "description": "Amount in sen (1/100).",
            "type": "integer"
          },
          "created_at": {
            "type": "string"
          },
          "customer": {
            "type": "string"
          },
          "id": {
            "type": "string"
          },
          "next_charge_at": {
            "type": "string"
          },
          "period": {
            "enum": [
              "month",
              "year"
            ],
            "type": "string"
          },
          "product_id": {
            "type": "string"
          },
          "product_name": {
            "type": "string"
          },
          "retry_attempts": {
            "type": "integer"
          },
          "status": {
            "enum": [
              "active",
              "past_due",
              "cancelled"
            ],
            "type": "string"
          }
        },
        "required": [
          "id",
          "status",
          "amount_sen"
        ],
        "type": "object"
      },
      "SubscriptionList": {
        "items": {
          "$ref": "#/components/schemas/Subscription"
        },
        "type": "array"
      },
      "Summary": {
        "additionalProperties": true,
        "description": "Workspace totals: sale counts by status, revenue net of refunds, and record counts.",
        "type": "object"
      },
      "Variation": {
        "properties": {
          "billing_period": {
            "enum": [
              "month",
              "year",
              null
            ],
            "type": [
              "string",
              "null"
            ]
          },
          "id": {
            "type": "string"
          },
          "images": {
            "items": {
              "type": "string"
            },
            "type": "array"
          },
          "name": {
            "type": "string"
          },
          "price_sen": {
            "description": "Price in sen (1/100).",
            "type": "integer"
          },
          "sort_order": {
            "type": "integer"
          },
          "stock": {
            "description": "null means unlimited.",
            "type": [
              "integer",
              "null"
            ]
          },
          "subscription_charges": {
            "type": [
              "integer",
              "null"
            ]
          },
          "trial_days": {
            "type": [
              "integer",
              "null"
            ]
          }
        },
        "required": [
          "name",
          "price_sen"
        ],
        "type": "object"
      },
      "Webhook": {
        "properties": {
          "active": {
            "type": "boolean"
          },
          "created_at": {
            "type": "string"
          },
          "events": {
            "items": {
              "type": "string"
            },
            "type": "array"
          },
          "id": {
            "type": "string"
          },
          "secret": {
            "type": "string"
          },
          "url": {
            "format": "uri",
            "type": "string"
          }
        },
        "type": "object"
      },
      "WebhookList": {
        "items": {
          "$ref": "#/components/schemas/Webhook"
        },
        "type": "array"
      }
    }
  },
  "info": {
    "description": "The JomForm REST API. Every route is under `/v1/rest/` and is authenticated with `Authorization: Bearer \u003capi key\u003e`; the same key also authenticates the JomForm MCP server. Keys are created in Settings → API keys, or with `POST /v1/rest/keys`.\n\n**Server to server only.** The API sends no `Access-Control-Allow-*` headers, so a browser page on another origin cannot read a response. This is deliberate, not an omission: the API is built for server-side calls.\n\n**Rate limits are the exception, not the rule.** Within this API only the routes marked `x-jomform-rate-limit` are throttled; every other route has no rate limit at all. Do not build a client that assumes a uniform quota.",
    "license": {
      "name": "MIT"
    },
    "title": "JomForm REST API",
    "version": "1.0.0"
  },
  "openapi": "3.1.0",
  "paths": {
    "/v1/rest/analytics/forms": {
      "get": {
        "description": "Each row counts exactly one table — a response form's responses, or a payment form's sales — so the two are never summed.",
        "operationId": "formAnalytics",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/FormStatsList"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Per-form entry counts and money",
        "tags": [
          "Analytics"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/analytics/products": {
      "get": {
        "operationId": "productAnalytics",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Per-product sales totals",
        "tags": [
          "Analytics"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/chat": {
      "get": {
        "operationId": "handleChatStatus",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Report whether the in-app assistant is available",
        "tags": [
          "In-app assistant"
        ],
        "x-jomform-access": "session"
      },
      "post": {
        "description": "Session-only, and read-only by construction: the assistant's tool calls run under a readonly identity, so nothing typed into the box can write. The limit is enforced in the handler, keyed on the user rather than the IP.\n\nResponse: The assistant's reply, the tool steps it took, and whether it filed feedback.\n\nRate limit: 60 messages per hour per user, burst 10.",
        "operationId": "handleChat",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "429": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Rate limited. The refusals of the throttled middleware carry `Retry-After`: the number of seconds THIS client must wait before it is admitted again, read from the bucket that refused it (so it differs per route — about a second on the coarse per-IP guard that refills at 1/s, up to an hour on the hourly guards)."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Ask the in-app assistant one message",
        "tags": [
          "In-app assistant"
        ],
        "x-jomform-access": "session",
        "x-jomform-rate-limit": {
          "policy": "60 messages per hour per user, burst 10",
          "scope": "handler"
        }
      }
    },
    "/v1/rest/chat/cancel": {
      "post": {
        "description": "Same request shape as confirm (token + args_hash), and it shares confirm's bucket. Withdrawing a proposal executes nothing.\n\nResponse: status and the tool whose proposal was discarded.\n\nRate limit: 30 confirmations per hour per user, burst 5.",
        "operationId": "handleChatCancel",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "429": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Rate limited. The refusals of the throttled middleware carry `Retry-After`: the number of seconds THIS client must wait before it is admitted again, read from the bucket that refused it (so it differs per route — about a second on the coarse per-IP guard that refills at 1/s, up to an hour on the hourly guards)."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Discard a write the assistant proposed",
        "tags": [
          "In-app assistant"
        ],
        "x-jomform-access": "session",
        "x-jomform-rate-limit": {
          "policy": "30 confirmations per hour per user, burst 5",
          "scope": "handler"
        }
      }
    },
    "/v1/rest/chat/confirm": {
      "post": {
        "description": "The only place a proposed write is executed. The body carries the opaque single-use token the browser holds and the args_hash the card showed; there is no confirm flag and nothing the caller can set to make the write happen, because the token IS the authorisation. A model cannot reach this endpoint — it is never given the token.\n\nResponse: status, the tool that ran, and its result.\n\nRate limit: 30 confirmations per hour per user, burst 5.",
        "operationId": "handleChatConfirm",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "429": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Rate limited. The refusals of the throttled middleware carry `Retry-After`: the number of seconds THIS client must wait before it is admitted again, read from the bucket that refused it (so it differs per route — about a second on the coarse per-IP guard that refills at 1/s, up to an hour on the hourly guards)."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Execute a write the assistant proposed",
        "tags": [
          "In-app assistant"
        ],
        "x-jomform-access": "session",
        "x-jomform-rate-limit": {
          "policy": "30 confirmations per hour per user, burst 5",
          "scope": "handler"
        }
      }
    },
    "/v1/rest/chat/sessions": {
      "get": {
        "description": "Archived conversations are included only with ?archived=1. A conversation belongs to one person in one workspace, so switching workspace shows a different list.\n\nResponse: The sidebar: each conversation's id, title, timestamps, archived flag and message count, most recently used first.",
        "operationId": "handleChatSessionsList",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "List your saved conversations in this workspace",
        "tags": [
          "In-app assistant"
        ],
        "x-jomform-access": "session"
      },
      "post": {
        "description": "Capped at 200 conversations per person per workspace; the refusal names the way out (delete old ones). The row is created eagerly so the sidebar shows the effect of the button.\n\nResponse: The new conversation; its title is empty until the first message names it.",
        "operationId": "handleChatSessionsCreate",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Start a new conversation",
        "tags": [
          "In-app assistant"
        ],
        "x-jomform-access": "session"
      }
    },
    "/v1/rest/chat/sessions/{id}": {
      "delete": {
        "description": "A real delete: the messages are removed with the conversation. Archiving is the reversible action.\n\nResponse: status=deleted.",
        "operationId": "handleChatSessionDelete",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Delete one of your conversations",
        "tags": [
          "In-app assistant"
        ],
        "x-jomform-access": "session"
      },
      "get": {
        "description": "404 for an unknown id AND for another person's id. A proposal's confirmation token is never stored, so a restored conversation offers no button that could execute anything.\n\nResponse: The conversation and its turns, oldest first, capped to the assistant's own window; each answer carries the tool trail it left.",
        "operationId": "handleChatSessionGet",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Read one saved conversation and its messages",
        "tags": [
          "In-app assistant"
        ],
        "x-jomform-access": "session"
      },
      "patch": {
        "description": "Send title and/or archived; a body with neither is a 400. An empty title clears the name, and the list falls back to the first message. Archiving is reversible.\n\nResponse: The updated conversation.",
        "operationId": "handleChatSessionPatch",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Rename or archive one of your conversations",
        "tags": [
          "In-app assistant"
        ],
        "x-jomform-access": "session"
      }
    },
    "/v1/rest/currency/rates": {
      "get": {
        "operationId": "getCurrencyRates",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read cached exchange rates (BNM)",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/dashboard-bundle": {
      "get": {
        "description": "Response: An object with currency, me, workspaces, chip_status, keys, domains, webhooks, social, einvoice, products, summary, sales, form_analytics and product_analytics. currency is the active workspace's currency — the money every price on the dashboard is labelled in.",
        "operationId": "dashboardBundle",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "One call for the whole dashboard payload",
        "tags": [
          "Account"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/domains": {
      "get": {
        "operationId": "listCustomDomains",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List custom domains",
        "tags": [
          "Domains"
        ],
        "x-jomform-access": "read"
      },
      "post": {
        "operationId": "addCustomDomain",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Add a custom domain",
        "tags": [
          "Domains"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/domains/{id}": {
      "delete": {
        "operationId": "deleteCustomDomainByID",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Deleted"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Delete a custom domain",
        "tags": [
          "Domains"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/domains/{id}/auto-cname": {
      "post": {
        "operationId": "autoCreateCNAMEForDomain",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Create the CNAME through the linked Cloudflare account",
        "tags": [
          "Domains"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/domains/{id}/custom-cert": {
      "post": {
        "operationId": "uploadCustomCert",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Upload a custom TLS certificate",
        "tags": [
          "Domains"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/domains/{id}/tls-email": {
      "post": {
        "operationId": "setDomainTLSEmail",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set the ACME contact email for a domain",
        "tags": [
          "Domains"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/domains/{id}/tls-mode": {
      "post": {
        "operationId": "setDomainTLSMode",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set a domain's TLS mode",
        "tags": [
          "Domains"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/domains/{id}/verify": {
      "post": {
        "operationId": "verifyCustomDomainByID",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Verify a custom domain's DNS",
        "tags": [
          "Domains"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/email-deliveries": {
      "get": {
        "operationId": "listEmailDeliveries",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Paged"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List email delivery events",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/events": {
      "get": {
        "operationId": "listEvents",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Paged"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List the account's audit/behaviour events",
        "tags": [
          "Analytics"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/events/breakdown": {
      "get": {
        "operationId": "breakdownEvents",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Break events down by name",
        "tags": [
          "Analytics"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/events/count": {
      "get": {
        "operationId": "countEvents",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Count"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Count events",
        "tags": [
          "Analytics"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/events/stream": {
      "get": {
        "description": "Server-sent events. The stream is account-scoped by the authenticating credential, so it can never carry another tenant's events.\n\nResponse: A text/event-stream of sale.paid, sale.failed, response.submitted and subscription.charged events for the authenticated account.",
        "operationId": "handleEventsStream",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Stream this workspace's events (SSE)",
        "tags": [
          "Analytics"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/feedback": {
      "get": {
        "operationId": "handleListFeedback",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "List your feedback threads",
        "tags": [
          "Feedback"
        ],
        "x-jomform-access": "session"
      },
      "post": {
        "operationId": "handleCreateFeedback",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "File a feedback thread",
        "tags": [
          "Feedback"
        ],
        "x-jomform-access": "session"
      }
    },
    "/v1/rest/feedback/{id}": {
      "get": {
        "operationId": "handleGetFeedback",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Read one of your feedback threads",
        "tags": [
          "Feedback"
        ],
        "x-jomform-access": "session"
      }
    },
    "/v1/rest/feedback/{id}/messages": {
      "post": {
        "operationId": "handleReplyFeedback",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Reply on one of your feedback threads",
        "tags": [
          "Feedback"
        ],
        "x-jomform-access": "session"
      }
    },
    "/v1/rest/forms": {
      "get": {
        "description": "Every form with its slug, status, public URL and mode. mode is payment (default) or response.",
        "operationId": "listForms",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/FormList"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List forms",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "read"
      },
      "post": {
        "description": "A blank slug is generated from the title and uniquified within the workspace. An explicit slug must be a usable URL path segment — 1-40 characters of a-z, 0-9 and hyphens, and not a name JomForm reserves for itself (a workspace-claim label such as login/api/docs, or a route served on the same host such as privacy/terms/refund/healthz): a malformed slug is refused with 400 form.slugInvalid and a reserved one with 400 form.slugReserved, before anything is stored. The slug must also be unique among the workspace's live forms: a slug another live form already serves is refused with 409 form.slugInUse (the same code and sentence a slug rename onto it answers). The form is created as a draft; publish it with POST /v1/rest/forms/{id}/publish.",
        "operationId": "createForm",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/FormWrite"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Form"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Create a form",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/ai-generate": {
      "post": {
        "description": "This route calls a language model and is charged accordingly, which is why it — and only it among the form routes — is throttled.\n\nResponse: A draft form config (blocks + theme) to review and then save.\n\nRate limit: 5 per hour per IP, burst 3.",
        "operationId": "aiGenerateForm",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Form"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          },
          "429": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Rate limited. The refusals of the throttled middleware carry `Retry-After`: the number of seconds THIS client must wait before it is admitted again, read from the bucket that refused it (so it differs per route — about a second on the coarse per-IP guard that refills at 1/s, up to an hour on the hourly guards)."
          }
        },
        "summary": "Draft a form config from a prompt (AI)",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write",
        "x-jomform-rate-limit": {
          "policy": "5 per hour per IP, burst 3",
          "scope": "middleware"
        }
      }
    },
    "/v1/rest/forms/bulk": {
      "post": {
        "description": "Up to 500 ids. A payment form with no products is skipped on publish and reported in needs_products rather than being published empty. A draft or delete also drops the cached social card of every form that was serving one (see the status route's edge fields).",
        "operationId": "bulkForms",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/BulkResult"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Publish, draft or delete many forms at once",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/trash": {
      "get": {
        "description": "Response: The account's trashed forms, each with the number of sales attached to it, so a caller can tell a restorable form from one that can never be destroyed.",
        "operationId": "listTrashedForms",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List the forms in the trash",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/forms/{id}": {
      "delete": {
        "description": "The form goes to the trash and can be restored; its public page and its share card stop being served. A card the CDN had cached is dropped in the same request and the reply names the outcome in edge.",
        "operationId": "deleteForm",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Deleted"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Delete a form (soft delete)",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write"
      },
      "get": {
        "description": "Includes the current config (blocks) and theme, so you can read, mutate the parts you want and send the whole config back.",
        "operationId": "getForm",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Form"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read one form",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "read"
      },
      "patch": {
        "description": "Partial update: an omitted title, config, theme or slug keeps the stored value, so a body that renames the form does not have to resend its title. An empty title (or slug) also means \"leave unchanged\" — this route never blanks a form's title. A slug is held to the same rule as create: 1-40 characters of a-z, 0-9 and hyphens and not a name JomForm reserves, so a malformed slug is 400 form.slugInvalid and a reserved one 400 form.slugReserved. A slug another live form already serves is refused with 409 form.slugInUse, and nothing else in the same request is written. A rename leaves a temporary 301 from the old URL.",
        "operationId": "updateForm",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/FormWrite"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Form"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Update a form's title, config, theme or slug",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/chip-feeds": {
      "get": {
        "operationId": "getFormChipFeeds",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read a form's CHIP feeds",
        "tags": [
          "Payments (CHIP)"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "description": "Admin only: these feeds carry payment credentials.",
        "operationId": "setFormChipFeeds",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set a form's CHIP feeds",
        "tags": [
          "Payments (CHIP)"
        ],
        "x-jomform-access": "admin"
      }
    },
    "/v1/rest/forms/{id}/chip-override": {
      "delete": {
        "operationId": "clearFormChipOverride",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Clear a form's CHIP credential override",
        "tags": [
          "Payments (CHIP)"
        ],
        "x-jomform-access": "admin"
      },
      "put": {
        "operationId": "setFormChipOverride",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Override a form's CHIP credentials",
        "tags": [
          "Payments (CHIP)"
        ],
        "x-jomform-access": "admin"
      }
    },
    "/v1/rest/forms/{id}/confirmations": {
      "get": {
        "operationId": "getFormConfirmations",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read a form's confirmations",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "operationId": "saveFormConfirmations",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set a form's confirmations",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/email-settings": {
      "delete": {
        "operationId": "clearFormEmailOverrides",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Clear a form's email overrides (inherit the workspace again)",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      },
      "get": {
        "description": "Refused for a readonly key: this route is reached from the email settings editor and is gated as a write even though it only reads.",
        "operationId": "getFormEmailSettings",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read a form's email overrides",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      },
      "put": {
        "operationId": "saveFormEmailSettings",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set a form's email overrides",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/fee-override": {
      "get": {
        "operationId": "getFormFeeOverride",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read a form's fee override",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "operationId": "putFormFeeOverride",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set a form's fee override ('' inherit, 'off' switch off)",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/menu": {
      "get": {
        "operationId": "getFormMenu",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read a form's restaurant-menu configuration",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "operationId": "putFormMenu",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set a form's restaurant-menu configuration",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/notifications": {
      "get": {
        "description": "Gated as a write, like the email-settings read above.",
        "operationId": "listFormNotifications",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List a form's notification rules",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      },
      "post": {
        "operationId": "createFormNotification",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Create a notification rule",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/notifications/{nid}": {
      "delete": {
        "operationId": "deleteFormNotification",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          },
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "nid",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Delete a notification rule",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      },
      "put": {
        "operationId": "updateFormNotification",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          },
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "nid",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Update a notification rule",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/publish": {
      "post": {
        "description": "The form goes live at its URL. A payment form that cannot take money is refused with 409 and requires_ack=true, because it could never sell anything; send {\"acknowledge\":true} or ?acknowledge=1 to publish it anyway. The body's code says which shape: no_products (no product block at all) or no_product_selected (a product block exists but names no product — its block_id names the block, and buyers are served \"This form is not accepting orders right now\").",
        "operationId": "publishForm",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/FormStatus"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Publish a form",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/purge": {
      "delete": {
        "description": "Only a form already in the trash can be destroyed. The body must repeat the form's own slug as confirm_slug; a mismatch is refused with 400. A form that has ever been sold is refused with 409 and sales_count, because sales history is never destroyed. The destroyed form's share card and QR PNG are dropped from the CDN in the same request and the reply names the outcome in edge.\n\nResponse: status=purged and the form id.",
        "operationId": "purgeForm",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Destroy a trashed form for good",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/qr-stand": {
      "get": {
        "description": "Served for a form the caller owns at ANY status, including a draft: this is the merchant's own print artefact, and a form is usually printed before it goes live. It is what the dashboard's \"Print stand\" action opens. The public /qr/{slug}/stand route is the buyer-facing one and follows the page's status rule instead — a draft's stand answers 404 there, because the stand prints the form's title.\n\nResponse: An HTML document: the printable QR stand — the form's title, its public URL encoded as a QR image, and the ?table=N badge when a table was given.",
        "operationId": "getFormQRStand",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "text/html; charset=utf-8": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Render a form's printable QR stand",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/forms/{id}/restore": {
      "post": {
        "description": "Restoring touches no CASCADE. If another form has taken the trashed form's slug in the meantime the restore is refused with 409 — change that form's slug first, then restore.\n\nResponse: status=restored and the form id.",
        "operationId": "restoreForm",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Restore a form from the trash",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/restrictions": {
      "put": {
        "operationId": "putFormRestrictions",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set a form's open/close window and entry limit",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/settings": {
      "put": {
        "operationId": "putFormSettings",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set per-form display settings",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/status": {
      "post": {
        "description": "status=private also sets an access password if one is supplied (hashed, never returned). Leaving a publicly served status (published or unlisted) drops the form's cached share card (/og/{slug}.png) and QR PNG (/qr/{slug}.png) at the CDN in the SAME request: the reply then carries edge (purged | not_configured | out_of_zone | failed) and, when nothing was purged, edge_max_age_s, the freshness window that then bounds how long the old copies can still be served. The status change itself never fails because the purge did.",
        "operationId": "setFormStatus",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/FormStatus"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set a form's status (draft | private | published)",
        "tags": [
          "Forms"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/forms/{id}/submissions": {
      "get": {
        "description": "Response-mode forms store responses, not sales, so this is how you read them. Supports ?page= and ?limit= (max 100).",
        "operationId": "listSubmissions",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Paged"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List a response form's submissions",
        "tags": [
          "Submissions"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/forms/{id}/submissions/count": {
      "get": {
        "operationId": "countSubmissions",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Count"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Count a response form's submissions",
        "tags": [
          "Submissions"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/forms/{id}/tracking": {
      "put": {
        "operationId": "putFormTracking",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set per-form analytics/tracking",
        "tags": [
          "Form settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/keys": {
      "get": {
        "operationId": "listKeys",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/KeyList"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List API keys (never the secret)",
        "tags": [
          "API keys"
        ],
        "x-jomform-access": "read"
      },
      "post": {
        "description": "Returns the raw key exactly once — store it then. Scopes default to [\"readonly\"]; the supported values are readonly and readwrite. The same key authenticates MCP.",
        "operationId": "createKey",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/KeyCreated"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Create an API key",
        "tags": [
          "API keys"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/keys/{id}": {
      "delete": {
        "operationId": "revokeKey",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Deleted"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Revoke an API key",
        "tags": [
          "API keys"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/media": {
      "get": {
        "description": "Newest first and paginated (?page/?limit; the limit caps at 100), with optional ?form=\u003cid\u003e and ?source=merchant|customer filters. Pass ?url=\u003ca media URL this workspace owns\u003e instead of ?page to ask for that ONE row: the URL is resolved to its object key, so a row older than the newest 100 rows — or a URL whose advertised host has changed since it was stored — is still found. A URL this workspace does not own answers an EMPTY items list with 200, not 404: 'not in this library' is an ordinary answer, not a failed request.",
        "operationId": "listMedia",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/MediaList"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List uploaded media",
        "tags": [
          "Media"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/media/trash": {
      "get": {
        "description": "object_present reports whether the bytes are still in the store. A row can be in the bin while the object is already gone (removed by an operator or a bucket lifecycle rule), which is why a restore checks rather than assumes.\n\nResponse: The workspace's trashed media, each with deleted_at (RFC3339) and object_present.",
        "operationId": "listTrashedMedia",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List the media in the trash",
        "tags": [
          "Media"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/media/{id}": {
      "delete": {
        "description": "This marks the row trashed. The object itself is NOT removed from storage and the bytes keep counting against the workspace's 1GB quota: the bin holds real storage, so it is not a free tier. Destroy the object for good with DELETE /v1/rest/media/{id}/purge, which is also where the quota is released.",
        "operationId": "deleteMedia",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Deleted"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Move a media item to the trash",
        "tags": [
          "Media"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/media/{id}/purge": {
      "delete": {
        "description": "Only a media row already in the trash can be destroyed; a live row is a 404. The object and its derived variants (sized images, PDF preview) are removed from the store and the bytes are released from the 1GB quota — this is the only path that releases them.\n\nResponse: status=purged, the media id and the object key that was removed.",
        "operationId": "purgeMedia",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Destroy a trashed media item for good",
        "tags": [
          "Media"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/media/{id}/restore": {
      "post": {
        "description": "Clears deleted_at, so the same URL works again — the bytes were never removed. If the object is no longer in the store the restore is refused with 409 and nothing is changed, because restoring would put a broken URL back in the library. Quota is untouched: the bytes were never released on soft delete.\n\nResponse: status=restored, the media id and the public URL that serves it (a code asset's URL is on the workspace's own host; an image's is the media host).",
        "operationId": "restoreMedia",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Restore a media item from the trash",
        "tags": [
          "Media"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/products": {
      "get": {
        "description": "Response: The workspace's live products, each with its variations.",
        "operationId": "listProducts",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ProductList"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List the workspace's products",
        "tags": [
          "Products"
        ],
        "x-jomform-access": "read"
      },
      "post": {
        "description": "Money is integer sen. price_sen is required and may not be negative; a missing or null price_sen is refused rather than stored as 0. For a subscription set type=subscription and billing_period=month|year. Image URLs must be https://.",
        "operationId": "createProduct",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/ProductWrite"
              }
            }
          },
          "required": true
        },
        "responses": {
          "201": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Product"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Create a product",
        "tags": [
          "Products"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/products/bulk": {
      "post": {
        "description": "Action-based bulk archive (active=false). Up to 500 ids per call.",
        "operationId": "bulkProducts",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/BulkResult"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Archive many products at once",
        "tags": [
          "Products"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/products/trash": {
      "get": {
        "description": "Each row also carries deleted_at (RFC3339). can_purge is false when the product has a sale or a subscription, which is exactly what DELETE .../purge would refuse with 409.\n\nResponse: The workspace's trashed products, each with its sales_count, subscription_count and can_purge, so a caller can tell a restorable product from one that can never be destroyed.",
        "operationId": "listTrashedProducts",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List the products in the trash",
        "tags": [
          "Products"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/products/{id}": {
      "delete": {
        "description": "This moves the product to the bin — it is not the same as taking it off sale. To take a product off sale and leave it in the catalogue, PUT {\"active\":false} instead; a trashed product is invisible to every other read until it is restored.",
        "operationId": "deleteProduct",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Deleted"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Move a product to the trash",
        "tags": [
          "Products"
        ],
        "x-jomform-access": "write"
      },
      "get": {
        "operationId": "getProduct",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Product"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read one product",
        "tags": [
          "Products"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "description": "Partial update: omitted fields keep their current value. variations is replaced wholesale — send the full set, or [] to turn a variable product back into a simple one.",
        "operationId": "updateProduct",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/ProductWrite"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Product"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Update a product",
        "tags": [
          "Products"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/products/{id}/purge": {
      "delete": {
        "description": "Only a product already in the trash can be destroyed; a live product is a 404. The body must repeat the product's own name as confirm_name; a mismatch is refused with 400. A product that has ever been sold is refused with 409 and sales_count (sales history is never destroyed), and one a subscription still points at is refused with 409 and subscription_count.\n\nResponse: status=purged, the product id and its name.",
        "operationId": "purgeProduct",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Destroy a trashed product for good",
        "tags": [
          "Products"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/products/{id}/restore": {
      "post": {
        "description": "Restoring touches no CASCADE and destroys nothing. Unlike a form restore there is no slug to collide with — products have no unique name — so this only fails when the product is not in the trash (404).\n\nResponse: status=restored and the product id.",
        "operationId": "restoreProduct",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Restore a product from the trash",
        "tags": [
          "Products"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/profile/avatar": {
      "put": {
        "description": "The URL must point at JomForm-hosted media; arbitrary external URLs are refused.",
        "operationId": "updateProfileAvatar",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "503": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The backing service for this route is not configured on this deployment."
          }
        },
        "summary": "Set the signed-in user's avatar URL",
        "tags": [
          "Profile"
        ],
        "x-jomform-access": "session"
      }
    },
    "/v1/rest/sales": {
      "get": {
        "description": "Without a form filter this is the whole record set: sales AND response-mode submissions, merged newest first. Filters: ?form_id= ?status= ?from= ?to= ?q= ?page= ?limit=. Omit ?page= for the legacy plain-array shape.",
        "operationId": "listSales",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Paged"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List sales and responses",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/sales/bulk-capture": {
      "post": {
        "operationId": "bulkCaptureSales",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/BulkResult"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Capture many skip-capture authorisations",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/sales/export.csv": {
      "get": {
        "description": "Response: A text/csv body with the same rows and filters as the list.",
        "operationId": "exportSalesCSV",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Export sales/responses as CSV",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/sales/{ref}": {
      "get": {
        "description": "Returns the full sale row plus available_events: every event this sale's current state accepts, each with a stable event name, the dashboard label key, and — when a guard refuses it — enabled=false with the English reason. The dashboard builds its action buttons from this list; an agent can ask before it proposes an action.",
        "operationId": "getSale",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "ref",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read one sale, including its available lifecycle events",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/sales/{ref}/capture": {
      "post": {
        "description": "Omit amount_sen to capture the full authorisation; send it to capture part of it.",
        "operationId": "captureSale",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "ref",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Capture a skip-capture authorisation",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/sales/{ref}/confirm": {
      "post": {
        "operationId": "confirmSale",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "ref",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Confirm a sale (for example a bank transfer)",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/sales/{ref}/email-status": {
      "get": {
        "operationId": "saleEmailStatus",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "ref",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read a sale's receipt email timeline",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/sales/{ref}/refund": {
      "post": {
        "description": "A partial refund keeps the sale paid with refunded_sen set and outstanding_sen still refundable; only reaching the total marks it refunded. An amount that would over-refund is refused.",
        "operationId": "refundSale",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "ref",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Refund a paid sale, in full or in part",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/sales/{ref}/release": {
      "post": {
        "operationId": "releaseSale",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "ref",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Release (void) a skip-capture authorisation",
        "tags": [
          "Sales"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/settings/chip": {
      "delete": {
        "operationId": "deleteChipCreds",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Clear the workspace's CHIP credentials",
        "tags": [
          "Payments (CHIP)"
        ],
        "x-jomform-access": "admin"
      },
      "get": {
        "operationId": "chipStatus",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read whether CHIP is configured",
        "tags": [
          "Payments (CHIP)"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "operationId": "setChipCreds",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set the workspace's CHIP credentials",
        "tags": [
          "Payments (CHIP)"
        ],
        "x-jomform-access": "admin"
      }
    },
    "/v1/rest/settings/chip/test": {
      "post": {
        "description": "Runs the same live CHIP credential check the save path runs and stores nothing. An empty body tests the stored Secret Key; a secret_key field tests a key the merchant has just pasted. Admin only.",
        "operationId": "testChipCreds",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Test CHIP credentials without saving",
        "tags": [
          "Payments (CHIP)"
        ],
        "x-jomform-access": "admin"
      }
    },
    "/v1/rest/settings/custom-domain": {
      "delete": {
        "operationId": "deleteCustomDomain",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Clear the workspace's custom domain",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "write"
      },
      "get": {
        "operationId": "getCustomDomain",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read the workspace's custom domain",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "operationId": "setCustomDomain",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set the workspace's custom domain",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/settings/einvoice": {
      "get": {
        "operationId": "getEinvoicing",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read e-Invoice (LHDN) settings",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "operationId": "putEinvoicing",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set e-Invoice (LHDN) settings",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/settings/payment-fees": {
      "get": {
        "operationId": "getFeeSettings",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read the per-method platform fee + acknowledgement",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "operationId": "putFeeSettings",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set the per-method platform fee (requires the disclosure acknowledgement)",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/settings/tracking": {
      "get": {
        "operationId": "getTracking",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read workspace tracking/analytics",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "operationId": "putTracking",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Set workspace tracking/analytics",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/settings/webhook-key": {
      "get": {
        "description": "Refused for a readonly key: the account's Ed25519 keypair is generated on first use, so the FIRST call on a workspace writes and this read is gated as a write. Once a keypair exists the same readonly key is served — the gate is state-dependent, and Access states the strongest one the handler applies. The key scope is the only gate here: the handler does not read a ?workspace= role.",
        "operationId": "getWebhookKey",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read the webhook signing key",
        "tags": [
          "Settings"
        ],
        "x-jomform-access": "write",
        "x-jomform-scope-only": true
      }
    },
    "/v1/rest/shipping": {
      "get": {
        "description": "The managed rate source a form's shipping block uses when its source is \"zones\": zone sets, each zone's match rules (states / districts / postcode range) and its rate rows (price in integer sen, an optional weight band in grams and an optional free-shipping threshold). Scoped to the account the request resolved to (?workspace= resolves that workspace's own account).",
        "operationId": "getShipping",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read the workspace's shipping zone sets and rates",
        "tags": [
          "Shipping"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "description": "One transaction: a set sent with its id keeps it (so every form pointing at it keeps resolving), a new set is inserted, and a set the payload OMITS is deleted — refused with 409 when a form still points at that set. Every zone needs at least one rate and every non-fallback zone needs a match rule; a configuration that could not resolve is refused with 400 rather than saved and discovered by a buyer.",
        "operationId": "putShipping",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Replace the workspace's shipping zone sets and rates",
        "tags": [
          "Shipping"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/shipping/couriers": {
      "get": {
        "description": "Every courier provider this deployment knows (lalamove, easyparcel) with: whether the SERVER holds usable credentials for it (never the value — only the source), whether THIS workspace has connected it, the pickup address a parcel ships from, the connection's environment (sandbox/live, so a simulated rate is visible as simulated), and what is still missing (credentials / connect / grant / origin) before a real quote can happen. A provider whose production keys do not exist yet is listed as not configured rather than hidden.",
        "operationId": "getShippingCouriers",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read the workspace's courier connections",
        "tags": [
          "Shipping"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/shipping/couriers/{provider}/connect": {
      "post": {
        "description": "Lalamove authenticates server to server, so connecting means naming the PICKUP point (lat/lng) it will be quoted from and it is immediately ready. EasyParcel is OAuth 2.0 and its consent belongs to the MERCHANT: this returns an authorize URL (with a single-use, 30-minute state) for the account owner to open on any device. No password is ever accepted here. A refusal names its remedy: missing server credentials, a missing pickup point, or an unregistered redirect_uri.",
        "operationId": "postShippingCourierConnect",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "provider",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Connect a courier provider for the workspace",
        "tags": [
          "Shipping"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/shipping/couriers/{provider}/disconnect": {
      "post": {
        "description": "Marks the connection disconnected and deletes the sealed OAuth grant, so a revoked authorization can never be quoted with. The pickup address is KEPT: it is the merchant's configuration and reconnecting must not make them retype it.",
        "operationId": "postShippingCourierDisconnect",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "provider",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Disconnect a courier provider, dropping its stored grant",
        "tags": [
          "Shipping"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/spam/sales": {
      "get": {
        "operationId": "listSpamSales",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Paged"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List spam-flagged sales",
        "tags": [
          "Spam review"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/spam/sales/{ref}/unspam": {
      "post": {
        "operationId": "unspamSale",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "ref",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Un-flag a sale (false positive)",
        "tags": [
          "Spam review"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/spam/submissions": {
      "get": {
        "operationId": "listSpamSubmissions",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Paged"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List spam-flagged submissions",
        "tags": [
          "Spam review"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/spam/submissions/{id}/unspam": {
      "post": {
        "operationId": "unspamSubmission",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Un-flag a submission (false positive)",
        "tags": [
          "Spam review"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/subscriptions": {
      "get": {
        "operationId": "listSubscriptions",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SubscriptionList"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List subscriptions",
        "tags": [
          "Subscriptions"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/subscriptions/{id}/cancel": {
      "post": {
        "description": "Cancelling does not revoke the customer's card token; the subscription can be revived later.",
        "operationId": "cancelSubscription",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Cancel a subscription",
        "tags": [
          "Subscriptions"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/subscriptions/{id}/retry": {
      "post": {
        "operationId": "retrySubscription",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Retry a past-due subscription now",
        "tags": [
          "Subscriptions"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/subscriptions/{id}/update-card": {
      "post": {
        "operationId": "updateSubscriptionCard",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Re-tokenise a subscription's card",
        "tags": [
          "Subscriptions"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/summary": {
      "get": {
        "operationId": "summary",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Summary"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Workspace totals (sales, revenue, counts)",
        "tags": [
          "Account"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/templates": {
      "get": {
        "operationId": "listOwnTemplates",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List the workspace's own templates",
        "tags": [
          "Templates"
        ],
        "x-jomform-access": "read"
      },
      "post": {
        "operationId": "createOwnTemplate",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Create a template",
        "tags": [
          "Templates"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/templates/{id}": {
      "delete": {
        "operationId": "deleteOwnTemplate",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Deleted"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Delete a template",
        "tags": [
          "Templates"
        ],
        "x-jomform-access": "write"
      },
      "get": {
        "operationId": "getOwnTemplate",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Read a template",
        "tags": [
          "Templates"
        ],
        "x-jomform-access": "read"
      },
      "put": {
        "operationId": "updateOwnTemplate",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Update a template",
        "tags": [
          "Templates"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/templates/{id}/use": {
      "post": {
        "operationId": "useOwnTemplate",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Form"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Clone a template into a new draft form",
        "tags": [
          "Templates"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/uploads/asset": {
      "post": {
        "description": "Merchant-only. A code asset is scanned before it is served. An svg must be a well-formed XML document: a body with text before the root element (for example a /* */ banner) is refused with 400 upload.svgInvalid, because a browser parses image/svg+xml as XML and would show a parsererror instead of the image.\n\nResponse: The stored asset, which stays unserved until its scan passes.\n\nRate limit: 2 per second per IP, burst 6.",
        "operationId": "uploadAsset",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          },
          "429": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Rate limited. The refusals of the throttled middleware carry `Retry-After`: the number of seconds THIS client must wait before it is admitted again, read from the bucket that refused it (so it differs per route — about a second on the coarse per-IP guard that refills at 1/s, up to an hour on the hourly guards)."
          }
        },
        "summary": "Upload a code asset (multipart: css/js/html/svg/woff2)",
        "tags": [
          "Media"
        ],
        "x-jomform-access": "write",
        "x-jomform-rate-limit": {
          "policy": "2 per second per IP, burst 6",
          "scope": "middleware"
        }
      }
    },
    "/v1/rest/uploads/avatar": {
      "post": {
        "description": "Response: The stored avatar with its public https URL.\n\nRate limit: 2 per second per IP, burst 6.",
        "operationId": "uploadAvatar",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          },
          "429": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Rate limited. The refusals of the throttled middleware carry `Retry-After`: the number of seconds THIS client must wait before it is admitted again, read from the bucket that refused it (so it differs per route — about a second on the coarse per-IP guard that refills at 1/s, up to an hour on the hourly guards)."
          }
        },
        "summary": "Upload a profile avatar (multipart)",
        "tags": [
          "Media"
        ],
        "x-jomform-access": "write",
        "x-jomform-rate-limit": {
          "policy": "2 per second per IP, burst 6",
          "scope": "middleware"
        }
      }
    },
    "/v1/rest/uploads/image": {
      "post": {
        "description": "multipart/form-data with the file in file. Send the bytes, not JSON. An svg must be a well-formed XML document: a body with text before the root element (for example a /* */ banner) is refused with 400 upload.svgInvalid, because a browser parses image/svg+xml as XML and would show a parsererror instead of the image.\n\nResponse: The stored media item with its public https URL.\n\nRate limit: 2 per second per IP, burst 6.",
        "operationId": "uploadImage",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          },
          "429": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Rate limited. The refusals of the throttled middleware carry `Retry-After`: the number of seconds THIS client must wait before it is admitted again, read from the bucket that refused it (so it differs per route — about a second on the coarse per-IP guard that refills at 1/s, up to an hour on the hourly guards)."
          }
        },
        "summary": "Upload an image (multipart)",
        "tags": [
          "Media"
        ],
        "x-jomform-access": "write",
        "x-jomform-rate-limit": {
          "policy": "2 per second per IP, burst 6",
          "scope": "middleware"
        }
      }
    },
    "/v1/rest/webhooks": {
      "get": {
        "operationId": "listWebhooks",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/WebhookList"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List outbound webhooks",
        "tags": [
          "Webhooks"
        ],
        "x-jomform-access": "read"
      },
      "post": {
        "operationId": "createWebhook",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Webhook"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Create an outbound webhook",
        "tags": [
          "Webhooks"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/webhooks/{id}": {
      "delete": {
        "operationId": "deleteWebhook",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Deleted"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Delete an outbound webhook",
        "tags": [
          "Webhooks"
        ],
        "x-jomform-access": "write"
      },
      "put": {
        "operationId": "updateWebhook",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "additionalProperties": true,
                "type": "object"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Webhook"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Update an outbound webhook",
        "tags": [
          "Webhooks"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/webhooks/{id}/deliveries": {
      "get": {
        "operationId": "listWebhookDeliveries",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Paged"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "List a webhook's recent deliveries",
        "tags": [
          "Webhooks"
        ],
        "x-jomform-access": "read"
      }
    },
    "/v1/rest/webhooks/{id}/deliveries/{deliveryId}/retry": {
      "post": {
        "operationId": "retryWebhookDelivery",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          },
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "deliveryId",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Retry a failed delivery",
        "tags": [
          "Webhooks"
        ],
        "x-jomform-access": "write"
      }
    },
    "/v1/rest/webhooks/{id}/retries/{retryId}/retry": {
      "post": {
        "operationId": "retryDeadLettered",
        "parameters": [
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          },
          {
            "description": "Identifier in the URL path.",
            "in": "path",
            "name": "retryId",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "additionalProperties": true,
                  "type": "object"
                }
              }
            },
            "description": "Success."
          },
          "400": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The request body or parameters are invalid. Error messages are returned in the API's own words, not a fixed enumeration."
          },
          "401": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "Missing, unknown or revoked credential. Body: {\"error\":\"authentication required\"}."
          },
          "403": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Error"
                }
              }
            },
            "description": "The credential is not allowed to do this: a readonly key on a write, or a ?workspace= scope the caller has no manage/admin role in."
          }
        },
        "summary": "Retry a dead-lettered delivery",
        "tags": [
          "Webhooks"
        ],
        "x-jomform-access": "write"
      }
    }
  },
  "servers": [
    {
      "description": "Production. Served through Cloudflare (requests reach the API through the proxy).",
      "url": "https://api.jomform.com"
    }
  ],
  "tags": [
    {
      "name": "Forms"
    },
    {
      "name": "Form settings"
    },
    {
      "name": "Submissions"
    },
    {
      "name": "Products"
    },
    {
      "name": "Sales"
    },
    {
      "name": "Subscriptions"
    },
    {
      "name": "Payments (CHIP)"
    },
    {
      "name": "Webhooks"
    },
    {
      "name": "Domains"
    },
    {
      "name": "Media"
    },
    {
      "name": "Templates"
    },
    {
      "name": "Analytics"
    },
    {
      "name": "Spam review"
    },
    {
      "name": "Settings"
    },
    {
      "name": "API keys"
    },
    {
      "name": "Account"
    },
    {
      "name": "Profile"
    },
    {
      "name": "Feedback"
    },
    {
      "name": "In-app assistant"
    }
  ],
  "x-jomform-cors": "None. The API sends no Access-Control-Allow-* header and does not answer browser preflight requests; it is for server-to-server calls.",
  "x-jomform-scope": "Every /v1/rest/* route. /v1/auth/*, /v1/admin/*, /v1/cf/* and the public form pages are outside this document.",
  "x-jomform-versioning": "v1 is the stable surface. A breaking change means a new prefix (/v2), not a change under /v1."
}
;
