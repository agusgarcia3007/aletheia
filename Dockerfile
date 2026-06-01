# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/aletheia ./cmd/aletheia
ARG ALETHEIA_TRAIN_STEPS=0
RUN /out/aletheia dataset build \
    --profile mikros-v1 \
    --out /out/datasets/mikros_v1.jsonl
RUN /out/aletheia train \
    --config configs/aletheia-mikros-v1.yaml \
    --dataset /out/datasets/mikros_v1.jsonl \
    --out /out/checkpoints/aletheia-mikros \
    --steps ${ALETHEIA_TRAIN_STEPS}
RUN /out/aletheia train \
    --config configs/aletheia-hephaestus.yaml \
    --dataset datasets/aletheia_hephaestus.jsonl \
    --out /out/checkpoints/aletheia-hephaestus \
    --steps ${ALETHEIA_TRAIN_STEPS}
RUN /out/aletheia train-router \
    --dataset datasets/router_mikros.jsonl \
    --out /out/checkpoints/router-mikros
# Seed an empty data dir owned by nonroot so a freshly-created volume mounted at
# /data inherits writable ownership (uid 65532).
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/aletheia /app/aletheia
# NOTE: the build-stage `train` steps above only (re)create the BASE bootstrap
# checkpoints baked into the image. They never touch /data, so the self-improved
# corpus and trained model in the persistent volume survive every redeploy.
COPY --from=build /out/checkpoints /app/checkpoints
# Bundle the verified, type-checked coding knowledge corpus so coding questions
# are answered from curated examples instead of always learning from the web.
COPY --from=build /src/knowledge /app/knowledge
# Persistent data volume: DB, harvested datasets and trained checkpoints live
# here so nothing is lost on redeploy. Pre-created as nonroot above.
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME ["/data"]

ENV ALETHEIA_ADDR=:8080
ENV ALETHEIA_CHECKPOINTS_DIR=/app/checkpoints
ENV ALETHEIA_MODEL=aletheia-mikros
ENV ALETHEIA_ROUTER_CHECKPOINT=/app/checkpoints/router-mikros
ENV ALETHEIA_KNOWLEDGE=/app/knowledge
# Persist DB + harvested datasets + trained checkpoints in the /data volume.
ENV ALETHEIA_DATA_DIR=/data
ENV ALETHEIA_DB=/data/memory.sqlite
# Self-improvement loop: every 6h harvest the verified corpus, retrain, and
# hot-swap (only trains once ≥20 verified examples exist). Generation stays gated,
# so it can only improve output, never degrade it.
ENV ALETHEIA_AUTOLEARN_INTERVAL=6h

EXPOSE 8080
ENTRYPOINT ["/app/aletheia", "serve"]
