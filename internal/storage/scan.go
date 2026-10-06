package storage

import (
	"bufio"
	"errors"
	"io"

	"github.com/ed2-ufrrj/ed2-engine/internal/record"
)

// scanResult é o resultado de percorrer o log.
type scanResult struct {
	validEnd int64 // offset logo após o último registro válido
	records  int   // registros válidos (PUT + DELETE)
	stop     error // por que parou antes do fim do arquivo; nil se o arquivo terminou num limite de registro
}

// scanLog percorre os registros a partir do fim do cabeçalho do arquivo e
// chama apply para cada registro válido, em ordem. Para no primeiro registro
// inválido (cabeçalho/valor incompleto, tipo ou vlen inválido, CRC divergente).
// Só devolve erro em falha de E/S; corrupção vai em scanResult.stop.
// Compartilhado pela recuperação (Open) e por verify.
func scanLog(f io.ReaderAt, size int64, apply func(h record.Header, valueOff int64)) (scanResult, error) {
	res := scanResult{validEnd: fileHeaderSize}
	if size < fileHeaderSize {
		return res, nil
	}
	br := bufio.NewReaderSize(io.NewSectionReader(f, fileHeaderSize, size-fileHeaderSize), 1<<20)

	var hdr [record.HeaderSize]byte
	var value []byte
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return res, nil // fim limpo
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				res.stop = record.ErrTruncated
				return res, nil
			}
			return res, err
		}
		h, err := record.DecodeHeader(hdr[:])
		if err != nil {
			res.stop = err
			return res, nil
		}

		if cap(value) < int(h.VLen) {
			value = make([]byte, h.VLen)
		}
		value = value[:h.VLen]
		if _, err := io.ReadFull(br, value); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				res.stop = record.ErrTruncated
				return res, nil
			}
			return res, err
		}
		if err := h.Verify(value); err != nil {
			res.stop = err
			return res, nil
		}

		apply(h, res.validEnd+record.HeaderSize)
		res.validEnd += int64(record.Size(int(h.VLen)))
		res.records++
	}
}
