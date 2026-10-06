package storage

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ed2-ufrrj/ed2-engine/internal/record"
)

// Testes de recuperação (docs/SPEC.md §10.1): reabertura, truncamento
// exaustivo por offset e lixo no fim do log.

const keyUniverse = 6 // chaves 0..5: poucas, para forçar sobrescritas e deletes

type state map[uint64]string

func (s state) clone() state {
	c := make(state, len(s))
	for k, v := range s {
		c[k] = v
	}
	return c
}

// checkState confere que o engine tem exatamente o estado want.
func checkState(t *testing.T, e *Engine, want state) {
	t.Helper()
	if e.Len() != len(want) {
		t.Fatalf("Len = %d, esperado %d", e.Len(), len(want))
	}
	for k := uint64(0); k < keyUniverse; k++ {
		got, ok, err := e.Get(k)
		if err != nil {
			t.Fatalf("Get(%d): %v", k, err)
		}
		v, exists := want[k]
		if ok != exists || string(got) != v {
			t.Fatalf("Get(%d) = (%q, %v), esperado (%q, %v)", k, got, ok, v, exists)
		}
	}
}

// history é um log construído operação a operação, com o estado esperado e o
// tamanho do arquivo depois de cada operação que escreveu algo.
type history struct {
	dir    string
	states []state // states[i] = estado após as i primeiras operações
	ends   []int64 // ends[i] = tamanho do log após as i primeiras operações
}

// buildHistory aplica n operações aleatórias (seed fixa) num log novo.
// Inclui sobrescritas, valores vazios, deletes e um valor maior.
func buildHistory(t *testing.T, seed int64, n int) history {
	t.Helper()
	e, dir := openTemp(t)
	rng := rand.New(rand.NewSource(seed))
	cur := state{}
	h := history{dir: dir, states: []state{{}}, ends: []int64{fileHeaderSize}}

	for len(h.states) <= n {
		k := uint64(rng.Intn(keyUniverse))
		if _, exists := cur[k]; exists && rng.Intn(4) == 0 {
			if ok, err := e.Delete(k); err != nil || !ok {
				t.Fatalf("Delete(%d) = %v, %v", k, ok, err)
			}
			delete(cur, k)
		} else {
			size := rng.Intn(12) // inclui 0: valor vazio
			if rng.Intn(10) == 0 {
				size = 200
			}
			v := strings.Repeat(string(rune('a'+rng.Intn(26))), size)
			if err := e.Put(k, []byte(v)); err != nil {
				t.Fatal(err)
			}
			cur[k] = v
		}
		h.states = append(h.states, cur.clone())
		h.ends = append(h.ends, fileSize(t, dir))
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h history) bytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.dir, LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(b)) != h.ends[len(h.ends)-1] {
		t.Fatalf("log com %d bytes, esperado %d", len(b), h.ends[len(h.ends)-1])
	}
	return b
}

// prefixAt devolve quantas operações cabem inteiras nos primeiros size bytes.
func (h history) prefixAt(size int64) int {
	p := 0
	for p+1 < len(h.ends) && h.ends[p+1] <= size {
		p++
	}
	return p
}

// writeLog grava b como data.log de dir (sobrescrevendo).
func writeLog(t *testing.T, dir string, b []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, LogFileName), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// openQuiet abre o engine guardando os diagnósticos em *logs.
func openQuiet(t *testing.T, dir string, logs *[]string) (*Engine, error) {
	return openQuietSync(t, dir, logs, SyncAlways)
}

// openQuietSync é openQuiet com SyncMode escolhido. Os testes que reabrem
// milhares de vezes no mesmo processo usam SyncNone: a durabilidade entre
// processos não é o alvo deles e o fsync os deixaria lentos (o fsync da
// truncagem na recuperação continua acontecendo).
func openQuietSync(t *testing.T, dir string, logs *[]string, mode SyncMode) (*Engine, error) {
	return Open(dir, Options{Sync: mode, Logf: func(format string, args ...any) {
		*logs = append(*logs, fmt.Sprintf(format, args...))
	}})
}

