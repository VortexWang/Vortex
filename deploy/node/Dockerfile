# syntax=docker/dockerfile:1
FROM golang:1.27.3@sha256:c292963ad0e5f51e843c726edc204a4e4c26a859ba1e4d736d91028aa3633048 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/vortex-node ./cmd/vortex-node

FROM scratch
COPY --from=build /out/vortex-node /vortex-node
USER 65532:65532
WORKDIR /state
VOLUME ["/state"]
STOPSIGNAL SIGTERM
ENTRYPOINT ["/vortex-node"]
CMD ["run", "--db", "/state/agent.db", "--credential-dir", "/state/credentials", "--admin-socket", "/state/run/admin.sock"]
