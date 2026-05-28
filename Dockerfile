# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/aletheia ./cmd/aletheia
RUN /out/aletheia train \
    --config configs/tiny.yaml \
    --dataset datasets/bootstrap_actions.jsonl \
    --out /out/checkpoints/tiny-actions

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/aletheia /app/aletheia
COPY --from=build /out/checkpoints /app/checkpoints

ENV ALETHEIA_ADDR=:8080
ENV ALETHEIA_CHECKPOINT=/app/checkpoints/tiny-actions
ENV ALETHEIA_MODEL=tiny-actions

EXPOSE 8080
ENTRYPOINT ["/app/aletheia", "serve"]
