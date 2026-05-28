# Aletheia-μ: Arquitectura local desde cero en Go

> **Objetivo:** construir una inteligencia local, verificable, pequeña y evolutiva que corra en una Mac / Mac mini, escrita principalmente en Go, sin depender de modelos chinos ni bootstrapear un LLM existente como núcleo principal.

Este documento es un **prompt maestro** para un agente de ingeniería. La misión no es clonar GPT ni Claude. La misión es construir una arquitectura nueva: un organismo cognitivo local donde un micro-modelo propio actúa como selector, planificador y controlador de herramientas verificables.

---

## 0. Principio rector

No vamos a competir con GPT-5.5 o Claude Opus 4.7 tratando de meter “todo el conocimiento del mundo” en pocos parámetros.

Vamos a competir cambiando la forma del problema:

```text
inteligencia útil = micro-modelo propio
                  + búsqueda
                  + verificadores
                  + memoria causal
                  + herramientas locales
                  + aprendizaje continuo
                  + selección de hipótesis
```

El modelo pequeño no debe “saberlo todo”.

Debe saber:

1. descomponer problemas,
2. elegir acciones,
3. recuperar evidencia,
4. generar candidatos,
5. ejecutar verificadores,
6. aprender de errores,
7. abstenerse cuando no puede verificar,
8. comprimir experiencias exitosas en skills reutilizables.

La inteligencia no vive solo en los pesos. Vive en el loop.

---

## 1. Restricciones no negociables

### Lenguaje principal

- Go.
- Código simple, modular, auditable.
- Sin frameworks innecesarios.
- Todo debe poder correr localmente.

### Hardware inicial

Target principal:

- MacBook / Mac mini Apple Silicon.
- CPU first.
- GPU/Metal opcional para fases futuras.
- RAM objetivo inicial: 16–32 GB.
- Disco: almacenamiento local para datasets, memoria, trazas y checkpoints.

### Filosofía

- Offline-first.
- Local-first.
- Determinístico cuando sea posible.
- Evidencia antes que fluidez.
- Tests antes que estética.
- Métricas antes que hype.

### Prohibiciones

- No usar modelos chinos como base.
- No usar un LLM comercial remoto como núcleo.
- No depender de OpenAI/Anthropic para funcionar.
- No hacer solo un wrapper sobre llama.cpp.
- No vender humo de “frontier model desde una Mac” sin medición.

### Permitido

- Usar datasets públicos.
- Usar papers como inspiración.
- Usar tokenizers propios.
- Usar embeddings propios simples al inicio.
- Usar herramientas externas locales: compilador Go, SQLite, ripgrep, parsers, fuzzers, linters.
- Usar C/Assembly/Metal solo para kernels críticos si Go puro no alcanza.

---

## 2. Qué significa “desde cero”

“Desde cero” NO significa entrenar un modelo de lenguaje general de frontera en una Mac.

Significa:

1. construir el runtime en Go,
2. construir tokenizer propio,
3. construir dataset pipeline propio,
4. construir un micro-transformer propio,
5. entrenar un modelo pequeño desde inicialización aleatoria,
6. entrenarlo primero como controlador cognitivo, no como chatbot universal,
7. añadir memoria, verificadores y búsqueda para multiplicar su capacidad.

El primer modelo puede ser diminuto:

```text
10M → 30M → 100M → 300M parámetros
```

Luego, si el sistema funciona, escalar.

El punto no es que el primer checkpoint “sepa todo”. El punto es que aprenda a operar una máquina de razonamiento verificable.

---

## 3. Nombre del sistema

Nombre interno:

```text
Aletheia-μ
```

Pronunciación:

```text
Aletheia micro
```

Significado:

- Aletheia: verdad / desocultamiento.
- μ: micro, local, mínimo, eficiente.

---

## 4. Arquitectura general

