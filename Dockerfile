FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --chmod=0755 prometheus-dingtalk-hook /app/prometheus-dingtalk-hook
RUN mkdir -p /app/templates

EXPOSE 8080

ENTRYPOINT ["/app/prometheus-dingtalk-hook"]
CMD ["-config", "/app/config.yaml"]
