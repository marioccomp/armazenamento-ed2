package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
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
		{"init não implementado", []string{"init", "--data-dir", "x"}, 1},
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

func TestUnknownCommandMentionsName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run([]string{"foo"}, &stdout, &stderr)
	if !strings.Contains(stderr.String(), `"foo"`) {
		t.Errorf("stderr deveria citar o subcomando: %q", stderr.String())
	}
}