```text
┌─────────────────────────────────────────────────────────────┐
│                         User Goal                            │
└─────────────────────────────┬───────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      Cognitive VM                            │
│  Interpreta acciones cognitivas, estado, presupuesto y riesgo │
└───────────────┬──────────────────────────────┬──────────────┘
                │                              │
                ▼                              ▼
┌───────────────────────────┐       ┌─────────────────────────┐
│     Micro Model Runner     │       │       Memory Graph       │
│  Transformer propio en Go  │       │ SQLite + embeddings + DAG │
└───────────────┬───────────┘       └──────────────┬──────────┘
                │                                  │
                ▼                                  ▼
┌───────────────────────────┐       ┌─────────────────────────┐
│         Selector           │       │      Retriever           │
│ Re-rankea top-k acciones   │       │ Evidencia local/contexto │
└───────────────┬───────────┘       └──────────────┬──────────┘
                │                                  │
                └──────────────┬───────────────────┘
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                       Search Engine                          │
│             MCTS / beam search sobre acciones                 │
└─────────────────────────────┬───────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                       Verifier Bus                            │
│ go test, fuzzing, linters, parsers, benchmarks, citas, etc.   │
└─────────────────────────────┬───────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Answer / Patch / Skill                     │
└─────────────────────────────────────────────────────────────┘
```

---

## 5. Componentes principales

## 5.1 `cmd/aletheia`

CLI principal.

Debe permitir:

```bash
aletheia init
aletheia train
aletheia chat
aletheia solve ./path/to/repo
aletheia index ./docs
aletheia memory inspect
aletheia eval
aletheia bench
```

Comandos mínimos iniciales:

```bash
aletheia init
aletheia train --config configs/micro.yaml
aletheia run --prompt "explica este repo"
aletheia solve --task tasks/example.json
aletheia eval --suite evals/bootstrap
```

---

## 5.2 `internal/tokenizer`

Crear un tokenizer propio.

No empezar con BPE complejo si retrasa todo.

Fase 1:

- byte-level tokenizer,
- vocabulario de 256 bytes,
- tokens especiales,
- tokens funcionales.

Tokens especiales:

```text
<BOS>
<EOS>
<PAD>
<UNK>
<USER>
<SYSTEM>
<ASSISTANT>
<STATE>
<EVIDENCE>
<ACTION>
<RESULT>
```

Tokens funcionales iniciales:

```text
<ACT_DECOMPOSE>
<ACT_RETRIEVE>
<ACT_PLAN_DAG>
<ACT_GEN_CANDIDATES>
<ACT_RUN_TESTS>
<ACT_RUN_CMD>
<ACT_PARSE_CODE>
<ACT_MUTATE_CODE>
<ACT_SEARCH_MEMORY>
<ACT_VERIFY>
<ACT_FIND_COUNTEREXAMPLE>
<ACT_REPAIR>
<ACT_RANK>
<ACT_RESPOND>
<ACT_ABSTAIN>
<ACT_COMPRESS_SKILL>
```

Fase 2:

- unigram tokenizer o BPE propio,
- vocabulario 8k–32k,
- conservar tokens funcionales como símbolos atómicos.

---

## 5.3 `internal/model`

Implementar un transformer decoder-only pequeño.

Arquitectura inicial:

```yaml
model:
  type: decoder_transformer
  vocab_size: 512 inicialmente
  context_length: 512
  n_layers: 4
  n_heads: 4
  d_model: 256
  d_ff: 1024
  norm: rmsnorm
  activation: swiglu
  positional_encoding: rope
  dropout: 0.0
```

Primer objetivo:

- que entrene,
- que overfittee un dataset minúsculo,
- que genere tokens funcionales válidos,
- que pueda elegir acciones simples.

No optimizar demasiado antes de demostrar el loop.

Escalas sugeridas:

### Modelo 0: `seed-10m`

```yaml
n_layers: 4
d_model: 256
n_heads: 4
d_ff: 1024
context_length: 512
params: ~10M-20M
```

