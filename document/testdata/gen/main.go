// testdata/gen/main.go 는 테스트용 PDF·DOCX 고정 파일을 생성하는 보조 도구다.
// `go run ./document/testdata/gen/` 으로 실행한다.
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
)

func main() {
	if err := genPDF(); err != nil {
		fmt.Fprintln(os.Stderr, "PDF 생성 실패:", err)
		os.Exit(1)
	}
	if err := genDOCX(); err != nil {
		fmt.Fprintln(os.Stderr, "DOCX 생성 실패:", err)
		os.Exit(1)
	}
	fmt.Println("testdata 파일 생성 완료")
}

// genPDF 는 단순한 1페이지 PDF 고정 파일을 생성한다.
// PDF 스펙 기준 최소 구조: header, body(카탈로그·페이지 트리·페이지·폰트·콘텐츠 스트림), xref, trailer.
func genPDF() error {
	// ledongthuc/pdf 가 파싱 가능한 최소 PDF 를 수동으로 조립한다.
	// 텍스트: "Hello PDF World"
	var buf bytes.Buffer

	// 1) 헤더
	buf.WriteString("%PDF-1.4\n")

	// 2) 오브젝트들
	offsets := make([]int, 0)

	// obj 1: 카탈로그
	offsets = append(offsets, buf.Len())
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	// obj 2: 페이지 트리
	offsets = append(offsets, buf.Len())
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	// obj 3: 페이지
	offsets = append(offsets, buf.Len())
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")

	// obj 4: 콘텐츠 스트림
	stream := "BT /F1 12 Tf 100 700 Td (Hello PDF World) Tj ET\n"
	offsets = append(offsets, buf.Len())
	fmt.Fprintf(&buf, "4 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(stream), stream)

	// obj 5: 폰트
	offsets = append(offsets, buf.Len())
	buf.WriteString("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	// 3) xref
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 6\n0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}

	// 4) trailer
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)

	return os.WriteFile("../sample.pdf", buf.Bytes(), 0644)
}

// genDOCX 는 텍스트 "Hello DOCX World" 를 담은 최소 DOCX 고정 파일을 생성한다.
// DOCX 는 ZIP 구조이므로 직접 조립한다.
func genDOCX() error {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	// [Content_Types].xml
	ct, _ := w.Create("[Content_Types].xml")
	ct.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`))

	// _rels/.rels
	rels, _ := w.Create("_rels/.rels")
	rels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`))

	// word/_rels/document.xml.rels
	docRels, _ := w.Create("word/_rels/document.xml.rels")
	docRels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>`))

	// word/document.xml
	docXML, _ := w.Create("word/document.xml")
	docXML.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r>
        <w:t>Hello DOCX World</w:t>
      </w:r>
    </w:p>
  </w:body>
</w:document>`))

	w.Close()
	return os.WriteFile("../sample.docx", buf.Bytes(), 0644)
}
