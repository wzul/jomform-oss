"use client";

// In-app assistant (phase 1) — a chat about the caller's OWN data that can file
// feedback and can change nothing else.
//
// THE UI'S JOB IS TO MAKE THE BOUNDARY VISIBLE, not just to send messages:
//
//   1. It asks GET /v1/rest/chat before showing anything. That flag means "this
//      deployment can actually answer": the server reports available:false when
//      there is no assistant wired OR when its model has no credential
//      configured, and the page says the assistant is unavailable instead of
//      offering a box that only ever errors (dev and bare mode are real, and a
//      box that always fails is worse than no box).
//
//      A key that is CONFIGURED but REJECTED is the same answer, discovered one
//      step later: the status flag is a presence check (only the provider can
//      say a key still works), so the POST reports 503 and the branch below
//      treats it exactly like available:false — the box closes rather than
//      every message dying.
//   2. It shows a read-only badge and one line stating what the assistant can
//      and cannot do, with an inline "?" link into the docs page. That is not
//      decoration: "it cannot delete your form" is the design, and a user who
//      asks it to delete a form must be able to read why it will not.
//   3. A FILED FEEDBACK is announced in the transcript with a link to
//      /feedback. Filing must be visible at the moment it happens — the thread
//      also appears on the Feedback page, and a user discovering it later there
//      would be the wrong order of events.
//   4. A REFUSED tool call is shown, not hidden — and SAYS WHY, in this visitor's
//      language. The server returns the steps it attempted; rendering them is how
//      the user learns the assistant tried something and was not allowed, rather
//      than experiencing it as a broken answer. The why is a machine key
//      (`chat.refused.*`) resolved by lib/chatRefusal.ts, because the API does not
//      know the dashboard's language and its own sentence reached every locale in
//      one language (t_3839995c; issue #6177).
//
//   5. EVERY call carries the active workspace (the `?workspace=` query the
//      shared workspaceQuery() helper builds). The chat is one of the dashboard
//      surfaces that resolve the account from the SELECTED workspace, and it is
//      the only one that can WRITE through a confirmation — so an unscoped call is
//      not a read-scope wobble here: the server falls back to the user's FIRST
//      owned account, the assistant names that account's product on the card, and
//      confirming the card edits it. A merchant with two workspaces would be shown
//      a card for the account they are not looking at.
//
//   6. A REFUSED probe says WHY, in the visitor's language. The availability
//      fetch hands runLoad the server's machine `code` through lib/apiErr
//      (#6177), and the sentence that translation produced is what the page's
//      alert renders. It used to be computed and then thrown away — the failure
//      branch returned without reading `r.error`, so a refused workspace read as
//      the generic "the assistant is not available right now" and the whole
//      translation was inert (t_57a77d02). `r.error` is errorKey's ONLY consumer;
//      nothing else in this file reads it.
//
//   7. THE CONVERSATION LIVES ON THE SERVER, NOT IN THIS BROWSER. It used to be
//      kept in localStorage and DELETED whenever the session ended — so logging
//      out (or a session expiring, or another device) destroyed every
//      conversation the user had had. The history is now stored per person and
//      per workspace (GET/POST /v1/rest/chat/sessions, and the exchange itself is
//      appended by POST /v1/rest/chat), the sidebar lists it, and the most recent
//      conversation is re-opened on load. Signing out deletes nothing: the next
//      login shows the same list.
//      Only the WORDS are stored: a proposal card's token is a single-use
//      capability with a five-minute life, so a restored conversation shows the
//      answer and the tool trail and never a button whose token expired hours
//      ago.
//      "New conversation" starts a new stored conversation (the old one stays in
//      the sidebar), and each row can be renamed or deleted. The parsing of the
//      wire lives in lib/chatSessions.ts; this page only decides WHEN to load,
//      create, rename and delete.
//
//   8. THE ANSWER ARRIVES WHILE THE MODEL WRITES IT (card t_148d7100). The
//      exchange used to be one POST whose body appeared only at the end: measured
//      on staging, the transcript jumped from 45 to 361 characters in a single
//      frame, after 2–8 seconds of a static "Looking that up…". The request now
//      asks for `?stream=1`, the server answers text/event-stream, and this page
//      renders each `delta` into the bubble. THE FALLBACK IS THE POINT OF THE
//      CONTENT-TYPE CHECK: a proxy that buffers, an older server, or a runner
//      with no streaming capability all answer ONE JSON body, which is read
//      exactly as before — the feature degrades to the previous behaviour rather
//      than to an empty transcript.
//
//   9. STOP REALLY STOPS, AND KEEPS WHAT WAS WRITTEN. The send path had no
//      AbortController at all, so a wrong question cost a whole answer to read and
//      a full round of model calls to pay for. Stop aborts the fetch (the server
//      sees the connection close and stops the model work behind it), and the
//      partial text stays in the transcript, marked as incomplete — deleting it
//      would throw away the only thing the user waited for.
//
//  10. THE STEP LINE SAYS WHAT IS HAPPENING. Tool steps used to be rendered ONLY
//      when they were refused; the ordinary case showed nothing at all. The
//      server emits a `step` event BEFORE it dispatches a tool, and this page says
//      "Reading your forms…" while it runs (lib/chatSteps.ts turns the tool name
//      into a sentence in the visitor's language — the API sends the identifier,
//      never prose). The finished `called` steps stay in the transcript as a short
//      trail, so the answer can be audited rather than merely believed.
//
//  11. THE EMPTY STATE IS CLICKABLE, THE ANSWER IS COPYABLE, THE BOX GROWS.
//      Chips send a real question instead of describing one; a Copy button puts
//      the assistant's own words on the clipboard; the textarea grows with its
//      content up to eight lines (lib/chatComposerGrow.ts) instead of being a
//      fixed two-line window with the resize handle disabled.
//
//  12. THE BUTTON IS A BUTTON AND THE STATUS IS A STATUS. While sending, the Send
//      button used to change its label to "Looking that up…" — so the only control
//      on the page was also the only state display, and the same sentence appeared
//      twice. The button now always says Send (the e2e specs look it up by name),
//      a Stop button appears beside it, and the state is its own `role="status"`
//      line.
//
//  13. THE TRANSCRIPT NO LONGER ANNOUNCES ITSELF. `aria-live="polite"` sat on the
//      WHOLE log, so a screen reader re-read the entire conversation every time a
//      message arrived. The log is now a plain scroll region; only the LATEST
//      assistant message is a live region, and it is `aria-busy` while the answer
//      is being written, so assistive tech waits for the end of the answer instead
//      of reading it character by character.
//
//  14. SCROLLING IS THE USER'S. The page called `scrollIntoView` on every change
//      of `msgs.length`/`sending`, which pulled a reader who had scrolled up back
//      to the bottom — and would have done it on every streamed fragment. The
//      follow is now conditional on the reader being at the bottom (lib/chatScroll.ts);
//      otherwise a "Jump to latest" control appears and the view stays put.
//
// The history it sends is the transcript the user can see, capped to the SERVER'S
// own retention cap — the `max_history_turns` the availability probe reports,
// whose value is chat.MaxTurns in api/internal/chat/chat.go. A literal here and a
// constant there is how this page sent 32 turns at a server that kept 16
// (t_4c29963a): the assistant answered about a window three turns shorter than the
// one the user could scroll, and nothing said so. The server re-caps and drops any
// client-forged system/tool turn, so the wire shape here is a convenience, not a
// trust boundary.

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { Markdown } from "../../components/Markdown";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import {
  fetchJson,
  runLoad,
  classifyResponseFailure,
  classifyStatus,
  scopeSlugOfRequest,
} from "../../lib/wsfetch";
import { translateApiErr } from "../../lib/apiErr";
import { confirmDecisionFor } from "../../lib/chatConfirm";
// A REFUSED step's sentence is resolved here from the machine key the server sent
// — never from a string on the wire (t_3839995c; see the module's header for the
// defect and for why the server's own sentence reached every locale).
import { refusalHeading, refusalText } from "../../lib/chatRefusal";
import { answerText } from "../../lib/chatAnswer";
// The card's own wording is rendered through the dictionary from the KEYS the
// server sends — never from a sentence on the wire (t_30ffa28a; see the module's
// header for the defect and for why the assistant's `note` is the exception).
import {
  proposalLabel,
  proposalSummary,
  proposalTitle,
  proposalValue,
  toolLabel,
  type ProposalCardData,
} from "../../lib/chatProposal";
import { dropWorkspaceScopeIfCurrent, workspaceQuery, workspaceUrl } from "../../lib/workspace";
// The stored conversations: the wire codec lives there (pure, executed by
// lib/chatSessions.test.ts), so this page only decides WHEN to load one, create
// one, rename one or delete one.
import {
  parseSessionDetail,
  parseSessionList,
  readSession,
  sessionAfterDelete,
  sessionLabel,
  type ChatSession,
} from "../../lib/chatSessions";
// The streamed answer (card t_148d7100): the frame parser and the rule that turns
// progress into the bubble's text. Both are pure and executed by
// src/lib/chatStream.test.ts — a frame split across two network chunks is not a
// case a rendering test can produce on demand.
import {
  applyChatStreamEvent,
  createChatStreamParser,
  type ChatStreamEvent,
} from "../../lib/chatStream";
// Whether the transcript may follow new content — the decision that used to be
// "always", which is what pulled a reader who had scrolled up back to the bottom.
import { isAtBottom } from "../../lib/chatScroll";
// The composer's auto-growing height (eight lines, then it scrolls).
import { CHAT_COMPOSER_MAX_ROWS, composerHeight } from "../../lib/chatComposerGrow";
// A tool name becomes a sentence here, in this visitor's language: the API sends
// the identifier and never prose (issue #6177).
import { stepLabel } from "../../lib/chatSteps";
// The empty state's questions, as keys — each chip SENDS its own text.
import { CHAT_STARTERS } from "../../lib/chatStarters";

