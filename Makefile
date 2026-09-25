BIN := bin/bologna
export BOLOGNA_DB_URL ?= postgres://bologna:bologna@localhost:5432/bologna?sslmode=disable

build: ; go build -o $(BIN) ./cmd/bologna
test:  ; go test ./...
db:    ; docker compose up -d db
migrate: build ; $(BIN) migrate
.PHONY: build test db migrate
