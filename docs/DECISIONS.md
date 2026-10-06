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

---

## ADR-003 — Engine da E1: escrita, falhas de E/S e `init`

**Contexto.** SPEC §4.2, §6 e §7. Passo 3 do checklist, com `--sync always`.

**Decisão.**
- A escrita usa `WriteAt(e.end)` e, em `SyncAlways`, `fsync` antes de
  retornar. O índice e `e.end` só avançam **depois** que a escrita (e o
  fsync) deram certo.
- **Erro de E/S é pegajoso.** Depois de uma falha de `write` ou `fsync`, o
  engine recusa todas as escritas seguintes. Após um fsync falho, o kernel pode
  ter descartado as páginas sujas ("fsyncgate"), e tentar de novo poderia
  confirmar uma operação que não está no disco. A recuperação correta é
  reabrir, e o `Open` então refaz o índice a partir do que está de fato no
  arquivo.
- Um valor maior que `MaxValueSize` **não** é falha de E/S: o `Put` devolve
  `ErrValueTooLarge` sem escrever nada, e o engine continua utilizável.
- `Get` lê com `ReadAt` sob `RLock`, então leituras concorrentes não se bloqueiam.
- `Close` faz `fsync`, exceto em `SyncNone`.
- `init` sobre um `data.log` existente apenas valida o cabeçalho. Se o
  cabeçalho for inválido, `init` falha (exit 1) e não recria nada, o mesmo
  comportamento do `Open`.
- O fsync do diretório no `init` é no-op no Windows (`syncdir_windows.go`):
  lá não dá para fazer `FlushFileBuffers` num diretório aberto com `os.Open`.
  No Linux (Docker, ambiente da entrega) ele é feito.

**Alternativas.** Usar `O_APPEND`, o que impediria reescrever sobre um
`write` parcial. Tentar de novo após um fsync falho, o que é incorreto pelo
motivo acima.

**Consequências.** Uma falha de disco derruba as escritas até reabrir, mas
nunca produz uma confirmação falsa. Não há lock entre processos: dois
`engine run` no mesmo diretório corromperiam o log. Isso fica fora do escopo
da E1.