### Modelo 1: `core-100m`

```yaml
n_layers: 8
d_model: 512
n_heads: 8
d_ff: 2048
context_length: 1024
params: ~100M
```

### Modelo 2: `core-300m`

```yaml
n_layers: 16
d_model: 768
n_heads: 12
d_ff: 3072
context_length: 2048
params: ~300M
```

No avanzar a `core-300m` hasta que el sistema de evaluación muestre mejoras reales.

---

## 5.4 `internal/tensor`

Construir una librería tensorial mínima.

Operaciones necesarias:

- matmul,
- embedding lookup,
- RMSNorm,
- softmax,
- attention,
- RoPE,
- SwiGLU,
- cross entropy,
- AdamW,
- gradient clipping,
- checkpoint save/load.

Fase 1:

- float32 simple.
- correcto antes que rápido.

Fase 2:

- float16/bfloat16 si conviene.
- quantización int8 para inferencia.
- kernels acelerados.
- mmap para pesos.

Fase 3:

- ternary / 1.58-bit experimental.
- packed weights.
- kernels SIMD/Metal.

---

## 5.5 `internal/runner`

Responsable de inferencia.

Debe exponer:

```text
Forward(tokens) -> logits
Generate(prompt, options) -> tokens
TopK(state, k) -> candidates
Score(sequence) -> logprob
```

Opciones:

```yaml
temperature: 0.2
top_k: 16
top_p: 0.95
max_tokens: 512
stop_tokens:
  - <EOS>
  - <ACT_RESPOND>
```

Importante:

El runner no debe ocultar los logits. El sistema necesita top-k para que el Selector pueda elegir mejor que greedy decoding.

---

## 5.6 `internal/selector`

Este es el núcleo diferencial.

No elegir el próximo token solo por probabilidad.

Crear un selector que reciba:

```text
state
top_k_candidates
memory_hits
verifier_history
budget
risk
```

Y devuelva:

```text
selected_action
confidence
reason
```

Score inicial:

```text
score = model_logprob
      + evidence_score
      + verifier_prior
      + memory_consistency
      - uncertainty_penalty
      - cost_penalty
      - contradiction_penalty
```

El selector puede empezar como heurística.

Después entrenar un selector pequeño:

- logistic regression,
- MLP simple,
- transformer chico,
- o adapter dentro del modelo.

Dataset del selector:

Cada vez que el sistema explore varias opciones, guardar:

```json
{
  "state": "...",
  "candidates": ["...", "..."],
  "chosen": "...",
  "verifier_result": "pass/fail",
  "reward": 0.87
}
```

Objetivo:

El modelo base propone. El selector aprende qué propuestas suelen sobrevivir a la realidad.

---

## 5.7 `internal/cognitivevm`

La VM cognitiva interpreta tokens funcionales.

Ejemplo:

```text
<ACT_RETRIEVE>        -> llamar retriever
<ACT_RUN_TESTS>       -> llamar verifier go test
<ACT_MUTATE_CODE>     -> crear patch candidato
<ACT_VERIFY>          -> correr bus de verificadores
<ACT_ABSTAIN>         -> responder con incertidumbre explícita
<ACT_COMPRESS_SKILL>  -> guardar skill reusable
```

La VM mantiene estado:

```json
{
  "goal": "...",
  "budget": {
    "tokens": 4000,
    "seconds": 120,
    "tool_calls": 50
  },
  "working_memory": [],
  "evidence": [],
  "candidate_actions": [],
  "verifier_results": [],
  "uncertainty": 0.0,
  "risk": "low|medium|high"
}
```

Regla importante:

Toda acción que modifique archivos debe pasar por:

1. generación de patch,
2. preview,
3. verificación,
4. aplicación controlada.

---

## 5.8 `internal/verifier`

Crear un bus de verificadores.

Interfaz conceptual:

```go
type Verifier interface {
    Name() string
    CanCheck(Task) bool
    Check(ctx context.Context, candidate Candidate) Evidence
    Cost() CostEstimate
}
```

Verificadores iniciales:

### Go compiler verifier

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`

### Static verifier

- parsear AST,
- detectar imports rotos,
- detectar funciones eliminadas,
- detectar cambios peligrosos.

### Fuzz verifier

- `go test -fuzz`
- límite de tiempo configurable.

### Benchmark verifier

- `go test -bench`
- comparar contra baseline.

### Text evidence verifier

- cada afirmación factual debe tener fuente local,
- cada cita debe corresponder a chunk real.

### Contradiction verifier

- buscar contradicciones entre respuesta y memoria.
- buscar contradicciones entre patch y tests.

Evidence object:

```json
{
  "verifier": "go_test",
  "status": "pass|fail|unknown",
  "score": 0.0,
  "stdout": "...",
  "stderr": "...",
  "artifacts": [],
  "timestamp": "..."
}
```

---

## 5.9 `internal/search`

Implementar búsqueda sobre acciones, no solo texto.

Empezar simple:

### Fase 1: beam search

- expandir top-k acciones,
- verificar parcialmente,
- mantener mejores N estados.

### Fase 2: MCTS

Cada nodo:

```json
{
  "state": "...",
  "action": "...",
  "children": [],
  "visits": 0,
  "value": 0.0,
  "evidence": []
}
```

Reward:

```text
+ tests pasan
+ contradicciones bajan
+ evidencia sube
+ patch pequeño
+ benchmark mejora
- costo alto
- incertidumbre alta
- falla compilación
- rompe API
```

La búsqueda debe poder decir:

```text
no encontré solución verificada con este presupuesto
```

Eso es mejor que alucinar.

---

## 5.10 `internal/memory`

Memoria local.

Usar SQLite inicialmente.

Tablas mínimas:

```sql
documents(id, path, hash, created_at, updated_at, text)
chunks(id, document_id, offset_start, offset_end, text, embedding_id)
episodes(id, goal, result, reward, created_at)
evidence(id, episode_id, verifier, status, score, payload)
skills(id, name, trigger, action_sequence, success_rate, created_at)
nodes(id, type, label, payload)
edges(id, from_node, to_node, relation, weight)
```

Relaciones útiles:

```text
depends_on
fixes
breaks
verifies
contradicts
derived_from
replaces
improves
failed_because
```

La memoria debe guardar causalidad, no solo texto.

Ejemplo:

```text
patch_913 fixes test_failure_271
patch_913 breaks benchmark_latency
hypothesis_44 refuted_by fuzz_case_882
parser_config depends_on env_contract_v4
```

---

## 5.11 `internal/retriever`

Retriever híbrido:

1. keyword search,
2. embeddings simples,
3. grafo causal,
4. recency,
5. confidence.

Embeddings iniciales:

- empezar con hashing vectorizer / bag of bytes / char n-grams.
- luego entrenar embedding chico propio.
- no depender de embeddings remotos.

Ranking:

```text
score = semantic_similarity
      + keyword_overlap
      + graph_relevance
      + recency
      + trust_score
