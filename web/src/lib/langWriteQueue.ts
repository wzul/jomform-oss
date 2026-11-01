// The dashboard-language WRITE queue — two rapid picks must reach the profile
// IN ORDER (card t_04b728a9).
//
// THE DEFECT
//   `setLang()` fires `POST /api/auth/profile/lang` and never ordered it, so two
//   picks made in quick succession produced two CONCURRENT POSTs. A concurrent
//   pair has no defined arrival order at the server: two HTTP/2 streams can be
//   multiplexed in either order, a second connection is scheduled
//   independently, and a slow first request is enough. If the FIRST settles
//   last, the server commits the OLDER choice after the newer one, so the
//   profile ends up holding a language the user has already replaced. Nothing
//   looks wrong in the page — the newer choice is on screen — and the next cold
//   load (or another browser, which is the whole point of a per-profile
//   setting) reads the older one back. Measured in a real browser with the first
//   POST held open: "#1 sent ms | #2 sent zh | #2 committed zh | #2 committed
//   ms"; the server ended on `ms` while the picker showed `zh`, and a reload
//   showed `ms` (web/e2e/profile-lang-write-order.spec.ts).
//
// THE RULE
//   Writes to the profile language are SERIALISED in call order: POST n+1 is not
//   issued until POST n has SETTLED (resolved or rejected). That makes "the last
//   explicit choice is the one the profile holds" a property of the transport
//   rather than a hope about scheduling.
//
//   A rejected write neither stalls the queue nor swallows the next choice: the
//   next pick is still sent, because the local mirror already shows it and the
//   profile has to agree. Persistence stays best-effort — a signed-out or
//   offline user keeps the browser value (see components/i18n.tsx).
//
// WHY MODULE-LEVEL
//   The chain must outlive a component remount: a provider that mounts twice
//   must not be able to interleave its writes with the previous mount's.
export function orderedWriter<V>(post: (v: V) => Promise<unknown>): (v: V) => Promise<void> {
  let tail: Promise<unknown> = Promise.resolve();
  return (v: V) => {
    // Wait for the previous write to SETTLE, whichever way it went, then send.
    tail = tail.then(
      () => post(v),
      () => post(v),
    );
    // The returned promise must never reject: the caller does not await it (the
    // UI already moved), and an unhandled rejection here would surface as a
    // page error. `tail` is still handled by the next call or by this branch.
    return tail.then(
      () => {},
      () => {},
    );
  };
}
