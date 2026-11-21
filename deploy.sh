#!/usr/bin/env bash
# Builds the WASM engine and deploys the static frontend to Cloudflare Pages.
# Deploy the FastAPI backend separately on a Python host.
#
# Requires: Go toolchain, Node 22+, and `npx wrangler login` already done.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PAGES_PROJECT="${PAGES_PROJECT:-glass}"

echo "== 1/3  Building engine.wasm =="
cd "$ROOT"
GOOS=js GOARCH=wasm go build -o web/public/engine.wasm ./cmd/wasm

echo "== 2/3  Building frontend static export =="
cd "$ROOT/web"
npm run build

echo "== 3/3  Deploying frontend to Cloudflare Pages ($PAGES_PROJECT) =="
npx wrangler pages deploy out --project-name "$PAGES_PROJECT"

echo "== Done =="
