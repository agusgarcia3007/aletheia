# Aletheia y Modelos Pequeños: Investigación y Tesis Técnica

Este documento resume la investigación relevante sobre modelos pequeños y
explica por qué Aletheia puede ser un cambio importante si se mantiene fiel a su
arquitectura: modelo chico, router entrenable, herramientas, verificadores,
memoria, research y aprendizaje por trayectorias.

La conclusión central es simple: Aletheia no debe competir como "un GPT
pequeño que sabe todo". Debe competir como un **agente verificado pequeño**:
menos conocimiento paramétrico, más evidencia, más herramientas, más feedback
determinístico y mejor aprendizaje local.

## Tesis Corta

Los modelos pequeños son útiles cuando el sistema deja de pedirles que hagan el
trabajo de un modelo gigante. Un modelo chico no debería memorizar el mundo,
adivinar hechos, simular un IDE completo ni reparar código a ciegas. Debe
aprender a:

- entender intención;
- elegir modo de trabajo;
- pedir herramientas correctas;
- usar memoria y evidencia;
- abstenerse cuando falta soporte;
- generar respuestas en dominios acotados;
- mejorar con feedback verificable.

Aletheia ya tiene varias piezas que los papers sugieren como esenciales:

- verifiers determinísticos (`go test`, parseo, citation verifier);
- memoria persistente local;
- research con fuentes;
- graph de causalidad;
- beam/MCTS sobre acciones;
- selector entrenable;
- skills comprimidas;
- OpenAI-compatible API;
- router y answerers antes de retrieval/research.

La oportunidad es cerrar el loop: **generate -> verify -> reward -> learn**.

## Qué Enseñan Los Papers

### 1. Phi / Textbooks: la calidad de datos pesa más que el tamaño bruto

