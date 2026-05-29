# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/aletheia ./cmd/aletheia
ARG ALETHEIA_TRAIN_STEPS=0
RUN /out/aletheia train \
    --config configs/aletheia-mikros.yaml \
    --dataset datasets/aletheia_mikros.jsonl \
    --out /out/checkpoints/aletheia-mikros \
    --steps ${ALETHEIA_TRAIN_STEPS}
RUN /out/aletheia train \
    --config configs/aletheia-hephaestus.yaml \
    --dataset datasets/aletheia_hephaestus.jsonl \
    --out /out/checkpoints/aletheia-hephaestus \
    --steps ${ALETHEIA_TRAIN_STEPS}

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/aletheia /app/aletheia
COPY --from=build /out/checkpoints /app/checkpoints

ENV ALETHEIA_ADDR=:8080
ENV ALETHEIA_CHECKPOINTS_DIR=/app/checkpoints
ENV ALETHEIA_MODEL=aletheia-mikros

EXPOSE 8080
ENTRYPOINT ["/app/aletheia", "serve"]