type Turn = { role: "user" | "assistant"; content: string };

type Step = {
  tool: string;
  status: string;
  // The KEY naming this step's detail, plus the values its sentence
  // interpolates — for a refused step, why it was refused; for a proposed step,
  // the card that was prepared. There is deliberately no prose field: the server
  // does not know this visitor's language (t_3839995c / issue #6177), and
  // lib/chatRefusal.ts is the only way this becomes text.
  detail_key?: string;
  detail_params?: Record<string, string>;
};

type Filed = { thread_id: string; subject: string; excerpt: string };

type Limits = {
  messages_per_hour: number;
  burst: number;
  max_message_chars: number;
  max_history_turns: number;
  max_tool_steps: number;
  max_proposals_per_message: number;
  proposal_ttl_seconds: number;
  confirms_per_hour: number;
  confirm_burst: number;
};

// A confirmation card (phase 2). It carries the opaque token, which is the ONLY
// thing that can authorise the write — and the args_hash, which binds the
// execution to the arguments shown here. Neither is ever sent to the model.
//
// The TYPE lives in lib/chatProposal.ts with the renderers: the card's wording is
// keys the dashboard resolves, and that module is where the key/data decision is
// executed by a test (t_30ffa28a).
type Proposal = ProposalCardData;

type ChatReply = {
  reply: string;
  // The KEY of the answer's sentence when the model wrote no prose of its own
  // (t_7f1694cb). The API does not know this visitor's language, so the fallback is
  // a dictionary key, resolved by lib/chatAnswer.ts — the same rule the refusal
  // block follows. Absent whenever `reply` carries real words.
  reply_key?: string;
  steps?: Step[];
  filed?: Filed;
  proposals?: Proposal[];
  limits?: Limits;
};

// A card's lifecycle in the UI. "pending" is the only state where the buttons
// exist: a confirmed or cancelled card is spent server-side, so offering the
// buttons again would produce a 409 the user cannot act on.
//
// "scopeRequired" is NOT a failure of the change, and it is deliberately not
// folded into "failed". The server refuses an unscoped call BEFORE it reads the
// token (rest_scope.go's ambiguous-scope path), so nothing was attempted and the
// token is still live. A card that said "The change could not be applied. Please
// try again." would be instructing the user to repeat an action that cannot work
// without a workspace — and it said that underneath an alert that had already
// told them to choose one (t_790b1016).
//
// "roleDenied" is the mirror image, and it exists for the same reason. The server
// refuses a confirmed write the workspace ROLE does not allow AFTER it has claimed
// the token (mcpsrv's write-role gate), so this one IS a failure of the change and
// the card keeps the failure badge — but the generic failure copy is wrong for it
// in both halves: it names no cause when the server gave one (the role), and its
// "try again" invites a click on a card that is already spent (t_1900adef). The
// cause sentence is carried ONCE, by the page's own role="alert", exactly like the
// other two named refusals.
//
// "workspaceRequired" and "refused" are the same shape for the two post-claim
// refusals that had no state at all (t_bf4feea2):
//
//   - workspaceRequired: the account has no workspace, so a form creation can never
//     land (the gate's own refusal, measured on staging). The remedy is two steps —
//     create a workspace, then ask for a fresh card — and neither is "try again".
//   - refused: the server answered and refused but this build cannot classify why.
//     The change did not run and the token is spent, so the copy says the card must
//     be prepared again. Only "failed" — no readable body, a 5xx, a transport error
//     — may suggest another attempt, because there nothing proves the token is gone.
type ProposalState =
  | "pending"
  | "confirming"
  | "cancelling"
  | "done"
  | "cancelled"
  | "stale"
  | "unexecutable"
  | "scopeRequired"
  | "roleDenied"
  | "workspaceRequired"
  | "refused"
  | "failed";

type Msg = {
  role: "user" | "assistant";
  content: string;
  // files / steps / proposals are only present on an assistant turn that
  // produced them.
  filed?: Filed;
  refused?: Step[];
  // steps is the trail of tools this answer DID call (status "called"), which the
  // page renders as a short list of sentences (lib/chatSteps.ts). It exists so the
  // answer is auditable: before this card a step was shown only when it was
  // REFUSED, so the ordinary "it read something and answered" left no trace at all.
  steps?: Step[];
  proposals?: Proposal[];
  // states maps a proposal token to what happened to it.
  states?: Record<string, ProposalState>;
  // streaming: this bubble is still being written (the cursor is shown, and the
  // message is the aria-busy live region).
  streaming?: boolean;
  // stopped: the user pressed Stop, so the text above is PARTIAL. Marking it is
  // the difference between an incomplete answer and an answer that looks finished
  // and happens to end mid-sentence.
  stopped?: boolean;
};

// What the assistant is doing right now, for the status line. "step" carries the
// tool name (not a sentence): the wording is the dashboard's, in the visitor's
// language — see lib/chatSteps.ts.
type Activity =
  | { kind: "thinking" }
  | { kind: "step"; tool: string }
  | { kind: "stopped" }
  | null;

// How much history the page sends when the server has not told it the cap yet,
// i.e. the window between the first paint and GET /v1/rest/chat answering.
//
// THE LIVE CAP IS THE SERVER'S, and it is not a constant in this file on purpose:
// `limits.max_history_turns` is reported by the probe, and its value is
// chat.MaxTurns in api/internal/chat/chat.go — the number of turns the runner
// KEEPS for the model. Sending by that number is what makes the client and the
// model agree; a literal mirrors nothing and drifts silently.
//
// The three numbers that disagreed (t_4c29963a, measured on production): this page
// sent 32, the runner kept 16, and rest_chat.go's chatMaxHistory (40) is a THIRD
// number — the edge's reject threshold for an implausible transcript, NOT a
// retention cap, which is what the comment here used to claim it was. A 19-turn
// conversation whose first turn carried a passphrase came back citing "giliran 4
// hingga 19": the model, answering confidently, about only the last 16 turns.
//
// This fallback is deliberately at or below the retention cap, and
// src/lib/chatHistoryCap.test.ts reads chat.MaxTurns out of the Go source and
// fails if it is not.
const MAX_HISTORY = 16;