```

---

## 5.12 `internal/training`

Entrenamiento inicial desde cero.

### Dataset 1: lenguaje mínimo

Objetivo:

- que el modelo aprenda sintaxis básica,
- formato de conversación,
- tokens funcionales,
- estructura de tareas.

Fuentes posibles:

- documentación propia,
- código propio,
- README,
- issues locales,
- datasets públicos no problemáticos,
- textos pequeños curados.

No intentar absorber internet.

### Dataset 2: acciones cognitivas

Crear ejemplos sintéticos:

```text
Usuario: "arregla este error de compilación"
Acciones:
<ACT_PARSE_CODE>
<ACT_RUN_TESTS>
<ACT_FIND_COUNTEREXAMPLE>
<ACT_MUTATE_CODE>
<ACT_VERIFY>
<ACT_RESPOND>
```

### Dataset 3: tareas verificables

Formato:

```json
{
  "goal": "fix failing test",
  "context": "...",
  "valid_actions": ["<ACT_RUN_TESTS>", "<ACT_MUTATE_CODE>"],
  "gold_action_sequence": [
    "<ACT_RUN_TESTS>",
    "<ACT_PARSE_CODE>",
    "<ACT_MUTATE_CODE>",
    "<ACT_VERIFY>"
  ],
  "expected_result": "tests pass"
}
```

### Dataset 4: selector

Recolectado por el propio sistema.

```json
{
  "state": "...",
  "candidate_actions": ["A", "B", "C"],
  "chosen": "B",
  "reward": 1.0,
  "evidence": "go test passed"
}
```

---

## 6. Training loop

### Fase A: overfit controlado

Antes de entrenar grande:

- 100 ejemplos.
- El modelo debe overfittear.
- Si no puede overfittear, hay bug.

### Fase B: pretraining mínimo

Objetivo:

- next-token prediction.
- aprender estructura del lenguaje y acciones.
- no buscar genialidad.

### Fase C: supervised action learning

Entrenar secuencias de tokens funcionales.

### Fase D: verifier-guided improvement

Loop:

1. modelo propone acción,
2. VM ejecuta,
3. verificador evalúa,
4. reward se guarda,
5. selector mejora,
6. skills se comprimen.

### Fase E: self-play local

Generar tareas pequeñas:

- romper un test y arreglarlo,
- esconder un bug y encontrarlo,
- crear docs y responder preguntas,
- generar una función y testearla,
- refactor con invariantes.

---

## 7. Evaluación

Crear `evals/bootstrap`.

Suites iniciales:

### `evals/go_compile`

Tareas:

- corregir errores de compilación simples.
- expected: `go test ./...` pasa.

### `evals/go_tests`

Tareas:

- arreglar función para pasar tests.
- expected: tests pasan.

### `evals/doc_qa`

Tareas:

- responder usando documentos indexados.
- expected: respuesta con evidencia exacta.

### `evals/planning`

Tareas:

- convertir objetivo en DAG de acciones.
- expected: plan válido.

### `evals/abstention`

Tareas:

- preguntas imposibles o sin evidencia.
- expected: abstenerse o pedir dato.

### `evals/memory`

Tareas:

- recordar decisión previa.
- expected: recuperar nodo correcto.

Métricas:

```text
task_success_rate
verified_success_rate
hallucination_rate
abstention_accuracy
tokens_per_success
seconds_per_success
tool_calls_per_success
patch_size
regression_rate
memory_hit_rate
```

Métrica principal:

```text
verified_success_rate / cost
```

No medir solo fluidez.

---

## 8. Roadmap de implementación

## Semana 1: esqueleto

Crear estructura:

```text
aletheia/
  cmd/aletheia/
  internal/tokenizer/
  internal/tensor/
  internal/model/
  internal/runner/
  internal/cognitivevm/
  internal/selector/
  internal/verifier/
  internal/search/
  internal/memory/
  internal/retriever/
  internal/training/
  internal/eval/
  configs/
  datasets/
  evals/
  checkpoints/
  docs/
