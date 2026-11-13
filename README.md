# goals

One text a day about the things you meant to do. Sign up at [goals.timeclimbers.com](https://goals.timeclimbers.com) with a US mobile number, list up to 10 daily goals, and each evening goals texts you the list. Reply `y`, `n`, or the numbers you did (`1 3`). The site shows streaks and the last 30 days.

## Run locally

```sh
docker run -d --name goals-pg -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=goals_test -p 5432:5432 postgres:18-alpine
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/goals_test?sslmode=disable"
BASE_URL=http://localhost:8080 task run
task test
task lint
```

Without the `TWILIO_*` variables the site runs but cannot text, so signup and login show "Texts are not set up yet".

## Configuration

| Variable | Purpose |
| --- | --- |
| `PORT` | Listen port, default `8080` |
| `DATABASE_URL` | Postgres URL. Tables are created on start. |
| `BASE_URL` | Public URL, such as `https://goals.timeclimbers.com`. Twilio signs webhooks against it. |
| `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN` | Twilio credentials |
| `TWILIO_MESSAGING_SERVICE_SID` | Messaging Service that sends reminders |
| `TWILIO_VERIFY_SERVICE_SID` | Verify service that sends login codes |

## Twilio setup

1. Buy a US number and add it to a new [Messaging Service](https://www.twilio.com/docs/messaging/services).
2. In the service's Integration settings, send incoming messages to the webhook `https://goals.timeclimbers.com/sms` (HTTP POST).
3. Keep [Advanced Opt-Out](https://www.twilio.com/docs/messaging/tutorials/advanced-opt-out) on, so Twilio answers STOP, START, and HELP. goals records the opt-out from the `OptOutType` field.
4. Register an [A2P 10DLC](https://www.twilio.com/docs/messaging/compliance/a2p-10dlc) brand and campaign. Point the opt-in evidence at `https://goals.timeclimbers.com/`, and the terms and privacy URLs at `/terms` and `/privacy`.
5. Create a [Verify service](https://www.twilio.com/docs/verify/api) for login codes.

## Deployment

The `main` branch publishes `ghcr.io/icco/goals:main`. The deployment sets the variables above from Secret Manager.

## Docs

- [Design](docs/superpowers/specs/2026-11-12-goals-design.md)
- [godoc](https://pkg.go.dev/go.icco.me/goals)
