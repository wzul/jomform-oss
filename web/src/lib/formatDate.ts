// Issue #191: shared timezone-aware date formatting.
//
// Dashboard timestamps should display in the user's preferred timezone: a
// hard-coded IANA timezone when set, or the browser/PC default (Auto) when
// the user's timezone is empty. These pure helpers format an ISO timestamp
// with Intl.DateTimeFormat, omitting the `timeZone` option entirely when no
// timezone is given so the browser default is used.

// formatDate renders a short date (e.g. "3 Sep 2026"). `tz` is an IANA
// timezone name or "" for Auto (browser default). Returns the raw input when
// it is not a valid date.
export function formatDate(iso: string, tz?: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const opts: Intl.DateTimeFormatOptions = { day: "numeric", month: "short", year: "numeric" };
  if (tz) opts.timeZone = tz;
  return d.toLocaleDateString(undefined, opts);
}

// formatDateTime renders a full date + time (e.g. "3 Sep 2026, 14:30"). `tz`
// is an IANA timezone name or "" for Auto (browser default). Returns the raw
// input when it is not a valid date.
export function formatDateTime(iso: string, tz?: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const opts: Intl.DateTimeFormatOptions = {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
  };
  if (tz) opts.timeZone = tz;
  return d.toLocaleString(undefined, opts);
}
