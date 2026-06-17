FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /protoc-gen-proto2type .

FROM scratch
COPY --from=build /protoc-gen-proto2type /protoc-gen-proto2type
ENTRYPOINT ["/protoc-gen-proto2type"]
