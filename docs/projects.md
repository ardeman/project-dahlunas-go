# Project catalog

Dahlunas sells unlock codes for **Ardeman's own projects only**. Adding a project
here does not authorize collecting payments for another business.

By default, the server embeds [Coba Lagi's catalog](../internal/shop/catalog/coba-lagi.json).
Its three support amounts still unlock project 1, product 1. Existing setups can
continue to use `SHOP_KEY_ID` (default 1) to rotate that project's key.

## Configure projects

Copy the default JSON to a tracked file, for example `config/projects.json`, then
set `SHOP_CATALOG_FILE=config/projects.json`. Unset `SHOP_KEY_ID` when using a
custom catalog: each project's `key_id` selects its signing key. Keep the file in
the deployed source tree or mount it in Cloud Run. Paths are relative to the
server's working directory. The file contains public copy and IDs, never seeds
or gateway credentials. Changes take effect on server restart.

The top level is a nonempty array of projects. Each project contains:

- `id`: unique nonzero 16-bit offline project ID, agreed with the app.
- `key_id`: nonzero 8-bit signing key ID. Provide its seed privately through
  `DAHLUNAS_KEY_<id>_<key_id>` and provision its public key in that app.
- `email`: `subject`, `thank_you`, and `instructions`, each with nonempty `id`
  (Indonesian) and `en` (English) text. Instructions appear on the paid page too.
- `products`: a nonempty array, each containing `slug`, `id`, `price`, `name`,
  and `description`, plus optional `gateway`, `currency`, and `variant_id`. Names and descriptions each contain `id` and `en` text.

Product slugs must be unique across the entire shop, at most 100 characters,
using lowercase letters, digits and single separating hyphens. Product IDs are
nonzero 16-bit app entitlement IDs; multiple support prices may use the same ID.
Prices are integer minor units: whole rupiah for Midtrans/IDR (default), US cents
for Lemon Squeezy/USD. Limits are 1–10,000,000 rupiah or 1–100,000 cents.
Midtrans offers QRIS; Lemon Squeezy uses a hosted checkout and requires a numeric
`variant_id`. Other gateway/currency pairs are rejected. The gateway's applicable
limits still apply. See [Lemon Squeezy setup](lemonsqueezy.md).
Unknown fields, missing translations, invalid IDs, duplicate projects/slugs,
invalid prices, and files larger than 1 MiB prevent startup when sales are enabled.
All configured signing keys are checked before checkout opens.

Names and descriptions appear in product choices and terms. Describe each
product's entitlement, supported builds, store exclusions and purchase conditions
explicitly. Do not assume different prices grant the same features. Email copy
is plain text, not a template: Dahlunas adds the code, order ID and support email.
Use the project's instructions to describe where buyers enter the code; children's
apps must continue verifying codes offline without contacting Dahlunas.

## Existing orders and rotation

An order stores its price, product/project IDs, key ID, descriptions and delivery
wording at creation. Editing prices, removing a project or changing the active
key does not change existing orders or webhook retries. Keep old signing seeds
available for unpaid orders and old public keys in the app for issued codes.
Keep each project's website privacy disclosure current before opening sales.

Orders created before configurable catalogs retain Coba Lagi's original delivery
wording when their stored project/product IDs are 1/1. No data migration is needed.
Sandbox and production must continue using separate signing seeds and databases.