```

Comandos:

```bash
go mod init github.com/agusgarcia3007/aletheia
go test ./...
go run ./cmd/aletheia init
```

Deliverable:

- CLI corre.
- tests básicos.
- tokenizer byte-level.
- SQLite memory inicial.

---

## Semana 2: micro-transformer mínimo

Implementar:

- tensor float32,
- embedding,
- attention causal,
- RMSNorm,
- FFN,
- logits,
- loss,
- backward simple.

Deliverable:

- modelo overfittea 100 ejemplos.
- checkpoint save/load.
- generación mínima.

---

## Semana 3: tokens funcionales + VM

Implementar:

- tokens funcionales,
- Cognitive VM,
- estado,
- acciones mock,
- selector heurístico.

Deliverable:

- ante una tarea simple, el sistema produce secuencia de acciones válida.

---

## Semana 4: verificadores reales

Implementar:

- go test verifier,
- command runner sandboxed,
- evidence objects,
- memory logging.

Deliverable:

- sistema puede intentar arreglar una tarea toy y verificarla.

---

## Semana 5: memoria y retriever

Implementar:

- indexación de documentos/repos,
- chunks,
- keyword search,
- embeddings simples,
- grafo causal.

Deliverable:

- pregunta sobre documento local recibe respuesta con evidencia.

---

## Semana 6: búsqueda

Implementar:

- beam search sobre acciones,
- reward simple,
- guardar trayectorias.

Deliverable:

- mejora sobre greedy en evals bootstrap.

---

## Semana 7: selector entrenable

Implementar:

- dataset de selector,
- modelo selector simple,
- entrenamiento,
- evaluación A/B.

Deliverable:

- selector supera greedy en elección de acciones.

---

## Semana 8: skill compression

Implementar:

- detectar trayectorias exitosas,
- comprimirlas en skills,
- reusar skills por trigger.

Deliverable:

- tareas repetidas cuestan menos y salen mejor.

---

## Semana 9. Diseño del archivo de configuración

Ejemplo `configs/micro.yaml`:

```yaml
project:
  name: aletheia-mu
  data_dir: ./data
  checkpoint_dir: ./checkpoints
  memory_db: ./data/memory.sqlite

model:
  name: seed-10m
  vocab_size: 512
  context_length: 512
  n_layers: 4
  n_heads: 4
  d_model: 256
  d_ff: 1024
  dropout: 0.0
  rope: true
  norm: rmsnorm
  activation: swiglu

training:
  batch_size: 16
  learning_rate: 0.0003
  weight_decay: 0.01
  max_steps: 10000
  grad_clip: 1.0
  checkpoint_every: 500
  eval_every: 250

inference:
  temperature: 0.2
  top_k: 16
  top_p: 0.95
  max_tokens: 512

search:
  strategy: beam
  beam_width: 4
  max_depth: 8
  budget_seconds: 120
  budget_tool_calls: 50

verifiers:
  go_test:
    enabled: true
    command: "go test ./..."
    timeout_seconds: 60
  go_vet:
    enabled: true
    command: "go vet ./..."
    timeout_seconds: 60
  fuzz:
    enabled: false
    timeout_seconds: 120

memory:
  chunk_size: 1200
  chunk_overlap: 200
  embedding: hashing
  graph_enabled: true
```

---

## 10. Loop principal del agente

El loop conceptual:

```text
while budget remains:
    state = CognitiveVM.State()
    logits = Runner.Forward(state.tokens)
    candidates = TopK(logits)
    evidence = Memory.Retrieve(state)
    selected = Selector.Choose(state, candidates, evidence)

    if selected is functional_action:
        result = CognitiveVM.Execute(selected)
        Memory.Record(result)
        if result requires verification:
            verifier_result = VerifierBus.Check(result)
            Memory.Record(verifier_result)
    else:
        CognitiveVM.AppendToken(selected)

    if goal verified:
        return final_answer_with_evidence

return abstain_or_partial_answer
```

---

## 11. Seguridad local

No ejecutar comandos arbitrarios sin límites.

Implementar:

- allowlist de comandos,
- timeout obligatorio,
- working directory restringido,
- no borrar archivos sin confirmación,
- dry-run para patches,
- backup antes de modificar,
- diff visible,
- logs de acciones.

Allowlist inicial:

```text
go test
go test ./...
go vet ./...
go test -run
go test -bench
grep/rg
cat
ls
git diff
git status
```

Bloquear por defecto:

```text
rm -rf
curl | sh
sudo
chmod -R
ssh
scp
dd
diskutil
network exfiltration
```

---

## 12. El primer demo que debe funcionar

Crear un repo toy:

```text
examples/buggy-go/
  calculator.go
  calculator_test.go
