FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /dahlunas ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /dahlunas /dahlunas
ENTRYPOINT ["/dahlunas"]
