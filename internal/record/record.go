// Package record codifica e decodifica os registros do log (cabeçalho de 17 B
// + valor), com CRC32-C. Ver docs/SPEC.md §4.3.
package record

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// Layout do registro (little-endian), docs/SPEC.md §4.3:
//
//	+---------+---------+--------+-----------+-----------------+
//	| crc u32 | key u64 | type u8| vlen u32  | value [vlen]    |
//	+---------+---------+--------+-----------+-----------------+
//
// crc = CRC32-C de key‖type‖vlen‖value.
const (
	offCRC  = 0
	offKey  = 4
	offType = 12
	offVLen = 13

	// HeaderSize é o tamanho fixo do cabeçalho do registro.
	HeaderSize = 17

	// MaxValueSize é o maior valor aceito. Um vlen acima disso no disco é
	// tratado como corrupção (evita alocar memória absurda com lixo).
	MaxValueSize = 64 << 20 // 64 MiB
)

// Type é o tipo do registro.
type Type uint8

const (
	TypePut    Type = 1
	TypeDelete Type = 2 // lápide; vlen sempre 0
)

func (t Type) valid() bool { return t == TypePut || t == TypeDelete }

var (
	// ErrTruncated: o buffer termina antes do fim do registro
	// (cabeçalho ou valor incompleto), o resto típico de um crash.
	ErrTruncated = errors.New("record: registro incompleto")
	// ErrInvalidType: type diferente de PUT/DELETE.
	ErrInvalidType = errors.New("record: tipo inválido")
	// ErrValueTooLarge: vlen > MaxValueSize.
	ErrValueTooLarge = errors.New("record: valor maior que MaxValueSize")
	// ErrTombstoneValue: lápide com vlen != 0.
	ErrTombstoneValue = errors.New("record: lápide com valor")
	// ErrChecksum: CRC não confere.
	ErrChecksum = errors.New("record: CRC divergente")
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Record é um registro lógico do log.
type Record struct {
	Key   uint64
	Type  Type
	Value []byte
}

// Size devolve o tamanho em disco de um registro com valor de vlen bytes.
func Size(vlen int) int { return HeaderSize + vlen }

// Append codifica r no fim de dst e devolve o slice estendido.
func Append(dst []byte, r Record) ([]byte, error) {
	if err := validate(r.Type, uint64(len(r.Value))); err != nil {
		return dst, err
	}
	start := len(dst)
	dst = append(dst, make([]byte, HeaderSize)...)
	h := dst[start:]
	binary.LittleEndian.PutUint64(h[offKey:], r.Key)
	h[offType] = byte(r.Type)
	binary.LittleEndian.PutUint32(h[offVLen:], uint32(len(r.Value)))
	dst = append(dst, r.Value...)

	crc := crc32.Update(0, castagnoli, dst[start+offKey:])
	binary.LittleEndian.PutUint32(dst[start+offCRC:], crc)
	return dst, nil
}

// Header é o cabeçalho decodificado de um registro.
type Header struct {
	CRC  uint32
	Key  uint64
	Type Type
	VLen uint32
	raw  [HeaderSize - offKey]byte // key‖type‖vlen, para o CRC
}

// DecodeHeader lê e valida (tipo e vlen) os primeiros HeaderSize bytes de b.
// O CRC só pode ser conferido depois, com o valor, via Header.Verify.
// Útil para quem lê do arquivo em duas etapas (cabeçalho, depois valor).
func DecodeHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, ErrTruncated
	}
	h := Header{
		CRC:  binary.LittleEndian.Uint32(b[offCRC:]),
		Key:  binary.LittleEndian.Uint64(b[offKey:]),
		Type: Type(b[offType]),
		VLen: binary.LittleEndian.Uint32(b[offVLen:]),
	}
	copy(h.raw[:], b[offKey:HeaderSize])
	if err := validate(h.Type, uint64(h.VLen)); err != nil {
		return Header{}, err
	}
	return h, nil
}

// Verify confere o CRC do registro dado o valor lido (len(value) deve ser VLen).
func (h Header) Verify(value []byte) error {
	if len(value) != int(h.VLen) {
		return fmt.Errorf("record: valor com %d bytes, esperado %d: %w", len(value), h.VLen, ErrTruncated)
	}
	crc := crc32.Update(0, castagnoli, h.raw[:])
	crc = crc32.Update(crc, castagnoli, value)
	if crc != h.CRC {
		return ErrChecksum
	}
	return nil
}

// Decode lê um registro completo do início de b e devolve o registro e o
// número de bytes consumidos. O Value aponta para dentro de b (sem cópia).
func Decode(b []byte) (Record, int, error) {
	h, err := DecodeHeader(b)
	if err != nil {
		return Record{}, 0, err
	}
	n := Size(int(h.VLen))
	if len(b) < n {
		return Record{}, 0, ErrTruncated
	}
	value := b[HeaderSize:n]
	if err := h.Verify(value); err != nil {
		return Record{}, 0, err
	}
	return Record{Key: h.Key, Type: h.Type, Value: value}, n, nil
}

func validate(t Type, vlen uint64) error {
	switch {
	case !t.valid():
		return ErrInvalidType
	case vlen > MaxValueSize:
		return ErrValueTooLarge
	case t == TypeDelete && vlen != 0:
		return ErrTombstoneValue
	}
	return nil
}
