FROM golang:1.27.3 AS build
WORKDIR /src/engine
COPY engine/ .
RUN go build -o /out/vortex-relay ./cmd/vortex-relay

FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends python3 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/vortex-relay /usr/local/bin/vortex-relay
COPY tools/phase4/echo_target.py /usr/local/bin/echo_target.py
ENTRYPOINT ["/usr/local/bin/vortex-relay"]
