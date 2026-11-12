# goals design

## Intent

goals is a text message app that makes sure you complete your goals. Anyone can sign up at goals.timeclimbers.com with a phone number, list a few daily goals, and get one text a day asking which ones they did. Their replies build a streak and history they can see on the web.

The current repo is a 2019 sketch: it sends an SMS for a hardcoded goal list and discards replies. This work rebuilds it on the go-template standards and ships it on mist.

## Goals

- Public signup by phone number, with explicit SMS consent (A2P 10DLC compliant).
- Web: log in with an SMS code, manage goals, set reminder time and timezone, pause, delete account, view streaks and history.
- One daily SMS per user at their local reminder time, listing numbered active goals.
- SMS replies record check-ins: `y`/`yes` = all done, `n`/`no` = none, numbers such as `1 3` or `1,3` = those done and the rest not done. Anything else gets a short help reply.
- STOP/START/HELP handled; opted-out users are never texted.
- Repo meets go-template standards and deploys like `icco/likes`.

## Non-goals

- Goal frequencies other than daily, multiple reminders per day, email, push, native apps, social features, admin UI.

## Anti-goals

- Texting anyone who did not opt in, or after STOP.
- Unbounded SMS spend (SMS pumping through signup/login).
- Losing or silently overwriting check-in history.

## Constraints

- Go, module `go.icco.me/goals`, default branch `main`.
- go-template config: `.golangci.yml`, `.yamllint`, workflows (codeql, golangci-lint, pr-title, release, test with 80% coverage floor, yaml-json), plus `docker.yml` and a docker dependabot ecosystem as in `likes`. goreleaser builds the binary like `likes`.
- chi v5 routing, gutil for logging/JSON/ETags, GORM `AutoMigrate` on Postgres (rope), `lib/dbtest` per-test schema pattern from `likes`.
- Twilio: official `github.com/twilio/twilio-go`. Sending through a Messaging Service (needed for A2P and built-in STOP handling). Webhooks validated with `X-Twilio-Signature`.
- Deploy: `ghcr.io/icco/goals:main` in `mist/docker-compose.yml` behind caddy at `goals.timeclimbers.com`; DNS via `timeclimbers_subdomains`; database and URL secret via `postgres.tf`/`secrets.tf`/`update.sh` like `likes`; uptime monitor like the other sites.
- A2P 10DLC brand/campaign registration and Twilio credentials are manual steps for the owner. The app must run (web works, sending is disabled with a logged warning) until Twilio env vars are set.

## Approaches

- **A. Web app + daily SMS (chosen).** Full web UI plus SMS reminders and replies. Most code, but the only option where a stranger who signs up can see their progress.
- B. SMS-first with a consent-only website; goals managed by text commands. About half the code, but history is a text and commands are easy to forget.
- C. Separate SPA frontend on a JSON API. Rejected: a second deployable for a small app, and the owner's Go services render server-side.

## Design

### Data (GORM, AutoMigrate)

- `User`: phone (E.164, unique), timezone (IANA, default `America/New_York`), remind minute-of-day (default 20:00), paused, opted out, consented at, created/updated.
- `Goal`: user, text (1-140 chars), position, archived at. At most 10 active goals per user.
- `CheckIn`: goal, date (user-local `YYYY-MM-DD`), done, source (`sms`/`web`), updated at. Unique (goal, date); a later answer for the same day overwrites the earlier one.
- `Reminder`: user, date, sent at, Twilio message SID. Unique (user, date).
- `Session`: SHA-256 of a random token, user, expires (30 days). Cookie is `HttpOnly`, `Secure`, `SameSite=Lax`.

### Login

Phone plus a 6-digit code through Twilio Verify, which brings its own rate limits and fraud guard against SMS pumping. The signup form needs a consent checkbox with A2P language (program name, message frequency "1 message per day", "Msg & data rates may apply", "Reply STOP to cancel, HELP for help", links to terms and privacy). Login for an existing phone skips the checkbox. Every state-changing form carries a CSRF token.

### Reminders

A goroutine ticks every minute. For each user who is not paused, not opted out, and has at least one active goal, it computes local time. Once local time is at or after the reminder time and no `Reminder` row exists for the local date, it inserts the row (the unique constraint makes it at-most-once, even across restarts or two instances) and then sends:

```
goals: did you do these today?
1. read 20 pages
2. run
Reply y, n, or the numbers you did (e.g. "1 3").
```

### Replies

`POST /sms` validates the signature, looks up the user by `From`, and applies the reply to the goals and date of that user's most recent `Reminder`, using the goals as numbered in that message (active goals at send time, ordered by position). It replies with a short confirmation and the current streak. Unknown numbers get no reply. Twilio's Messaging Service handles STOP/START/HELP keywords. The webhook reads `OptOutType` to set or clear the opted-out flag.

### Web pages

`/` landing and signup, `/login`, `/verify`, `/app` (today's goals with done/not-done toggles, each goal's current and best streak, and a 30-day grid), `/app/goals` (add, edit, reorder, archive), `/settings` (timezone, time, pause, delete account), `/terms`, `/privacy`, `/healthz`. Server-rendered `html/template` with embedded assets. The web can edit check-ins for the last 7 days.

A streak counts consecutive local days with `done` ending today or yesterday. A missing day breaks it.

### Config

`PORT`, `DATABASE_URL`, `BASE_URL` (for signature validation and links), `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_MESSAGING_SERVICE_SID`, `TWILIO_VERIFY_SERVICE_SID`.

### Testing

Twilio sits behind an interface with a fake. Tests cover the reply parser, streaks, scheduler idempotency and timezone handling, webhook signature rejection, the login flow, and handlers against dbtest Postgres. Coverage floor is 80%.

## Builder calls

- `Reminder` stores the ordered goal IDs it listed, so a reply's numbers map to that message even if goals change afterward.
- Signup accepts `+1` numbers only, since A2P 10DLC covers US numbers.
- Deleting an account hard-deletes the user and all their goals, check-ins, reminders, and sessions.
- Twilio secrets live in GCP Secret Manager as `mist-goals-twilio-*` and are read by `update.sh`, like the database URL. The owner fills in the values.

## Left to the builder

- Visual style: simple, readable, in line with timeclimbers.com.
- Package layout under `lib/`.
- Exact copy for the terms and privacy pages.
