FROM golang:1.27-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server .

FROM gcr.io/distroless/static:nonroot

# checkov:skip=CKV_DOCKER_2: distroless has no shell or HTTP client to probe with; compose and CI check readiness over HTTP

COPY --from=build /server /server

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/server"]