Fuente: [Textbooks Are All You Need](https://arxiv.org/abs/2306.11644).

Phi muestra que un modelo relativamente pequeño puede rendir fuerte en código si
se entrena con datos de calidad tipo textbook: explicaciones limpias, ejercicios,
soluciones, patrones correctos y poco ruido. La lección para Aletheia:

- no entrenar Mikros con web bruto;
- no entrenar sólo con diálogos casuales;
- generar curriculum de alta densidad;
- incluir explicación, contraejemplo, test y verificación;
- preferir ejemplos pequeños y correctos a corpus masivos sin estructura.

Traducción a Aletheia:

- `dataset build --profile mikros-live-v1` debe producir ejemplos con intent,
  slots, expected mode y respuesta esperada;
- coding debe venir de mini-lecciones estilo textbook;
- repair debe venir de failure -> diagnosis -> patch -> verifier pass;
- research debe venir de pregunta -> evidencia -> canonical answer -> citas.

### 2. TinyStories: los modelos diminutos hablan bien sólo en dominios controlados

Fuente: [TinyStories](https://arxiv.org/abs/2305.07759).

TinyStories demuestra que modelos muy pequeños pueden generar lenguaje coherente
si el dominio, vocabulario y distribución de datos están muy controlados. La
lección no es "un modelo diminuto puede hacer todo"; es la opuesta:

- con dominio controlado, el modelo parece mucho más capaz;
- sin dominio controlado, se degrada rápido;
- la evaluación debe medir capacidades concretas, no sensación de fluidez.

Traducción a Aletheia:

- Mikros debe tener un estilo y rol bien definidos;
- no debe responder hechos sin evidence path;
- no debe ir a research para código local simple;
- no debe pegar chunks como respuesta;
- cada modo debe tener límites explícitos.

### 3. SmolLM2: los modelos pequeños buenos son data-centric

Fuente: [SmolLM2](https://arxiv.org/abs/2502.02737).

SmolLM2 refuerza una idea clave: un modelo pequeño competitivo no nace sólo de
reducir parámetros. Requiere dataset curado, mezcla de datos, sobreentrenamiento
cuidadoso, post-training y evaluación fuerte. La lección para Aletheia:

- el cuello de botella inicial no es sólo arquitectura;
- el dataset vivo y los evals son el producto;
- sin gates, un checkpoint "mejor" puede ser peor en seguridad;
- hay que promover modelos sólo si mejoran métricas sin regresiones.

Traducción a Aletheia:

- `evals/mikros_live`, `evals/mikros_artifact` y `evals/production` son gates,
  no decoración;
- `learn` debe exportar ejemplos reales desde memoria;
- la promoción de checkpoints debe ser explícita;
- no conviene escalar a 150M/360M sin evidencia de mejora.

### 4. MobileLLM: para sub-1B importan arquitectura y uso en dispositivo

Fuente: [MobileLLM](https://arxiv.org/abs/2402.14905).

MobileLLM muestra que modelos sub-billion pueden ser útiles para casos on-device
si la arquitectura está optimizada y el objetivo es realista: latencia baja,
costo bajo, uso local, API/tool calling y tareas comunes. La lección para
Aletheia:

- un VPS CPU no va a competir por fuerza bruta;
- sí puede competir por latencia, costo, privacidad y auditabilidad;
- el modelo debe ser pequeño pero asistido por tools;
- contexto y límites deben anunciarse honestamente.

Traducción a Aletheia:

- Mikros debe ser rápido y estable antes que "profundo";
- la API debe exponer contexto real, no prometer 65K si no existe;
- tools y memoria compensan parámetros;
- cuantización y `TransformerV2` importan después de tener gates.

### 5. Distilling Step-by-Step: racionales ayudan a modelos chicos

Fuente: [Distilling Step-by-Step](https://arxiv.org/abs/2305.02301).

El paper muestra que modelos pequeños aprenden mejor cuando reciben no sólo la
respuesta, sino también señales intermedias o racionales. La lección para
Aletheia:

- no guardar sólo final answers;
- guardar trayectoria, intención, evidencia, patch, verifier output y reward;
- entrenar router/selector/ranker con pasos intermedios;
- usar traces verificados como material de aprendizaje.

Traducción a Aletheia:

- cada solve exitoso debe generar ejemplos de selector/router/repair;
- cada research exitoso debe generar query -> claims -> answer -> sources;
- cada tool loop exitoso debe generar tool-use examples;
- DPO/SFT futuro debe usar pares positivos/negativos desde evals.

### 6. RAG: conocimiento externo debe vivir fuera de los pesos

Fuente: [Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks](https://arxiv.org/abs/2005.11401).

RAG demuestra que tareas de conocimiento se benefician de combinar modelo con
memoria no paramétrica. La lección para Aletheia:

- los hechos cambiantes no deben estar en pesos;
- deben estar en memoria con fuente, fecha y confianza;
- la respuesta debe citar evidencia;
- si no hay evidencia, hay que buscar o abstenerse.

Traducción a Aletheia:

- SearXNG llena memoria, no "conversa" directamente;
- `research_jobs.answer` debe guardar canonical answer;
- `web_sources` y `web_claims` son más importantes que texto suelto;
- chat nunca debe mostrar `chunk=...` como producto.

### 7. Toolformer, ReAct y Gorilla: el modelo debe saber usar herramientas

Fuentes:

- [Toolformer](https://arxiv.org/abs/2302.04761)
- [ReAct](https://arxiv.org/abs/2210.03629)
- [Gorilla](https://arxiv.org/abs/2305.15334)

Toolformer enseña que un modelo puede aprender cuándo llamar APIs, con qué
argumentos y cómo incorporar resultados. ReAct enseña que razonar y actuar se
potencian cuando se intercalan. Gorilla muestra que API calling puede entrenarse
como capacidad central.

La lección para Aletheia:

- el agente no debe "hablar sobre tools"; debe pedir tools válidas;
- OpenCode debe recibir `assistant.tool_calls` correcto;
- tool results deben compactarse y usarse para responder;
- el servidor público no debe ejecutar herramientas peligrosas;
- la seguridad vive en el protocolo y en el cliente local.

Traducción a Aletheia:

- OpenCode V1 es proveedor pasivo;
- el cliente ejecuta tools localmente;
- Aletheia decide tool, args y siguiente paso;
- no hay shell server-side;
- no hay patch sin verifier.

### 8. CodeRL y DeepSeek-R1: el reward verificable es la clave

Fuentes:

- [CodeRL](https://arxiv.org/abs/2207.01780)
- [DeepSeek-R1](https://arxiv.org/abs/2501.12948)

CodeRL apunta al uso de reinforcement learning para código con feedback. R1
demuestra que RL con rewards claros puede inducir mejoras de razonamiento en una
base capaz. Para Aletheia, el insight más importante es este:

> El reward más valioso no necesita un modelo juez si el dominio tiene
> verificadores confiables.

Aletheia ya tiene rewards baratos y determinísticos:

- `go test` pasa o falla;
- `static_go_parse` pasa o falla;
- citation verifier valida o rechaza;
- tool protocol es válido o inválido;
- abstención correcta se puede evaluar;
- patch materializado requiere verifier pass.

Traducción a Aletheia:

- no empezar RL intentando mejorar chat general;
- empezar RL sobre selector, router, repair y tool policy;
- cada trayectoria tiene reward;
- cada failure enseña;
- cada solve verificado puede mejorar el sistema.

## Por Qué Aletheia Es Un Game Changer

Aletheia es interesante porque cambia la unidad de inteligencia. En vez de
apostar todo a un modelo gigante, distribuye la inteligencia en un sistema:

1. **Modelo chico** para routing, lenguaje, slots y tool policy.
2. **Memoria local** para conocimiento persistente.
3. **Research** para conocimiento externo verificable.
4. **Verifiers** para verdad operacional.
5. **Search** para explorar acciones.
6. **Skills** para comprimir trayectorias exitosas.
7. **Learning loop** para mejorar desde evidencia real.

Esto crea una forma distinta de competir:

- no gana por "saber más en pesos";
- gana por no inventar;
- gana por costo bajo;
- gana por auditabilidad;
- gana porque cada acción deja evidencia;
- gana porque cada éxito se puede convertir en dataset;
- gana porque el reward es gratis en dominios verificables.

Un modelo gigante puede responder con fluidez. Aletheia debe responder con
pruebas.

## Diferencial Frente A Un Chatbot General

Un chatbot general intenta contestar todo con el mismo mecanismo. Aletheia no.

| Pregunta | Chatbot general | Aletheia |
| --- | --- | --- |
| "Hola" | genera saludo | smalltalk local |
| "CSV en Python" | genera desde pesos | coding answerer local |
| "Quién ganó X" | puede inventar | memoria/research/cita/abstención |
| "Analiza repo" | simula si no tiene tools | pide tools o se niega |
| "Arregla tests" | sugiere patch | patch + verifier + rollback |
| "Mejora con uso" | depende de logs externos | exporta trajectories y rewards |

La ventaja no está en una respuesta aislada. Está en el ciclo completo:

```text
request
-> route intent
-> choose mode
-> retrieve/search/tool/verify
-> answer with provenance
-> store evidence
-> extract examples
-> train selector/router/ranker/model
-> run eval gates
-> promote only if better
```

## Qué NO Hay Que Hacer

Para no destruir la ventaja de Aletheia:

- no convertir Mikros en un diccionario gigante de respuestas;
- no permitir que factual/current caiga a generación libre;
- no responder con links sin síntesis;
- no guardar sólo respuestas finales;
- no entrenar con web bruto sin limpieza;
- no reemplazar verifiers por "confianza del modelo";
- no exponer shell remoto por API pública;
- no escalar parámetros antes de mejorar datos/evals;
- no anunciar capacidades de contexto o tools que no existen;
- no promover checkpoints sin eval before/after.

## Arquitectura Objetivo

La arquitectura viva debería quedar así:

```text
chat/api request
-> router entrenable
-> modo:
   - smalltalk
   - coding
   - math
   - translation
   - repo/tool-agent
   - factual/research
   - document QA
   - abstain
-> answerer/tool/retriever/research/verifier
-> canonical response
-> evidence + trajectory storage
-> learning export
-> eval gate
-> checkpoint promotion
```

El modelo no desaparece. Se vuelve más específico:

- router;
- planner;
- tool policy;
- answer style;
- code skeletons;
- repair hypothesis generator;
- compression of verified trajectories.

Los hechos y la verdad operacional viven fuera del modelo:

- SQLite memory;
- sources;
- claims;
- verifier output;
- graph causality;
- eval results.

## Roadmap Técnico Recomendado

### Fase 1: Router y Answerers Vivos

Estado: iniciado.

Objetivo:

- router entrenable con `datasets/router_mikros.jsonl`;
- answerers locales para coding/math/translation;
- research sólo para factual/current;
- eval `mikros_live`.

Por qué importa:

- corta respuestas absurdas;
- reduce dependencia del checkpoint legacy;
- evita research para prompts de código;
- da estructura para aprender desde logs.

### Fase 2: Research Como Memoria Verificada

Objetivo:

- canonical answers obligatorios;
- claims y sources confiables;
- stale policy;
- conflict detection;
- query-answer cache verificado;
- nunca chunks crudos en chat.

Por qué importa:

- Aletheia aprende hechos sin meterlos en pesos;
- futuras preguntas usan memoria local;
- el producto se vuelve más útil con el tiempo.

### Fase 3: Repair Verificable

Objetivo:

- más reglas Go reales;
- parser de failures;
- patch candidates pequeños;
- verifier obligatorio;
- trajectories exportables.

Por qué importa:

- coding verificable es el dominio donde Aletheia puede ganar antes;
- `go test` es reward perfecto;
- cada repo solve alimenta aprendizaje.

### Fase 4: Learning Loop Manual

Objetivo:

- `learn` exporta router examples, selector examples, tool traces, research QA,
  repair trajectories;
- entrena router/selector/ranker;
- prepara SFT/DPO simple;
- corre eval before/after;
- promueve sólo si mejora.

Por qué importa:

- convierte uso real en entrenamiento;
- evita regressions;
- mantiene control humano antes de daemon automático.

### Fase 5: TransformerV2 Promovido

Objetivo:

- tokenizer BPE real;
- `TransformerV2` como backend productivo;
- targets: 35M -> 150M -> 360M sólo con evidencia;
- cuantización CPU;
- checkpoints versionados.

Por qué importa:

- el modelo legacy no alcanza para lenguaje general;
- pero escalar sin router/evals sólo produce fluidez frágil;
- el modelo nuevo debe llegar después del curriculum.

## Métricas Que Definen Éxito

Mikros no debe medirse por "parece inteligente". Debe medirse por:

- `natural_answer_rate`;
- `links_only_rate = 0`;
- `raw_chunk_leakage = 0`;
- `false_verified_rate = 0`;
- `hallucination_rate`;
- `citation_validity`;
- `coding_language_accuracy`;
- `tool_loop_no_repeat`;
- `repair_pass_rate`;
- `abstention_accuracy`;
- `cost_per_success`;
- `seconds_per_success`.

Estas métricas hacen que Aletheia sea auditable. Un modelo gigante puede ser
mejor en conversación abierta, pero Aletheia puede ser mejor en confianza por
dólar en tareas verificables.

## Hipótesis De Producto

Aletheia puede ganar si se posiciona así:

> Un agente local pequeño que no intenta saber todo: investiga cuando falta
> evidencia, verifica cuando puede, aprende de trayectorias y deja todo auditable.

Casos donde puede ser superior:

- repos pequeños y medianos con tests;
- documentación interna;
- research privado con SearXNG interno;
- QA con citas;
- tareas repetibles que se comprimen en skills;
- equipos que necesitan privacidad/costo bajo;
- workflows donde "no sé" es mejor que inventar.

Casos donde no debe prometer superioridad:

- conversación creativa abierta;
- razonamiento largo sin herramientas;
- conocimiento mundial sin research;
- coding autónomo sobre repos grandes sin contexto suficiente;
- reemplazo general de frontier models.

## La Frase Clave

Aletheia no es un modelo pequeño intentando imitar a un gigante.

Aletheia es un **sistema pequeño que convierte verificación en aprendizaje**.

Ese es el game changer.

## Fuentes

- [Textbooks Are All You Need](https://arxiv.org/abs/2306.11644)
- [TinyStories: How Small Can Language Models Be and Still Speak Coherent English?](https://arxiv.org/abs/2305.07759)
- [SmolLM2: When Smol Goes Big](https://arxiv.org/abs/2502.02737)
- [MobileLLM: Optimizing Sub-billion Parameter Language Models for On-Device Use Cases](https://arxiv.org/abs/2402.14905)
- [Distilling Step-by-Step](https://arxiv.org/abs/2305.02301)
- [Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks](https://arxiv.org/abs/2005.11401)
- [Toolformer](https://arxiv.org/abs/2302.04761)
- [ReAct](https://arxiv.org/abs/2210.03629)
- [Gorilla](https://arxiv.org/abs/2305.15334)
- [CodeRL](https://arxiv.org/abs/2207.01780)
- [DeepSeek-R1](https://arxiv.org/abs/2501.12948)
