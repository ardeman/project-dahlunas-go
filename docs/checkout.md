# Checkout setup and operation

The shop sells Coba Lagi sponsor codes for **GitHub builds only**, using project 1,
product 1. Rp5,000, Rp19,000 and Rp49,000 each unlock the same sponsor plan (up to
six players and progress reports). Prices and IDs follow Coba Lagi's README and
`assets/config/donations.json`. Store builds continue to use store billing.

## Local sandbox

1. Copy `.env.example` to `.env.sandbox` (`cp -n` preserves an existing file).
   If the local copy predates checkout, add the new settings from the template.
2. Fill in sandbox `MIDTRANS_SERVER_KEY` and `MIDTRANS_CLIENT_KEY`. Keep
   `MIDTRANS_PRODUCTION=false`. Never paste server keys into chats or logs.
3. Choose a sandbox Google Cloud project with a Firestore Native mode **default**
   database, or run a Firestore emulator on `localhost:8081` and set
   `FIRESTORE_EMULATOR_HOST=localhost:8081`. The emulator's data must be exported
   and imported between runs if you need to preserve test orders. Keep Firestore
   client Security Rules denying all reads and writes; do not use open test-mode
   rules for real orders. Service-account IAM access bypasses client rules. The app does
   not use browser Firestore credentials or Firebase Security Rules; production
   accesses Firestore through its service account and IAM.
4. Set `GOOGLE_CLOUD_PROJECT`. For a real database in local development, use a
   short-lived access token from `gcloud auth print-access-token`:

   ```sh
   export GOOGLE_OAUTH_ACCESS_TOKEN="$(gcloud auth print-access-token)"
   ```

   Leave `GOOGLE_OAUTH_ACCESS_TOKEN` unset in the env file when exporting it in
   the shell, so sourcing the file does not clear it. Refresh it when it expires.
   On Cloud Run, leave it empty: the app obtains tokens from the attached service
   account. Grant that account `roles/datastore.user` on the selected project.
5. Provision a sandbox signing key with `go run ./cmd/mint new-key -project 1 -key 1`.
   Set `DAHLUNAS_KEY_1_1` privately from the seed file. Add **only the printed public
   key** under key ID `1` in a test Coba Lagi build's `purchased_codes.public_keys`.
   Use different signing keys for sandbox and production. `SHOP_KEY_ID` selects
   the active signing key; keep old public keys when rotating. Coba Lagi's public
   key list was empty when this checkout was implemented, so it needs provisioning
   before codes can unlock the app. Do not ship sandbox public keys in releases.
6. Set up a verified sender domain in Resend. Set `RESEND_API_KEY`, `RESEND_FROM`
   (quote display names containing spaces), and `SHOP_SUPPORT_EMAIL`. Emails are
   bilingual and contain the code and order ID. Resend's test sender only works
   with permitted test recipients; a verified domain is needed for buyers.
7. Set `SHOP_BASE_URL` to your local origin (`http://localhost:8080`) or a public
   HTTPS sandbox origin. Set your actual refund terms in `SHOP_REFUND_ID` and
   `SHOP_REFUND_EN`, with quotes around values containing spaces. Sandbox drafts
   may be used for local tests; approve real terms before enabling production.
8. Set `SHOP_ENABLED=true`, then run:

   ```sh
   (set -a; source .env.sandbox && go run ./cmd/server)
   ```

For a full sandbox payment, Midtrans must reach your public HTTPS notification URL:
`https://<sandbox-host>/webhooks/midtrans`. Configure it in the Midtrans sandbox
dashboard's payment notification settings. A localhost URL is not reachable from
Midtrans. Use Snap Checkout settings to configure the notification URL as offered
by the dashboard. The per-order finish URL is provided by the server; it does not
confirm payment. Use Midtrans's sandbox QRIS simulator to finish a test purchase.

## Production

