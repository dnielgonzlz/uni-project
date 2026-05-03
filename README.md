# PT Scheduler — Local Setup

This project has three services that all need to run at the same time:

| Service  | Tech            | Port |
| -------- | --------------- | ---- |
| Backend  | Go              | 8080 |
| Solver   | Python/FastAPI  | 8081 |
| Frontend | React/Vite      | 5173 |
| ngrok    | tunnel          | —    |

---

## Prerequisites

- [Go](https://go.dev/dl/) 1.22+
- [Node.js](https://nodejs.org/) 20+ and npm
- [Python](https://www.python.org/downloads/) 3.11+
- [ngrok](https://ngrok.com/download) (free account is fine)
- A running PostgreSQL database

---

## 1. Get the API Keys

Check the `API_KEYS.md` file (shared separately) for all the credentials you need. It contains the values for:

- Database connection string
- OpenRouter (AI parser)
- Resend (email)
- Twilio (SMS / WhatsApp)
- Stripe (test keys)

---

## 2. Backend

```bash
cd backend
```

Copy the env file and fill in the values from `API_KEYS.md`:

```bash
cp .env.example .env
# Edit .env with the values from API_KEYS.md
```

Run migrations, then start the server:

```bash
make migrate-up
make run
```

The API will be available at `http://localhost:8080`.

---

## 3. Solver

```bash
cd solver
```

Create and activate the virtual environment, then install dependencies:

```bash
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

Start the solver service:

```bash
make run
```

The solver will be available at `http://localhost:8081`.

---

## 4. Frontend

```bash
cd frontend
```

Copy the env file and fill in the values from `API_KEYS.md`:

```bash
cp .env.local.example .env.local
# Edit .env.local with the values from API_KEYS.md
```

Install dependencies and start the dev server:

```bash
npm install
npm run dev
```

The app will open at `http://localhost:5173`.

---

## 5. ngrok (required for Twilio webhooks — local only)

Twilio needs a public URL to deliver WhatsApp/SMS messages to your local backend. ngrok creates a tunnel for that.

In a new terminal:

```bash
ngrok http 8080
```

ngrok will print a public URL like `https://abc123.ngrok-free.app`. Copy it, then go to the [Twilio Console](https://console.twilio.com) and set the webhook URL for your WhatsApp sandbox to:

```
https://abc123.ngrok-free.app/api/v1/webhooks/twilio
```

> **Important:** WhatsApp messaging will only work fully on the deployed version. The production Twilio webhook is permanently pointed at the AWS URL — ngrok is only needed locally to test incoming messages, and the URL changes every time you restart it.

---

## Running the tests

### Backend

```bash
cd backend
make test
```

### Solver

```bash
cd solver
make test
```

To run a specific scheduling scenario against the solver (useful for manual verification):

```bash
make fixture                          # runs the default scenario
make fixture SCENARIO=heavy_week      # or any other scenario
```

---

## All four terminals at once

Open four separate terminal tabs and run them in this order: backend → solver → frontend → ngrok.
