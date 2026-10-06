# ED2 — Adaptive Storage Engine (contexto para o Claude Code)

Trabalho de Estruturas de Dados 2 (UFRRJ), feito em grupo de 3. Construímos um
**storage engine chave-valor persistente** em Go, em três entregas cumulativas:

| Entrega | Tema                                                                  | Peso | Status           |
| ------- | --------------------------------------------------------------------- | ---- | ---------------- |
| E1      | Persistent Storage Engine (PUT/GET/DELETE, persistência, recuperação) | 25%  | **em andamento** |
| E2      | Indexed Storage Engine (SCAN ordenado, índice em memória secundária)  | 30%  | futuro           |
| E3      | Adaptive Storage Engine (eficiência sob workloads mutáveis)           | 45%  | futuro           |

- Enunciado completo: `docs/ENUNCIADO.md`
- Especificação técnica da E1 (o que construir, formato de disco, critérios de aceite): `docs/SPEC.md`
- **Leia `docs/SPEC.md` antes de qualquer mudança no engine.**

## Regras inegociáveis

1. **Proibido** usar DBMS ou storage pronto (SQLite, BoltDB, Badger, Pebble, LevelDB, RocksDB...).
   O formato de disco é nosso. Use só a biblioteca padrão do Go, salvo decisão registrada em `docs/DECISIONS.md`.
2. **Correção vem antes de desempenho.** Uma resposta errada zera o desempenho naquele workload.
3. **Nenhuma resposta pode ser escrita no output antes da operação estar durável no disco**
   (ver "Política de durabilidade" na SPEC).
4. A chave é `uint64` em toda a pilha. **Nunca** deixe o JSON decodificar a chave como `float64`,
   porque chaves acima de 2^53 perdem precisão.
5. Toda decisão de arquitetura não óbvia vira uma entrada em `docs/DECISIONS.md`
   (contexto → decisão → alternativas → consequências). O grupo precisa defender as escolhas na apresentação.

## Convenções

- Go ≥ 1.22, módulo `github.com/ed2-ufrrj/ed2-engine` (placeholder; trocar pelo nome real do grupo se houver repositório remoto).
- Identificadores em inglês; comentários, docs e mensagens de commit em **português**.
- `gofmt` + `go vet` limpos; `go test ./...` passando antes de qualquer commit.
- Commits pequenos e descritivos, um por passo do checklist da SPEC.
- Testes novos para todo bug corrigido.

## Comandos úteis

```bash
go build -o bin/engine ./cmd/engine
go test ./...
go test -race ./...
./scripts/crashtest.sh          # teste de crash com kill -9
go run ./cmd/gen --help         # gerador de workloads
```

## Estado atual

Mantenha esta seção atualizada ao fim de cada sessão de trabalho:
o que foi feito, o que falta e decisões pendentes.

- [x] E1 iniciada (ver checklist em `docs/SPEC.md`)
- [x] Passo 1: esqueleto (go.mod, layout, Makefile, CLI com 4 subcomandos; `describe` funcionando)
- [x] Passo 2: `internal/record` (encode/decode + CRC32-C, ADR-002)
- [x] Passo 3: `internal/storage` (Init atômico, Open com recuperação, Put/Get/Delete/Close com SyncAlways; `engine init` ligado; ADR-003)
- [x] Passo 4: testes de recuperação (truncamento em todo offset, lixo no fim, corrupção no meio, reaberturas)
- [ ] Próximo: passo 5 (`internal/oracle` + teste diferencial)
- Obs.: `go test -race` não roda nesta máquina (MinGW 32-bit no PATH; o race exige gcc 64-bit)
