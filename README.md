# Dahlunas

One checkout for all of Ardeman's own projects. A buyer picks a product in Dahlunas's own shop and pays there through a payment gateway (Midtrans first), the gateway's webhook tells Dahlunas, and Dahlunas sends back what was bought: for offline apps such as Coba Lagi, a signed unlock code the app checks without going online.

Go (standard library first) with htmx pages, running on Cloud Run behind Firebase Hosting.

## Setup

- Go 1.24 or newer.
- `gcloud` (Google Cloud SDK) to deploy; Docker isn't needed, since Cloud Run builds from source.
- Firebase CLI (`npm i -g firebase-tools`) for the Hosting rewrite. Pick the Firebase project once with `firebase use --add`.
- For local configuration, use the single `.env.example` template:

  ```sh
  cp -n .env.example .env.development
  cp -n .env.example .env.production
  ```

  Fill each local file with the matching Midtrans credentials. Keep `MIDTRANS_PRODUCTION=false` in `.env.development`; set it to `true` in `.env.production`. Use separate signing keys for sandbox codes and production codes. Both local files are ignored by Git and excluded from build uploads. On Cloud Run, configure secrets through Secret Manager.
  Keep `SHOP_PRODUCTION` equal to `MIDTRANS_PRODUCTION` in each file. Both environments use project `dah-lunas`, with separate order collections and Cloud Run services. The example includes the verified sender `receipts@dahlunas.ardeman.com`; both environments share one sending-only Resend key, secret `resend-api-key-development`. Production needs its actual public HTTPS base URL. Leave checkout disabled until setup is complete.
