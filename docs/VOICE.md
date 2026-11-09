# Voice

Write plainly. One idea per sentence. If it needs a second read, rewrite it.

## Rules

1. One idea per sentence. 20 words is enough; 30 is the limit.
2. One dash per paragraph. Prefer a comma, a colon, or a full stop.
3. One word per idea — see the table.
4. The merchant is "you". The payer is "the buyer". A recurring payer is "a member".
5. Labels match the screen: **Products → Add product**.
6. Money has two decimals: RM25.00.
7. An error says what to do next. "Enter a price like 25.00", not "Price is not valid".
8. British spelling: colour, behaviour, customise.
9. Write nine and below in words. Use figures from 10.
10. Show, don't claim. A screenshot beats the word "beautiful".

## Words

| Use | Not |
| --- | --- |
| sale | order |
| buyer | customer |
| member | customer on a recurring plan |
| submission | response |

## Before you ship

No sentence over 30 words · one dash at most · all five locales
(`en ms zh ta ar`) · labels match the screen · two decimals · every error says what to do next.

`web/src/lib/voiceRules.test.ts` checks rules 1, 2, 6 and 8 on every English string.
