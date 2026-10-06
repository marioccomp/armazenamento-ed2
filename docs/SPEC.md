# Especificação técnica — Entrega 1 (Persistent Storage Engine)

Este documento define **o que** o engine da E1 faz e **como** ele grava os dados.
Quando algo aqui conflitar com o enunciado (`docs/ENUNCIADO.md`), o enunciado vence;
registre o conflito em `docs/DECISIONS.md`.

---

## 1. Visão geral da arquitetura

**Log estruturado append-only + índice hash em memória** (modelo "Bitcask").

- Todos os dados ficam em um arquivo de log (`data.log`) onde **só se escreve no final**.
- PUT acrescenta um registro com o valor novo. A versão mais recente de uma chave é a que vale.
- DELETE acrescenta uma **lápide** (tombstone). Nada é apagado fisicamente.
- Na RAM fica só o índice `map[uint64]location` (onde está o valor no arquivo). Os valores nunca ficam inteiros em memória.
- Ao abrir, o engine percorre o log, reconstrói o índice e **trunca o fim do arquivo no primeiro registro inválido**, que é a sobra de um crash.

**Por que essa arquitetura na E1** (registrar em `docs/DECISIONS.md` como ADR-001):

- É a forma mais simples de garantir atomicidade por registro e recuperação após crash. Nunca se sobrescreve dado válido, então um crash só pode corromper o _último_ registro.
- Escrita sequencial é o padrão mais rápido para disco.
- Evolui naturalmente para a E2: o log vira o WAL de uma LSM Tree (memtable + SSTables ordenadas → SCAN). A alternativa é manter o log e adicionar uma B+ Tree.

**Limitações conhecidas** (documentar, não resolver na E1):

- O índice inteiro em RAM ocupa ~16 B de payload por chave, mais o overhead do `map`. Isso não escala para datasets enormes sob limite de memória. A E2 resolve.
- O arquivo só cresce: versões antigas e lápides ocupam espaço. A compactação fica para a E2/E3.
- O tempo de recuperação é proporcional ao tamanho do log. Um checkpoint ou _hint file_ fica para depois.

---

## 2. Estrutura do repositório

```
ed2-engine/
├── CLAUDE.md
├── README.md                  # como compilar, rodar, testar (para o professor)
├── go.mod
├── Dockerfile                 # multi-stage; binário estático em /engine
├── Makefile                   # build, test, crashtest, bench, docker
├── cmd/
│   ├── engine/main.go         # CLI: init | run | verify | describe
│   └── gen/main.go            # gerador de workloads JSONL (seed reprodutível)
├── internal/
│   ├── record/                # codificação/decodificação do registro + CRC
│   ├── storage/               # Engine: Open, Put, Get, Delete, Close, recuperação, verify
│   ├── protocol/              # leitura do workload JSONL e escrita das respostas
│   └── oracle/                # implementação de referência em memória (só para testes)
├── scripts/
│   ├── crashtest.sh           # loop: roda, kill -9 em tempo aleatório, verifica
│   └── bench.sh               # roda benchmarks e gera CSV em results/
├── results/                   # CSVs dos experimentos (versionados)
└── docs/
    ├── ENUNCIADO.md
    ├── SPEC.md                # este arquivo
    ├── ARCHITECTURE.md        # explicação para humanos, com diagrama do formato de disco
    ├── DECISIONS.md           # ADRs numerados
    ├── EXPERIMENTS.md         # metodologia, tabelas, gráficos, análise
    ├── ROADMAP.md             # ideias para E2 e E3
    └── AI_ASSISTED.md         # seção "AI-Assisted Engineering" (exigida na entrega final)
```

Mantenha `internal/storage` sem dependência de `protocol`: o engine não sabe nada de JSON.

---

## 3. Interface de linha de comando

Seguir o formato de referência do enunciado:

```
engine init     --data-dir DIR
engine run      --data-dir DIR --input FILE --output FILE [--sync batch|always|none] [--batch-size N]
engine verify   --data-dir DIR
engine describe
```

