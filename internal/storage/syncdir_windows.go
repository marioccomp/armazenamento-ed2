//go:build windows

package storage

// syncDir é no-op no Windows: não dá para abrir um diretório com os.Open e
// fazer FlushFileBuffers nele, e no NTFS o rename é registrado no journal de
// metadados. A entrega roda em Linux (Docker), onde o fsync do diretório é feito.
func syncDir(dir string) error { return nil }
