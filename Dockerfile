FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/testapp .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/testapp /app/testapp
COPY release.json /app/release.json
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/app/testapp", "healthcheck"]
ENTRYPOINT ["/app/testapp"]
