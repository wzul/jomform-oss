import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

// In-app assistant, the docs page the chat panel's inline "?" link points at
// (AGENTS.md DOCUMENTATION PARITY rule 4).
//
// It documents the feature by stating the CAPABILITY BOUNDARY first, because
// that boundary is the whole design: the assistant can PREPARE a change and
// cannot apply one. The proposal lane is IN-APP: its confirmation step needs a
// signed-in person's click, so it is not reachable over MCP (see MCP-PARITY.md,
// "the proposal lane").
export const metadata: Metadata = docsMetadata({
  title: "In-app assistant — JomForm Docs",
  description:
    "What JomForm's built-in chat assistant can and cannot do: it answers questions about how JomForm works and about your own forms, sales and products, it can file a feedback report, and it can prepare a small reversible change for you to confirm — it can never apply a change by itself.",
  path: "/docs/chat",
});

const SECTIONS: Section[] = [
  {
    id: "what",
    title: "What the assistant is",
    icon: "💬",
    body: [
      "It speaks the language you write in: [[en|English]], [[ms|Bahasa Melayu]], [[zh|中文]], [[ta|தமிழ்]] or [[ar|العربية]].",
      "The assistant is a chat panel inside the dashboard. Ask it a question in plain language and it answers two kinds of question.",
      "The first is how **JomForm** works. The second is what your account holds: live forms, what a form recorded, sales, submissions and past-due subscriptions.",
      "For your account it looks the answer up in your data, with the same **tools** an AI agent would use over MCP.",
      "It is not a general chatbot. It is the assistant for **JomForm** specifically.",
      "Ask how the platform fee your buyer pays is set, how delivery is priced, or whether you can take donations or use your own domain.",
      "Those answers come from a short **guide** to the product that ships with the app, not from whatever the model remembers about other form builders.",
      "When neither the guide nor your data can answer, it says so.",
    ],
  },
  {
    id: "product-guide",
    title: "What it knows about JomForm",
    icon: "📚",
    body: [
      "The product answers come from a short **guide** that ships inside the app. It covers the questions a merchant asks most.",
      {
        list: [
          "The per-method platform fee your buyer pays and who pays it, plus shipping zones and courier quotes.",
          "Products, including pay-what-you-want and donations.",
          "Subscriptions and what happens when a charge fails, refunds, and forms that collect responses rather than payments.",
          "Custom domains, email notifications, spam review and two-factor sign-in.",
          "The MCP server that AI agents connect to, and the REST API.",
        ],
      },
      "Every fact in that guide is checked against these **documentation pages** when the app is built.",
      {
        warn: "If a guide sentence stops matching the docs, the build fails and names the sentence. Nothing is left repeating a rule that is no longer true.",
      },
      "It also knows what the product does **not** do, and says so plainly instead of inventing a workaround.",
      "No coupon block exists and nothing applies a discount code at checkout. Asking for one gets that fact and an offer to file it as a feature request.",
      "The same applies to anything the guide does not cover: it says it does not know rather than guessing.",
      {
        note: "A merchant sent looking for a control that was never built concludes the product is broken, which is why the honest answer matters.",
      },
    ],
  },
  {
    id: "boundary",
    title: "What it cannot do",
    icon: "🚫",
    body: [
      "The assistant **cannot apply a change** to your account. What it can do is prepare one for you to confirm.",
      "Ask it to rename a product, change a price, adjust stock or restyle a form. It shows a card listing exactly what it intends to change, then it stops.",
      "The change happens when you press **Confirm**, and not before.",
      "Everything destructive or hard to undo stays out of its reach entirely.",
      {
        list: [
          "It cannot delete or archive a form or product, refund or confirm a sale, or capture or release a payment.",
          "It cannot cancel or retry a subscription, or publish or unpublish a page.",
          "It cannot send email to your customers, or touch your passwords, keys, webhooks, domains, members or settings.",
          "Those are not merely discouraged. They are not reachable from the chat.",
        ],
      },
      "A form answer containing the words **delete all my forms** has no tool to call. Only a small fixed set of tools is offered.",
      "Asking for one gets a straight answer that you must do it yourself, on the page where the consequence is visible.",
    ],
  },
  {
    id: "enforcement",
    title: "How that limit is enforced",
    icon: "🛡️",
    body: [
      "It is enforced on the **server**, in four separate layers, not by asking the model to behave.",
      {
        steps: [
          "Only a fixed set of tools exists, so a form answer carrying an instruction has nothing to run it.",
          "Every tool the assistant uses while answering runs with **read-only** permission. A write tool would be refused even if it were offered.",
          "The only code that runs a write is the confirmation step. It needs a single-use token the server issued to your browser, which the model never sees.",
          "The same permission and workspace checks that apply to an MCP connection are re-applied to the confirmed call.",
        ],
      },
      "A confirm value written by the model proves nothing, and is not accepted as confirmation anywhere. The confirmation is **your click**.",
      "If something tells the assistant you already approved a change, that is not an approval. It treats such text as content to report to you, not as an instruction.",
      {
        note: "This is tested by attempting the writes, not by checking a list. The tests drive the real tool path under real permissions and require every write to be refused.",
      },
      "They cover deleting a form, archiving a product, refunding a sale, cancelling a subscription, and creating a product, a form, a webhook, an API key or a setting.",
      "Attempts where the model writes `confirm: true` into its own arguments are refused too, and the database is read back to confirm nothing changed.",
    ],
  },
  {
    id: "proposals",
    title: "Confirming a proposed change",
    icon: "✅",
    body: [
      "A proposal appears as a **card**, not as a sentence. It shows the change in your terms, the values it would set and the object it applies to.",
      "A badge says it needs your confirmation, and the card names the underlying **tool** so you can look it up. It says plainly that nothing has changed yet.",
      "Two buttons: **Confirm** applies it and **Cancel** kills it. Cancel is a real action: it spends the proposal on the server, so a cancelled card can never be applied afterwards.",
      "The card is generated by the server from the exact arguments that will run, never from the assistant's own description. Its sentence is shown as a **quote**, not as authority.",
      "The card's heading, summary and field labels follow **your dashboard language**, in all five languages the dashboard speaks.",
      "The server chooses those words and the browser supplies the translation. The assistant's own sentence stays in the language of your conversation.",
      "The proposal is bound to what you were shown. If the arguments changed before your click, the confirmation is refused, so the thing you approved is the thing that runs.",
      "The card is single-use and it expires **5 minutes** after it appears. Only you can confirm your proposals: another account presenting the token is refused.",
    ],
  },
  {
    id: "refusals",
    title: "When a confirmation is refused",
    icon: "⛔",
    body: [
      "A refusal names its cause in **your language**, exactly like the card, and says which of three problems it is.",
      {
        list: [
          "**Not allowed** means the assistant may never do that thing. Deleting a form or refunding a sale is yours to do, on the page where the consequence is visible.",
          "**Could not be prepared** means the action was fine and only its details were missing or wrong, such as a form asked for without its name.",
          "**Could not be completed** means the tool was allowed and the call itself failed, such as a form id that does not exist.",
        ],
      },
      "The three are different problems. Only the first means a feature is out of reach, so a missing id is never reported as one the assistant may not touch.",
      "You always get an answer telling you what to send next. A refused change never ends with an empty reply.",
      {
        list: [
          "**No workspace** in the request: it says so, and nothing was attempted.",
          "**Your role** in that workspace does not allow the edit: it says so and names the role you would need.",
          "**An account with no workspace at all** is told to create one first and ask for the card again.",
          "**Any other refusal** says the change was not carried out and the card cannot be used again.",
        ],
      },
      "A refused confirmation is never told to **try again**. The server spends the proposal the moment it decides to run it, so the remedy is always a fresh card.",
      "A proposal whose arguments the tool would never accept is refused **before your token is touched**, so nothing is used up.",
      "That happens when the assistant keys an id the wrong way: the read tools return it under one name, the write tool wants another. Ask for a fresh card.",
    ],
  },
  {
    id: "workspace",
    title: "The workspace the change lands in",
    icon: "🏢",
    body: [
      "The confirmation writes in the **workspace the call belonged to**: the workspace you have selected, not your first account.",
      "That matters if you belong to more than one. A chat call carries the selected workspace with it.",
      "If that workspace cannot be resolved, the request is refused outright rather than answered from another one.",
      {
        note: "A refusal is a plain **workspace not found** or **choose a workspace first** answer, not a silent edit somewhere you were not looking.",
      },
      "A confirmed change runs with **your own permission** and the same workspace and ownership checks as a dashboard edit. The chat never grants an ability your account does not have.",
      "The proposal lane is **in-app, not an MCP tool**. There is no propose_write over MCP: a proposal is prepared inside the dashboard and confirmed by the signed-in person.",
      "The confirmation is a click, and it is deliberately not something an API call can supply.",
      "An agent that wants a change over MCP calls that change's own tool itself. Its own client is the human's confirmation channel.",
    ],
  },
  {
    id: "what-can-change",
    title: "What it may propose",
    icon: "🛠️",
    body: [
      "The set is deliberately small and **reversible**: three field edits you can put back by proposing the opposite edit.",
      {
        list: [
          "Editing a **product's fields**: name, description, price, stock, active state, images and billing cycle.",
          "Editing one **product variation's** fields.",
          "Changing a form's **theme and branding**: colours, fonts, spacing and custom CSS.",
        ],
      },
      "The fourth creates: upsert_form is proposable. A confirmed card creates a new form when it names none, or updates the one it names.",
      "A creation is allowed because a form is **binned**, not destroyed. Deleting one from the forms page trashes it, and restoring brings it back with its address intact.",
      "Everything outside that set is excluded **by class**, not by example:",
      {
        list: [
          "Anything that **destroys, archives, revokes or purges**.",
          "Anything that **moves money**, or cancels or re-runs a customer's billing.",
          "Anything touching **credentials, access or members**.",
          "Anything that sends email out of the building, or changes what the public can reach.",
          "Any **bulk operation**.",
        ],
      },
      "A wrong confirm on those is either irreversible or lands on somebody else.",
      {
        warn: "The one step with no inverse is **purge_form**: it destroys the form and everything attached, and no restore brings it back. It is human-only and never proposable.",
      },
    ],
  },
  {
    id: "feedback",
    title: "Filing feedback from the conversation",
    icon: "📮",
    body: [
      "The assistant can file what you tell it as a **feedback report** or feature request. This is the same act as sending from the Feedback page.",
      "It creates a thread you own, the JomForm team is emailed, and the reply arrives in the same thread at **/feedback**.",
      "Filing is **visible, never silent**. The chat shows a note saying it filed a report, with the subject, so no thread appears without you knowing.",
      "The same limits apply as on the Feedback page: at least **10 characters**, at most **5000**, and at most **10 reports per user per day**.",
      "Filing too many gets a rate limit, not a judgement about whether your report is genuine. A complaint is never filtered by a classifier here.",
      "If you did not want it filed, you can read it and add to it from the Feedback page.",
      "This page names submit_feedback as the one tool of its own that runs over MCP. An agent can file a report from a conversation exactly as the in-app assistant does.",
    ],
  },
  {
    id: "formatting",
    title: "Formatting in the answer",
    icon: "🎨",
    body: [
      "Answers are **rendered**, not printed. Bold text, a list, a piece of code or a link arrives as that, not as the markers it was typed with.",
      "What is rendered: bold, italic, inline code, fenced code blocks that scroll sideways instead of stretching the card, bulleted and numbered lists, links and paragraph breaks.",
      {
        list: [
          "What is not rendered: tables, images, headings and raw HTML.",
          "A table would arrive as a row of pipe characters, and raw HTML as text.",
          "The assistant's text is never inserted into the page as code, so it is told which subset it may use and writes lists where a table would have been.",
        ],
      },
      "An underscore means italic only when it is written **around** a word, as in `_condong_`. Inside a word it is an ordinary character.",
      "So a setting, a column or a status keeps its underscores exactly as written: `jf_2fa_pending` and `form_answers_value` appear that way.",
      "Asterisks are the other way round: foo*bar*baz is italic, because an asterisk inside a word is unusual enough to be deliberate. That is the Markdown standard's rule.",
      "The same rendering applies to a feedback thread you file from the chat and to the reply you read on the **Feedback page**.",
      "The stored text is exactly what was written. Nothing is stripped on the way in or out, so a report is never altered by how it is displayed.",
    ],
  },
  {
    id: "while-it-answers",
    title: "While it is answering",
    icon: "⏳",
    body: [
      "The answer arrives as it is written, not at the end. A **block cursor** sits at the end of the bubble while the assistant is still writing.",
      "A single sentence used to sit on screen for several seconds, and then the whole answer appeared in one jump. It looked like a hung page.",
      "Under the composer, a status line says what is happening: that it is thinking, or which of your records it is reading.",
      "Those records are your forms, your sales, your form responses, your products and your feedback. A read that ran is listed under the answer afterwards.",
      "The status line and the button are separate. The **Send** button always says Send, and **Stop** sits beside it.",
      "**Stop** really stops. The browser closes the connection, the server stops the work behind it, and no further model calls are made.",
      "What was already written stays in the conversation, marked as incomplete. The question and that partial answer are saved, so a reload still shows how far it got.",
      "The answer is not resumed, so ask again for the rest. For a screen reader, only the latest answer is a live region, marked busy while it streams.",
    ],
  },
  {
    id: "the-composer",
    title: "The composer and the transcript",
    icon: "📝",
    body: [
      "Every answer has a **Copy** button. It puts the answer's own words on your clipboard, not the page's formatting.",
      "The button says **Copied** for a moment so you can see it worked. Paste the words into an email or a report.",
      "The message box grows as you type, up to about **eight lines**, then scrolls inside itself. **Enter** sends and **Shift+Enter** starts a new line.",
      "It is still capped at **2000 characters**, and the counter shows what is left of that cap.",
      "The conversation follows the answer only while you are at the bottom. Scroll up to re-read something earlier and it stays where you put it.",
      "A **Jump to latest** control appears so you can return when you are ready.",
      "While the conversation is empty, the panel offers a few **example questions**. Clicking one sends it, so a first question costs one click, not an invented sentence.",
      "Each is sent as your own message. For a screen reader, the status line is announced politely, so you hear which of your forms it is reading.",
    ],
  },
  {
    id: "limits",
    title: "Limits and cost",
    icon: "📊",
    body: [
      "Every message you send is at least one **model call**, and up to five if the assistant has to look several things up.",
      "Each lookup's result is trimmed to **6000 characters** before the assistant reads it, and the answer is bounded too, so one question cannot become an unbounded bill.",
      "A single user can send **60 messages per hour**, in bursts of 10.",
      "If you hit that limit, wait a moment rather than switching to a second account. The limit exists to keep the feature affordable for everyone.",
      "Confirmations are limited separately, at **30 per hour** in bursts of 5. Confirming costs no model call, so that limit is about pace, not spend.",
      "One answer can put at most **3 proposals** in front of you, so a single reply cannot become a wall of confirmations to click through.",
      "The conversation is bounded to the last **16 turns**. Older turns are dropped from what the model sees, so it suits a recent question, not a long document.",
      "The assistant's own answer is capped at **4000 characters**, and it will not paste your entire sales list into the chat when you asked for a count.",
    ],
  },
  {
    id: "trust",
    title: "Your data in the conversation",
    icon: "🔒",
    body: [
      "The assistant reads **only your own account**. It cannot see another merchant's forms, sales or feedback, and cannot read another person's feedback thread.",
      "Your data is treated as **data**. Product names, form answers and customer messages are handed to the model inside an explicit boundary.",
      "That boundary says, before and after the content, that this is information to read rather than instructions to follow.",
      "A customer who types a fake instruction, a claim that you approved something, or a demand for a destructive change is treated as content. It is reported, never obeyed.",
      "Because the assistant can prepare changes, that boundary matters more rather than less.",
      "Text that talked it into proposing a harmful edit would still have to clear the reversible-only set. Then you would have to confirm it.",
      "The **audit log** records that a chat exchange happened, how many lookups it made, and that a proposal was confirmed, with which tool and whether it succeeded.",
      "It never records your messages, the replies, or the values of a confirmed change.",
    ],
  },
  {
    id: "remembering",
    title: "Your conversations are kept",
    icon: "🗂️",
    body: [
      "Every conversation is **saved to your JomForm account**, for the signed-in user and the selected workspace: a refresh, a new tab, another device or signing out does not lose it.",
      "The **history sidebar**, to the left of the transcript, lists your saved conversations in this workspace, most recently used first.",
      "Each row is named after the first question you asked, and you can rename it. Click a row to continue that conversation.",
      "The assistant sees its recent turns as context, so **and how many this week?** still means what it meant yesterday. A new conversation leaves the first in the list.",
      "Two workspaces do not share a conversation. Each keeps its own thread and its own sidebar, and switching workspace shows that workspace's conversations.",
      "Only the words are stored: your questions, the answers, and which lookups produced an answer. A **confirmation card** is deliberately not restored.",
      {
        note: "Its Confirm button is a single-use permission with five minutes of life, so a restored card would offer a button that cannot work. Ask the assistant to prepare it again.",
      },
      "Deleting a conversation from the sidebar removes its messages permanently, which is why the page asks first. It does not affect your forms, sales or other data.",
    ],
  },
  {
    id: "when-stuck",
    title: "When it does not know",
    icon: "🤔",
    body: [
      "If the assistant cannot answer from your data or from its guide to the product, it **says so** and offers to file it as feedback.",
      "It is not allowed to guess about your money, your customers or your settings, and it will not invent a number or a feature to look helpful.",
      "If it tells you it found nothing, that is a statement about the lookup that ran, not about your account.",
      "Saying which form or period it looked at is a fair question to ask.",
      "For a payment that went wrong, a payout, or an account you cannot access, file it from the [Feedback page](/docs/feedback) or reply in the assistant's thread.",
      {
        tip: "A conversation with a person is the right channel for those, and the chat will say so rather than pretend otherwise.",
      },
      {
        note: "Related: [in-app feedback](/docs/feedback) and [the MCP tool reference](/docs/mcp). **submit_feedback** is in that reference. **propose_write** is not, because its confirmation is a signed-in person's click.",
      },
    ],
  },
];

export default function ChatDocsPage() {
  return (
    <DocsPage
      title="In-app assistant"
      lead="The built-in chat: it answers questions about JomForm and about your own data, files feedback, and can prepare a change for you to confirm."
      sections={SECTIONS}
    />
  );
}