- Select an environment with one of the commands below. Each runs in a subshell, so switching does not leave credentials in your current shell. The server does not load environment files automatically. You can also copy the template to `.env` and load it using the same command pattern.
- GitHub Actions signs in to Google Cloud through Workload Identity Federation (pool `github`, provider `dahlunas-repo`, accepting only this repository's `main` branch) as `dahlunas-deployer`, which may deploy from source and act as the two runtime accounts; no key is stored in GitHub. The repository's `production` environment lists the owner as required reviewer.
- Production uses real money, including test purchases. Checkout is disabled by default (`SHOP_ENABLED=false`). Follow [checkout setup](docs/checkout.md) for Midtrans, Firestore, Resend, signing keys and refund terms before enabling it.

## Commands

| What | Command |
| --- | --- |
| Run locally | `go run ./cmd/server` (http://localhost:8080) |
| Run with development configuration (sandbox payments) | `(set -a; source .env.development && go run ./cmd/server)` |
| Run with production configuration | `(set -a; source .env.production && go run ./cmd/server)` |
| Test | `go test ./...` |
| Checks | `test -z "$(gofmt -l .)" && go vet ./... && go test ./...` |
| Make a signing key | `go run ./cmd/mint new-key -project 1 -key 1` (writes the seed to a `.key` file, prints the public key) |
| Mint a code by hand | `go run ./cmd/mint code -project 1 -product 1 -key 1` (reads `DAHLUNAS_KEY_1_1`) |
| Deploy the services | Automatic: a push to `main` that passes the checks deploys `dahlunas-development`, then `dahlunas` after the owner approves the `production` environment in GitHub Actions; a newer push cancels an approval still waiting |
| Deploy the service by hand | `gcloud run deploy dahlunas --source . --region asia-southeast2` |
| Deploy Hosting | `firebase deploy --only hosting` |

## Layout

- `cmd/server/` starts the HTTP server (`PORT`, set by Cloud Run).
- `cmd/mint/` makes signing keys and unlock codes by hand.
- `internal/unlock/` makes and checks unlock codes ([format](docs/code-format.md)) and reads signing keys: `DAHLUNAS_KEY_<project>_<key id>` holds the key's 32-byte Ed25519 seed in hex.
- `internal/web/` holds routes, page templates (`templates/`) and static files (`static/`, htmx vendored at 2.0.4).
- `internal/app/` validates environment configuration and assembles the service.
- `internal/shop/` owns the validated project catalog, orders, fulfillment and delivery leases; its embedded default is `catalog/coba-lagi.json` ([project configuration](docs/projects.md)).
- `internal/payment/` defines the gateway interface; `internal/midtrans/` implements Snap and verified notifications, and `internal/lemonsqueezy/` implements optional USD hosted checkout and signed notifications ([setup](docs/lemonsqueezy.md)).
- `internal/orderstore/` persists orders in Firestore with conditional writes; `internal/delivery/` sends bilingual code emails through Resend.
- `public/` is Firebase Hosting's folder; every request is rewritten to the Cloud Run service `dahlunas` in `asia-southeast2` (Jakarta).
- `.github/workflows/checks.yml` runs the Checks command on pushes and pull requests, then deploys both Cloud Run services from `main` unless a push changes only docs, Markdown, `public/`, `firebase.json` or CI files.
- `docs/code-format.md` specifies the shared offline unlock-code format for Dahlunas and the apps.

The server serves a bilingual shop at `/`, policies at `/terms`, `/refunds` and `/privacy`, private order pages at `/orders/{id}/{token}`, Midtrans notifications at `/webhooks/midtrans`, optional Lemon Squeezy notifications at `/webhooks/lemonsqueezy`, the scheduled delivery sweep at `/tasks/deliveries` (only with `DAHLUNAS_TASK_TOKEN`), and a health check at `/health`. With checkout disabled, the homepage says sales are not open. Pages show one language at a time: `?lang=id` or `?lang=en` picks it (links keep the choice), otherwise the browser's `Accept-Language`, otherwise Indonesian.

## Roadmap

1. **Code format:** [version 1](docs/code-format.md) carries project, product, id and date, signed with Ed25519, in 148 characters for copy/paste or QR scanning. `internal/unlock` makes and checks it, tested against the documented fixture.
2. **Minting:** `cmd/mint` makes a project's signing key (the seed goes to a file for Secret Manager, never to the screen) and codes by hand, for orders before the webhook exists.
3. **Shop and Midtrans:** implemented for Coba Lagi with three rupiah support amounts, Snap's on-page popup, QRIS, policy pages, Firestore orders, signature/status verification, idempotent unlock codes and Resend email. [Setup and operation](docs/checkout.md). Remaining before sales: configure services and approved refund terms, provision Coba Lagi's public signing key, and update its purchase privacy disclosure. An end-to-end sandbox payment (QRIS → notification → code → email) passed on 2026-10-26 on the `dahlunas-development` Cloud Run service. The individual account ("Registrasi perorangan") was submitted on 2026-10-26; use sandbox until approved. Other payment methods and their minimum-price thresholds remain future work.
4. **Projects:** implemented through a validated JSON catalog with per-project products, prices, signing key IDs, and Indonesian/English email wording. Orders retain their original delivery copy. [Configuration and rotation](docs/projects.md). More of Ardeman's own side projects can join through the catalog; today every product delivers a signed unlock code, so a project that needs another kind of delivery (such as turning on a web account or calling its own server) adds that delivery when it arrives. Other people's businesses stay out without a Bank Indonesia payment licence (see `AGENTS.md`).
5. **Worldwide:** Lemon Squeezy implemented as an optional USD gateway behind the shared payment interface, with hosted checkout, verified order notifications and idempotent code delivery. [Setup and test-mode verification](docs/lemonsqueezy.md) remain before sales; no USD products are enabled by default. Xendit can join once there is a company (it accepts only legal entities).
6. **Ready for a rush** (traffic stays small, since apps check codes offline; these are what break first):
   - Resend domain `dahlunas.ardeman.com` is verified (sandbox delivery passed on 2026-10-26); monitor delivery. Codes are also shown on the paid page.
   - Checkout has a per-instance rate limit and server-verified Turnstile protection; configure Turnstile before production (see checkout setup). Fleet-wide edge limits remain future work.
   - Delivery retries: a scheduled sweep resends stuck emails and logs orders needing review ([delivery monitoring](docs/checkout.md#delivery-monitoring)); its Cloud Scheduler job and log alerts run on `dahlunas-development` since 2026-10-26; set them up for production before sales. One code per payment is enforced by conditional writes and a fixed code ID. The same sweep asks Midtrans about unpaid orders whose notification never arrived and fulfils those found paid; Lemon Squeezy orders rely on its webhook retries.
   - Card fraud and chargebacks: keep amounts small; codes checked offline can't be taken back.
   - Midtrans may ask for more documents as volume grows; taxes (NPWP; VAT abroad unless a merchant of record handles it).

## License

Dahlunas is licensed under the [MIT License](LICENSE). The vendored [htmx 2.0.4](https://github.com/bigskysoftware/htmx/tree/v2.0.4) script uses the [Zero-Clause BSD license](internal/web/static/htmx.LICENSE).
