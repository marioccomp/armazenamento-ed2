// Package storage implementa o Engine: Open (com recuperação), Put, Get,
// Delete, Close e verify sobre um log append-only com índice hash em memória.
// Não depende de protocol: o engine não sabe nada de JSON. Ver docs/SPEC.md §4–§7.
package storage

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/ed2-ufrrj/ed2-engine/internal/record"
)

// LogFileName é o nome do arquivo de log dentro do diretório de dados.
const LogFileName = "data.log"

var (
	// ErrNotInitialized: o diretório não tem data.log (rode Init antes).
	ErrNotInitialized = errors.New("storage: diretório de dados não inicializado")
	// ErrClosed: operação depois de Close.
	ErrClosed = errors.New("storage: engine fechado")
)

// SyncMode define quando o log recebe fsync (docs/SPEC.md §6).
type SyncMode int

const (
	// SyncAlways faz fsync a cada escrita, antes de a operação retornar.
	SyncAlways SyncMode = iota
	// SyncNone nunca faz fsync. Só serve para medir o custo do fsync.
	SyncNone
)

// Options configura o Engine. O valor zero é válido (SyncAlways, logs em stderr).
type Options struct {
	Sync SyncMode
	// Logf recebe diagnósticos (ex.: bytes descartados na recuperação).
	// Se nil, usa log em stderr.
	Logf func(format string, args ...any)
}

type location struct {
	offset int64  // posição do primeiro byte do value
	size   uint32 // vlen
}

// Engine é um storage chave-valor persistente. É seguro para uso concorrente.
type Engine struct {
	mu    sync.RWMutex
	f     *os.File
	opts  Options
	index map[uint64]location
	end   int64  // fim do último registro válido; próxima escrita vai aqui
	buf   []byte // buffer de codificação reutilizado (protegido por mu)

	// err é o primeiro erro de E/S na escrita. Depois de uma falha de write
	// ou fsync não dá para saber o que chegou ao disco, então o engine
	// passa a recusar escritas (só Close resolve).
	err    error
	closed bool
}

// Init cria dir (se preciso) e um data.log vazio com cabeçalho, de forma
// atômica: grava num temporário, fsync, rename e fsync do diretório.
// Se o data.log já existir com cabeçalho válido, não faz nada (idempotente).
// Um data.log com cabeçalho inválido é erro: nunca é recriado.
func Init(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, LogFileName)

	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		return readFileHeader(f)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	tmp := path + ".tmp"
	t, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	hdr := encodeFileHeader()
	if _, err := t.Write(hdr[:]); err != nil {
		t.Close()
		return err
	}
	if err := t.Sync(); err != nil {
		t.Close()
		return err
	}
	if err := t.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// Open abre o engine em dir e recupera o estado: valida o cabeçalho, percorre
// o log reconstruindo o índice e trunca o fim no primeiro registro inválido
// (docs/SPEC.md §7).
func Open(dir string, opts Options) (*Engine, error) {
	if opts.Logf == nil {
		logger := log.New(os.Stderr, "storage: ", 0)
		opts.Logf = logger.Printf
	}

	path := filepath.Join(dir, LogFileName)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInitialized
	}
	if err != nil {
		return nil, err
	}

	e := &Engine{f: f, opts: opts, index: make(map[uint64]location)}
	if err := e.recover(); err != nil {
		f.Close()
		return nil, err
	}
	return e, nil
}

func (e *Engine) recover() error {
	if err := readFileHeader(e.f); err != nil {
		return err
	}
	info, err := e.f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()

	res, err := scanLog(e.f, size, func(h record.Header, valueOff int64) {
		switch h.Type {
		case record.TypePut:
			e.index[h.Key] = location{offset: valueOff, size: h.VLen}
		case record.TypeDelete:
			delete(e.index, h.Key)
		}
	})
	if err != nil {
		return err
	}
	e.end = res.validEnd

	if garbage := size - res.validEnd; garbage > 0 {
		e.opts.Logf("descartando %d bytes no fim do log (offset %d: %v)", garbage, res.validEnd, res.stop)
		if err := e.f.Truncate(res.validEnd); err != nil {
			return fmt.Errorf("storage: truncando log: %w", err)
		}
		if err := e.f.Sync(); err != nil {
			return fmt.Errorf("storage: fsync após truncar: %w", err)
		}
	}
	return nil
}

// Put grava (ou sobrescreve) key = value.
func (e *Engine) Put(key uint64, value []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	off, err := e.append(record.Record{Key: key, Type: record.TypePut, Value: value})
	if err != nil {
		return err
	}
	e.index[key] = location{offset: off + record.HeaderSize, size: uint32(len(value))}
	return nil
}

// Get devolve uma cópia do valor de key e se ela existe.
func (e *Engine) Get(key uint64) ([]byte, bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return nil, false, ErrClosed
	}
	loc, ok := e.index[key]
	if !ok {
		return nil, false, nil
	}
	value := make([]byte, loc.size)
	if _, err := e.f.ReadAt(value, loc.offset); err != nil {
		return nil, false, fmt.Errorf("storage: lendo key %d: %w", key, err)
	}
	return value, true, nil
}

// Delete remove key. Devolve false (sem escrever nada) se key não existir.
func (e *Engine) Delete(key uint64) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return false, ErrClosed
	}
	if _, ok := e.index[key]; !ok {
		return false, nil
	}
	if _, err := e.append(record.Record{Key: key, Type: record.TypeDelete}); err != nil {
		return false, err
	}
	delete(e.index, key)
	return true, nil
}

// Len devolve o número de chaves vivas.
func (e *Engine) Len() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.index)
}

// Close faz fsync (exceto em SyncNone) e fecha o arquivo. Chamar de novo não faz nada.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	var syncErr error
	if e.opts.Sync != SyncNone && e.err == nil {
		syncErr = e.f.Sync()
	}
	closeErr := e.f.Close()
	return errors.Join(syncErr, closeErr)
}

// append grava r no fim do log e devolve o offset do registro. Só retorna
// sucesso depois que o registro está durável, conforme o SyncMode.
// Chamar com e.mu travado para escrita.
func (e *Engine) append(r record.Record) (int64, error) {
	if e.closed {
		return 0, ErrClosed
	}
	if e.err != nil {
		return 0, e.err
	}
	buf, err := record.Append(e.buf[:0], r)
	if err != nil {
		return 0, err // registro inválido (ex.: valor grande demais); nada foi escrito
	}
	e.buf = buf

	off := e.end
	if _, err := e.f.WriteAt(buf, off); err != nil {
		return 0, e.fail(err)
	}
	if e.opts.Sync == SyncAlways {
		if err := e.f.Sync(); err != nil {
			return 0, e.fail(err)
		}
	}
	e.end += int64(len(buf))
	if cap(e.buf) > 1<<20 {
		e.buf = nil // não segurar buffers de valores grandes
	}
	return off, nil
}

func (e *Engine) fail(err error) error {
	e.err = fmt.Errorf("storage: falha de E/S no log, engine em estado de erro: %w", err)
	return e.err
}

// readFileHeader lê e valida o cabeçalho de 16 bytes no offset 0.
func readFileHeader(f io.ReaderAt) error {
	var b [fileHeaderSize]byte
	n, err := f.ReadAt(b[:], 0)
	if n < fileHeaderSize {
		if err == nil || errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: arquivo com %d bytes", ErrBadHeader, n)
		}
		return err
	}
	return checkFileHeader(b[:])
}