export default function ChatPage() {
  const { t } = useI18n();
  const { anon, workspace, user } = useSession();
  const router = useRouter();

  const [available, setAvailable] = useState<boolean | null>(null);
  const [limits, setLimits] = useState<Limits | null>(null);
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [err, setErr] = useState("");
  // What the assistant is doing, for the status line — SEPARATE from `sending`
  // (which drives the controls) and from the button's label (which is always
  // "Send"). "stopped" is sticky until the next send, so a cancelled answer keeps
  // saying it was cancelled.
  const [activity, setActivity] = useState<Activity>(null);
  // Whether the transcript should follow new content. `true` until the reader
  // scrolls away from the bottom — see lib/chatScroll.ts for the measurement and
  // the tolerance.
  const [pinned, setPinned] = useState(true);
  // Which assistant message was just copied, so the button can say so for a beat.
  const [copied, setCopied] = useState<number | null>(null);

  const endRef = useRef<HTMLDivElement>(null);
  const logRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  // The transcript as it stands, readable from inside an async callback whose
  // closure may be a frame behind (the history load decides whether to re-open a
  // stored conversation, and it must not overwrite a message the user just sent).
  const msgsRef = useRef<Msg[]>([]);
  // The in-flight request. Aborting it is Stop: the browser closes the
  // connection, the API sees its write fail, and the runner stops the model work
  // behind it (api/internal/httpapi/rest_chat_stream.go).
  const controllerRef = useRef<AbortController | null>(null);
  // The answer's text as it has arrived so far. Stop's handler runs in a `catch`,
  // where the component's `msgs` closure may be a frame behind the stream (each
  // fragment re-renders), so "was anything written?" is answered from this ref
  // rather than from a stale render.
  const partialRef = useRef("");

  // The retention cap: the server's `max_history_turns` once the probe has
  // answered, the pre-probe fallback before that. ONE derivation, read by the
  // send path, by the restore and by the save — three places that must agree on
  // how much conversation is kept, because a transcript longer than the window
  // the model is given is the 32-against-16 divergence this page already shipped
  // once (see MAX_HISTORY above).
  //
  // `||`, not `??`: a reported 0 would make `slice(-0)` the WHOLE transcript —
  // the opposite of a cap. The API's own guard rules a 0 out (every field of
  // chatLimitsOut is asserted nonzero), so this is the cheap direction to fail
  // in, not a state the server can send.
  const historyCap = limits?.max_history_turns || MAX_HISTORY;

  // Keep the ref in step with the state, so a callback can ask "is the transcript
  // empty right now?" without a stale closure.
  useEffect(() => {
    msgsRef.current = msgs;
  }, [msgs]);

  // The stored conversations in this scope, most recently used first — the order
  // the server decides (see lib/chatSessions.ts: this page never re-sorts it).
  const [sessions, setSessions] = useState<ChatSession[]>([]);
  // The conversation on screen, or null for one that has not been created yet.
  const [activeId, setActiveId] = useState<string | null>(null);
  // Which row is being renamed (its id), or null. An inline input rather than a
  // prompt(): the label belongs in the list, next to the others, not in a modal
  // that covers them.
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");
  // The scope whose conversations are ON SCREEN (ref, not state): the load
  // effect must not re-run when it records what it has loaded. A `shownScope`
  // STATE here cancelled its own fetch — setting it re-rendered, the dependency
  // array changed, the cleanup set `cancelled = true`, and the effect's second
  // pass returned early at the "already shown" guard, so the list silently
  // stayed empty. Measured in the browser (e2e/chat-history.spec.ts), not
  // reasoned about.
  const shownScopeRef = useRef<string | null>(null);

  // The scope this page's history belongs to. A conversation is stored per person
  // AND per workspace, so a workspace switch is a different sidebar — and the
  // value is built from the server's own answers (the session bundle's user id and
  // the shell's workspace slug), never from anything a caller could type.
  const historyScope = user?.id ? `${user.id}:${workspace || "-"}` : "";

  const goToLogin = useCallback(() => {
    // Nothing to clear here any more: the conversation is on the server, so a dead
    // session loses nothing and a shared device hands the next person an empty
    // page (the sign-out path still sweeps the keys the OLD browser-only version
    // wrote — see lib/chatTranscript.ts and Shell.signOut).
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  // Read one stored conversation and put it on screen.
  const openSession = useCallback(
    async (id: string) => {
      const r = await runLoad(
        () =>
          fetchJson<unknown>(`/api/rest/chat/sessions/${id}${workspaceQuery()}`),
        { onUnauth: goToLogin, message: t("chat.historyLoadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
      );
      if (!r.ok) {
        // A refused read is reported, but it does not close the box: the
        // conversation could not be restored, and the user can still ask a new
        // question.
        setErr(r.error || t("chat.historyLoadFail"));
        return;
      }
      const detail = parseSessionDetail(r.data);
      if (!detail) {
        // A payload this page cannot render (a shape change, a partial write):
        // start from an empty transcript rather than half a conversation.
        setErr(t("chat.historyLoadFail"));
        return;
      }
      setErr("");
      setActiveId(detail.session.id);
      setMsgs(
        detail.turns.map((turn) => ({
          role: turn.role,
          content: turn.content,
          steps: turn.steps,
          // A restored answer the user stopped: the marker must survive the
          // reload, or a partial answer reads as a finished one (t_babeaa84).
          stopped: turn.stopped,
        })),
      );
    },
    [goToLogin, t],
  );

  // Start a conversation on the server ("New conversation", and the first message
  // of a conversation that has no row yet).
  //
  // The row is created EAGERLY rather than on the first message: the card's
  // contract is that the button starts a conversation, and a sidebar that only
  // grows after you type is a control whose effect the user cannot see. The cost
  // is that an abandoned new conversation stays in the list — which is why each
  // row can be deleted, and why an unnamed one is labelled ("New conversation")
  // rather than left blank.
  const createSession = useCallback(async (): Promise<ChatSession | null> => {
    const r = await runLoad(
      () =>
        fetchJson<unknown>(`/api/rest/chat/sessions${workspaceQuery()}`, {
          method: "POST",
          body: "{}",
        }),
      { onUnauth: goToLogin, message: t("chat.historyCreateFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setErr(r.error || t("chat.historyCreateFail"));
      return null;
    }
    const row = readSession((r.data as { session?: unknown } | null)?.session);
    if (!row) {
      setErr(t("chat.historyCreateFail"));
      return null;
    }
    return row;
  }, [goToLogin, t]);

  // Reload the sidebar after an exchange. The server names a conversation from its
  // first question and moves it to the top, so the row on screen is stale as soon
  // as an answer lands. Best-effort: the transcript itself is already correct.
  const refreshSessions = useCallback(async () => {
    const r = await runLoad(
      () => fetchJson<unknown>(`/api/rest/chat/sessions${workspaceQuery()}`),
      { onUnauth: goToLogin, message: t("chat.historyLoadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (r.ok) setSessions(parseSessionList(r.data));
  }, [goToLogin, t]);

  // Rename one conversation. The input is inline (see `renaming`), and an empty
  // value means "leave it alone" rather than "blank the label": the sidebar has a
  // fallback for an unnamed row, but a user who opens the field and changes their
  // mind must not lose the name the server gave it.
  async function commitRename(id: string) {
    const title = renameValue.trim();
    setRenaming(null);
    if (!title) return;
    try {
      const r = await fetch(`/api/rest/chat/sessions/${id}${workspaceQuery()}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title }),
      });
      if (!r.ok) {
        setErr(t("chat.historySaveFail"));
        return;
      }
      setSessions((prev) => prev.map((s) => (s.id === id ? { ...s, title } : s)));
    } catch {
      setErr(t("chat.historySaveFail"));
    }
  }

  // Delete one conversation. A delete destroys the messages (the server cascades),
  // so it asks first — a mis-click on a list of similar-looking labels must not
  // silently erase a transcript.
  async function removeSession(id: string) {
    if (!window.confirm(t("chat.deleteConfirm"))) return;
    try {
      const r = await fetch(`/api/rest/chat/sessions/${id}${workspaceQuery()}`, { method: "DELETE" });
      if (!r.ok) {
        setErr(t("chat.historySaveFail"));
        return;
      }
    } catch {
      setErr(t("chat.historySaveFail"));
      return;
    }
    const next = sessionAfterDelete(sessions, id, activeId);
    setSessions((prev) => prev.filter((s) => s.id !== id));
    if (activeId === id) {
      setMsgs([]);
      setActiveId(null);
      if (next) await openSession(next);
    }
  }

  // Switch the transcript to another stored conversation. The row is marked
  // active immediately and the turns are dropped while the read is in flight, so
  // the header can never sit over the PREVIOUS conversation's messages.
  async function selectSession(id: string) {
    // Re-clicking the conversation already on screen is a no-op — unless it is
    // empty because the last read was refused, in which case the click is the
    // retry.
    if (id === activeId && msgsRef.current.length > 0) return;
    setErr("");
    setRenaming(null);
    setActiveId(id);
    setMsgs([]);
    await openSession(id);
  }

  // Open the rename field for one row, seeded with the label the user is
  // looking at (empty for a conversation the server has not named yet).
  function startRename(s: ChatSession) {
    setRenaming(s.id);
    setRenameValue(s.title || "");
  }

  // LOAD THE SIDEBAR, THEN RE-OPEN THE MOST RECENT CONVERSATION.
  //
  // This runs on mount and again on a scope change (a different user, or the
  // workspace the shell publishes after it answers — the same two-step the
  // availability probe already handles). `shownScopeRef` is what makes the effect
  // run once per scope, and it is a REF so recording it does not re-render and
  // cancel the very fetch it was recording (see the ref's own comment).
  //
  // A message already sent in this mount is never replaced: the page only opens a
  // stored conversation into an EMPTY transcript, which is the state a fresh
  // mount is in.
  useEffect(() => {
    if (!historyScope || shownScopeRef.current === historyScope) return;
    let cancelled = false;
    // A scope CHANGE invalidates the transcript on screen (it belongs to the
    // previous workspace/user); the FIRST resolution does not, because the user
    // may have typed before the session bundle answered and that message is newer
    // than anything stored.
    const firstScope = shownScopeRef.current === null;
    shownScopeRef.current = historyScope;
    setRenaming(null);
    setSessions([]);
    setActiveId(null);
    if (!firstScope) setMsgs([]);
    void (async () => {
      const r = await runLoad(
        () => fetchJson<unknown>(`/api/rest/chat/sessions${workspaceQuery()}`),
        { onUnauth: goToLogin, message: t("chat.historyLoadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
      );
      if (cancelled) return;
      if (!r.ok) {
        // The availability probe above already reports a broken deployment; this
        // is the history half, and it says so rather than showing an empty
        // sidebar as if the user had never talked to the assistant.
        setErr(r.error || t("chat.historyLoadFail"));
        return;
      }
      const rows = parseSessionList(r.data);
      setSessions(rows);
      // Re-open the most recent conversation only into an EMPTY transcript: a
      // question the user sent while the session bundle was still answering is
      // newer than anything stored, and replacing it would drop what they asked.
      if (rows.length > 0 && msgsRef.current.length === 0) await openSession(rows[0].id);
    })();
    return () => {
      cancelled = true;
    };
    // `openSession` is stable, and adding it would re-run this on every render of
    // the parent context — the scope is the trigger.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [historyScope, goToLogin]);

  // THE MESSAGE LIST IS SAVED BY THE SERVER, not by an effect here: POST
  // /v1/rest/chat appends the question and the answer to the conversation named
  // by `session_id` (api/internal/httpapi/rest_chat_sessions.go). There is no
  // client-side write, so there is nothing to mirror — and nothing for a failed
  // render or a closing tab to lose.

  // Availability first: with no LLM the box must not be offered at all.
  useEffect(() => {
    void (async () => {
      const r = await runLoad(
        () =>
          fetchJson<{ available?: boolean; limits?: Limits }>(
            `/api/rest/chat${workspaceQuery()}`,
          ),
        { onUnauth: goToLogin, message: t("chat.unavailable"), errorKey: (b: unknown) => translateApiErr(b, t) },
      );
      if (!r.ok) {
        // A "scope" failure is the TENANCY refusal for a stale `?workspace=`
        // (t_da4b2031): runLoad has already dropped the slug, so the right move is
        // to say what happened and let the re-probe below run once the session
        // publishes the reconciled workspace — NOT to close the box, and above all
        // NOT to navigate away. It is reported with the same copy the missing-scope
        // 400 uses, because the user's action is the same.
        if (r.kind === "scope") {
          setErr(t("chat.scopeRequired"));
          return;
        }
        // A 400 here is the WORKSPACE-SCOPE refusal (rest_scope.go's
        // `scopeAmbiguous`: an unscoped call from a session that can reach more
        // than one workspace), and it is NOT the same answer as
        // available:false. The assistant is available — this request simply
        // carried no scope for the server to resolve an account from — so
        // reporting "The assistant is not available right now." tells the user
        // something false, and it closes the box for a condition the user fixes
        // by choosing a workspace. It is the same 400 the message path already
        // separates out in send(); the probe consumed it as a generic failure
        // and was the one call site left saying the wrong thing (t_997bb646).
        //
        // WHY THERE IS NO INLINE FETCH AND NO NEW runLoad OPTION: the outcome
        // runLoad already returns carries the `status` (LoadOutcome), so the
        // branch needs nothing new — and reading it here leaves runLoad's
        // 401/403 → onUnauth and the API-code translation untouched for every
        // other page. The GET route has exactly one 400 (the scope gate); a
        // genuinely unavailable deployment answers 200 with available:false
        // and a rejected credential answers 503 on the message path, so this
        // branch cannot swallow either.
        if (r.status === 400) {
          setErr(t("chat.scopeRequired"));
          return;
        }
        // EVERYTHING ELSE is a genuine failure, and the sentence runLoad
        // computed for it is what the page renders — in the alert slot, which is
        // `r.error`'s ONLY consumer. `r.error` is the translated machine code
        // when the server named one it could translate, and the generic
        // `t("chat.unavailable")` when it could not (runLoad's own `|| fallback`
        // contract), so both states are one call and the server's prose is never
        // reachable. Discarding it here is what made the whole #6177 translation
        // inert on this page: a refused workspace rendered the generic "the
        // assistant is not available right now" — false, the assistant IS
        // available and the workspace is simply not the caller's — with an empty
        // alert, and dropping the `errorKey` option changed nothing a user sees
        // (measured in a browser, t_57a77d02).
        //
        // The transcript's own generic line stands down for it (`!err` in the
        // render below) rather than printing the same sentence a second time one
        // region over, exactly as the scope path above refuses to report a false
        // "not available" for a condition the user can fix (t_997bb646).
        setAvailable(false);
        setErr(r.error);
        return;
      }
      // The probe succeeded, so any scope message from THIS probe is resolved:
      // clear it, or the user keeps reading "choose a workspace first" over a page
      // that is working (the stale-slug path above). It runs only on mount and on
      // a workspace change, so it cannot wipe an unrelated message mid-read.
      setErr("");
      setAvailable(r.data.available === true);
      if (r.data.limits) setLimits(r.data.limits);
    })();
    // `workspace` is a dependency (t_da4b2031): on a stale slug the first probe is
    // refused, the slug is dropped, the session then publishes a workspace the
    // caller really has, and THIS is what makes the page probe again for it
    // instead of sitting on the refusal.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [goToLogin, workspace]);

  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  // WHICH SURFACE THE READER IS SCROLLING (card t_148d7100).
  //
  // The transcript is a BOUNDED scroll box (`max-h-[60vh]` on it below), so
  // normally IT is what moves. On a short viewport the page around it scrolls
  // too, and a reader who dragged THAT up is reading something else just as
  // much. Measured on the built page before the box was capped: the log's
  // `scrollHeight` equalled its `clientHeight` (404/404) on a transcript long
  // enough to overflow, because a `flex-1` box in an auto-height column grows
  // instead of scrolling — so the log's `onScroll` never fired, `pinned` stayed
  // `true`, and the follow yanked the reader's WINDOW back down. That is the
  // original defect with a different scrollbar, which is why the decision reads
  // whichever surface actually scrolls rather than assuming the box does.
  const decidePinned = useCallback(() => {
    const el = logRef.current;
    if (!el) return;
    const scrollsItself = el.scrollHeight > el.clientHeight + 1;
    setPinned(
      isAtBottom(
        scrollsItself
          ? { scrollTop: el.scrollTop, scrollHeight: el.scrollHeight, clientHeight: el.clientHeight }
          : {
              scrollTop: window.scrollY,
              scrollHeight: document.documentElement.scrollHeight,
              clientHeight: window.innerHeight,
            },
      ),
    );
  }, []);

  // The page itself can be the scroller, so it is listened to as well as the log.
  // `passive` because this handler only reads — it must never delay the reader's
  // own scrolling.
  useEffect(() => {
    window.addEventListener("scroll", decidePinned, { passive: true });
    return () => window.removeEventListener("scroll", decidePinned);
  }, [decidePinned]);

  // FOLLOW THE CONVERSATION ONLY WHEN THE READER IS AT THE BOTTOM.
  //
  // This effect used to run unconditionally on `msgs.length`/`sending`. Two
  // things were wrong with that: a reader who had scrolled UP to re-read an
  // earlier answer was yanked back down by the next message, and once the answer
  // streams, "new content" happens many times a second — so the pull would have
  // been continuous. `pinned` is the decision (lib/chatScroll.ts, executed by its
  // test); the reader sets it by scrolling, and "Jump to latest" sets it back.
  // `scrollIntoView` is the RIGHT primitive for two possible scrollers: it moves
  // every scrollable ancestor as needed, so it follows the log when the log
  // scrolls and the page when the page does.
  useEffect(() => {
    if (!pinned) return;
    endRef.current?.scrollIntoView({ block: "end" });
  }, [msgs, sending, activity, pinned]);

  // THE COMPOSER GROWS WITH ITS CONTENT. `resize-none` + a fixed `rows` was a
  // two-line window onto a 2,000-character message, and it removed the one
  // affordance that could have made it bigger. The height is measured from the
  // element itself (scrollHeight knows about wrapping, which a character count
  // does not) and clamped to CHAT_COMPOSER_MAX_ROWS lines by the pure function.
  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    const cs = window.getComputedStyle(el);
    const lineHeight = parseFloat(cs.lineHeight) || 20;
    const chrome =
      (parseFloat(cs.paddingTop) || 0) +
      (parseFloat(cs.paddingBottom) || 0) +
      (parseFloat(cs.borderTopWidth) || 0) +
      (parseFloat(cs.borderBottomWidth) || 0);
    // Measure from `auto` so the box can SHRINK again when text is deleted —
    // without the reset, scrollHeight is the height it already has and the box
    // would only ever grow.
    el.style.height = "auto";
    el.style.height = `${composerHeight(el.scrollHeight, lineHeight, chrome, CHAT_COMPOSER_MAX_ROWS)}px`;
  }, [input]);

  // ── Sending, streaming, and Stop ────────────────────────────────────────────

  // Replace the text of the LAST message. Only the streaming answer writes here,
  // and only ever to the last turn — so a fragment that arrives after the answer
  // was replaced (or after a Stop) cannot scribble on an earlier message.
  function editLastMessage(fn: (m: Msg) => Msg) {
    setMsgs((prev) => {
      const i = prev.length - 1;
      if (i < 0) return prev;
      const next = [...prev];
      next[i] = fn(next[i]);
      return next;
    });
  }

  // The finished answer, applied to the transcript. ONE renderer for both
  // transports: the server puts the SAME body in a `done` frame that it would
  // have returned from the plain POST, so a deployment without streaming renders
  // the identical transcript.
  function applyReply(d: ChatReply, hadPlaceholder: boolean) {
    if (d.limits) setLimits(d.limits);
    const refused = (d.steps ?? []).filter((s) => s.status === "refused");
    // The steps that RAN. They used to be dropped from the transcript entirely
    // unless they were refused, so "it looked something up" left no trace.
    const called = (d.steps ?? []).filter((s) => s.status === "called");
    const proposals = d.proposals ?? [];
    const states: Record<string, ProposalState> = {};
    for (const p of proposals) states[p.token] = "pending";
    const msg: Msg = {
      role: "assistant",
      // The answer the visitor reads. The model's own words when it wrote any;
      // otherwise the dictionary sentence the server named (t_7f1694cb), so a
      // refused proposal cannot end the turn with an empty bubble. Both empty
      // stays empty — the page must not invent a sentence of its own.
      content: answerText(d.reply, d.reply_key, t),
      filed: d.filed,
      steps: called.length ? called : undefined,
      refused: refused.length ? refused : undefined,
      proposals: proposals.length ? proposals : undefined,
      states: proposals.length ? states : undefined,
    };
    setMsgs((prev) => {
      const last = prev[prev.length - 1];
      if (hadPlaceholder && last?.role === "assistant") {
        return [...prev.slice(0, -1), msg];
      }
      return [...prev, msg];
    });
  }

  // Read one streamed answer into the transcript. Throws when the stream fails or
  // ends without a `done` frame, so the caller's catch is the ONE failure path for
  // both transports.
  async function readStream(body: ReadableStream<Uint8Array>) {
    const reader = body.getReader();
    const decoder = new TextDecoder();
    const parser = createChatStreamParser();
    // The answer as the stream has produced it. This is the accumulator the
    // reducer is applied to — not the message in state, which React may not have
    // committed yet when a second frame arrives in the same chunk.
    let text = "";
    let finished = false;
    try {
      while (!finished) {
        const { value, done } = await reader.read();
        if (done) break;
        const events: ChatStreamEvent[] = parser.push(decoder.decode(value, { stream: true }));
        for (const ev of events) {
          if (ev.type === "delta" || ev.type === "retract") {
            text = applyChatStreamEvent(text, ev);
            partialRef.current = text;
            editLastMessage((m) => ({ ...m, content: text }));
            continue;
          }
          if (ev.type === "step") {
            setActivity({ kind: "step", tool: ev.tool });
            continue;
          }
          if (ev.type === "error") {
            throw new Error("the assistant failed while answering");
          }
          if (ev.type === "done") {
            applyReply(ev.result as ChatReply, true);
            finished = true;
            break;
          }
        }
      }
      if (!finished) throw new Error("the stream ended before the answer did");
    } finally {
      // Closing the reader is what tears the connection down; on the Stop path the
      // abort has already done it, and cancelling twice is harmless.
      void reader.cancel().catch(() => {});
    }
  }

  // The one send path, used by the form AND by the starter chips. A chip that
  // had its own fetch would be a second place for the scope, the cap and the
  // streaming fallback to drift.
  async function sendText(raw: string) {
    const text = raw.trim();
    if (!text || sending) return;
    setErr("");
    setCopied(null);

    const cap = limits?.max_message_chars ?? 0;
    if (cap > 0 && text.length > cap) {
      setErr(t("chat.tooLong"));
      return;
    }

    // The transcript the user can see, minus the filing/refusal cards (which are
    // UI affordances, not conversation turns), capped to what the server KEEPS.
    // `historyCap` is the component-level derivation (see its comment): the same
    // number the restore and the save use, so the turns sent are the turns kept.
    // On a page that has just restored a conversation this is what carries the
    // restored turns to the model — the conversation continues instead of
    // restarting mid-thread.
    const history: Turn[] = msgs
      .slice(-historyCap)
      .map((m) => ({ role: m.role, content: m.content }));

    setMsgs((prev) => [...prev, { role: "user", content: text }]);
    setInput("");
    setSending(true);
    setActivity({ kind: "thinking" });
    partialRef.current = "";
    // Sending brings the reader back to the conversation: they just asked
    // something, so the answer is what they want to see.
    setPinned(true);

    // THE CONVERSATION THIS EXCHANGE IS STORED IN. A page that has never sent
    // anything has no row yet, so one is created here — lazily, so a visit that
    // asks nothing does not leave an empty conversation behind.
    //
    // A failure here does NOT block the answer: the message is still sent, it just
    // is not stored, and the page says so. Losing the copy is bad; refusing to
    // answer because a side store is unavailable would be worse.
    let sessionId = activeId;
    if (!sessionId) {
      const created = await createSession();
      if (created) {
        sessionId = created.id;
        setActiveId(created.id);
        setSessions((prev) => [created, ...prev]);
      }
    }

    const ctrl = new AbortController();
    controllerRef.current = ctrl;
    let placeholder = false;
    try {
      // `?stream=1` is the trigger the dashboard can use: its /api proxy forwards
      // the body, cookie and client address but NOT the Accept header
      // (web/src/app/api/[...path]/route.ts). The API honours either.
      const r = await fetch(workspaceUrl("/api/rest/chat?stream=1"), {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "text/event-stream",
        },
        body: JSON.stringify({ message: text, history, session_id: sessionId || undefined }),
        signal: ctrl.signal,
      });
      // ONE decision for the failure statuses, shared with runLoad: a 401 (or an
      // uncoded 403) is a dead session; a 403 that NAMES the tenancy code
      // (workspace.notFound) is a STALE SCOPE, and treating it as a dead session
      // is exactly the defect this card fixes — the merchant was bounced to
      // /login for a slug in their own localStorage (t_da4b2031). The scope case
      // re-reads the reconciled slug instead of navigating away, and says what to
      // do, so no data is lost and no session is thrown away.
      //
      // Every one of these refusals is answered BEFORE the stream opens
      // (rest_chat_stream.go), so they still arrive as statuses rather than as
      // frames — which is why this block is unchanged by the streaming switch.
      if (!r.ok) {
        const failure = await classifyResponseFailure(r);
        if (failure === "scope") {
          setErr(t("chat.scopeRequired"));
          return;
        }
        if (failure === "unauth") {
          goToLogin();
          return;
        }
      }
      if (r.status === 429) {
        setErr(t("chat.rateLimited"));
        return;
      }
      // 400 = the request itself was refused (a missing/unusable workspace scope),
      // NOT a dead session: the 401/403 rule above deliberately does not reach
      // here. Showing the generic failure would hide the one thing the user can
      // act on (choose a workspace), so the server's scope refusal gets its own
      // copy.
      if (r.status === 400) {
        setErr(t("chat.scopeRequired"));
        return;
      }
      if (r.status === 503) {
        setAvailable(false);
        setErr(t("chat.unavailable"));
        return;
      }
      if (!r.ok) {
        setErr(t("chat.fail"));
        return;
      }

      const contentType = r.headers.get("content-type") ?? "";
      if (r.body && contentType.includes("text/event-stream")) {
        placeholder = true;
        // The bubble exists BEFORE the first fragment, so the cursor has somewhere
        // to sit while the model thinks — the visible difference between "nothing
        // happened" and "it is answering".
        setMsgs((prev) => [...prev, { role: "assistant", content: "", streaming: true }]);
        await readStream(r.body);
      } else {
        // THE FALLBACK: one JSON body — a buffering proxy, an older server, or a
        // runner with no streaming capability. Rendered by the same code as a
        // `done` frame.
        const d = (await r.json()) as ChatReply;
        applyReply(d, false);
      }
      // The answer landed, so the conversation has two more turns and (on the
      // first question) a name. Re-read the sidebar so the row says what the
      // server now says; the transcript itself is already on screen.
      void refreshSessions();
    } catch {
      if (ctrl.signal.aborted) {
        // STOP. What was written stays: it is the only thing the user waited for.
        // An EMPTY bubble is removed instead — there is nothing to keep, and an
        // empty assistant turn is noise — and in that case the status line carries
        // the "incomplete" sentence, because there is no bubble left to mark.
        const kept = partialRef.current.trim() !== "";
        setMsgs((prev) => {
          const last = prev[prev.length - 1];
          if (!placeholder || last?.role !== "assistant") return prev;
          if (!kept) return prev.slice(0, -1);
          return [...prev.slice(0, -1), { ...last, streaming: false, stopped: true }];
        });
        setActivity(kept ? null : { kind: "stopped" });
      } else {
        setErr(t("chat.fail"));
        setMsgs((prev) => {
          const last = prev[prev.length - 1];
          if (!placeholder || last?.role !== "assistant") return prev;
          return last.content.trim()
            ? [...prev.slice(0, -1), { ...last, streaming: false }]
            : prev.slice(0, -1);
        });
      }
    } finally {
      controllerRef.current = null;
      setSending(false);
      // The cursor goes on EVERY path, including a throw between the placeholder
      // and the answer.
      setMsgs((prev) => {
        const last = prev[prev.length - 1];
        if (!last?.streaming) return prev;
        return [...prev.slice(0, -1), { ...last, streaming: false }];
      });
      setActivity((a) => (a?.kind === "stopped" ? a : null));
    }
  }

  function send(e: React.FormEvent) {
    e.preventDefault();
    void sendText(input);
  }

  // Stop: abort the request. The browser closes the connection, the API's write
  // fails, and the runner returns that error — which stops the model work instead
  // of paying for an answer nobody will read (rest_chat_stream.go).
  //
  // It does NOT set the status line to "stopped" here: the partial answer carries
  // its own incomplete marker, and saying the same sentence twice (once under the
  // composer, once in the bubble) is the duplication this card removed from the
  // Send button. The status line gets it only when there is no bubble to mark.
  function stop() {
    controllerRef.current?.abort();
  }

  // "Jump to latest" — the control that makes NOT following the conversation
  // acceptable. Without it, staying scrolled up would be a dead end.
  function jumpToLatest() {
    setPinned(true);
    endRef.current?.scrollIntoView({ block: "end" });
  }

  async function copyMessage(index: number, text: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(index);
      window.setTimeout(() => setCopied((c) => (c === index ? null : c)), 2000);
    } catch {
      // A refused clipboard (an insecure origin, or a browser that wants a user
      // gesture) must not claim success: the text stays selectable by hand.
    }
  }

  // ── The confirmation lane ──────────────────────────────────────────────────
  //
  // The ONLY place this page can cause a write. Everything the model controls is
  // already fixed by the time we get here: the token is opaque and single use, the
  // args_hash binds the execution to the arguments rendered above, and the server
  // re-checks the token, the hash, the expiry and the caller. So this function's
  // whole job is to send those two values and report honestly what came back —
  // it cannot "confirm the wrong thing" because it does not decide what runs.
  //
  // The model never sees the token: it went straight from the server's response
  // into this component's state.
  async function decide(msgIndex: number, p: Proposal, action: "confirm" | "cancel") {
    setErr("");
    setState(msgIndex, p.token, action === "confirm" ? "confirming" : "cancelling");
    // The two routes are written LITERALLY rather than built from `action`: the
    // route-parity guard resolves frontend paths against the backend mux by
    // reading this source, and a computed path is a path nothing can check. A
    // typo here would then be a 404 at the one moment the user has committed to a
    // decision.
    const url = action === "confirm" ? "/api/rest/chat/confirm" : "/api/rest/chat/cancel";
    try {
      // The decision carries the active workspace for the same reason the message
      // does: the server resolves the WRITE account from it, and a confirmation is
      // the one chat call that changes data. Without it a two-workspace session
      // falls back to the user's FIRST account — the card names the account they
      // selected, but the write would land on the other one.
      const r = await fetch(`${url}${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(
          action === "confirm"
            ? { token: p.token, args_hash: p.args_hash }
            : { token: p.token },
        ),
      });
      // 401 → a dead session. A 403 is decided by the BODY's machine code, not by
      // the status: an uncoded 403 is an authorization failure (→ /login), while
      // one naming the tenancy code (workspace.notFound) is a stale `?workspace=`
      // slug and must NOT sign the merchant out (t_da4b2031). So the body is read
      // FIRST and handed to the shared decision below; only an "unauth" outcome
      // navigates, and the two reads cannot disagree because the same (status,
      // body) pair drives both. The old `r.status === 401 || r.status === 403`
      // line sent the scope refusal straight to /login.
      const body = await r.json().catch(() => null);
      const failure = classifyStatus(r.status, body);
      if (failure === "scope") {
        // The stale slug must stop being sent (t_da4b2031), exactly as runLoad
        // does for a page load: publishWorkspaceScope("") drops it and notifies,
        // so the next request is scoped to whatever workspace the session then
        // publishes rather than repeating a refusal the server already answered.
        //
        // ROUTED TO THE SLUG THIS REQUEST CARRIED (t_1cfb4044), not to whatever
        // is current: this send awaited a response, and the session may have
        // reconciled the scope while it was in flight. An unconditional drop
        // would wipe the reconciled value and leave every later request unscoped.
        dropWorkspaceScopeIfCurrent(scopeSlugOfRequest(r));
      }
      if (failure === "unauth") {
        goToLogin();
        return;
      }
      // EVERY other answer goes through ONE decision that branches on the API's
      // machine CODE, not on the bare status (lib/chatConfirm.ts). The 400 is the
      // case that matters: it is NOT necessarily a workspace-scope refusal — the
      // server maps every unrecognised confirm failure to 400, including a tool it
      // refused AFTER claiming the token — so a status-only branch told a member
      // whose write was refused by their ROLE to "choose a workspace first", which
      // they cannot act on (t_fb393abe). The body was read above; a body that
      // cannot be read yields no code, which falls to the failure branch and never
      // the scope copy.
      const outcome = confirmDecisionFor(r.status, body);
      if (outcome === "scopeRequired") {
        // The card gets its OWN state, not "failed": the server refuses this
        // BEFORE it reads the token (requireResolvedWorkspaceScope runs ahead of
        // the token checks in handleChatConfirm), so nothing was attempted, the
        // token is still live, and the card must not wear a "Did not go through"
        // pill over a "try again" footer — retrying unchanged cannot work
        // (t_790b1016). The scope copy is carried ONCE, by the alert below:
        // rendering it in the card as well would duplicate the sentence, which is
        // the pre-fix shape.
        setState(msgIndex, p.token, "scopeRequired");
        setErr(t("chat.scopeRequired"));
        return;
      }
      if (outcome === "rateLimited") {
        setState(msgIndex, p.token, "failed");
        setErr(t("chat.confirmRateLimited"));
        return;
      }
      if (outcome === "roleDenied") {
        // The confirmed write was refused by the workspace ROLE, AFTER the server
        // claimed the token. Two things follow, and both are why this is not the
        // generic failure branch:
        //
        //   * the cause is KNOWN and actionable ("you need manage or admin here"),
        //     so the sentence names it instead of the page guessing; and
        //   * the card is SPENT — the refusal happens after the atomic claim — so
        //     "Please try again" would invite the user to press a button that
        //     cannot work. The sentence says the card cannot be used again and
        //     points at the only remedy (a fresh card).
        //
        // Carried ONCE, by the page's single role="alert" (setErr below), exactly
        // as the scope and unexecutable refusals are: the card itself renders no
        // sentence for this state, so the copy cannot appear twice. The badge still
        // says the change did not go through, which is TRUE of it — unlike
        // scopeRequired, where nothing was attempted.
        //
        // There is no cancel arm here on purpose: a cancel never dispatches the
        // tool, so the write-role gate cannot refuse one. A branch for a shape that
        // cannot fire is a branch nothing can ever prove.
        setState(msgIndex, p.token, "roleDenied");
        setErr(t("chat.confirmRoleDenied"));
        return;
      }
      if (outcome === "workspaceRequired") {
        // The account has NO workspace, so the confirmed creation could never land
        // (the gate refused it after the server claimed the token). Two facts have
        // to reach the user and only this sentence may carry them (once, by the
        // alert): WHY it did not run, and that this card is finished — the token
        // was spent, so pressing Confirm again cannot work. "Create a workspace" is
        // the first step of the remedy; a fresh card afterwards is the second.
        setState(msgIndex, p.token, "workspaceRequired");
        setErr(t("chat.confirmWorkspaceRequired"));
        return;
      }
      if (outcome === "refused") {
        // The server answered and refused; this build cannot classify why. The
        // change did not run and the token is SPENT, so the copy may not offer a
        // retry — it says the card has to be prepared again (t_bf4feea2). A cancel
        // is the exception only in words: a cancel that the server refused leaves
        // the card unsettled, and its own sentence already says so.
        setState(msgIndex, p.token, "refused");
        setErr(action === "confirm" ? t("chat.confirmRefused") : t("chat.cancelFail"));
        return;
      }
      if (outcome === "stale") {
        // Spent or expired — the card cannot be answered any more. The user needs
        // the assistant to prepare it again, which is what the copy says.
        setState(msgIndex, p.token, "stale");
        setErr(action === "confirm" ? t("chat.proposalStale") : t("chat.cancelStale"));
        return;
      }
      if (outcome === "unexecutable") {
        // A card whose arguments the tool would never accept (t_623676d1). The
        // server refuses it BEFORE the claim, so the token is UNSPENT — the card
        // must not wear "expired", which is false of it, and must not tell the
        // user to try again on an action that can never work. Its own state and
        // its own sentence say what is actually true: nothing was attempted, and
        // a freshly prepared card is the fix.
        //
        // Cancel is the exception: cancelling such a card is a real, successful
        // action (the token IS spent, the proposal IS settled), so it is reported
        // as a done/cancelled card rather than as this refusal.
        if (action === "cancel") {
          setState(msgIndex, p.token, "cancelled");
          return;
        }
        setState(msgIndex, p.token, "unexecutable");
        setErr(t("chat.proposalUnexecutable"));
        return;
      }
      if (outcome === "failed") {
        // A genuine failure of the change — including a 400 that names a refusal
        // which is not the scope. The card says so, and here that is accurate: for
        // the post-claim tool refusal the token really is spent and the change
        // really did not happen. The sentence is the PAGE's own, never the server's
        // prose (issue #6177): the wire `error` is Malay and the API does not know
        // this visitor's language.
        setState(msgIndex, p.token, "failed");
        setErr(action === "confirm" ? t("chat.confirmFail") : t("chat.cancelFail"));
        return;
      }
      setState(msgIndex, p.token, action === "confirm" ? "done" : "cancelled");
    } catch {
      setState(msgIndex, p.token, "failed");
      setErr(action === "confirm" ? t("chat.confirmFail") : t("chat.cancelFail"));
    }
  }

  function setState(msgIndex: number, token: string, state: ProposalState) {
    setMsgs((prev) =>
      prev.map((m, i) =>
        i === msgIndex ? { ...m, states: { ...(m.states ?? {}), [token]: state } } : m,
      ),
    );
  }

  // "New conversation" — a NEW stored conversation, with the previous one left
  // where it is. It used to clear the browser's copy; now it creates a row on the
  // server so the user can come back to the old one from the sidebar.
  //
  // The row is created immediately (see createSession). Starting a conversation is
  // an action with a visible effect — a new entry in the list — and a control
  // whose effect appears only after the next message reads as a control that did
  // nothing.
  async function newConversation() {
    setErr("");
    setInput("");
    setMsgs([]);
    setRenaming(null);
    setActiveId(null);
    if (!historyScope) return; // not signed in yet: nothing to create under
    const created = await createSession();
    if (!created) return;
    setActiveId(created.id);
    setSessions((prev) => [created, ...prev]);
  }

  const disabled = sending || available === false;

  return (
    <Shell active="chat">
      <div className="flex w-full flex-1 flex-col gap-6 px-6 py-8 lg:flex-row">
        {/* THE HISTORY SIDEBAR. The conversation is stored on the server, so this
            is where every saved conversation lives — re-open one by clicking it,
            rename it in place, or delete it. It is `aria-label`ed and each row is
            a real button rather than a div so the whole list is reachable from a
            keyboard. On a narrow screen it stacks above the transcript rather
            than disappearing: a control that vanishes on a phone is a conversation
            the user cannot get back there. */}
        <aside aria-label={t("chat.history")} className="w-full shrink-0 lg:w-64">
          <h2 className="text-jf-2xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
            {t("chat.history")}
          </h2>
          {sessions.length === 0 ? (
            <p className="mt-2 text-xs text-[var(--jf-muted)]">{t("chat.historyEmpty")}</p>
          ) : (
            <ul className="mt-2 space-y-1">
              {sessions.map((s) => {
                const label = sessionLabel(s, t("chat.newConversation"));
                const active = s.id === activeId;
                return (
                  <li key={s.id}>
                    {renaming === s.id ? (
                      // RENAME, IN PLACE. A form so Enter submits and the browser's
                      // own "a text field in a form" behaviour applies; Escape
                      // abandons the edit and leaves the stored label alone.
                      <form
                        onSubmit={(e) => {
                          e.preventDefault();
                          void commitRename(s.id);
                        }}
                        className="flex items-center gap-1"
                      >
                        <input
                          autoFocus
                          value={renameValue}
                          onChange={(e) => setRenameValue(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Escape") setRenaming(null);
                          }}
                          aria-label={t("chat.rename")}
                          maxLength={80}
                          className="jf-input min-w-0 flex-1 px-2 py-1 text-xs"
                        />
                        <button
                          type="submit"
                          className="rounded px-2 py-1 text-xs font-medium text-[var(--jf-primary)] hover:underline"
                        >
                          {t("chat.renameSave")}
                        </button>
                      </form>
                    ) : (
                      <div
                        className={`flex items-center gap-1 rounded-md border-l-2 ${
                          active
                            ? "border-[var(--jf-primary)] bg-slate-100"
                            : "border-transparent hover:bg-slate-50"
                        }`}
                      >
                        <button
                          type="button"
                          onClick={() => void selectSession(s.id)}
                          aria-current={active ? "true" : undefined}
                          title={label}
                          className="min-w-0 flex-1 truncate px-2 py-1.5 text-left text-xs text-[var(--jf-text)]"
                        >
                          {label}
                        </button>
                        <button
                          type="button"
                          onClick={() => startRename(s)}
                          aria-label={`${t("chat.rename")}: ${label}`}
                          title={t("chat.rename")}
                          className="inline-flex min-h-8 min-w-8 shrink-0 items-center justify-center rounded text-jf-2xs text-[var(--jf-muted-on-tint)] hover:text-[var(--jf-primary)]"
                        >
                          ✎
                        </button>
                        <button
                          type="button"
                          onClick={() => void removeSession(s.id)}
                          aria-label={`${t("chat.delete")}: ${label}`}
                          title={t("chat.delete")}
                          className="inline-flex min-h-8 min-w-8 shrink-0 items-center justify-center rounded text-jf-2xs text-[var(--jf-muted-on-tint)] hover:text-red-600"
                        >
                          ×
                        </button>
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </aside>

        <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
              {t("chat.title")}
              <span className="rounded-full bg-slate-200 px-2 py-0.5 text-jf-3xs font-semibold uppercase tracking-wide text-slate-600">
                {t("chat.readOnlyBadge")}
              </span>
            </h1>
            <p className="mt-1 max-w-2xl text-sm text-[var(--jf-muted)]">{t("chat.subtitle")}</p>
          </div>
          {/* Starts a SECOND conversation; the one on screen stays in the sidebar.
              It sits beside the "?" doc link, in the header rather than over the
              transcript, so it cannot be hit by accident while reading. Disabled
              while a message is in flight (the reply would land in the new
              conversation) and while the current conversation is an EMPTY new one
              (pressing it again would only add a second empty row). */}
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={newConversation}
              disabled={sending || (!!activeId && msgs.length === 0)}
              title={t("chat.newConversationHint")}
              className="jf-btn-ghost"
            >
              {t("chat.newConversation")}
            </button>
            {/* Inline "?" doc link (DOCUMENTATION PARITY). */}
            <a
              href="/docs/chat"
              target="_blank"
              rel="noopener noreferrer"
              title={t("chat.docs")}
              className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-200 text-jf-2xs font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
            >
              ?
            </a>
          </div>
        </div>

        <div className="mt-6 flex min-h-0 flex-1 flex-col rounded-lg border border-[var(--jf-border)] bg-white">
          {/* THE LOG IS NOT A LIVE REGION. It was `role="log"` +
              `aria-live="polite"`, which tells a screen reader to announce the
              WHOLE transcript's new content — so every message re-read the
              conversation, and once the answer streams it would have read it
              fragment by fragment. `role="log"` alone implies aria-live="polite"
              (WAI-ARIA 1.2 §5.2.9), so the role goes too: the scroll region is a
              plain group, and the LAST assistant message is the only live region
              (see its own comment below).

              `ref`/`onScroll` are how the page learns whether the reader is at the
              bottom — the decision that replaced an unconditional scroll. */}
          <div
            ref={logRef}
            onScroll={decidePinned}
            aria-label={t("chat.title")}
            role="group"
            // `max-h-[60vh]` is what makes this a REAL scroll region. Without a
            // ceiling a `flex-1` box inside an auto-height column grows with its
            // content instead of scrolling (measured: scrollHeight === clientHeight
            // on a 2,000-character answer), so the reader's transcript was the
            // whole page and this element's own onScroll could never fire — which
            // is why "do not follow a reader who scrolled up" needs the box to be
            // able to scroll in the first place.
            className="max-h-[60vh] min-h-[18rem] flex-1 space-y-3 overflow-y-auto p-4"
          >
            {/* The unavailable STATE (a deployment that cannot answer) — and
                only when the alert is not already carrying the reason for a
                FAILURE. A failure's sentence is rendered once, by the page's
                single role="alert": an unknown/absent machine code reaches that
                slot as this same chat.unavailable string, and a named one
                reaches it translated, so this line would otherwise print the
                identical sentence a second time one region over. */}
            {available === false && !err && (
              <p className="text-sm text-[var(--jf-muted)]">{t("chat.unavailable")}</p>
            )}
            {available !== false && msgs.length === 0 && (
              <div>
                <p className="text-sm text-[var(--jf-muted)]">{t("chat.empty")}</p>
                {/* THE EMPTY STATE IS CLICKABLE. It used to be that one sentence
                    — a description of what to type, with nothing to press. Each
                    chip SENDS its own (translated) question through the same
                    send path as the composer, so a first-time user can see the
                    assistant work without composing anything, and the chip can
                    never promise a question the server refuses
                    (lib/chatStarters.ts pins them against the read allowlist). */}
                <div className="mt-3 flex flex-wrap gap-2">
                  {CHAT_STARTERS.map((key) => {
                    // Resolved ONCE per chip: the button's label and the text it
                    // sends must be the same sentence, and one lookup keeps the
                    // i18n coverage scan's unresolved count at one per chip list.
                    const label = t(key);
                    return (
                      <button
                        key={key}
                        type="button"
                        onClick={() => void sendText(label)}
                        disabled={disabled}
                        className="rounded-full border border-[var(--jf-border)] px-3 py-1 text-xs text-[var(--jf-text)] hover:border-[var(--jf-primary)] hover:text-[var(--jf-primary)] disabled:opacity-50"
                      >
                        {label}
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
            {msgs.map((m, i) => {
              // The LAST assistant message is the live region: it is the only
              // part of the transcript that changes, so announcing it is the
              // whole point and announcing anything older is repetition.
              const isLast = i === msgs.length - 1;
              const live = isLast && m.role === "assistant";
              return (
                <div
                  key={i}
                  // aria-busy tells assistive tech the text is still changing and
                  // to wait for the end of the answer (WAI-ARIA 1.2 §6.6.1), which
                  // is what stops a streamed answer being read out fragment by
                  // fragment.
                  aria-live={live ? "polite" : undefined}
                  aria-busy={live && m.streaming ? true : undefined}
                  className={m.role === "user" ? "flex justify-end" : "flex justify-start"}
                >
                  <div
                    className={`max-w-[46rem] min-w-0 rounded-lg px-3 py-2 text-sm ${
                      m.role === "user"
                        ? "bg-[var(--jf-primary)] text-white"
                        : "bg-slate-50 text-[var(--jf-text)]"
                    }`}
                  >
                    {/* The answer is rendered as ELEMENTS, not raw text: the model
                        writes Markdown, and showing its markers was the reported
                        defect. One shared renderer with /feedback. */}
                    <Markdown text={m.content} variant={m.role === "user" ? "user" : "assistant"} />
                    {/* The cursor: the answer is still arriving. It is a glyph,
                        not a spinner, because the text after it is the thing the
                        user is reading. */}
                    {m.streaming && (
                      <span aria-hidden="true" className="ml-0.5 inline-block animate-pulse font-semibold">
                        ▍
                      </span>
                    )}
                    {/* WHAT IT READ, AND A WAY TO KEEP THE ANSWER — under THIS
                        answer, not collected at the bottom of the log: a trail
                        rendered after the whole transcript would read as belonging
                        to the newest message, and to every message at once.

                        The trail is the half the card was missing: a step used to
                        be rendered ONLY when it was refused, so an ordinary "it
                        read your forms and answered" left no trace of the read.
                        These are the steps that RAN (status "called"); refusals
                        keep their own amber block below, which says why. */}
                    {m.role === "assistant" && (m.steps?.length || (m.content && !m.streaming)) && (
                      <div className="mt-1 flex flex-wrap items-center gap-2 text-jf-2xs">
                        {m.steps?.map((s) => (
                          <span key={s.tool} className="rounded-full bg-slate-100 px-2 py-0.5">
                            {stepLabel(s.tool, t, true)}
                          </span>
                        ))}
                        {m.content && !m.streaming && (
                          <button
                            type="button"
                            onClick={() => void copyMessage(i, m.content)}
                            // The accessible name is the ACTION, and it is the
                            // same word the visible label shows — "Copied" after
                            // the click, so a sighted and a screen-reader user get
                            // the same feedback.
                            aria-label={copied === i ? t("chat.copied") : t("chat.copy")}
                            className="underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary)]"
                          >
                            {copied === i ? t("chat.copied") : t("chat.copy")}
                          </button>
                        )}
                      </div>
                    )}
                    {/* A STOPPED answer says so. Without the marker a partial
                        answer is indistinguishable from a finished one that
                        happens to end mid-sentence, which is how a user loses
                        track of what the assistant actually said. */}
                    {m.stopped && (
                      <p className="mt-1 text-jf-2xs font-medium text-slate-700">{t("chat.stopped")}</p>
                    )}
                  </div>
                </div>
              );
            })}

            {/* A filing is announced in the transcript, with a way to go read it. */}
            {msgs.map((m, i) =>
              m.filed ? (
                <div
                  key={`f${i}`}
                  className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-800"
                >
                  <span className="font-medium">{t("chat.filed")}</span>
                  {m.filed.subject && (
                    <span className="block truncate opacity-80">{m.filed.subject}</span>
                  )}
                  <a
                    href="/feedback"
                    className="mt-1 inline-block underline decoration-dotted underline-offset-2"
                  >
                    {t("chat.viewThread")}
                  </a>
                </div>
              ) : null,
            )}

            {/* A refusal is shown: the user should learn the assistant TRIED —
                and WHY, in THIS visitor's language. The why is a machine key the
                server sent (`chat.refused.*`), resolved through the dictionary by
                lib/chatRefusal.ts: the API does not receive the caller's language
                on a JSON call, so its own sentence reached every locale in Malay
                (t_3839995c; issue #6177).

                The HEADING is a key decision too (t_7f1694cb): "That action is not
                allowed here." is right for a forbidden tool and wrong for a
                proposal whose arguments were refused, so refusalHeading() picks it
                from the steps in this block rather than from a constant here. */}
            {msgs.map((m, i) =>
              m.refused ? (
                <div key={`r${i}`} className="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-800">
                  <span className="font-medium">{refusalHeading(m.refused, t)}</span>
                  {m.refused.map((s) => {
                    const why = refusalText(s, t);
                    return (
                      <span key={s.tool} className="mt-1 block opacity-80">
                        {toolLabel(s.tool, t)}
                        {why ? ` — ${why}` : ""}
                      </span>
                    );
                  })}
                </div>
              ) : null,
            )}

            {/* A proposal is a DECISION, so it is rendered as a card with the
                specific arguments, not as assistant prose. The badge says plainly
                that nothing has happened yet — the model cannot write, and a user
                who believed the change was already made would not read the card
                at all. */}
            {msgs.map((m, i) =>
              m.proposals
                ? m.proposals.map((p) => {
                    const state = m.states?.[p.token] ?? "pending";
                    const open = state === "pending";
                    return (
                      <div
                        key={`p${i}-${p.token}`}
                        className="rounded-lg border border-sky-200 bg-sky-50 px-3 py-2 text-xs text-sky-900"
                      >
                        <div className="flex flex-wrap items-center gap-2">
                          {/* The badge must not keep saying "needs your confirmation"
                              once the user has settled it — a done card headed
                              "NEEDS CONFIRMATION" reads as an unfinished task and
                              invites a second click the server would refuse.
                              The OPEN pill is sky-700, not sky-600: white on
                              #0084d1 measures 4.02:1 at this 10px size (axe
                              color-contrast, serious) and on #0369a1 it measures
                              5.93:1, while still reading as the sky family the
                              card is built from — the tint, the border and the
                              status lines all stay sky. The FAILED pill is
                              red-600 for the same reason: white on #ef4444 is
                              3.76:1 and on #dc2626 it is 4.83:1. (t_f5f269a8;
                              the "done"/cancelled pill is bg-slate-500, which
                              already clears the floor at 4.76:1.) */}
                          <span
                            className={`rounded-full px-2 py-0.5 text-jf-3xs font-semibold uppercase tracking-wide text-white ${
                              open ? "bg-sky-700" : state === "failed" ? "bg-red-600" : "bg-slate-500"
                            }`}
                          >
                            {open
                              ? t("chat.proposalBadge")
                              : state === "done"
                                ? t("chat.proposalBadgeDone")
                                : state === "cancelled"
                                  ? t("chat.proposalBadgeCancelled")
                                  : state === "stale"
                                    ? t("chat.proposalBadgeStale")
                                    : state === "unexecutable"
                                      ? t("chat.proposalBadgeUnexecutable")
                                      : state === "scopeRequired"
                                        ? t("chat.proposalBadgeScopeRequired")
                                        : /* roleDenied falls here with "failed", and that is deliberate: the
                                             write really was attempted and really did not go through, so the
                                             failure badge is TRUE of it. The card carries no sentence of its own
                                             — the alert does, once (t_1900adef). */
                                          t("chat.proposalBadgeFailed")}
                          </span>
                          {/* The title and the summary come from the dictionary,
                              through the KEYS the server sent: the server does not
                              know this visitor's language, so a sentence built
                              there appeared in Malay on every dashboard
                              (t_30ffa28a). proposalTitle/proposalSummary are the
                              only way the card's own words become text. */}
                          <span className="font-semibold">{proposalTitle(p, t)}</span>
                        </div>
                        {p.summary_key && (
                          <p className="mt-1 opacity-80">{proposalSummary(p, t)}</p>
                        )}
                        <dl className="mt-2 space-y-0.5">
                          {p.fields?.map((f) => (
                            <div key={f.label_key || f.label_raw} className="flex gap-2">
                              {/* opacity-80, not 70: the label keeps its lighter-
                                  than-the-value hierarchy, but 80% of the card's
                                  text token composites to 5.32:1 on the sky-50
                                  tint where 70% composites to 4.11:1 — under the
                                  4.5:1 floor at this 12px size (t_f5f269a8). The
                                  card's own summary line above already uses 80. */}
                              <dt className="min-w-[7rem] shrink-0 opacity-80">{proposalLabel(f, t)}</dt>
                              <dd className="break-words font-medium">{proposalValue(f, t)}</dd>
                            </div>
                          ))}
                        </dl>
                        {/* The assistant's OWN wording, in the language of the
                            conversation — the one string on the card the server
                            does not author, and therefore the one that is not a
                            dictionary key (see lib/chatProposal.ts). */}
                        {p.note && (
                          <p className="mt-2 border-l-2 border-sky-300 pl-2 italic opacity-80">
                            {p.note}
                          </p>
                        )}
                        {/* The tool name, in HUMAN WORDS (card t_ec87d6c7).
                            It used to print the raw server identifier
                            (`upsert_form`), which is not a word in any language —
                            a merchant cannot look it up and it cannot be
                            translated. toolLabel resolves `chat.tool.<name>` in
                            the dashboard's language and humanises an unmapped
                            name, so no snake_case can reach the screen. It stays
                            the SMALLEST text on the card (10px) because it is a
                            footnote, not the decision. */}
                        <p className="mt-2 font-mono text-jf-3xs opacity-80">{toolLabel(p.tool, t)}</p>

                        {open && (
                          <div className="mt-2 flex flex-wrap items-center gap-2">
                            <button
                              type="button"
                              onClick={() => void decide(i, p, "confirm")}
                              className="jf-btn-primary disabled:opacity-50"
                            >
                              {t("chat.confirm")}
                            </button>
                            <button
                              type="button"
                              onClick={() => void decide(i, p, "cancel")}
                              className="jf-btn-ghost"
                            >
                              {t("chat.cancel")}
                            </button>
                            {/* opacity-80, not 70: the hint keeps its lighter-
                                than-the-buttons hierarchy, but 70% composites to
                                4.11:1 on the sky-50 tint, under the 4.5:1 floor at
                                this 12px size (t_f5f269a8). */}
                            <span className="opacity-80">{t("chat.confirmHint")}</span>
                          </div>
                        )}
                        {state === "confirming" && <p className="mt-2 font-medium">{t("chat.confirming")}</p>}
                        {state === "cancelling" && <p className="mt-2 font-medium">{t("chat.cancelling")}</p>}
                        {state === "done" && (
                          <p className="mt-2 font-medium text-emerald-700">{t("chat.confirmed")}</p>
                        )}
                        {state === "cancelled" && (
                          <p className="mt-2 font-medium opacity-80">{t("chat.cancelled")}</p>
                        )}
                        {state === "stale" && (
                          <p className="mt-2 font-medium text-amber-700">{t("chat.proposalStale")}</p>
                        )}
                        {/* NO footer for "unexecutable" either, and for the same
                            reason as "scopeRequired": the sentence is carried once,
                            by the page's single role="alert" (t_623676d1). The card's
                            job in this state is to stop claiming a failure — the
                            token is unspent, nothing was attempted — so it wears its
                            own badge and offers no "try again" footer for an action
                            that can never succeed as shown. */}
                        {/* NO footer for "scopeRequired". The scope refusal is
                            carried by the page's single role="alert" (setErr above)
                            — rendering it here as well would put the same sentence
                            on screen twice, which is the duplication this card's
                            own history calls out ("duplicated, but not
                            contradictory"). The card's job in this state is to stop
                            claiming a failure: the badge says what it needs, the
                            buttons are gone (nothing was attempted and the refusal
                            is not retryable unchanged), and NOTHING here tells the
                            user to try the same action again. */}
                        {/* The footer's ink is the family's DARKER step for a
                            measured reason: this sentence is painted on the
                            card's own sky-50 tint, where the 600 step measures
                            4.474:1 — under AA — while the white transcript
                            wrapper around the card would read 4.770:1 and hide
                            it. The 700 step is 6.023:1 on this tint. The
                            class literals are deliberately NOT spelled out
                            here: a class name written in a shipped comment is
                            still compiled into the bundle by Tailwind's
                            extractor (card t_850589b5). See
                            web/src/lib/dangerText.test.ts and card t_624b7275. */}
                        {state === "failed" && (
                          <p className="mt-2 font-medium text-red-700">{t("chat.confirmFail")}</p>
                        )}
                      </div>
                    );
                  })
                : null,
            )}

            {/* THE STATUS IS ITS OWN LINE, IN ITS OWN REGION (card t_148d7100).
                It used to exist only as the Send button's label, which meant the
                one control on the page was also the state display and the same
                sentence appeared a second time in the transcript. A `role="status"`
                region is announced politely when it changes, which is how the step
                line reaches a screen-reader user as progress rather than as a
                silent wait. */}
            {activity && (
              <p role="status" className="text-xs text-slate-700">
                {activity.kind === "thinking"
                  ? t("chat.thinking")
                  : activity.kind === "stopped"
                    ? t("chat.stopped")
                    : stepLabel(activity.tool, t)}
              </p>
            )}
            <div ref={endRef} />
          </div>

          {/* JUMP TO LATEST. The transcript no longer follows new content when the
              reader has scrolled up (lib/chatScroll.ts), so staying put must not
              be a dead end: this is the way back, and it appears only then. */}
          {!pinned && (
            <div className="flex justify-center pt-2">
              <button type="button" onClick={jumpToLatest} className="jf-btn-ghost">
                {t("chat.jumpLatest")}
              </button>
            </div>
          )}
          {err && (
            <p role="alert" className="px-4 text-xs text-red-700">
              {err}
            </p>
          )}

          <form onSubmit={send} className="flex items-end gap-2 border-t border-[var(--jf-border)] p-3">
            <label htmlFor="chat-input" className="sr-only">
              {t("chat.placeholder")}
            </label>
            <textarea
              id="chat-input"
              ref={inputRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                // Enter sends; Shift+Enter is a newline. A chat box that cannot
                // take a newline cannot take a pasted question either.
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  void sendText(input);
                }
              }}
              // ONE row, then the height follows the content up to
              // CHAT_COMPOSER_MAX_ROWS lines (the effect above sets it from
              // scrollHeight). `rows={2}` was a fixed window; `resize-none` stays
              // because a manual drag would be overwritten by the next keystroke,
              // and a control that fights the user is worse than one that cannot
              // be dragged.
              rows={1}
              disabled={disabled}
              maxLength={limits?.max_message_chars || 2000}
              placeholder={t("chat.placeholder")}
              className="jf-input flex-1 resize-none disabled:opacity-50"
            />
            {/* THE BUTTON IS ALWAYS "Send". It used to become "Looking that up…"
                while sending, so the control's NAME changed under the user and the
                state was shown in the one place a form's submit control should not
                carry it. The state is the status line above; Stop is its own
                control. */}
            <button type="submit" disabled={disabled || !input.trim()} className="jf-btn-primary disabled:opacity-50">
              {t("chat.send")}
            </button>
            {sending && (
              <button type="button" onClick={stop} className="jf-btn-ghost">
                {t("chat.stop")}
              </button>
            )}
          </form>
        </div>

        {limits && (
          <p className="mt-2 text-jf-2xs text-[var(--jf-muted)]">
            {t("chat.limitNote", { n: limits.messages_per_hour })}
          </p>
        )}
        </div>
      </div>
    </Shell>
  );
}
