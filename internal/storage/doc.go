// Package storage implementa o Engine: Open (com recuperação), Put, Get,
// Delete, Close e verify sobre um log append-only com índice hash em memória.
// Não depende de protocol: o engine não sabe nada de JSON. Ver docs/SPEC.md §4–§7.
package storage
