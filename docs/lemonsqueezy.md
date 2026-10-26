# Lemon Squeezy checkout

Lemon Squeezy is the optional USD gateway for Ardeman's own projects. Midtrans
continues to offer on-page QRIS payments in rupiah. The default catalog remains
Midtrans-only: no USD prices or Lemon Squeezy credentials are needed until you
configure USD products.

## Setup

1. Configure your Lemon Squeezy store for **USD**, tax-exclusive pricing, and
   one-time fixed-price products. Use quantity 1; disable discounts, bundles,
   upsells and subscriptions for these variants. Checkout creation checks variant
   availability and pricing type; fulfillment requires one item, quantity 1,
   no discount, and an exact USD subtotal matching the order. Tax is separate
   from the catalog price and displayed by the provider at checkout.
2. Select the environment with `SHOP_PRODUCTION=false` for tests and `true` for
   real payments. The original `MIDTRANS_PRODUCTION` setting remains supported;
   when both are set, they must agree. This single mode controls both gateways,
   HTTPS requirements, and the existing sandbox/production Firestore collections.
   Keep signing seeds and Google Cloud projects separate between environments.
3. Set `LEMONSQUEEZY_API_KEY`, `LEMONSQUEEZY_WEBHOOK_SECRET` and
   `LEMONSQUEEZY_STORE_ID` privately. Keys and the webhook secret belong in Secret
   Manager on Cloud Run. Store IDs and variant IDs are positive numeric strings.
   The signing secret must match the one configured for this webhook.
4. Copy the embedded catalog as described in [project configuration](projects.md).
   Add a separate product entry with a unique slug, `gateway: "lemonsqueezy"`,
   `currency: "USD"`, and your `variant_id`. Its integer `price` is **US cents**;
   `599` means US$5.99. This is an example, not a chosen selling price:

   ```json
   {
     "slug": "your-project-usd",
     "id": 1,
     "price": 599,
     "gateway": "lemonsqueezy",
     "currency": "USD",
     "variant_id": "123456",
     "name": {"id": "Kode proyek Anda", "en": "Your project code"},
     "description": {
       "id": "Jelaskan fitur, versi aplikasi, dan batasan pembelian.",
       "en": "Describe the features, app versions and purchase limitations."
     }
   }
   ```

   Use the same project/product IDs as the app entitlement being sold. Midtrans
   products may coexist in the same project; omit their gateway/currency fields
   or use `midtrans`/`IDR`. Catalog limits are Rp10,000,000 or US$1,000 per code;
   provider limits still apply. Test/live variants may have different IDs: use
   the matching catalog in each deployment. Restart after catalog changes.
5. Configure a public HTTPS webhook at
   `https://<host>/webhooks/lemonsqueezy`, subscribed to **order_created**. Browser
   redirects never grant access. Keep the credentials for a removed gateway
   configured while its pending orders still need notifications or delivery
   retries; the webhook route stays registered when its credentials remain.
6. Configure Firestore, Resend, signing keys, support email, base URL and approved
   bilingual refund terms as in [checkout setup](checkout.md). Midtrans credentials
   are required only when the catalog contains Midtrans products. Update each
   project's purchase privacy disclosure to name the providers it uses.

## Checkout and verification

Dahlunas saves the order before calling the provider. The private order page
links to a Lemon Squeezy-hosted checkout; the Midtrans Snap script is loaded only
for Midtrans orders. Custom checkout domains are not supported: returned links
must use HTTPS under `*.lemonsqueezy.com` with a `/checkout/` path. Checkout links
expire after 24 hours; start a new order if one expires. Do not share or log order
links, checkout links or payment proofs.

The checkout locks the offered variant and custom price, disables the discount
field, and passes the local order ID plus a separate random payment proof as
custom data. The viewing token is not used as that proof. Dahlunas verifies the
raw request body's HMAC-SHA256 against `X-Signature` before parsing or making API
calls. It then retrieves the current provider order and its line items, checking
store, mode, currency, quantity, subtotal and variant against the stored order.
Only a currently paid, non-refunded order can mint a code. Missing signatures,
changed data, wrong modes and mismatched amounts cannot fulfill an order.

The provider order ID is persisted with fulfillment. Duplicate notifications
retain one code and share the existing email lease/idempotency behavior. Order
wording, currency, price, gateway, variant and key ID survive catalog changes.
Legacy orders with no currency/gateway fields remain Midtrans/IDR orders.
Refunding a purchase does not revoke an offline code already issued.

Lemon Squeezy retries failed webhook deliveries three times with short backoff.
If delivery remains pending after those attempts, inspect the provider and
Dahlunas status and resend the event manually from the dashboard. The shared
23-hour email retry boundary still applies; follow the manual-review procedure
in checkout setup after that boundary. Other event types are acknowledged only
after signature verification and do not fulfill orders.

## Before opening sales

Complete a test-mode purchase through the public webhook. Check the paid page,
received email and offline unlock in the intended test app. Replay the notification
and confirm the same code remains without another email. Use separate production
keys/catalog IDs, complete the provider's store activation process, and approve
USD prices, product copy, refund terms and privacy disclosure before real sales.
No live payment, account setup or deployment is performed by repository tests.

References: [creating checkouts](https://docs.lemonsqueezy.com/api/checkouts/create-checkout),
[custom data](https://docs.lemonsqueezy.com/help/checkout/passing-custom-data),
[webhook signatures](https://docs.lemonsqueezy.com/help/webhooks/signing-requests),
[order fields](https://docs.lemonsqueezy.com/api/orders/the-order-object),
[order items](https://docs.lemonsqueezy.com/api/order-items/list-all-order-items),
[webhook delivery and retries](https://docs.lemonsqueezy.com/help/webhooks/webhook-requests).