| Comando    | Comportamento                                                                                                                                                         | Saída / exit code                                                             |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `init`     | Cria `DIR` se não existir e um `data.log` vazio com cabeçalho. Se já existir um diretório válido, não apaga nada (idempotente).                                       | 0 sucesso; ≠0 erro                                                            |
| `run`      | Abre o engine (com recuperação), processa o workload em ordem e escreve uma resposta por operação, **na mesma ordem**. Se `DIR` não estiver inicializado, inicializa. | 0 sucesso; ≠0 erro fatal de E/S                                               |
| `verify`   | **Somente leitura.** Percorre o log, checa cabeçalho, CRCs e tamanhos, e reporta. Não trunca nada.                                                                    | Imprime JSON com o resultado (ver §7); 0 se íntegro, 1 se encontrou corrupção |
| `describe` | Imprime JSON descrevendo o engine (ver §8).                                                                                                                           | 0                                                                             |

Logs e diagnósticos vão para **stderr**. O stdout e o arquivo de output são só para o protocolo.

---

## 4. Formato em disco

### 4.1 Diretório de dados

```
DIR/
└── data.log
```

Na E1 existe um único arquivo. Se algum dia houver mais (E2+), crie um `MANIFEST`.

### 4.2 Cabeçalho do arquivo (16 bytes, no offset 0)

| Offset | Tamanho | Campo      | Valor                   |
| ------ | ------- | ---------- | ----------------------- |
| 0      | 8       | magic      | `"ED2KVLOG"` (ASCII)    |
| 8      | 4       | version    | `1` (uint32 LE)         |
| 12     | 4       | header_crc | CRC32-C dos bytes 0..11 |

`init` grava o cabeçalho num arquivo temporário, faz `fsync` e `rename` para `data.log`,
e por fim `fsync` do diretório. Assim, ou o arquivo existe completo ou não existe.
Um cabeçalho inválido é erro fatal: **não** truncar nem recriar automaticamente.

### 4.3 Registro

Todos os inteiros em **little-endian**.

```
+---------+---------+--------+-----------+-----------------+
| crc u32 | key u64 | type u8| vlen u32  | value [vlen]    |
+---------+---------+--------+-----------+-----------------+
  4 B       8 B       1 B      4 B         vlen B
  └──────────── cabeçalho do registro: 17 B ──────────────┘
```

- `crc` = **CRC32-C (Castagnoli)** de `key‖type‖vlen‖value`. Use `hash/crc32` com `crc32.MakeTable(crc32.Castagnoli)`, que tem aceleração por hardware.
- `type`: `1` = PUT, `2` = DELETE (lápide, `vlen = 0`). Qualquer outro valor é registro inválido.
- `vlen ≤ MaxValueSize` (constante, ex.: 64 MiB; documentar). Um valor acima disso é tratado como corrupção.
- `value`: bytes UTF-8 da string recebida no JSON, exatamente como recebidos. **Valor vazio (`""`) é válido** e diferente de "não existe".

### 4.4 Índice em memória

```go
type location struct {
    offset int64  // posição do primeiro byte do value
    size   uint32 // vlen
}
index map[uint64]location
```

---

## 5. Semântica das operações

| Operação                           | Efeito                                                                                  | Resposta                                                                  |
| ---------------------------------- | --------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `put(k, v)`                        | Acrescenta um registro PUT e atualiza o índice. Sobrescreve se `k` existir.             | `{"id":N,"status":"ok"}`                                                  |
| `get(k)`                           | Lê o valor via índice.                                                                  | `{"id":N,"status":"ok","value":"..."}` ou `{"id":N,"status":"not_found"}` |
| `delete(k)`                        | Se `k` existe: acrescenta a lápide e remove do índice. Se não existe: não escreve nada. | `{"id":N,"status":"ok"}` ou `{"id":N,"status":"not_found"}` ⚠️            |
| `scan`                             | Fora do escopo da E1.                                                                   | `{"id":N,"status":"error","error":"scan not supported"}` ⚠️               |
| op desconhecida / linha malformada | Não altera estado.                                                                      | `{"id":N,"status":"error","error":"..."}` (se o id for legível)           |

⚠️ = **pendente de confirmação** com a especificação formal do professor (ver §12).
Isole essas escolhas em um único lugar no código para trocar fácil.

---

## 6. Política de durabilidade (crítico)

