import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

// AGENTS.md DOCUMENTATION PARITY rule: #652 changed how a block is edited on a
// phone (the Settings tab is gone; a tap on the block opens the panel) and
// replaced the card's four buttons with up/down plus one menu. Docs that
// described the old flow would be wrong, so the flow is documented here and the
// builder links to this page with an inline "?" (rule 4).
export const metadata: Metadata = docsMetadata({
  title: "JomForm Docs — the form builder",
  description:
    "How the JomForm form builder works: the field palette, the canvas, editing a block's settings on desktop and on a phone, and the per-block actions.",
  path: "/docs/builder",
});

const SECTIONS: Section[] = [
  {
    id: "three-areas",
    title: "The three areas",
    icon: "🧱",
    body: [
      "The builder is where a form's fields are laid out. It is the same surface for a form and for one of your own [templates](/docs/customize#templates).",
      "On a desktop (**768px** and wider) the builder is three columns.",
      {
        list: [
          "The **field palette** on the left holds every field type, grouped Standard, Advanced and Pricing.",
          "The **canvas** in the middle holds the fields in the order a buyer sees them.",
          "The **block settings** panel on the right edits whichever block is selected.",
        ],
      },
    ],
  },
  {
    id: "adding-a-field",
    title: "Adding a field",
    icon: "➕",
    body: [
      "Drag a palette item onto the canvas, or tap it. A tap appends the field to the end.",
      "On a phone the palette lives behind the **Add** tab.",
      {
        tip: "Adding a field returns you to the canvas with the new block highlighted, so you can add several in a row without leaving the canvas.",
      },
    ],
  },
  {
    id: "editing-settings",
    title: "Editing a block's settings",
    icon: "🎛️",
    body: [
      "Every block has settings: its **label** or text, description, placeholder, CSS class, inline style, required flag and validation message.",
      "A field type that needs one also gets a **choices** editor, a product picker or a conditional-logic rule. Which settings appear depends on the type.",
      {
        list: [
          "**On a desktop**, tap a block on the canvas and its settings appear in the right-hand panel.",
          "**On a phone**, tap a block and the settings panel replaces the canvas.",
          "The row at the top of the panel is **← Canvas** next to the block's type. It returns you to the canvas.",
          "The block stays highlighted after you close the panel, and tapping it again reopens its settings.",
          "The mobile tab bar has two tabs, **Add** and **Canvas**. There is no separate settings tab: editing a block is a tap on the block itself.",
        ],
      },
    ],
  },
  {
    id: "what-each-setting-does",
    title: "What each setting does",
    icon: "⚙️",
    body: [
      "Every one of these reaches the **published form**. Which setting appears depends on the field type.",
      {
        list: [
          "**Label** or text is the field's own label. The text block is a single-line answer field, so its label is the text you type.",
          "**Description** is helper text printed under the field, on every block type.",
          "**Placeholder** is the grey hint inside the field, on text, note, number, email, phone, address, website, captcha, list and repeater.",
          "A date or time field has no placeholder: the picker prints its own **format hint** instead.",
          "**Max length** is how many characters a text or note field accepts.",
          "**Default value** is the value a text, note, quantity or hidden field starts with.",
          "**CSS class** puts your own class name on the field's control, or on the block's own element for a text, section, HTML, image or divider block.",
          "**Inline style** takes safe CSS only, under Advanced. There are no background images and no external URLs.",
        ],
      },
      {
        list: [
          "**Required message** is the wording a buyer reads when a required field is left empty, in place of the browser's own.",
          "**Validation pattern** is a regular expression the answer must match, for text, email, phone and website fields.",
          "Browsers ignore a pattern on a number or note field, so it is not offered there.",
          "**Format** on a date or time block chooses a date picker, a date-and-time picker or a time picker.",
          "**Choices**, the **product** picker and **conditional logic** work as before. A dropdown, radio, image-choice, option or shipping block edits its own list of choices.",
          "A product block picks the product it sells. A rule shows or hides a field depending on an earlier answer.",
          "On an option or shipping block each choice carries its own price, and that price is exactly what the buyer is charged for the pick.",
          "A choice left without a price **blocks the save** instead of quietly costing nothing. Enter 0 for a free choice.",
        ],
      },
    ],
  },
  {
    id: "text-block-sentence",
    title: "When a text block holds a sentence",
    icon: "✍️",
    body: [
      "A text block is a short-answer field, so whatever you put in its **label** becomes the label of an empty input on the published form.",
      "Forms built under an older JomForm stored their introduction sentence in that block, so a live page ended up reading **Register for our event** above an empty box.",
      "The builder now warns you where the block is edited. When a label reads like a sentence, a notice appears under it with one action, **Treat as intro line**.",
      "That action turns the block into an **introduction**, a section in the palette, and keeps the block's id and its text exactly as you wrote them.",
      "Nothing is changed on its own. Short labels such as **Abc** or **Nama anda** never trigger the notice, and only your click converts a block.",
      "Changed your mind? The same panel then offers **Undo**, which makes it a field again with the same id, the same text and the same settings.",
      "Undo is offered for the block you converted, in the session you converted it. Once the change is saved and the page reloaded, the introduction is the block you chose.",
      "From then on it is edited like any other block.",
    ],
  },
  {
    id: "broken-rules",
    title: "When a rule's field no longer exists",
    icon: "🔗",
    body: [
      "A conditional rule names the field it watches by that field's **internal id**.",
      "Some older builders gave every block a new id each time a form was opened, so a saved form can carry a rule naming an id no block has.",
      "The rule picker used to show such a block as if it had **no rule at all**.",
      {
        warn: "Buyers got the opposite: the server still evaluated the rule, it never matched, and the block stayed hidden with every answer to it thrown away.",
      },
      "The builder now shows it. The block's card carries a **rule broken** label, and its panel lists the unknown id as **Not available**.",
      "A notice says the rule never matches. Nothing is changed on your behalf, and the same label appears on a rule that came from a **template**.",
      "Pick the field the rule should watch, or choose **Always show** to remove it. Opening and publishing does not rewrite a rule you have not edited.",
      {
        note: "A template stores its blocks and their rules exactly as it was saved, so an older one can bring a rule naming a field the new form does not have.",
      },
    ],
  },
  {
    id: "url-fields",
    title: "What a website field accepts",
    icon: "🌐",
    body: [
      "A website block is the browser's own URL input, so it accepts any **absolute URL**, such as https://…, ftp://… or mailto:….",
      "It refuses what is not a URL, such as **yourname.com** with no scheme.",
      "JomForm applies the same rule when the form is submitted. Two narrow exceptions go the other way: the server refuses a value the page accepted.",
      {
        list: [
          "**A script-bearing scheme** is refused even though the browser accepts the string: `javascript:`, a `data:` URI or `vbscript:`. The answer is stored and read back later.",
          "**A network URL broken in shape** is refused too: a missing second slash such as `http:/example.com`, or a space in the host such as `http://exa mple.com`.",
        ],
      },
      "The server reads the first as an address with no host and cannot parse the second at all.",
      "So it answers with a corrective message instead of storing a link it cannot vouch for.",
      "Apart from those two, what the page accepts the server accepts. `ftp://`, `ws://`, `tel:`, `mailto:` and `file:` values are stored exactly as typed.",
      "A custom-HTML response form's field registered as **type:\"url\"** follows exactly the same rule.",
    ],
  },
  {
    id: "reorder-duplicate-remove",
    title: "Reordering, duplicating, removing",
    icon: "🔀",
    body: [
      "Each block card carries **↑** and **↓** to move it one position. The arrows are disabled at the ends of the list.",
      "A **⋯** button opens a menu that holds **Duplicate** and **Remove**. You can also drag a block by its card and drop it anywhere on the canvas.",
      {
        tip: "None of these controls opens the settings panel. They stop the tap from reaching the card, so arranging a form never swaps you into an editor by accident.",
      },
    ],
  },
  {
    id: "canvas-preview",
    title: "The canvas preview is drawn, not interactive",
    icon: "👁️",
    body: [
      "The canvas shows what a block **looks like**, so you can arrange the form without being able to type into it.",
      "Checkboxes and radio choices are drawn as flat grey marks, the product quantity is a fixed **1**, and the totals are placeholders.",
      "They are deliberately not tappable. The real controls belong to the buyer on the published form.",
      {
        note: "An image block with no image yet shows a caption naming the setting to fill in. Set the URL, or upload a file, in that block's settings.",
      },
    ],
  },
  {
    id: "form-mode",
    title: "Form mode and everything else",
    icon: "🧭",
    body: [
      "The form's **mode** is in the builder's top bar, next to the slug — not in the settings panel. The button reads the current mode.",
      "Press it to change it. No block has to be selected first: the panel on the right stays the selected block's settings.",
      "**Payment** is the default: entries are sales. **Response** means no products, no payment, and entries are stored as responses.",
      "Changing the mode asks you to **confirm** first, because two consequences are not visible from the two choices.",
      "Product blocks carry a price, a quantity, an add-on, shipping or a total. A response form does not show them, so nothing is collected for them.",
      "A saved form's default notification is re-pointed to the event the new mode fires — unless you edited that rule.",
      "**Form Settings** sits next to the mode. Everything else about the form lives there: status, confirmation, restrictions, notifications, the CHIP feed, the menu.",
    ],
  },
];

export default function BuilderDocsPage() {
  return (
    <DocsPage
      title="The form builder"
      lead="Where a form's fields are laid out: the palette, the canvas, the block settings and the one action menu on each block."
      sections={SECTIONS}
    />
  );
}
