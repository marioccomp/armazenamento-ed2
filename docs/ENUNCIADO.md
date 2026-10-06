Trabalho De ED2— Adaptive Storage Engine Challenge

Grupo: grupos de três alunos
Entregas: três etapas cumulativas - datas a definir!

1. Visão geral

Neste trabalho, cada grupo projetará e implementará um mecanismo de armazenamento persistente (storage engine): um programa capaz de guardar, recuperar, remover e consultar dados em disco. O sistema deverá continuar correto após ser encerrado e reiniciado, operar sob limites de memória e lidar com conjuntos de dados maiores que a memória disponibilizada ao engine.

O objetivo não é reproduzir uma estrutura específica de dados. Cada grupo deverá justificar suas escolhas e demonstrar, experimentalmente, que seu engine é correto, robusto e adequado a diferentes padrões de carga.

O trabalho é cumulativo e será avaliado com apoio das ferramentas ED2Bench, além da documentação e da análise experimental do grupo. A modalidade operacional da avaliação automatizada será divulgada antes da primeira entrega. 2. Glossário inicial

Não é esperado que o grupo já domine todos os termos abaixo. Eles serão aprofundados durante a disciplina; esta lista serve como referência para a leitura do enunciado.
Termo Descrição breve
ACID Propriedades desejáveis para transações: atomicidade, consistência, isolamento e durabilidade. Não será exigida uma implementação ACID completa.
Benchmark Experimento controlado usado para medir desempenho, consumo de recursos ou robustez.
Baseline Implementação de referência usada como ponto de comparação; não é necessariamente a solução mais rápida.
Cache / buffer Área em RAM usada para manter temporariamente dados do disco e reduzir acessos repetidos.
Chave–valor Organização de dados em que uma chave identifica um valor associado.
Crash Interrupção abrupta de um programa ou ambiente de execução, sem encerramento normal.
Dataset Conjunto de dados manipulado pelos testes.
Engine Programa responsável por organizar, armazenar, localizar e recuperar dados persistentes.
Índice Estrutura auxiliar que acelera a localização de dados.
JSON Lines (JSONL) Arquivo de texto com um objeto JSON por linha.
Persistência Propriedade de manter dados após o programa ser encerrado ou reiniciado.
RAM Memória principal do computador: rápida, mas limitada e não persistente.
Recuperação Procedimento que restaura um estado consistente após um crash.
SCAN Consulta que retorna registros cujas chaves pertencem a um intervalo, em ordem de chave.
Seed Valor inicial de um gerador pseudorrandômico; permite reproduzir um workload.
Throughput Quantidade de operações concluídas por unidade de tempo.
uint64 Inteiro sem sinal de 64 bits; será o tipo inicial das chaves.
Workload Sequência de operações que simula uma forma de uso do engine.
Zipf Distribuição em que poucas chaves são muito acessadas e muitas são pouco acessadas. 3. Objetivos de aprendizagem

    Projetar estruturas de dados voltadas à memória secundária.
    Implementar persistência, recuperação e verificação de integridade.
    Administrar o uso de memória e espaço em disco.
    Escolher e justificar índices e organizações de arquivos.
    Medir desempenho de maneira reproduzível.
    Analisar compromissos entre tempo, espaço, escrita, leitura e robustez.

4. Entregas
   Entrega Tema Peso
   1 Persistent Storage Engine 25%
   2 Indexed Storage Engine 30%
   3 Adaptive Storage Engine 45%

Cada entrega deverá conter o código-fonte, o artefato de execução solicitado, a documentação exigida e os resultados experimentais correspondentes. Datas, modelo de submissão e rubrica detalhada serão divulgados separadamente. 5. Regras gerais do engine

    O engine manipulará pares (chave, valor) persistentes.
    A chave é um inteiro sem sinal de 64 bits (uint64).
    Os valores têm tamanho variável.
    Um PUT para uma chave existente substitui o valor anterior.
    Os dados devem permanecer disponíveis após reinicialização.
    Os datasets poderão exceder a memória disponibilizada ao engine.
    Não é permitido delegar o armazenamento principal a um DBMS pronto, como SQLite, RocksDB, LevelDB ou equivalente.
    Bibliotecas de propósito geral são permitidas, desde que não substituam o mecanismo de armazenamento que é objeto da disciplina.

6. Interface e formato de submissão

O formato final de submissão e o ambiente oficial de execução serão divulgados antes da primeira entrega. A disciplina poderá adotar imagem Docker Linux amd64, execução local assistida ou mecanismo equivalente que preserve reprodutibilidade e avaliação justa.

Como referência de integração, caso a modalidade Docker seja adotada, o executável do engine deverá estar disponível em /engine e aceitar comandos neste formato:

/engine init --data-dir /data

/engine run --data-dir /data --input /input/workload.jsonl --output /output/results.jsonl

/engine verify --data-dir /data

/engine describe

7. Protocolo de workload

Inicialmente, workloads e respostas usarão JSON Lines: um objeto JSON por linha, em UTF-8. Cada operação terá um id único, que deverá ser repetido na resposta correspondente.

{"id": 1, "op": "put", "key": 91, "value": "abc"}
{"id": 2, "op": "get", "key": 91}
{"id": 3, "op": "delete", "key": 91}
{"id": 4, "op": "scan", "start": 0, "end": 100}

{"id": 1, "status": "ok"}
{"id": 2, "status": "ok", "value": "abc"}
{"id": 3, "status": "ok"}
{"id": 4, "status": "ok", "records": [{"key": 91, "value": "abc"}]}
{"id": 10, "status": "not_found"}

Em uma operação SCAN, o intervalo será inclusivo (start <= key <= end) e os registros retornados deverão estar estritamente ordenados por chave, sem duplicatas. A especificação formal e o JSON Schema serão disponibilizados com as ferramentas da disciplina. 8. Entrega 1 — Persistent Storage Engine

Nesta etapa, o engine deverá implementar:

    PUT(key, value);
    GET(key);
    DELETE(key);
    persistência após encerramento e reinicialização;
    valores de tamanho variável;
    verificação básica de integridade;
    recuperação básica após interrupção abrupta.

Não há uma estrutura de dados obrigatória nesta entrega. O grupo pode adotar, por exemplo, arquivo heap, índice em disco, tabela hash, arquivo ordenado ou outra arquitetura. A escolha deverá ser documentada. 9. Entrega 2 — Indexed Storage Engine

Além dos requisitos anteriores, o engine deverá implementar SCAN(start, end) ordenado por chave, desempenho adequado em consultas pontuais e por intervalo e pelo menos uma estrutura de índice apropriada à memória secundária.

São exemplos possíveis: B+ Tree, hashing extensível, hashing linear, ISAM, LSM Tree, arquivo ordenado ou combinação dessas técnicas. A avaliação considerará a coerência entre arquitetura, implementação e resultados obtidos. 10. Entrega 3 — Adaptive Storage Engine

A meta final é tornar o engine eficiente diante de workloads desconhecidos e mutáveis. O grupo deverá evoluir a própria arquitetura e demonstrar essa evolução experimentalmente.

Estratégias possíveis incluem múltiplos índices, Bloom filters, LSM, compressão, cache adaptativo, prefetch, compactação, estatísticas, ajuste dinâmico, diferentes layouts de página e paralelismo. Essas sugestões não são obrigatórias. 11. Avaliação e workloads

As ferramentas ED2Bench confrontarão as respostas com um oracle independente. A modalidade de execução poderá ser centralizada, local assistida ou híbrida; ela será definida antes da primeira entrega. Workloads públicos serão disponibilizados para desenvolvimento. Workloads privados poderão variar seed, tamanho, cardinalidade, número de operações, distribuição de acessos e comprimento dos scans dentro de faixas conhecidas.

As famílias de testes poderão incluir leituras, escritas e atualizações intensivas; distribuições uniforme, sequencial, reversa, Zipf, hotspot e clustered; valores grandes; cargas mistas; mudança de fase; pouca memória; pressão de armazenamento e recuperação após crash.

Correção é pré-requisito para desempenho. Uma execução com resposta incorreta não receberá pontuação de desempenho naquele workload, mas uma falha isolada não implica automaticamente nota zero em toda a entrega.

Entre os aspectos avaliados estarão correção funcional, persistência, reinicialização, recuperação, ordenação, integridade de valores, uso de memória e disco, robustez, arquitetura e análise experimental. Os limites de recursos e as escalas de dados serão definidos antes de cada entrega. 12. Uso de ferramentas de IA

Ferramentas de IA generativa podem ser utilizadas livremente para programação, depuração, estudo, documentação, análise de resultados e apoio ao projeto. A responsabilidade pela correção, desempenho e compreensão do sistema permanece integralmente com o grupo.

O grupo deve ser capaz de explicar e defender as decisões técnicas adotadas, bem como interpretar os resultados de seus próprios experimentos. Na entrega final, haverá uma seção breve intitulada AI-Assisted Engineering. 13. Próximas publicações

    calendário e regras de submissão;
    especificação formal do protocolo e JSON Schemas;
    modalidade de submissão e execução da avaliação, incluindo eventual adoção de Docker;
    versão das ferramentas ED2Bench e instruções de uso local;
    workloads públicos e comandos de execução;
    limites finais de recursos e rubrica detalhada;
    tutorial do ambiente de submissão adotado.
