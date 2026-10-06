package record

import (
	"bytes"
	"errors"
	"math"
	"math/rand"
	"testing"
)

func sampleRecords() []Record {
	big := make([]byte, 3<<20) // 3 MiB
	rand.New(rand.NewSource(1)).Read(big)
	return []Record{
		{Key: 0, Type: TypePut, Value: []byte("a")},
		{Key: math.MaxUint64, Type: TypePut, Value: []byte("max")},
		{Key: 1 << 53, Type: TypePut, Value: []byte("acima de 2^53")},
		{Key: 42, Type: TypePut, Value: []byte{}}, // valor vazio é válido
		{Key: 42, Type: TypeDelete},
		{Key: 7, Type: TypePut, Value: []byte("ação \"aspas\"\n<tag> & 日本語 🚀")},
		{Key: 8, Type: TypePut, Value: []byte{0, 0xff, 0x00, '\n'}},
		{Key: 9, Type: TypePut, Value: big},
	}
}

func TestRoundTrip(t *testing.T) {
	for _, want := range sampleRecords() {
		buf, err := Append(nil, want)
		if err != nil {
			t.Fatalf("Append(key=%d): %v", want.Key, err)
		}
		if len(buf) != Size(len(want.Value)) {
			t.Fatalf("tamanho = %d, esperado %d", len(buf), Size(len(want.Value)))
		}
		got, n, err := Decode(buf)
		if err != nil {
			t.Fatalf("Decode(key=%d): %v", want.Key, err)
		}
		if n != len(buf) || got.Key != want.Key || got.Type != want.Type || !bytes.Equal(got.Value, want.Value) {
			t.Fatalf("ida e volta divergiu: key=%d type=%d n=%d", got.Key, got.Type, n)
		}
	}
}

func TestDecodeSequence(t *testing.T) {
	recs := sampleRecords()
	var buf []byte
	for _, r := range recs {
		var err error
		if buf, err = Append(buf, r); err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range recs {
		got, n, err := Decode(buf)
		if err != nil {
			t.Fatalf("registro %d: %v", i, err)
		}
		if got.Key != want.Key || got.Type != want.Type || !bytes.Equal(got.Value, want.Value) {
			t.Fatalf("registro %d divergiu", i)
		}
		buf = buf[n:]
	}
	if len(buf) != 0 {
		t.Fatalf("sobraram %d bytes", len(buf))
	}
}

func TestEncodingIsLittleEndian(t *testing.T) {
	buf, err := Append(nil, Record{Key: 0x0102030405060708, Type: TypePut, Value: []byte("xy")})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, // key
		0x01,                   // type
		0x02, 0x00, 0x00, 0x00, // vlen
		'x', 'y',
	}
	if !bytes.Equal(buf[4:], want) {
		t.Fatalf("layout = % x, esperado % x", buf[4:], want)
	}
}

// Trocar qualquer bit de um registro deve fazer Decode falhar.
func TestBitFlipDetected(t *testing.T) {
	recs := []Record{
		{Key: 0x1234, Type: TypePut, Value: []byte("valor de teste")},
		{Key: 99, Type: TypePut, Value: []byte{}},
		{Key: math.MaxUint64, Type: TypeDelete},
	}
	for _, r := range recs {
		orig, err := Append(nil, r)
		if err != nil {
			t.Fatal(err)
		}
		for bit := 0; bit < len(orig)*8; bit++ {
			buf := bytes.Clone(orig)
			buf[bit/8] ^= 1 << (bit % 8)
			if _, _, err := Decode(buf); err == nil {
				t.Errorf("key=%d: bit %d trocado não foi detectado", r.Key, bit)
			}
		}
	}
}