**Invariante:** uma resposta só é escrita no arquivo de output depois que todos os registros
das operações até ela estão em disco com `fsync`.

Modos (`--sync`):

- `batch` (**padrão**), também chamado _group commit_: as escritas no log passam por um buffer.
  A cada `--batch-size` operações (padrão 1024) **e** no fim do workload, o engine faz:
  1. flush do buffer do log;
  2. `fsync` do log;
  3. flush das respostas acumuladas para o output.
- `always`: `fsync` a cada escrita (lento; usado como baseline nos experimentos).
- `none`: sem `fsync` (só para medir o custo do fsync; **nunca** o padrão).

**Leituras e buffer:** um `GET` precisa enxergar escritas que ainda estão no buffer.
Solução simples: se o `offset` da chave for maior ou igual ao fim já descarregado,
faça flush do buffer (sem fsync) antes do `ReadAt`. Documente a escolha.

---

## 7. Recuperação e verificação

### Recuperação (em todo `Open`)

1. Validar o cabeçalho do arquivo.
2. Percorrer os registros sequencialmente a partir do offset 16:
   - cabeçalho do registro incompleto → parar;
   - `type` inválido ou `vlen > MaxValueSize` → parar;
   - valor incompleto → parar;
   - CRC divergente → parar;
   - caso contrário, aplicar no índice (PUT insere/atualiza, DELETE remove).
3. Se sobrar algo depois do último registro válido: logar em stderr quantos bytes foram descartados,
   `Truncate` para o fim válido e `fsync`.

> Decisão consciente: parar no **primeiro** registro inválido. Isso é correto para o crash
> (só o último registro pode estar parcial). Corrupção no _meio_ do arquivo, como bit flip de disco,
> faria descartar dados válidos posteriores. Isso fica documentado como limitação, e `verify` serve para detectar.

### `verify` (somente leitura)

Mesma varredura, sem modificar nada. Imprime em stdout:

```json
{
  "ok": true,
  "records": 12345,
  "live_keys": 9876,
  "valid_bytes": 1048576,
  "file_bytes": 1048576,
  "tail_garbage_bytes": 0,
  "error": null
}
```

---

## 8. `describe`

```json
{
  "name": "ed2-engine",
  "version": "1.0.0",
  "delivery": 1,
  "language": "go",
  "architecture": "append-only log + in-memory hash index (Bitcask-style)",
  "durability": "group commit (fsync per batch)",
  "integrity": "CRC32-C per record",
  "operations": ["put", "get", "delete"]
}
```

---

## 9. Protocolo JSONL — cuidados de implementação

- Ler com `bufio.Reader` e `ReadBytes('\n')`, ou com `bufio.Scanner` com buffer grande.
  O `Scanner` padrão quebra em linhas maiores que 64 KiB, e valores grandes são uma família de teste.
- Decodificar `key`, `start` e `end` direto em `uint64` (struct tipada) ou com `UseNumber()`.
  **Nunca** via `interface{}`/`float64`.
- Não escrever respostas com `map[string]any`: a ordem dos campos fica aleatória e é mais lento.
  Use structs com tags `json:",omitempty"`.
- `encoder.SetEscapeHTML(false)`, para que `<`, `>` e `&` saiam literais.
- Escrever as respostas com `bufio.Writer` respeitando a invariante do §6.
- Strings com caracteres não-ASCII e escapes (`\n`, `\"`, `é`) devem sair idênticas
  semanticamente ao que entrou. Faça teste de ida e volta.

---

## 10. Testes (critérios de aceite)

### 10.1 Unitários (`internal/record`, `internal/storage`)

- Encode/decode de registro: ida e volta; CRC detecta 1 bit trocado em qualquer posição.
- Engine: put/get/delete/sobrescrita/valor vazio/valor de vários MB/chave `0` e `math.MaxUint64`.
- Reabrir após `Close` preserva o estado.
- Recuperação: para **cada** offset de truncamento possível em um log pequeno, reabrir e conferir que o estado
  é igual ao de algum prefixo das operações (teste exaustivo por offset).
- Lixo aleatório no fim é descartado; lixo no cabeçalho do arquivo dá erro fatal.

