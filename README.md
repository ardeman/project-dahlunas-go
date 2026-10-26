# Dahlunas

One checkout for all of Ardeman's own projects. A buyer picks a product in Dahlunas's own shop and pays there through a payment gateway (Midtrans first), the gateway's webhook tells Dahlunas, and Dahlunas sends back what was bought: for offline apps such as Coba Lagi, a signed unlock code the app checks without going online.

Go (standard library first) with htmx pages, running on Cloud Run behind Firebase Hosting.

## Setup

- Go 1.24 or newer.
- `gcloud` (Google Cloud SDK) to deploy; Docker isn't needed, since Cloud Run builds from source.
- Firebase CLI (`npm i -g firebase-tools`) for the Hosting rewrite. Pick the Firebase project once with `firebase use --add`.
- For local configuration, copy `.env.example` to `.env`, then load it with `set -a; source .env; set +a` before running the server. The server reads environment variables; it does not load `.env` automatically. Defaults work without this step.

## Commands

| What | Command |
| --- | --- |
| Run locally | `go run ./cmd/server` (http://localhost:8080) |
| Test | `go test ./...` |
| Checks | `test -z "$(gofmt -l .)" && go vet ./... && go test ./...` |
| Make a signing key | `go run ./cmd/mint new-key -project 1 -key 1` (writes the seed to a `.key` file, prints the public key) |
| Mint a code by hand | `go run ./cmd/mint code -project 1 -product 1 -key 1` (reads `DAHLUNAS_KEY_1_1`) |
| Deploy the service | `gcloud run deploy dahlunas --source . --region asia-southeast2` |
| Deploy Hosting | `firebase deploy --only hosting` |

## Layout

- `cmd/server/` starts the HTTP server (`PORT`, set by Cloud Run).
- `cmd/mint/` makes signing keys and unlock codes by hand.
- `internal/unlock/` makes and checks unlock codes ([format](docs/code-format.md)) and reads signing keys: `DAHLUNAS_KEY_<project>_<key id>` holds the key's 32-byte Ed25519 seed in hex.
- `internal/web/` holds routes, page templates (`templates/`) and static files (`static/`, htmx vendored at 2.0.4).
- `public/` is Firebase Hosting's folder; every request is rewritten to the Cloud Run service `dahlunas` in `asia-southeast2` (Jakarta).
- `.github/workflows/checks.yml` runs the Checks command on pushes and pull requests.
- `docs/code-format.md` specifies the shared offline unlock-code format for Dahlunas and the apps.

The starter serves a bilingual homepage at `/`, a health check at `/healthz`, and embedded htmx at `/static/htmx.min.js`. Payments are planned below.

## Roadmap

1. **Code format:** [version 1](docs/code-format.md) carries project, product, id and date, signed with Ed25519, in 148 characters for copy/paste or QR scanning. `internal/unlock` makes and checks it, tested against the documented fixture.
2. **Minting:** `cmd/mint` makes a project's signing key (the seed goes to a file for Secret Manager, never to the screen) and codes by hand, for orders before the webhook exists.
3. **Shop and Midtrans:** the shop on Dahlunas's own domain, which Midtrans reviews before live payments: each product described with its price in rupiah, terms, a refund policy and contact details, and checkout on the same domain (Snap's pop-up; Midtrans rejects a checkout that sends the buyer to another site). The webhook (Midtrans' HTTP notification) is checked by its signature, then the code is made, shown on the "paid" page and emailed. A payment must never send two codes, even when the webhook comes twice. QRIS is the main method (0.7%; payable from any e-wallet or bank app); GoPay costs 2%, cards 2.9% + Rp2.000 and virtual accounts Rp4.000 flat, so those show only above a minimum price. The individual account ("Registrasi perorangan") was submitted on 2026-10-26; build against the sandbox until it is approved.
4. **Projects:** per-project config (products, prices, key, email wording in Indonesian and English).
5. **Worldwide:** Lemon Squeezy (merchant of record, accepts individuals, prices in dollars) as a second gateway behind the same interface; it handles foreign tax on its sales. Xendit can join once there is a company (it accepts only legal entities).
6. **Ready for a rush** (traffic stays small, since apps check codes offline; these are what break first):
   - Email codes through a real sender (Resend, Postmark or SES) on our domain, and show the code on the "paid" page too.
   - Rate-limit checkout and guard it against bots (Turnstile or App Check).
   - One code per payment, however often a gateway retries the webhook (keyed by gateway and payment id).
   - Card fraud and chargebacks: keep amounts small; codes checked offline can't be taken back.
   - Midtrans may ask for more documents as volume grows; taxes (NPWP; VAT abroad unless a merchant of record handles it).

## License

Dahlunas is licensed under the [MIT License](LICENSE). The vendored [htmx 2.0.4](https://github.com/bigskysoftware/htmx/tree/v2.0.4) script uses the [Zero-Clause BSD license](internal/web/static/htmx.LICENSE).