// Para cada offset de truncamento possível, o estado recuperado deve ser
// exatamente o do maior prefixo de operações completas, e o arquivo deve
// ficar truncado no fim desse prefixo.
func TestTruncateAtEveryOffset(t *testing.T) {
	seeds := []int64{1, 2}
	if testing.Short() {
		seeds = seeds[:1]
	}
	for _, seed := range seeds {
		h := buildHistory(t, seed, 30)
		full := h.bytes(t)
		dir := t.TempDir()

		for off := int64(0); off <= int64(len(full)); off++ {
			writeLog(t, dir, full[:off])
			var logs []string
			e, err := openQuietSync(t, dir, &logs, SyncNone)

			if off < fileHeaderSize {
				if !errors.Is(err, ErrBadHeader) {
					t.Fatalf("seed %d, offset %d: erro = %v, esperado ErrBadHeader", seed, off, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("seed %d, offset %d: Open: %v", seed, off, err)
			}

			p := h.prefixAt(off)
			checkState(t, e, h.states[p])
			if got := fileSize(t, dir); got != h.ends[p] {
				t.Fatalf("seed %d, offset %d: tamanho após recuperar = %d, esperado %d", seed, off, got, h.ends[p])
			}
			if truncated := off != h.ends[p]; truncated != (len(logs) > 0) {
				t.Fatalf("seed %d, offset %d: truncou=%v mas logs=%q", seed, off, truncated, logs)
			}

			// o engine recuperado continua escrevendo no lugar certo
			// (amostrado: reabrir em todo offset deixaria o teste lento)
			if off%5 != 0 {
				e.Close()
				continue
			}
			if err := e.Put(0, []byte("depois")); err != nil {
				t.Fatal(err)
			}
			if err := e.Close(); err != nil {
				t.Fatal(err)
			}
			want := h.states[p].clone()
			want[0] = "depois"
			e2, err := openQuietSync(t, dir, &logs, SyncNone)
			if err != nil {
				t.Fatal(err)
			}
			checkState(t, e2, want)
			e2.Close()
		}
	}
}

// Lixo de vários tipos depois de um log válido é descartado sem perder nada.
func TestGarbageTailDiscarded(t *testing.T) {
	h := buildHistory(t, 10, 30)
	full := h.bytes(t)
	final := h.states[len(h.states)-1]

	validRec, _ := record.Append(nil, record.Record{Key: 1, Type: record.TypePut, Value: []byte("fantasma")})
	badCRC := bytes.Clone(validRec)
	badCRC[0] ^= 0xff
	hugeVLen := bytes.Clone(validRec[:record.HeaderSize])
	copy(hugeVLen[13:], []byte{0xff, 0xff, 0xff, 0x7f})

	garbages := map[string][]byte{
		"zeros":                 make([]byte, 100),
		"0xff":                  bytes.Repeat([]byte{0xff}, 100),
		"1 byte":                {0x01},
		"registro com CRC ruim": badCRC,
		"vlen enorme":           hugeVLen,
		"registro sem o fim":    validRec[:len(validRec)-1],
	}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		g := make([]byte, 1+rng.Intn(300))
		rng.Read(g)
		garbages[fmt.Sprintf("aleatório %d", i)] = g
	}

	dir := t.TempDir()
	for name, g := range garbages {
		t.Run(name, func(t *testing.T) {
			writeLog(t, dir, append(bytes.Clone(full), g...))
			var logs []string
			e, err := openQuiet(t, dir, &logs)
			if err != nil {
				t.Fatal(err)
			}
			checkState(t, e, final)
			e.Close()
			if got := fileSize(t, dir); got != int64(len(full)) {
				t.Fatalf("tamanho = %d, esperado %d", got, len(full))
			}
			if len(logs) != 1 || !strings.Contains(logs[0], fmt.Sprintf("descartando %d bytes", len(g))) {
				t.Fatalf("diagnóstico inesperado: %q", logs)
			}

			// a truncagem é durável: reabrir não descarta mais nada
			logs = nil
			e2, err := openQuiet(t, dir, &logs)
			if err != nil {
				t.Fatal(err)
			}
			checkState(t, e2, final)
			e2.Close()
			if len(logs) != 0 {
				t.Fatalf("segunda abertura não deveria truncar: %q", logs)
			}
		})
	}
}

// Limitação documentada (SPEC §7): corrupção no meio do log descarta o
// registro corrompido e todos os seguintes, mesmo os válidos.
func TestMidLogCorruptionKeepsPrefix(t *testing.T) {
	h := buildHistory(t, 20, 20)
	full := h.bytes(t)
	dir := t.TempDir()

	for op := 1; op < len(h.ends); op++ {
		start, end := h.ends[op-1], h.ends[op]
		b := bytes.Clone(full)
		b[(start+end)/2] ^= 0x10 // um bit no meio do registro da operação op

		writeLog(t, dir, b)
		var logs []string
		e, err := openQuiet(t, dir, &logs)
		if err != nil {
			t.Fatal(err)
		}
		checkState(t, e, h.states[op-1])
		e.Close()
		if got := fileSize(t, dir); got != start {
			t.Fatalf("op %d: tamanho = %d, esperado %d", op, got, start)
		}
	}
}

// Ciclos de abrir/escrever/fechar acumulam o estado corretamente, e reabrir
// sem escrever não muda o arquivo.
func TestManyReopens(t *testing.T) {
	_, dir := openTemp(t)
	rng := rand.New(rand.NewSource(7))
	want := state{}

	for cycle := 0; cycle < 30; cycle++ {
		var logs []string
		e, err := openQuiet(t, dir, &logs)
		if err != nil {
			t.Fatal(err)
		}
		if len(logs) != 0 {
			t.Fatalf("ciclo %d: log íntegro não deveria truncar: %q", cycle, logs)
		}
		checkState(t, e, want)

		for i := 0; i < rng.Intn(10); i++ {
			k := uint64(rng.Intn(keyUniverse))
			if rng.Intn(3) == 0 {
				_, existed := want[k]
				ok, err := e.Delete(k)
				if err != nil || ok != existed {
					t.Fatalf("Delete(%d) = %v, %v; existia=%v", k, ok, err, existed)
				}
				delete(want, k)
			} else {
				v := fmt.Sprintf("c%d-i%d", cycle, i)
				if err := e.Put(k, []byte(v)); err != nil {
					t.Fatal(err)
				}
				want[k] = v
			}
		}
		checkState(t, e, want)
		if err := e.Close(); err != nil {
			t.Fatal(err)
		}

		size := fileSize(t, dir)
		e2, err := openQuiet(t, dir, &logs)
		if err != nil {
			t.Fatal(err)
		}
		e2.Close()
		if fileSize(t, dir) != size {
			t.Fatalf("ciclo %d: reabrir sem escrever mudou o arquivo", cycle)
		}
	}
}
