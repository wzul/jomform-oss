# media-reconcile

Read-only-by-default reconciliation between the media **bucket** and the `media`
**table**: which stored objects have a row, which are referenced by application
data without one, and what `accounts.media_used_bytes` should be if it were
derived from the bytes actually occupying the store.

It exists because the two records of "storage used" can disagree:

- every MCP image upload before v1.21.231 stored base + thumbnail/medium/full
  objects and reserved quota, but wrote **no media row** — the objects were
  invisible to the library and impossible to trash or purge through the app;
- rows written before #545 recorded only the **base** object's size, so a row can
  understate the bytes its own variant keys occupy;
- `accounts.media_used_bytes` is maintained by increments, which cannot be
  repaired by decrementing the same way: a purge subtracts the ROW's size, not
  the group's bytes.

The rule the tool enforces:

| question | source of truth |
| --- | --- |
| bytes occupied | the object store (sum of object sizes) |
| what the app can manage | the `media` row (live **and** trashed) |
| may this object be deleted at all | application data references |

**"No media row" does not mean "unreferenced".** A row-less object has been
measured as a live `users.avatar_url`; product images and form/landing/legal
content name objects the same way.

## Usage

Every flag falls back to the API's own environment variable, so the tool runs
beside the API with the same config.

```
media-reconcile                              # READ-ONLY report (default)
media-reconcile -json /tmp/report.json        # also write the machine report
media-reconcile -decisions d.json -apply      # delete approved groups, recompute counters
```

| flag | default | meaning |
| --- | --- | --- |
| `-dsn` | `$DATABASE_URL` | Postgres DSN |
| `-endpoint` / `-access-key` / `-secret-key` / `-bucket` / `-region` | `$S3_*` | bucket access |
| `-prefix` | `u/` | key prefix to reconcile |
| `-public-base` | `$S3_PUBLIC_BASE` | public base used for adopted row URLs |
| `-decisions` | — | JSON rulings; **required** with `-apply` |
| `-json` | — | write the report document here |
| `-apply` | `false` | perform the two writes below |

### Report mode is enforced by the database

Report mode opens the pool with `default_transaction_read_only=on` on every
connection **and then proves it**: `AssertReadOnly` issues an `UPDATE ... WHERE
false` inside a rolled-back transaction and requires Postgres to reject it with
SQLSTATE `25006`. If the write is accepted, the tool aborts before listing
anything. A flag alone is not treated as a guarantee.

### Classification

Every object group is `u/<account>/<uuid>[-thumbnail|-medium|-full|-preview].<ext>`
and gets exactly one classification:

- `rowed` — the base key has a `media` row (live or trashed). Manageable in the
  app; nothing to reconcile.
- `keep` — row-less, but referenced by application data. Never deletable.
- `adopt` — row-less and unreferenced. **The default**: the bytes stay and a row
  is created so they become manageable. Adopting does not add bytes to the
  counter; they are already in the bucket.
- `deletable` — row-less, unreferenced, **and** explicitly ruled deletable in a
  decisions file. There is no default delete and no rule infers one.

Reference surfaces scanned for every group (base uuid substring):

`users.avatar_url`, `forms.config`, `forms.theme`, `forms.logo_url`,
`products.images`, `product_variations.images`,
`workspace_settings.landing_page_json`, `workspace_settings.menu_json`,
`workspace_legal_pages.content`

There is no `templates` table in the schema; a template is a `forms` row with
`is_template = true`, so the three `forms.*` surfaces cover it.

Adding a place the product stores a media URL means adding it to
`referenceSurfaces` in `postgres.go`; the classification is only as safe as that
list is complete, and `TestReferencesCoversEverySurface` fails when a surface
stops finding a reference that exists.

**Residual risk, stated rather than hidden.** The list above is the surfaces the
product *knows* how to render a media URL in. Free-text fields a merchant can
paste anything into are NOT scanned: `products.description`, receipt/notification
HTML (`form_email_settings`, `workspace_email_settings`, `form_notifications`,
`form_confirmations`), the `sales.items` / `submissions.data` snapshots,
`audit_logs.diff`, `events.properties` and webhook payloads. Before a
`deletable` ruling is given, confirm the uuid appears nowhere else — a
data-only `pg_dump | grep <uuid>` over the whole database is the cheap check, and
it is what the first production run used in addition to the surfaces.

### Decisions file

```json
{"decisions": [
  {"account": "<uuid>", "base": "<uuid>", "decision": "deletable", "reason": "why"}
]}
```

Accepted decisions are `adopt`, `keep` and `deletable` (never `rowed` — that is
derived from the row). An unknown group, an unknown decision, a missing reason or
a duplicate ruling is an **error**: a typo must not silently leave an object
undeleted while the report claims the ruling was applied.

A `deletable` ruling is refused (and reported as such) when the group has a row
or a reference.

### What `-apply` writes

1. Re-scans the references for every group ruled deletable; if one gained a
   reference since the report, the whole run aborts with nothing deleted.
2. Creates the rows for `adopt` groups (metadata only — no counter change).
3. Deletes the `deletable` groups' object keys, then verifies each key is gone.
4. Re-lists the whole prefix and sets, per account:

   ```
   media_used_bytes := min(SUM(size) of every object under u/<account>/, 1 GiB cap)
   ```

   Never `old - size` per deleted object, and never `SUM(media.size)` — rows
   written before #545 omit the sized variants. The report names this method and
   shows the before/after of every account it changes.

## Running it against a running deployment

The binary is static (`CGO_ENABLED=0`), so it can be copied into the API
container, which already carries `DATABASE_URL` and the `S3_*` variables:

```
go build -trimpath -o /tmp/media-reconcile ./cmd/media-reconcile   # from api/
scp /tmp/media-reconcile host:/tmp/media-reconcile
ssh host 'docker cp /tmp/media-reconcile <api-container>:/tmp/media-reconcile'
ssh host 'docker exec <api-container> /tmp/media-reconcile -json /tmp/report.json'
```

Remove the copied binary and the JSON afterwards. The report run writes nothing
to the database or the bucket by construction (see above); only `-apply` writes,
and only the four steps listed.