```

Bug:

```text
Add(a, b int) int devuelve a - b
```

Tarea:

```json
{
  "goal": "Fix the Go project so all tests pass.",
  "repo": "./examples/buggy-go",
  "success": "go test ./..."
}
```

Aletheia debe:

1. leer task,
2. correr tests,
3. detectar falla,
4. abrir archivo,
5. proponer patch,
6. aplicar patch,
7. correr tests,
8. guardar evidencia,
9. responder con diff y resultado.

Este demo vale más que cualquier promesa.

---

## 13. El segundo demo

Indexar documentación local:

```bash
aletheia index ./docs
aletheia run --prompt "qué decisión tomamos sobre el selector?"
```

Debe responder usando memoria local y citar el documento/chunk interno.

---

## 14. El tercer demo

Skill compression.

Repetir 20 tareas similares de bugs simples.

El sistema debe aprender una skill:

```text
skill: fix_simple_go_test_failure
trigger: go test failure with expected/actual mismatch
sequence:
  <ACT_RUN_TESTS>
  <ACT_PARSE_CODE>
  <ACT_MUTATE_CODE>
  <ACT_RUN_TESTS>
  <ACT_RESPOND>
success_rate: ...
```

En la tarea 21, debe resolver con menos pasos.

---

## 15. Qué NO hacer al principio

No empezar por:

- interfaz web linda,
- chat UI,
- distributed training,
- Metal kernels,
- MoE,
- multimodal,
- millones de features,
- integración cloud,
- “AGI claims”,
- entrenar 1B parámetros desde el día uno.

Primero demostrar:

```text
modelo propio pequeño + VM + verificador + memoria > modelo propio solo
```

Si eso se cumple, el camino está vivo.

---

## 16. Visión de mediano plazo

Cuando el sistema base funcione:

### A. Ternary native model

Entrenar variante con pesos ternarios:

```text
{-1, 0, +1}
```

Objetivo:

- memoria mínima,
- CPU fast,
- Mac mini friendly.

### B. Mixture of micro-experts

No un modelo gigante.

Varios expertos chicos:

```text
planner
coder
retriever
verifier-predictor
summarizer
selector
```

Router local decide cuál activar.

### C. Latent action model

Reducir razonamiento textual.

Más tokens funcionales, menos palabras.

### D. Persistent overnight learning

Cuando la máquina está idle:

- reintentar tareas fallidas,
- generar tests,
- mejorar selector,
- comprimir skills,
- evaluar regresiones.

### E. Benchmark privado contra frontier models

Comparar:

- GPT-5.5,
- Claude Opus 4.7,
- Aletheia-μ local.

Solo en tareas verificables.

La métrica principal:

```text
verified_success_rate / dollar / privacy_cost
```

---

## 17. Manifiesto técnico

Aletheia-μ no es un chatbot.

Es una máquina de verdad local.

No intenta sonar inteligente.

Intenta reducir incertidumbre.

No responde porque “cree”.

Responde porque verificó.

No memoriza internet.

Construye una memoria causal de tu mundo.

No necesita ser gigante.

Necesita estar conectada a realidad, tests, herramientas y experiencia.

La revolución no es un modelo pequeño que imita a uno grande.

La revolución es un sistema pequeño que hace algo que los grandes no hacen por defecto:

```text
vivir en tu computadora,
aprender de tus errores,
verificar sus afirmaciones,
y mejorar cada noche.
```

---

## 18. Prompt para el agente de coding

Usar este prompt directamente:

```text
Quiero que construyas el repositorio Aletheia-μ en Go.