### 10.2 Teste diferencial contra o oracle

- `internal/oracle`: um `map[uint64]string` simples.
- Gerar N operações aleatórias (seed fixa), aplicar no engine e no oracle, e comparar toda resposta.
  Fechar e reabrir o engine em pontos aleatórios no meio da sequência.

### 10.3 Teste de crash (`scripts/crashtest.sh`)

Loop com X iterações:

1. gerar um workload com seed `i`;
2. iniciar `engine run` em background e dar `kill -9` após um tempo aleatório;
3. rodar `engine verify` (deve dar exit 0 depois que um `Open` recuperar) e um workload de GETs de todas as chaves tocadas;
4. **critério:** o estado final corresponde ao de um prefixo `P` do workload, com `P` ≥ número de respostas
   já gravadas no output antes do kill. Nada confirmado pode sumir, e nada pode aparecer pela metade.

### 10.4 Race detector

- `go test -race ./...` limpo.

---

## 11. Experimentos (para `docs/EXPERIMENTS.md`)

Gerar os workloads com `cmd/gen` (seed, nº de ops, nº de chaves, distribuição `uniform|sequential|zipf`,
tamanho de valor fixo ou faixa, proporção put/get/delete). Os resultados vão em CSV em `results/`.

Medições mínimas para a E1:

1. **Throughput** (ops/s) de put-only, get-only e misto 50/50, para valores de 100 B, 1 KiB e 64 KiB.
2. **Custo do fsync**: `--sync always` vs `batch` (vários `--batch-size`) vs `none`.
3. **Tempo de recuperação** em função do tamanho do log (ex.: 10 MB, 100 MB, 1 GB).
4. **Amplificação de espaço**: tamanho do log ÷ tamanho dos dados vivos, em workloads com muitas sobrescritas.
5. **Pico de memória** (RSS) em função do número de chaves.

Para cada experimento, registrar: máquina, versão do Go, comando exato, seed, média de ≥3 execuções.
A análise deve ligar os números às decisões de arquitetura (ex.: "a amplificação cresce linearmente com as
sobrescritas porque não há compactação, o que motiva a compactação na E2").

---

## 12. Pendências do professor (não inventar; parametrizar)

- [ ] Formato de submissão (Docker? caminho `/engine`?), limites de RAM/disco e escala dos datasets.
- [ ] JSON Schema formal: resposta de `delete` em chave inexistente, formato de erro, campos extras permitidos.
- [ ] O que conta como "durável" no teste de crash do ED2Bench.
- [ ] Workloads públicos e a versão do ED2Bench.

Quando sair alguma dessas, atualize esta seção e o código correspondente.

---

## 13. Checklist de implementação (ordem sugerida)

Cada item = um commit, com testes passando.

1. [x] Esqueleto: `go.mod`, layout de pastas, `Makefile`, `cmd/engine` com os 4 subcomandos vazios, `describe` funcionando.
2. [x] `internal/record`: encode/decode + CRC32-C + testes de ida e volta e de bit flip.
3. [x] `internal/storage`: `init` atômico, cabeçalho do arquivo, `Open` com recuperação, `Put`/`Get`/`Delete`/`Close` com `--sync always`.
4. [ ] Testes de storage: reabrir, truncamento exaustivo por offset, lixo no fim.
5. [ ] `internal/oracle` + teste diferencial com reaberturas aleatórias.
6. [ ] `internal/protocol` + `engine run` completo; teste de ida e volta com strings Unicode/escapes e valores grandes.
7. [ ] Group commit (`--sync batch`, `--batch-size`) respeitando a invariante do §6, com o GET enxergando o buffer.
8. [ ] `engine verify`.
9. [ ] `cmd/gen` (gerador de workloads).
10. [ ] `scripts/crashtest.sh` rodando ≥100 iterações sem falha.
11. [ ] `Dockerfile` multi-stage (binário estático, `CGO_ENABLED=0`) com o executável em `/engine`.
12. [ ] `scripts/bench.sh` + experimentos do §11 + `docs/EXPERIMENTS.md`.
13. [ ] `docs/ARCHITECTURE.md`, `docs/DECISIONS.md`, `README.md` finais.
