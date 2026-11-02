.PHONY: dev web build run test ingest
dev:        ## go server with Vite dev proxy (run `cd web && npm run dev` in another tab)
	VITE_DEV=http://localhost:5173 CHECK_GAP=2m go run ./cmd/server
web:        ## build the Preact app into web/dist
	cd web && npm install && npm run build
build: web  ## single static binary with the UI embedded
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/skinverdict ./cmd/server
run: build
	CHECK_GAP=2m ./bin/skinverdict
test:
	go test ./...
deploy:     ## VPS=user@host make deploy
	VPS=$(VPS) bash deploy/deploy.sh
