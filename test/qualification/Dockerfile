FROM golang:1.27.3 AS build
WORKDIR /src/engine
COPY engine/ .
RUN go build -o /out/vortex-relay ./cmd/vortex-relay

FROM ubuntu:24.04
COPY --from=build /out/vortex-relay /usr/local/bin/vortex-relay
ENTRYPOINT ["/usr/local/bin/vortex-relay"]
