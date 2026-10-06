package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// Cabeçalho do arquivo (docs/SPEC.md §4.2), 16 bytes no offset 0:
//
//	magic "ED2KVLOG" (8) | version u32 LE (4) | header_crc u32 LE (4)
//
// header_crc = CRC32-C dos bytes 0..11.
const (
	fileHeaderSize = 16
	fileMagic      = "ED2KVLOG"
	fileVersion    = 1
)

// ErrBadHeader: o cabeçalho do data.log é inválido. É erro fatal: o arquivo
// nunca é truncado nem recriado automaticamente.
var ErrBadHeader = errors.New("storage: cabeçalho do data.log inválido")

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

func encodeFileHeader() [fileHeaderSize]byte {
	var b [fileHeaderSize]byte
	copy(b[0:8], fileMagic)
	binary.LittleEndian.PutUint32(b[8:12], fileVersion)
	binary.LittleEndian.PutUint32(b[12:16], crc32.Checksum(b[:12], castagnoli))
	return b
}

func checkFileHeader(b []byte) error {
	if string(b[0:8]) != fileMagic {
		return fmt.Errorf("%w: magic %q", ErrBadHeader, b[0:8])
	}
	if crc := binary.LittleEndian.Uint32(b[12:16]); crc != crc32.Checksum(b[:12], castagnoli) {
		return fmt.Errorf("%w: CRC divergente", ErrBadHeader)
	}
	if v := binary.LittleEndian.Uint32(b[8:12]); v != fileVersion {
		return fmt.Errorf("%w: versão %d não suportada", ErrBadHeader, v)
	}
	return nil
}
