# QR table ordering — setup guide

A QR code on the table, a menu that opens in the diner's browser, and the order arrives with the
table number attached. Every screenshot below was captured from the running product; the red box
marks the control you touch at that step.

**What this does:** each dish is a *product*. A form in payment mode holds one **Product** block per
dish, and the *Restaurant menu* switch makes that form's own URL render a menu page instead of the
plain form. The QR code is just that URL with `?table=N` on the end, so the order always knows which
table it came from.

## 1. Add every dish as a product

![Products page with a dish filled in and the Add product button boxed](01-add-dish.png)

Products → fill **Product name**, **Price (RM)** and press **Add product**. Leave **Stock** blank
for ∞ (unlimited) and keep **Type** on *One-time*. Repeat once per dish — one product per line item
on the menu.

## 2. Check the catalogue

![Products list with four dishes and the Price column boxed](02-dish-catalogue.png)

The list is now the source of truth for the menu: Nasi Lemak RM12.00, Char Kway Teow RM14.00,
Teh Tarik RM3.50 and Roti Bakar RM5.00. The **PRICE (RM)** column is exactly what the diner pays,
so fix a price here rather than in the form.

## 3. Start from the F&B template

![Template gallery with the F&B Order + Payment card's Use template button boxed](03-fnb-template.png)

Forms → **New form** → **F&B Order + Payment** → **Use template**. The template arrives as a payment
form (a Section intro, one Product block, then Name / Email / Phone / Special instructions), which
is the shape a menu checkout needs.

## 4. One Product block per dish

![Builder with three dish blocks in the canvas and the Product dropdown boxed](04-product-blocks.png)

In the builder, click **Product** under *PRICING FIELDS* once per dish. With a Product block
selected, **BLOCK SETTINGS → Product** is the dropdown that binds the block to the dish, and
**Text / label** is the name the diner sees. Four dishes → four Product blocks.

## 5. Turn the form into a menu

![Form Settings → Restaurant menu with the Enable restaurant menu switch boxed and on](05-enable-menu.png)

**Form Settings → Restaurant menu → Enable restaurant menu.** The screen itself says what the switch
does: *the menu is served at this form's own URL and lists only the products this form's product
blocks reference; the workspace home page is never affected.* The same panel holds the optional
**Best-seller product**, the **Categories** list (**+ Add category**), the optional extra checkout
fields, and a **Menu preview** that counts the items (4 here). Press **Save**.

## 6. Generate the table QR

![General tab with the QR code (table ordering) section open and the generated QR boxed](06-table-qr.png)

**Form Settings → General → QR code (table ordering)**: type the table number, press **Generate QR**,
and the panel shows the code plus the URL it encodes —
`…/f/f-b-order-payment?table=7`. One code per table; print it, or download it and put it on the
table card. Nothing else has to change between tables.

## 7. What the diner scans into

![Public menu page for table 7 with four dishes, prices, add buttons and the Table 7 banner boxed](07-diner-menu.png)

The guest gets the restaurant name, a **Best Seller** filter, a search box, the category row and a
row per dish with its price and a **+** button. The **Table 7** banner at the top came from the QR
link, not from the diner, so the order that follows is already tied to the table.

## Notes

- **The form has to be published.** The menu is the published form's public page; a draft shows the
  normal form (or nothing) instead.
- **Changing a price is a Products change, not a form change.** The menu reads the live product, so
  a price fix in Products shows up on the next page load — no republish needed for the price itself.
- **A dish with no Product block never appears.** The menu is built from the blocks, so a product
  that exists in the catalogue but has no block is simply not on the menu.
