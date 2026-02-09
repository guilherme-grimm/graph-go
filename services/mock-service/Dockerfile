FROM golang:1.25-bookworm AS build
WORKDIR /app
COPY main.go .
RUN CGO_ENABLED=0 go build -o /mock-service main.go

FROM gcr.io/distroless/static-debian12
COPY --from=build /mock-service /mock-service
ENTRYPOINT ["/mock-service"]
