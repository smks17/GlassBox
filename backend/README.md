# GlassBox backend

Python 3.12 and FastAPI replace the Worker while preserving its snapshot API:

- `GET /health` returns `{"ok": true}`.
- `POST /sessions` stores the raw request bytes and returns `{"id": "..."}`.
- `GET /sessions/{id}` returns those bytes as `application/octet-stream`.
- Empty uploads return `400`; missing sessions return `404`.

Session storage uses SQLite with the existing `sessions` table layout. This
rewrite retains anonymous access and permissive CORS; it adds no authentication,
settings, or session-management features.

Run from `backend/` with Python 3.12:

```sh
python3.12 -m venv .venv
. .venv/bin/activate
python -m pip install -r requirements.txt
uvicorn main:app --reload
```

The service creates `sessions.sqlite3` on startup. Set `GLASS_DATABASE_PATH` to
use another SQLite file in an existing directory. Keep that file on persistent
storage when hosting the API.

Set `NEXT_PUBLIC_BACKEND_URL=http://localhost:8000` when running/building the
frontend. No frontend or engine API changes are needed for this rewrite.

Deploy FastAPI separately on a Python host; `deploy.sh` publishes only the WASM
engine and static frontend. Existing
D1 data is not automatically copied to the local SQLite file.
