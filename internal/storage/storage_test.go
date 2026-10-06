package storage

import (
	"bytes"
	"errors"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/ed2-ufrrj/ed2-engine/internal/record"
)

// openTemp inicializa um diretório temporário e abre o engine nele.
func openTemp(t *testing.T) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	e := mustOpen(t, dir)
	return e, dir
}

func mustOpen(t *testing.T, dir string) *Engine {
	t.Helper()
	e, err := Open(dir, Options{Logf: t.Logf})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func mustGet(t *testing.T, e *Engine, key uint64, want []byte) {
	t.Helper()
	got, ok, err := e.Get(key)
	if err != nil {
		t.Fatalf("Get(%d): %v", key, err)
	}
	if want == nil {
		if ok {
			t.Fatalf("Get(%d) = %q, esperado not_found", key, got)
		}
		return
	}
	if !ok {
		t.Fatalf("Get(%d) = not_found, esperado %d bytes", key, len(want))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Get(%d) = %d bytes divergentes, esperado %d bytes", key, len(got), len(want))
	}
}

func fileSize(t *testing.T, dir string) int64 {
	t.Helper()
	info, err := os.Stat(filepath.Join(dir, LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func TestInitCreatesHeader(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "novo", "dir")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	want := encodeFileHeader()
	if !bytes.Equal(b, want[:]) {
		t.Fatalf("data.log = % x, esperado % x", b, want)
	}
	if string(b[:8]) != "ED2KVLOG" {
		t.Fatalf("magic = %q", b[:8])
	}
	if _, err := os.Stat(filepath.Join(dir, LogFileName+".tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporário não deveria sobrar: %v", err)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	e, dir := openTemp(t)
	if err := e.Put(1, []byte("um")); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	before := fileSize(t, dir)
	if err := Init(dir); err != nil {
		t.Fatalf("Init sobre diretório válido: %v", err)
	}
	if after := fileSize(t, dir); after != before {
		t.Fatalf("Init alterou o log: %d -> %d bytes", before, after)
	}
	mustGet(t, mustOpen(t, dir), 1, []byte("um"))
}

func TestOpenNotInitialized(t *testing.T) {
	if _, err := Open(t.TempDir(), Options{}); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("erro = %v, esperado ErrNotInitialized", err)
	}
}

func TestBadHeaderIsFatal(t *testing.T) {
	corruptions := map[string]func(b []byte) []byte{
		"magic":      func(b []byte) []byte { b[0] = 'X'; return b },
		"versão":     func(b []byte) []byte { b[8] = 2; return b },
		"crc":        func(b []byte) []byte { b[15] ^= 1; return b },
		"curto":      func(b []byte) []byte { return b[:10] },
		"vazio":      func(b []byte) []byte { return b[:0] },
		"lixo à toa": func(b []byte) []byte { return []byte("isto não é um log de verdade") },
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			e, dir := openTemp(t)
			if err := e.Put(1, []byte("v")); err != nil {
				t.Fatal(err)
			}
			e.Close()
			path := filepath.Join(dir, LogFileName)
			b, _ := os.ReadFile(path)
			b = corrupt(b)
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := Open(dir, Options{Logf: t.Logf}); !errors.Is(err, ErrBadHeader) {
				t.Fatalf("Open: erro = %v, esperado ErrBadHeader", err)
			}
			if err := Init(dir); !errors.Is(err, ErrBadHeader) {
				t.Fatalf("Init: erro = %v, esperado ErrBadHeader", err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(after, b) {
				t.Fatalf("arquivo com cabeçalho inválido foi modificado")
			}
		})
	}
}

func TestPutGetDelete(t *testing.T) {
	e, _ := openTemp(t)

	mustGet(t, e, 1, nil)
	if err := e.Put(1, []byte("a")); err != nil {
		t.Fatal(err)
	}
	mustGet(t, e, 1, []byte("a"))

	if err := e.Put(1, []byte("b")); err != nil { // sobrescrita
		t.Fatal(err)
	}
	mustGet(t, e, 1, []byte("b"))

	ok, err := e.Delete(1)
	if err != nil || !ok {
		t.Fatalf("Delete(1) = %v, %v", ok, err)
	}
	mustGet(t, e, 1, nil)

	if err := e.Put(1, []byte("c")); err != nil { // recriar após delete
		t.Fatal(err)
	}
	mustGet(t, e, 1, []byte("c"))
	if e.Len() != 1 {
		t.Fatalf("Len = %d, esperado 1", e.Len())
	}
}

func TestDeleteMissingWritesNothing(t *testing.T) {
	e, dir := openTemp(t)
	if err := e.Put(1, []byte("x")); err != nil {
		t.Fatal(err)
	}
	before := fileSize(t, dir)
	ok, err := e.Delete(2)
	if err != nil || ok {
		t.Fatalf("Delete(inexistente) = %v, %v; esperado false, nil", ok, err)
	}
	if after := fileSize(t, dir); after != before {
		t.Fatalf("delete de chave inexistente escreveu %d bytes", after-before)
	}
}

func TestEdgeKeysAndValues(t *testing.T) {
	e, dir := openTemp(t)
	big := make([]byte, 5<<20)
	rand.New(rand.NewSource(1)).Read(big)

	cases := []struct {
		key   uint64
		value []byte
	}{
		{0, []byte("zero")},
		{math.MaxUint64, []byte("max")},
		{1<<53 + 1, []byte("não cabe em float64")},
		{10, []byte{}}, // valor vazio existe e é diferente de not_found
		{11, []byte("ação \"aspas\"\n日本語 🚀")},
		{12, big},
	}
	for _, c := range cases {
		if err := e.Put(c.key, c.value); err != nil {
			t.Fatalf("Put(%d): %v", c.key, err)
		}
	}
	for _, c := range cases {
		mustGet(t, e, c.key, c.value)
	}
	mustGet(t, e, 1<<53, nil) // vizinho de 2^53+1 não pode colidir

	e.Close()
	e2 := mustOpen(t, dir)
	for _, c := range cases {
		mustGet(t, e2, c.key, c.value)
	}
}

func TestReopenPreservesState(t *testing.T) {
	e, dir := openTemp(t)
	for k := uint64(0); k < 100; k++ {
		if err := e.Put(k, []byte{byte(k)}); err != nil {
			t.Fatal(err)
		}
	}
	for k := uint64(0); k < 100; k += 3 {
		if _, err := e.Delete(k); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Put(50, []byte("novo")); err != nil {
		t.Fatal(err)
	}
	size := fileSize(t, dir)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	e2 := mustOpen(t, dir)
	if fileSize(t, dir) != size {
		t.Fatalf("reabrir um log íntegro não deveria truncar")
	}
	for k := uint64(0); k < 100; k++ {
		switch {
		case k == 50:
			mustGet(t, e2, k, []byte("novo"))
		case k%3 == 0:
			mustGet(t, e2, k, nil)
		default:
			mustGet(t, e2, k, []byte{byte(k)})
		}
	}
	// o engine reaberto continua escrevendo no lugar certo
	if err := e2.Put(1000, []byte("depois")); err != nil {
		t.Fatal(err)
	}
	e2.Close()
	mustGet(t, mustOpen(t, dir), 1000, []byte("depois"))
}

func TestRecoveryTruncatesTail(t *testing.T) {
	e, dir := openTemp(t)
	if err := e.Put(1, []byte("ok")); err != nil {
		t.Fatal(err)
	}
	e.Close()
	valid := fileSize(t, dir)

	// simula um registro parcial de um crash
	partial, _ := record.Append(nil, record.Record{Key: 2, Type: record.TypePut, Value: []byte("perdido")})
	f, err := os.OpenFile(filepath.Join(dir, LogFileName), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(partial[:len(partial)-3])
	f.Close()

	e2 := mustOpen(t, dir)
	if got := fileSize(t, dir); got != valid {
		t.Fatalf("tamanho após recuperação = %d, esperado %d", got, valid)
	}
	mustGet(t, e2, 1, []byte("ok"))
	mustGet(t, e2, 2, nil)
}

func TestValueTooLargeIsNotFatal(t *testing.T) {
	e, dir := openTemp(t)
	before := fileSize(t, dir)
	if err := e.Put(1, make([]byte, record.MaxValueSize+1)); !errors.Is(err, record.ErrValueTooLarge) {
		t.Fatalf("erro = %v, esperado ErrValueTooLarge", err)
	}
	if fileSize(t, dir) != before {
		t.Fatalf("valor recusado não pode escrever no log")
	}
	if err := e.Put(1, []byte("ok")); err != nil {
		t.Fatalf("engine deveria continuar utilizável: %v", err)
	}
	mustGet(t, e, 1, []byte("ok"))
}

func TestGetReturnsCopy(t *testing.T) {
	e, _ := openTemp(t)
	if err := e.Put(1, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	v, _, _ := e.Get(1)
	v[0] = 'X'
	mustGet(t, e, 1, []byte("abc"))
}

func TestPutDoesNotRetainCallerSlice(t *testing.T) {
	e, _ := openTemp(t)
	v := []byte("abc")
	if err := e.Put(1, v); err != nil {
		t.Fatal(err)
	}
	v[0] = 'X'
	mustGet(t, e, 1, []byte("abc"))
}

func TestUseAfterClose(t *testing.T) {
	e, _ := openTemp(t)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close duplo: %v", err)
	}
	if err := e.Put(1, nil); !errors.Is(err, ErrClosed) {
		t.Errorf("Put: %v", err)
	}
	if _, _, err := e.Get(1); !errors.Is(err, ErrClosed) {
		t.Errorf("Get: %v", err)
	}
	if _, err := e.Delete(1); !errors.Is(err, ErrClosed) {
		t.Errorf("Delete: %v", err)
	}
}
