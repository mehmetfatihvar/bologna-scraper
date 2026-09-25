BIN := bin/bologna
export BOLOGNA_DB_URL ?= postgres://bologna:bologna@localhost:5432/bologna?sslmode=disable

build: ; go build -o $(BIN) ./cmd/bologna
test:  ; go test ./...
# PostgreSQL gerektirir (make db). Testler geçici bir şemada çalışır ve sonunda siler.
test-integration: ; BOLOGNA_TEST_DB_URL="$(BOLOGNA_DB_URL)" go test -count=1 -race ./...
lint:  ; gofmt -l . | (! grep .) && go vet ./...
db:    ; docker compose up -d db
migrate: build ; $(BIN) migrate
.PHONY: build test test-integration lint db migrate
