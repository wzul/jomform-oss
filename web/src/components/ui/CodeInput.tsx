// CodeInput (card t_8f20418d): the one-time-code field.
//
// WHY IT IS A PRIMITIVE AND NOT A CLASSNAME ON `Input`. The two places that ask
// for a 6-digit code (`/login`'s 2FA step, `/profile`'s authenticator setup) both
// give it the same treatment — centred, monospaced, wide letter-spacing, one type
// step larger than body text — and that treatment is the meaning of the control,
// not decoration. It cannot be expressed as an override on `Input`: `Input` starts
// from `CONTROL_CLASS`, whose `text-jf-read-sm` is emitted AFTER `text-jf-read-2xl`
// in the built stylesheet, so a call-site `className="text-jf-read-2xl"` loses the
// cascade silently (measured — see `control.ts`). This primitive starts from
// `CONTROL_SURFACE` and states its own step, so there is no fight to lose.
//
// The digit contract lives here too, because a caller that forgets one of them
// gets a field that accepts letters or offers no numeric keypad on a phone:
// `length` → `maxLength` + the numeric `pattern`, `inputMode="numeric"`,
// `autoComplete="one-time-code"` (so a phone offers the SMS code), and `type="text"`
// — NOT `type="number"`, which strips the leading zero and shows spinners.
//
// The caller still owns the value: it trims to `length` digits itself, because a
// primitive that rewrote its own value would fight a controlled form.

import { forwardRef } from "react";
import { CONTROL_SURFACE } from "./control";

export type CodeInputProps = Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  "type" | "inputMode" | "pattern" | "maxLength" | "autoComplete"
> & {
  /** Digits accepted. Default 6, the length of a TOTP code. */
  length?: number;
};

export const CodeInput = forwardRef<HTMLInputElement, CodeInputProps>(function CodeInput(
  { length = 6, className, ...props },
  ref,
) {
  return (
    <input
      {...props}
      ref={ref}
      type="text"
      inputMode="numeric"
      pattern="[0-9]*"
      maxLength={length}
      autoComplete="one-time-code"
      className={[
        CONTROL_SURFACE,
        "text-jf-read-2xl text-center font-mono tracking-[0.5em]",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    />
  );
});

CodeInput.displayName = "CodeInput";
