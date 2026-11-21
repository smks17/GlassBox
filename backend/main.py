import os
import sqlite3
import time
import uuid
from contextlib import asynccontextmanager, closing, contextmanager

from fastapi import FastAPI, Request, Response
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from starlette.concurrency import run_in_threadpool
from starlette.exceptions import HTTPException

DATABASE_PATH = os.environ.get("GLASS_DATABASE_PATH", "sessions.sqlite3")


@contextmanager
def database():
    with closing(sqlite3.connect(DATABASE_PATH)) as connection:
        with connection:
            yield connection


def initialize_database():
    with database() as connection:
        connection.execute(
            "CREATE TABLE IF NOT EXISTS sessions "
            "(id TEXT PRIMARY KEY, snapshot BLOB NOT NULL, updated_at INTEGER NOT NULL)"
        )


@asynccontextmanager
async def lifespan(app: FastAPI):
    await run_in_threadpool(initialize_database)
    yield


app = FastAPI(lifespan=lifespan)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["GET", "POST", "OPTIONS"],
    allow_headers=["Content-Type"],
)


@app.exception_handler(HTTPException)
async def http_error(request: Request, error: HTTPException):
    return JSONResponse({"error": "not found"}, status_code=404)


@app.options("/{path:path}")
def options(path: str):
    return Response(status_code=204)


@app.get("/health")
def health():
    return {"ok": True}


def store_snapshot(snapshot: bytes):
    session_id = str(uuid.uuid4())
    with database() as connection:
        connection.execute(
            "INSERT INTO sessions (id, snapshot, updated_at) VALUES (?, ?, ?)",
            (session_id, snapshot, time.time_ns() // 1_000_000),
        )
    return {"id": session_id}


@app.post("/sessions")
async def save_session(request: Request):
    snapshot = await request.body()
    if not snapshot:
        return JSONResponse({"error": "empty body"}, status_code=400)
    return await run_in_threadpool(store_snapshot, snapshot)


@app.get("/sessions/{session_id}")
def fetch_session(session_id: str):
    with database() as connection:
        row = connection.execute(
            "SELECT snapshot FROM sessions WHERE id = ?", (session_id,)
        ).fetchone()
    if row is None:
        return JSONResponse({"error": "not found"}, status_code=404)
    return Response(content=row[0], media_type="application/octet-stream")
