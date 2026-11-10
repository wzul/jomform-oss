# Affiliate programme: setup guide

Hand someone a link, and every paid sale that arrives through it pays them a commission.

**What this does:** an affiliate is a unique code in the workspace. A sale that arrives with that
code attached is attributed to the affiliate, and the commission is computed on the **paid** amount.

## 1. Create the affiliate

![Settings → Affiliates with Code SITI, Name Siti Zubaidah, Commission 10 and the Add affiliate button boxed](01-create-affiliate.png)

Settings → **Affiliates**: fill **Code** (`SITI` here), **Name**, **Commission %** and press
**Add affiliate**. A code is unique inside the workspace, so a second `SITI` is refused.

## 2. Hand over the link

![The affiliate row with its referral link boxed](02-referral-link.png)

The row carries that affiliate's link, `https://<workspace>.jomform.com/?aff=SITI`, and anything a
visitor buys after opening it is attributed to SITI at the rate on the row.

## Notes

- **Only paid sales count.** An unpaid checkout, a refused card or a form response with no product
  contributes nothing to an affiliate.
- **The sale records the code, not the money.** Each sale carries the code that brought it in, and
  cancelling a card never revokes anything on the affiliate side.
- **One code per promotion, not per person.** Two channels (a WhatsApp group and an Instagram bio)
  take two codes, so each gets its own commission line.
- **The same three operations exist as MCP tools** (`create_affiliate`, `list_affiliates`,
  `set_affiliate_commission`), and they return the same `referral_url` the tab shows.