Copy the same template to `.env.production` for local production testing. Set
`MIDTRANS_PRODUCTION=true`, production Midtrans keys and a public HTTPS
`SHOP_BASE_URL`. **These payments use real money.** Production refuses an emulator.
Use a separate Google Cloud project from sandbox; collections are also separated
as `orders-sandbox` and `orders-production`.

On Cloud Run, configure keys and the signing seed through Secret Manager. Configure
other settings as environment variables. Local env files are not uploaded by the
Docker or gcloud build. Create the database and grant the service account access
before opening checkout. No orders or external services are created by startup.

Before opening sales, approve the product copy and refund policy, provision the
public signing key in Coba Lagi, and update Coba Lagi's website privacy policy to
describe Dahlunas, Midtrans and the email provider's handling of purchase data.
The app remains offline; only its website/parent purchase link leads to the shop.
That separate repository is not modified by this checkout change.

## Payment and delivery behavior

- `POST /checkout` accepts a catalog slug and delivery email, with consent and
  same-browser CSRF protection. Amounts come from the catalog, never the browser.
  Only QRIS is offered initially. Snap opens on Dahlunas's own page.
- Orders are stored before a gateway session is created. A failed gateway request
  can leave an unpaid order; it never creates a code. Retrying checkout starts a
  new order. Each order has a separate random private viewing token.
- `POST /webhooks/midtrans` validates Midtrans's SHA-512 signature, fetches current
  status from Midtrans (the signature does not cover every status field), and
  checks the exact expected amount before fulfillment. Capture requires fraud
  acceptance; settlement rejects an explicit fraud rejection/challenge.
- Conditional Firestore writes protect against concurrent webhook retries across
  instances. The signed code ID/date are fixed per order. A duplicate or stale
  notification cannot mint a second code or undo fulfillment.
- The paid page displays the persisted code even when email fails. An email lease
  and stable Resend idempotency key prevent concurrent duplicate deliveries.
  Failed delivery returns a retryable notification response. A later webhook retry
  can send again after the one-minute lease expires, within 23 hours of the first
  attempt. There is no background worker; retries depend on gateway notifications
  or a dashboard resend of the notification.
- If delivery remains ambiguous after 23 hours, automatic sends stop before
  Resend's 24-hour idempotency window expires. Review the order and provider logs
  manually, help the buyer recover their private order link, and confirm delivery
  before arranging a manual email. Do not clear delivery fields blindly.
- If a payment is missing a notification, check Midtrans and resend its HTTP
  notification from the dashboard. Browser success callbacks never grant access.
  Refunds are handled in Midtrans; already-issued offline codes cannot be revoked.
- Order pages use `Cache-Control: no-store` and `Referrer-Policy: no-referrer`.
  Keep private order URLs out of access logs and analytics. Cloud Run request logs
  can include the full URL: configure a Cloud Logging exclusion for `/orders/`
  requests before production, and apply equivalent redaction/exclusion at any
  reverse proxy. No buyer data, provider
  response bodies, signing keys, or gateway keys are written to application logs.

## Verification

The repository Checks command covers gateway payloads/authentication, forged and
pending statuses, amount mismatches, concurrent/duplicate webhooks, durable store
preconditions, private order access, checkout CSRF, and email retries. Tests use
fake HTTP providers and do not create payments or send emails.

The Firestore adapter's HTTP contract is tested with a local fake; an emulator or
real database smoke test and a live Midtrans sandbox transaction are separate setup
checks. They require your configured accounts and public notification endpoint.

References: [Midtrans Snap integration](https://docs.midtrans.com/docs/snap-snap-integration-guide),
[notification verification](https://docs.midtrans.com/reference/handle-notifications),
[Firestore REST](https://firebase.google.com/docs/firestore/use-rest-api),
[conditional writes](https://cloud.google.com/firestore/docs/reference/rest/v1/projects.databases.documents/patch),
[Resend idempotency](https://resend.com/docs/dashboard/emails/idempotency-keys).
