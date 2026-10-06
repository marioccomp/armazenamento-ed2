// Comando engine é a CLI do storage engine (docs/SPEC.md §3):
//
//	engine init     --data-dir DIR
//	engine run      --data-dir DIR --input FILE --output FILE [--sync batch|always|none] [--batch-size N]
//	engine verify   --data-dir DIR
//	engine describe
//
// Logs e diagnósticos vão para stderr; stdout é reservado ao protocolo.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ed2-ufrrj/ed2-engine/internal/storage"
)

const usage = `uso:
  engine init     --data-dir DIR
  engine run      --data-dir DIR --input FILE --output FILE [--sync batch|always|none] [--batch-size N]
  engine verify   --data-dir DIR
  engine describe
`

// errNotImplemented marca subcomandos ainda não implementados no checklist.
var errNotImplemented = errors.New("ainda não implementado")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run despacha o subcomando e devolve o exit code. Separado de main para testes.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	var err error
	switch args[0] {
	case "init":
		err = cmdInit(args[1:], stderr)
	case "run":
		err = cmdRun(args[1:], stderr)
	case "verify":
		err = cmdVerify(args[1:], stderr)
	case "describe":
		err = cmdDescribe(stdout)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "engine: subcomando desconhecido %q\n%s", args[0], usage)
		return 2
	}

	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(stderr, "engine %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func cmdInit(args []string, stderr io.Writer) error {
	fs := newFlagSet("init", stderr)
	dataDir := fs.String("data-dir", "", "diretório de dados")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dataDir == "" {
		return errors.New("--data-dir é obrigatório")
	}
	return storage.Init(*dataDir)
}

func cmdRun(args []string, stderr io.Writer) error {
	fs := newFlagSet("run", stderr)
	dataDir := fs.String("data-dir", "", "diretório de dados")
	input := fs.String("input", "", "arquivo de workload JSONL")
	output := fs.String("output", "", "arquivo de respostas JSONL")
	syncMode := fs.String("sync", "batch", "política de fsync: batch|always|none")
	batchSize := fs.Int("batch-size", 1024, "operações por group commit (--sync batch)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dataDir == "" || *input == "" || *output == "" {
		return errors.New("--data-dir, --input e --output são obrigatórios")
	}
	switch *syncMode {
	case "batch", "always", "none":
	default:
		return fmt.Errorf("--sync inválido %q (use batch, always ou none)", *syncMode)
	}
	if *batchSize <= 0 {
		return fmt.Errorf("--batch-size deve ser > 0, recebido %d", *batchSize)
	}
	return errNotImplemented
}

func cmdVerify(args []string, stderr io.Writer) error {
	fs := newFlagSet("verify", stderr)
	dataDir := fs.String("data-dir", "", "diretório de dados")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dataDir == "" {
		return errors.New("--data-dir é obrigatório")
	}
	return errNotImplemented
}

// description é a saída de `engine describe` (docs/SPEC.md §8).
// Struct em vez de map para manter a ordem dos campos estável.
type description struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Delivery     int      `json:"delivery"`
	Language     string   `json:"language"`
	Architecture string   `json:"architecture"`
	Durability   string   `json:"durability"`
	Integrity    string   `json:"integrity"`
	Operations   []string `json:"operations"`
}

var engineDescription = description{
	Name:         "ed2-engine",
	Version:      "1.0.0",
	Delivery:     1,
	Language:     "go",
	Architecture: "append-only log + in-memory hash index (Bitcask-style)",
	Durability:   "group commit (fsync per batch)",
	Integrity:    "CRC32-C per record",
	Operations:   []string{"put", "get", "delete"},
}

func cmdDescribe(stdout io.Writer) error {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(engineDescription)
}
