FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN set -eux; \
	GOARM=""; \
	if [ "$TARGETARCH" = "arm" ] && [ -n "${TARGETVARIANT:-}" ]; then GOARM="${TARGETVARIANT#v}"; fi; \
	CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOARM="$GOARM" \
		go build -trimpath -ldflags "-s -w" -o /out/prometheus-dingtalk-hook ./cmd/prometheus-dingtalk-hook

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /etc/prometheus-DingTalk-Hook
VOLUME ["/etc/prometheus-DingTalk-Hook"]

COPY --from=builder /out/prometheus-dingtalk-hook /usr/local/bin/prometheus-dingtalk-hook

COPY --from=builder --chown=65532:65532 /src/config.example.yml /etc/prometheus-DingTalk-Hook/config.example.yml
COPY --from=builder --chown=65532:65532 /src/config.example.yml /etc/prometheus-DingTalk-Hook/config.yml
COPY --from=builder --chown=65532:65532 /src/templates/ /etc/prometheus-DingTalk-Hook/templates/

EXPOSE 9098

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/prometheus-dingtalk-hook"]
CMD ["-config", "/etc/prometheus-DingTalk-Hook/config.yml"]
