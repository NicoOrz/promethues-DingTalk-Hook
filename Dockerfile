FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY prometheus-dingtalk-hook /app/prometheus-dingtalk-hook
RUN chmod +x /app/prometheus-dingtalk-hook && mkdir -p /app/templates /app/configs

EXPOSE 8080

ENTRYPOINT ["/app/prometheus-dingtalk-hook"]
CMD ["-config", "/app/config.yaml"]
