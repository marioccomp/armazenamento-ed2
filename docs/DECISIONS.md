# Registro de decisões de arquitetura (ADRs)

Formato: contexto → decisão → alternativas → consequências.

---

## ADR-001 — Log append-only + índice hash em memória (Bitcask)

**Contexto.** A E1 exige PUT/GET/DELETE persistentes e recuperação após crash.
Correção vem antes de desempenho.

**Decisão.** Um único `data.log` onde só se escreve no final; PUT e DELETE
(lápide) viram registros; o índice `map[uint64]location` fica em RAM e é
reconstruído percorrendo o log no `Open`.

**Alternativas.** B+ Tree em disco desde a E1 (mais complexa, exige páginas e
atualização in-place com cuidado de atomicidade); LSM Tree completa (prematuro).

**Consequências.** Nunca se sobrescreve dado válido, então um crash só corrompe
o último registro. Escrita sequencial é rápida. Em compensação: o índice inteiro
fica em RAM, o arquivo só cresce (sem compactação) e a recuperação é linear no
tamanho do log. O log evolui para o WAL de uma LSM na E2.

---

## ADR-002 — Formato do registro e validações no `internal/record`

**Contexto.** SPEC §4.3 define o layout (`crc|key|type|vlen|value`, LE, CRC32-C),
mas deixa em aberto o limite de valor e o que fazer com combinações estranhas.

**Decisão.**
- `MaxValueSize = 64 MiB`. Um `vlen` maior é tratado como corrupção, para que
  lixo no fim do arquivo não provoque uma alocação gigante.
- Uma lápide (`type = 2`) com `vlen != 0` é registro **inválido**
  (`ErrTombstoneValue`). Isso só pode acontecer por corrupção, então
  rejeitar torna a detecção mais rígida.
- O tipo, o `vlen` e a lápide são validados **antes** do CRC (`DecodeHeader`),
  para que o leitor nunca tente ler `vlen` bytes de um cabeçalho absurdo.
- A API tem duas etapas (`DecodeHeader` + `Header.Verify`), que servem à
  varredura do arquivo (lê 17 B e depois o valor), além do `Decode` sobre um
  buffer completo.
- `Decode` devolve o `Value` apontando para dentro do buffer, sem cópia. Quem
  guarda o valor precisa copiá-lo.

**Alternativas.** Limite menor (ex.: 16 MiB), que pode recusar um workload
legítimo de valores grandes; ou nenhum limite, o que deixaria um `vlen`
corrompido virar um `make([]byte, 4 GiB)`.

**Consequências.** Valores acima de 64 MiB são recusados no PUT. Se o professor
divulgar um limite diferente, basta trocar a constante. Os testes cobrem a troca
de 1 bit em qualquer posição, todos os prefixos truncados e um fuzz de `Decode`.
