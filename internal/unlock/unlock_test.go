package unlock

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

// The verification fixture from docs/code-format.md.
const (
	fixturePublicKey = "008c7deff2cbf3a7a1a584bcd07688840e98a21e79758af3d14dae5f60c44c1f"
	fixturePayload   = "0101000100010102030405060708090a0b0c0d0e0f100000510e"
	fixtureSignature = "1019410784ca3c9cc4665b73619cd4ef8391214291318c19c46506a8586b3ba4ae6ca4eaccccc6c247763aa89325cca4322dbe4d7b51c84a9b34d77a4a617408"
	fixtureCode      = "DL1.AEAQAAIAAEAQEAYEAUDAOCAJBIFQYDIOB4IAAACRBYIBSQIHQTFDZHGEMZNXGYM42TXYHEJBIKITDDAZYRSQNKCYNM52JLTMUTVMZTGGYJDXMOVISMS4ZJBSFW7E262RZBFJWNGXPJFGC5AI"
)

func fixtureKeys(t *testing.T) map[uint8]ed25519.PublicKey {
	t.Helper()
	key, err := hex.DecodeString(fixturePublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return map[uint8]ed25519.PublicKey{1: key}
}

func verifyFixture(t *testing.T, input string) (Code, error) {
	t.Helper()
	return Verify(input, 1, []uint16{1}, fixtureKeys(t))
}

// envelopeCode encodes raw envelope bytes as code text, valid or not.
func envelopeCode(t *testing.T, payloadHex, signatureHex string) string {
	t.Helper()
	b, err := hex.DecodeString(payloadHex + signatureHex)
	if err != nil {
		t.Fatal(err)
	}
	return Prefix + encoding.EncodeToString(b)
}

// withByte returns the fixture payload with byte i set to v.
func withByte(i int, v byte) string {
	b, _ := hex.DecodeString(fixturePayload)
	b[i] = v
	return hex.EncodeToString(b)
}

func TestFixtureEncodes(t *testing.T) {
	if got := envelopeCode(t, fixturePayload, fixtureSignature); got != fixtureCode {
		t.Fatalf("envelope encodes as %s, want %s", got, fixtureCode)
	}
	if len(fixtureCode) != 148 {
		t.Fatalf("fixture is %d characters, want 148", len(fixtureCode))
	}
}

func TestVerifyAcceptsFixture(t *testing.T) {
	c, err := verifyFixture(t, fixtureCode)
	if err != nil {
		t.Fatal(err)
	}
	wantID, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f10")
	if c.Version != 1 || c.KeyID != 1 || c.Project != 1 || c.Product != 1 || string(c.ID[:]) != string(wantID) || c.IssuedDay != 20750 {
		t.Fatalf("fields = %+v", c)
	}
	if day, _ := IssuedDay(time.Date(2026, 10, 24, 23, 59, 0, 0, time.UTC)); day != c.IssuedDay {
		t.Fatalf("2026-10-24 is day %d, want %d", day, c.IssuedDay)
	}
	if c.Canonical() != fixtureCode {
		t.Fatalf("canonical = %s", c.Canonical())
	}
}

func TestVerifyAcceptsPresentationVariants(t *testing.T) {
	grouped := Grouped(fixtureCode)
	if len(grouped) != 165 {
		t.Fatalf("grouped code is %d characters, want 165", len(grouped))
	}
	for name, input := range map[string]string{
		"lowercase":                     strings.ToLower(fixtureCode),
		"mixed case":                    "dL1." + strings.ToLower(fixtureCode[4:80]) + fixtureCode[80:],
		"whitespace":                    " " + fixtureCode[:40] + "\r\n\t" + fixtureCode[40:] + " \n",
		"grouped":                       grouped,
		"grouped lowercase with spaces": strings.ReplaceAll(strings.ToLower(grouped), "-", " - "),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := verifyFixture(t, input)
			if err != nil {
				t.Fatal(err)
			}
			if c.Canonical() != fixtureCode {
				t.Fatalf("canonical = %s", c.Canonical())
			}
		})
	}
}

func TestVerifyIgnoresClockAndRepeats(t *testing.T) {
	// Verify takes no time at all, so a wrong device clock can't matter;
	// checking the same code twice gives the same entitlement.
	first, err1 := verifyFixture(t, fixtureCode)
	second, err2 := verifyFixture(t, Grouped(fixtureCode))
	if err1 != nil || err2 != nil || first.Canonical() != second.Canonical() || first.Product != second.Product {
		t.Fatalf("repeat check differs: %v %v", err1, err2)
	}
}

