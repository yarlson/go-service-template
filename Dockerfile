# syntax=docker/dockerfile:1

FROM golang:1.26.5-bookworm AS build

WORKDIR /src

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/service ./cmd/service

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/service /service

ENV APP_ENV=production
ENV HTTP_ADDRESS=:8080

EXPOSE 8080
USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/service", "healthcheck"]

ARG VERSION=dev
ARG COMMIT=unknown
ARG SOURCE_URL=https://github.com/yarlson/go-service-template
LABEL org.opencontainers.image.source="${SOURCE_URL}" \
    org.opencontainers.image.version="${VERSION}" \
    org.opencontainers.image.revision="${COMMIT}" \
    org.opencontainers.image.licenses="MIT"

ENTRYPOINT ["/service"]
CMD ["api"]
