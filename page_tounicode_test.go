package pdf

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

// buildPDF assembles a minimal single-object-per-line PDF and fixes up the
// xref offsets, so these tests carry their fixtures inline rather than as
// binary testdata.
func buildPDF(objs []string, trailerExtra string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, o := range objs {
		offsets[i+1] = buf.Len()
		buf.WriteString(o)
	}
	xref := buf.Len()
	buf.WriteString("xref\n")
	fmt.Fprintf(&buf, "0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<</Size %d %s>>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, trailerExtra, xref)
	return buf.Bytes()
}

// A /Pages node listing itself in /Kids. Reader.Page descends with num
// unchanged, so without a depth bound this loops forever. It raises no panic,
// so a recover() in the caller cannot catch it.
func TestPageCyclicPageTreeTerminates(t *testing.T) {
	doc := buildPDF([]string{
		"1 0 obj\n<</Type/Catalog/Pages 2 0 R>>\nendobj\n",
		"2 0 obj\n<</Type/Pages/Count 10/Kids[2 0 R]>>\nendobj\n",
	}, "/Root 1 0 R")

	r, err := NewReader(bytes.NewReader(doc), int64(len(doc)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	done := make(chan Page, 1)
	go func() { done <- r.Page(1) }()

	select {
	case p := <-done:
		if !p.V.IsNull() {
			t.Fatalf("cyclic page tree returned a page; want null")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Reader.Page did not terminate on a cyclic page tree")
	}
}

// A font declaring /Encoding /Identity-H alongside a ToUnicode CMap must
// decode through the CMap. Returning a pass-through encoder yields mojibake
// for every subsetted font, which is what Chrome, Word and Acrobat all emit.
func TestFontEncoderIdentityHUsesToUnicode(t *testing.T) {
	cmap := "/CIDInit /ProcSet findresource begin\n" +
		"12 dict begin\nbegincmap\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		"2 beginbfchar\n<0039> <0056>\n<0048> <0065>\nendbfchar\n" +
		"endcmap\nend\nend\n"

	doc := buildPDF([]string{
		"1 0 obj\n<</Type/Catalog/Pages 2 0 R>>\nendobj\n",
		"2 0 obj\n<</Type/Pages/Count 0/Kids[]>>\nendobj\n",
		"3 0 obj\n<</Type/Font/Subtype/Type0/Encoding/Identity-H/ToUnicode 4 0 R>>\nendobj\n",
		"4 0 obj\n<</Length " + itoa(len(cmap)) + ">>\nstream\n" + cmap + "\nendstream\nendobj\n",
	}, "/Root 1 0 R")

	r, err := NewReader(bytes.NewReader(doc), int64(len(doc)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	v, err := r.GetObject(3)
	if err != nil {
		t.Fatalf("GetObject(3): %v", err)
	}
	f := Font{v}
	got := f.Encoder().Decode("\x00\x39\x00\x48")
	if got != "Ve" {
		t.Fatalf("Identity-H + ToUnicode decoded %q; want %q", got, "Ve")
	}
	if strings.ContainsRune(got, noRune) {
		t.Fatalf("decode produced replacement runes: %q", got)
	}
}
