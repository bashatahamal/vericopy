FROM golang:1.26.5-alpine3.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=0.1.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X github.com/bashatahamal/vericopy/internal/version.Version=${VERSION} -X github.com/bashatahamal/vericopy/internal/version.Commit=${COMMIT} -X github.com/bashatahamal/vericopy/internal/version.BuildDate=${BUILD_DATE}" \
    -o /out/vericopy ./cmd/vericopy

FROM scratch
COPY --from=build /out/vericopy /vericopy
ENTRYPOINT ["/vericopy"]

