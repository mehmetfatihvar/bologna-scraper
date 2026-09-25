FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bologna ./cmd/bologna

FROM gcr.io/distroless/static:nonroot
COPY --from=build /bologna /bologna
ENTRYPOINT ["/bologna"]
