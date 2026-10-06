//go:build !windows

package storage

import "os"

// syncDir faz fsync do diretório, tornando durável um rename/criação de arquivo.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
