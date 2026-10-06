package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ed2-ufrrj/ed2-engine/internal/storage"
)

func TestDescribe(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"describe"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("describe não deveria escrever em stderr: %q", stderr.String())
	}

	var got description
	dec := json.NewDecoder(&stdout)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("saída não é JSON válido: %v", err)
	}
	if !reflect.DeepEqual(got, engineDescription) {
		t.Errorf("describe = %+v, esperado %+v", got, engineDescription)
	}
	if got.Delivery != 1 || got.Name != "ed2-engine" {
		t.Errorf("campos essenciais incorretos: %+v", got)
	}
}

func TestExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"sem argumentos", nil, 2},
		{"subcomando desconhecido", []string{"foo"}, 2},
		{"help", []string{"help"}, 0},
		{"init sem data-dir", []string{"init"}, 1},
		{"run sem flags", []string{"run"}, 1},
		{"run sync inválido", []string{"run", "--data-dir", "d", "--input", "i", "--output", "o", "--sync", "xyz"}, 1},
		{"run batch-size inválido", []string{"run", "--data-dir", "d", "--input", "i", "--output", "o", "--batch-size", "0"}, 1},
		{"verify sem data-dir", []string{"verify"}, 1},
		{"flag desconhecida", []string{"init", "--nope"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr); got != tt.want {
				t.Errorf("exit code = %d, esperado %d (stderr: %q)", got, tt.want, stderr.String())
			}
			if tt.want != 0 && stdout.Len() != 0 {
				t.Errorf("erros não devem ir para stdout: %q", stdout.String())
			}
		})
	}
}

func TestInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dados")
	for i := 0; i < 2; i++ { // a segunda chamada testa a idempotência
		var stdout, stderr bytes.Buffer
		if code := run([]string{"init", "--data-dir", dir}, &stdout, &stderr); code != 0 {
			t.Fatalf("init #%d: exit code = %d, stderr = %q", i+1, code, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("init não deve escrever em stdout: %q", stdout.String())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, storage.LogFileName)); err != nil {
		t.Fatal(err)
	}

	// cabeçalho inválido: erro, sem recriar
	if err := os.WriteFile(filepath.Join(dir, storage.LogFileName), []byte("lixo"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--data-dir", dir}, &stdout, &stderr); code != 1 {
		t.Fatalf("init sobre cabeçalho inválido: exit code = %d, esperado 1", code)
	}
}

func TestUnknownCommandMentionsName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run([]string{"foo"}, &stdout, &stderr)
	if !strings.Contains(stderr.String(), `"foo"`) {
		t.Errorf("stderr deveria citar o subcomando: %q", stderr.String())
	}
}
