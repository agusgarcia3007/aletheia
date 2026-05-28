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
RUN /out/aletheia train \
    --config configs/chat-basic.yaml \
    --dataset datasets/chat_basic.jsonl \
    --out /out/checkpoints/aletheia-chat-basic

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/aletheia /app/aletheia
COPY --from=build /out/checkpoints /app/checkpoints

ENV ALETHEIA_ADDR=:8080
ENV ALETHEIA_CHECKPOINT=/app/checkpoints/aletheia-chat-basic
ENV ALETHEIA_MODEL=aletheia-chat-basic

EXPOSE 8080
ENTRYPOINT ["/app/aletheia", "serve"]
