FROM golang:1.27.3 AS build
WORKDIR /src/engine
COPY engine/ .
RUN go build -o /out/vortex-relay ./cmd/vortex-relay && go build -o /out/tls-baseline ./cmd/tls-baseline && go build -o /out/phase4-probe ./cmd/phase4-probe

FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends python3 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ /usr/local/bin/
COPY tools/phase4/*.py /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/vortex-relay"]
