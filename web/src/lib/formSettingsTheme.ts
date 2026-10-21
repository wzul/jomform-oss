// The `theme` a Form Settings → General Save writes (card t_106955f6).
//
// THE DEFECT. The General tab owns four theme keys — `primary`,
// `product_image_size`, `product_image_layout` and `custom_js` — and its save
// used to build the PATCH body's `theme` from THOSE FOUR ALONE. The API's
// PATCH replaces the whole `theme` jsonb whenever the key is present
// (api/internal/httpapi/rest_handlers.go: a nil theme keeps the stored value,
// a non-nil one is written as-is), so one click on Save silently deleted every
// other key the row carried: `layout`, `spacing`, `background`, `text`,
// `card_background`, `card_border_radius`, `font_family`, `custom_css`.
//
// Those keys are not decoration. `set_form_theme` / `create_form` and
// `POST /templates/use` store them, and the block renderer emits its
// layout/spacing CSS ONLY when they exist
// (api/internal/httpapi/render_blocks.go: `{{if .ThemeLayout}}body
// {justify-content:…;padding:…} .card{max-width:…}{{end}}`,
// `{{if .ThemeSpacing}}.card{padding:…}{{end}}`). Measured on prod: a form
// carrying `layout:"centered", spacing:"comfortable"` came back from a routine
// General Save with only the four owned keys, and every block-rendered page for
// it lost its centring and card padding.
//
// THE RULE. What this tab does not own, it must not touch: the payload is the
// theme the page READ, laid over with the keys this tab owns. That is the same
// shape the builder already uses for a template's config
// (`configWithStoredKeys` in lib/templateEdit.ts) and for a template's
// theme (`templateSaveBody`), and for the same reason.
//
// `custom_js` is the one owned key the merchant can CLEAR: an empty field means
// "drop it", so the key is deleted rather than written as "". Writing "" would
// store an empty custom-JS entry, and the renderer treats a present key as
// content.
export type GeneralSaveThemeInput = {
  /** The `primary` colour the General tab shows. */
  primary: string;
  productImageSize: string;
  productImageLayout: string;
  /** The custom-JS field as typed; empty (or whitespace) means "clear it". */
  customJS: string;
};

export function generalSaveTheme(
  stored: Record<string, unknown> | null | undefined,
  own: GeneralSaveThemeInput,
): Record<string, unknown> {
  // A copy, never the object held in page state: this map is serialised into the
  // request body, and mutating `form.theme` in place would make the next save
  // believe the server already holds keys it has only just sent.
  const theme: Record<string, unknown> =
    stored && typeof stored === "object" ? { ...stored } : {};
  theme.primary = own.primary;
  theme.product_image_size = own.productImageSize;
  theme.product_image_layout = own.productImageLayout;
  // The value is stored untrimmed (that is what the field held); only the
  // emptiness test is trimmed, so a field of spaces is a clear, not content.
  if (own.customJS.trim() !== "") theme.custom_js = own.customJS;
  else delete theme.custom_js;
  return theme;
}
