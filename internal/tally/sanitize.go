package tally

import (
	"bufio"
	"io"
	"unicode/utf8"
)

// Sanitizer is a streaming reader that removes characters Tally sometimes emits but
// XML 1.0 forbids: raw control bytes and numeric references like "&#4;".
type Sanitizer struct {
	r   *bufio.Reader
	out []byte
}

// NewSanitizer wraps r.
func NewSanitizer(r io.Reader) *Sanitizer { return &Sanitizer{r: bufio.NewReaderSize(r, 32*1024)} }

func badCtl(n int) bool { return n < 0x20 && n != 0x09 && n != 0x0A && n != 0x0D }

func (s *Sanitizer) Read(p []byte) (int, error) {
	for len(s.out) < len(p) {
		b, err := s.r.ReadByte()
		if err != nil {
			if len(s.out) > 0 {
				break
			}
			return 0, err
		}
		if b < 0x20 && badCtl(int(b)) {
			continue
		}
		if b >= 0x80 {
			// Multi-byte: keep valid UTF-8; repair invalid bytes (seen on a pilot: text typed
			// in a legacy encoding) by reading the byte as Windows-1252, so one odd character
			// never stops a sync.
			s.r.UnreadByte()
			peek, _ := s.r.Peek(utf8.UTFMax)
			if r, size := utf8.DecodeRune(peek); r != utf8.RuneError || size > 1 {
				s.out = append(s.out, peek[:size]...)
				s.r.Discard(size)
				continue
			}
			s.r.Discard(1)
			s.out = utf8.AppendRune(s.out, cp1252(b))
			continue
		}
		if b != '&' {
			s.out = append(s.out, b)
			continue
		}
		// Possible numeric reference: look ahead without consuming beyond it.
		peek, _ := s.r.Peek(8)
		if len(peek) >= 2 && peek[0] == '#' {
			n, i, ok := 0, 1, false
			for ; i < len(peek); i++ {
				c := peek[i]
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
					continue
				}
				ok = c == ';' && i > 1
				break
			}
			if ok && badCtl(n) {
				s.r.Discard(i + 1)
				continue
			}
		}
		s.out = append(s.out, b)
	}
	n := copy(p, s.out)
	s.out = s.out[n:]
	return n, nil
}

// cp1252 maps a single byte from Windows-1252 to a rune (0x80-0x9F differ from Latin-1).
func cp1252(b byte) rune {
	if b >= 0x80 && b <= 0x9F {
		if r := cp1252High[b-0x80]; r != 0 {
			return r
		}
		return utf8.RuneError
	}
	return rune(b)
}

var cp1252High = [32]rune{0x20AC, 0, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017D, 0,
	0, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0, 0x017E, 0x0178}