No es un wrapper de modelos existentes. No uses modelos chinos. No dependas de APIs remotas. El objetivo inicial es una arquitectura local desde cero: tokenizer propio, micro-transformer propio, runtime propio, Cognitive VM, selector, memoria local, verificadores y evaluación.

Prioridad absoluta:
1. Correctitud.
2. Tests.
3. Arquitectura modular.
4. Demo verificable.
5. Simplicidad.

Crea el monorepo con esta estructura:

aletheia/
  cmd/aletheia/
  internal/tokenizer/
  internal/tensor/
  internal/model/
  internal/runner/
  internal/cognitivevm/
  internal/selector/
  internal/verifier/
  internal/search/
  internal/memory/
  internal/retriever/
  internal/training/
  internal/eval/
  configs/
  datasets/
  evals/
  checkpoints/
  examples/
  docs/

Implementa primero:
- CLI básico.
- tokenizer byte-level con tokens funcionales.
- memoria SQLite mínima.
- verifier para go test.
- task runner sandboxed con allowlist.
- modelo dummy que pueda emitir acciones funcionales hardcodeadas.
- demo examples/buggy-go que arregle un bug simple pasando go test.

Después implementa:
- tensor float32 mínimo.
- micro-transformer decoder-only.
- training loop con overfit de 100 ejemplos.
- runner con top-k logits.
- selector heurístico.
- Cognitive VM ejecutando acciones.
- memory logging de episodios/evidencia.
- eval suite bootstrap.

No hagas UI web. No optimices kernels al principio. No agregues features que no sirvan al primer demo.

La definición de éxito del primer milestone:
`go test ./...` pasa en el repo principal y `aletheia solve --task examples/buggy-go/task.json` produce un patch que hace pasar los tests del ejemplo, guardando evidencia en SQLite.

La definición de éxito del segundo milestone:
un modelo propio seed-10m puede overfittear 100 ejemplos de secuencias de acciones y participar en el loop de la Cognitive VM.

La definición de éxito del tercer milestone:
el selector + verificador resuelve más tareas bootstrap que greedy decoding.

Documenta cada decisión en docs/decisions.md.
Cada módulo debe tener tests.
Cada acción que toque archivos debe producir diff.
Cada verificador debe guardar Evidence.
Si una tarea no puede verificarse, el sistema debe abstenerse explícitamente.
```

---

## 19. Primer issue para crear en el repo

```md
# Milestone 0: Aletheia-μ skeleton

## Goal

Create the initial Go repository for Aletheia-μ: a local, verifier-first cognitive architecture with a future custom micro-transformer.

## Scope

- CLI
- tokenizer
- memory
- verifier
- task format
- toy Go bug demo
- tests

## Tasks

- [ ] Initialize Go module.
- [ ] Create directory structure.
- [ ] Implement byte-level tokenizer with functional tokens.
- [ ] Implement CLI with `init`, `solve`, `eval`.
- [ ] Implement SQLite memory schema.
- [ ] Implement Evidence object.
- [ ] Implement command allowlist.
- [ ] Implement Go test verifier.
- [ ] Create `examples/buggy-go`.
- [ ] Create `examples/buggy-go/task.json`.
- [ ] Implement a dummy solver that can patch the toy bug.
- [ ] Store episode and verifier result in memory.
- [ ] Add tests for tokenizer, memory and verifier.
- [ ] Document architecture in `docs/architecture.md`.

## Definition of Done

- `go test ./...` passes.
- `go run ./cmd/aletheia solve --task examples/buggy-go/task.json` fixes the toy project.
- Evidence is written to SQLite.
- The final response includes the patch and verifier result.
```

---

## 20. First principles reminder

If the model is weak, make the world do more work.

If generation is uncertain, branch.

If branches are many, verify.

If verification is expensive, learn a selector.

If a trajectory succeeds, compress it into a skill.

If a claim lacks evidence, abstain.

If a giant model wins in pass@1, beat it with pass@N + reality.

That is Aletheia-μ.