func TestVerifyRejects(t *testing.T) {
	otherKey, _, _ := ed25519.GenerateKey(rand.Reader)
	flippedSignature := "2" + fixtureSignature[1:]
	cases := []struct {
		name     string
		input    string
		project  uint16
		products []uint16
		keys     map[uint8]ed25519.PublicKey
		want     error
	}{
		{name: "changed payload", input: envelopeCode(t, withByte(25, 0x0f), fixtureSignature), want: ErrBadSignature},
		{name: "changed signature", input: envelopeCode(t, fixturePayload, flippedSignature), want: ErrBadSignature},
		{name: "wrong key", input: fixtureCode, keys: map[uint8]ed25519.PublicKey{1: otherKey}, want: ErrBadSignature},
		{name: "wrong project", input: fixtureCode, project: 2, want: ErrWrongProject},
		{name: "unsupported product", input: fixtureCode, products: []uint16{2}, want: ErrUnsupportedProduct},
		{name: "unknown key id", input: envelopeCode(t, withByte(1, 2), fixtureSignature), want: ErrUnknownKey},
		{name: "binary version 2", input: envelopeCode(t, withByte(0, 2), fixtureSignature), want: ErrUnsupportedVersion},
		{name: "prefix version 2", input: "DL2." + fixtureCode[4:], want: ErrUnsupportedVersion},
		{name: "zero version", input: envelopeCode(t, withByte(0, 0), fixtureSignature), want: ErrUnsupportedVersion},
		{name: "zero key id", input: envelopeCode(t, withByte(1, 0), fixtureSignature), want: ErrMalformed},
		{name: "zero project", input: envelopeCode(t, withByte(3, 0), fixtureSignature), want: ErrMalformed},
		{name: "zero product", input: envelopeCode(t, withByte(5, 0), fixtureSignature), want: ErrMalformed},
		{name: "zero code id", input: envelopeCode(t, "010100010001"+strings.Repeat("00", 16)+"0000510e", fixtureSignature), want: ErrMalformed},
		{name: "no prefix", input: fixtureCode[4:], want: ErrMalformed},
		{name: "short", input: fixtureCode[:147], want: ErrMalformed},
		{name: "long", input: fixtureCode + "A", want: ErrMalformed},
		{name: "extra bytes", input: envelopeCode(t, fixturePayload, fixtureSignature+"0000000000"), want: ErrMalformed},
		{name: "padding", input: fixtureCode + "====", want: ErrMalformed},
		{name: "digit zero", input: fixtureCode[:10] + "0" + fixtureCode[11:], want: ErrMalformed},
		{name: "digit one", input: fixtureCode[:10] + "1" + fixtureCode[11:], want: ErrMalformed},
		{name: "punctuation", input: fixtureCode[:10] + "_" + fixtureCode[11:], want: ErrMalformed},
		{name: "unicode lookalike", input: fixtureCode[:10] + "Ａ" + fixtureCode[11:], want: ErrMalformed},
		{name: "too long input", input: fixtureCode + strings.Repeat(" ", 400), want: ErrMalformed},
		{name: "empty", input: "", want: ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, products, keys := uint16(1), []uint16{1}, fixtureKeys(t)
			if tc.project != 0 {
				project = tc.project
			}
			if tc.products != nil {
				products = tc.products
			}
			if tc.keys != nil {
				keys = tc.keys
			}
			if _, err := Verify(tc.input, project, products, keys); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewRoundTrips(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
	code, err := New(private, 3, 1, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Verify(code, 1, []uint16{1, 7}, map[uint8]ed25519.PublicKey{3: public})
	if err != nil {
		t.Fatal(err)
	}
	if c.KeyID != 3 || c.Product != 7 || c.IssuedDay != 20751 || c.Canonical() != code {
		t.Fatalf("fields = %+v", c)
	}
	again, _ := New(private, 3, 1, 7, now)
	if again == code {
		t.Fatal("two codes share a code ID")
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	for name, mint := range map[string]func() (string, error){
		"zero key id":  func() (string, error) { return New(private, 0, 1, 1, now) },
		"zero project": func() (string, error) { return New(private, 1, 0, 1, now) },
		"zero product": func() (string, error) { return New(private, 1, 1, 0, now) },
		"short key":    func() (string, error) { return New(private[:32], 1, 1, 1, now) },
		"before 1970":  func() (string, error) { return New(private, 1, 1, 1, time.Unix(-1, 0)) },
	} {
		if _, err := mint(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
