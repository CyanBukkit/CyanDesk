FROM rust:1.83-bookworm AS gateway
WORKDIR /src/gateway-rs
COPY gateway-rs/Cargo.toml gateway-rs/Cargo.lock* ./
COPY gateway-rs/src ./src
RUN cargo build --release

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/cyandesk ./cmd/cyandesk

FROM node:22-bookworm AS web
WORKDIR /web
COPY admin-web/package.json admin-web/package-lock.json* ./
RUN npm install
COPY admin-web/ ./
RUN npm run build

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates ffmpeg \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 65532 --home /var/lib/cyandesk --create-home cyandesk
COPY --from=build /out/cyandesk /usr/local/bin/cyandesk
COPY --from=gateway /src/gateway-rs/target/release/cyandesk-gateway /usr/local/bin/cyandesk-gateway
COPY --from=web /web/dist /usr/share/cyandesk/admin
COPY deploy/entrypoint.sh /entrypoint.sh
RUN chmod 755 /entrypoint.sh
WORKDIR /var/lib/cyandesk
EXPOSE 13110 13111
ENTRYPOINT ["/entrypoint.sh"]
CMD ["control", "--data", "/var/lib/cyandesk", "--listen", "0.0.0.0:13111", "--internal", "0.0.0.0:13112", "--admin", "/usr/share/cyandesk/admin"]