// Qualquer prefixo estrito de um registro é incompleto.
func TestTruncatedPrefix(t *testing.T) {
	buf, err := Append(nil, Record{Key: 5, Type: TypePut, Value: []byte("abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(buf); n++ {
		if _, _, err := Decode(buf[:n]); !errors.Is(err, ErrTruncated) {
			t.Errorf("prefixo de %d bytes: erro = %v, esperado ErrTruncated", n, err)
		}
	}
}

func TestDecodeHeaderValidation(t *testing.T) {
	valid, err := Append(nil, Record{Key: 1, Type: TypePut, Value: []byte("v")})
	if err != nil {
		t.Fatal(err)
	}

	for _, typ := range []byte{0, 3, 0xff} {
		b := bytes.Clone(valid)
		b[offType] = typ
		if _, err := DecodeHeader(b); !errors.Is(err, ErrInvalidType) {
			t.Errorf("type=%d: erro = %v, esperado ErrInvalidType", typ, err)
		}
	}

	b := bytes.Clone(valid)
	b[offVLen], b[offVLen+1], b[offVLen+2], b[offVLen+3] = 0xff, 0xff, 0xff, 0xff
	if _, err := DecodeHeader(b); !errors.Is(err, ErrValueTooLarge) {
		t.Errorf("vlen enorme: erro = %v, esperado ErrValueTooLarge", err)
	}

	b = bytes.Clone(valid)
	b[offType] = byte(TypeDelete)
	if _, err := DecodeHeader(b); !errors.Is(err, ErrTombstoneValue) {
		t.Errorf("lápide com valor: erro = %v, esperado ErrTombstoneValue", err)
	}
}

func TestHeaderVerify(t *testing.T) {
	buf, err := Append(nil, Record{Key: 3, Type: TypePut, Value: []byte("abc")})
	if err != nil {
		t.Fatal(err)
	}
	h, err := DecodeHeader(buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Verify(buf[HeaderSize:]); err != nil {
		t.Errorf("Verify do valor correto: %v", err)
	}
	if err := h.Verify([]byte("abd")); !errors.Is(err, ErrChecksum) {
		t.Errorf("valor alterado: erro = %v, esperado ErrChecksum", err)
	}
	if err := h.Verify([]byte("ab")); !errors.Is(err, ErrTruncated) {
		t.Errorf("valor curto: erro = %v, esperado ErrTruncated", err)
	}
}

func TestAppendRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		rec  Record
		want error
	}{
		{"tipo inválido", Record{Type: 0}, ErrInvalidType},
		{"lápide com valor", Record{Type: TypeDelete, Value: []byte("x")}, ErrTombstoneValue},
		{"valor grande demais", Record{Type: TypePut, Value: make([]byte, MaxValueSize+1)}, ErrValueTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := []byte("prefixo")
			out, err := Append(prefix, tt.rec)
			if !errors.Is(err, tt.want) {
				t.Fatalf("erro = %v, esperado %v", err, tt.want)
			}
			if !bytes.Equal(out, prefix) {
				t.Fatalf("dst foi modificado em caso de erro")
			}
		})
	}
}

func TestAppendMaxValueSize(t *testing.T) {
	buf, err := Append(nil, Record{Key: 1, Type: TypePut, Value: make([]byte, MaxValueSize)})
	if err != nil {
		t.Fatalf("valor de exatamente MaxValueSize deveria ser aceito: %v", err)
	}
	if _, _, err := Decode(buf); err != nil {
		t.Fatal(err)
	}
}

// Decode nunca deve entrar em pânico, e o que ele aceita precisa
// ser reencodável para os mesmos bytes.
func FuzzDecode(f *testing.F) {
	for _, r := range sampleRecords()[:7] {
		b, _ := Append(nil, r)
		f.Add(b)
	}
	f.Add([]byte{})
	f.Add(make([]byte, HeaderSize))
	f.Fuzz(func(t *testing.T, b []byte) {
		r, n, err := Decode(b)
		if err != nil {
			return
		}
		again, err := Append(nil, r)
		if err != nil {
			t.Fatalf("registro decodificado não reencoda: %v", err)
		}
		if !bytes.Equal(again, b[:n]) {
			t.Fatalf("reencode divergiu")
		}
	})
}
