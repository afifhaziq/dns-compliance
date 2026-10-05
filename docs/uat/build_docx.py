"""Usage: python3 docs/uat/build_docx.py   (needs pandoc 3.x)

Convert docs/uat/UAT_Citadel_v0.1.tex to .docx, styled like Benchmark_reportV5.pdf.

Pandoc's LaTeX reader drops \\clearpage, \\tableofcontents and all page styling, so this
patches pandoc's default reference.docx (Verdana, black headings, grid tables, footer) and
uses a Lua filter to put page breaks + a Word Contents field where the .tex has them.
"""
import os, re, shutil, subprocess, tempfile, zipfile

PANDOC = shutil.which("pandoc") or os.path.expanduser("~/.local/bin/pandoc")
UAT = os.path.dirname(os.path.abspath(__file__))
SRC, OUT = f"{UAT}/UAT_Citadel_v0.1.tex", f"{UAT}/UAT_Citadel_v0.1.docx"
FOOTER_TEXT = "Citadel - DNS Blocking Tracker System: User Acceptance Test"
FONT = "Verdana"
PAGE_W, MARGIN = 11906, 1134  # A4 portrait, 2 cm margins (twips)

W = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"'
BLACK_BORDERS = "".join(f'<w:{e} w:val="single" w:sz="4" w:space="0" w:color="000000"/>'
                        for e in ("top", "left", "bottom", "right", "insideH", "insideV"))


def style(sid, name, based, ppr, rpr, extra=""):
    return (f'<w:style w:type="paragraph" w:styleId="{sid}"><w:name w:val="{name}"/>'
            f'<w:basedOn w:val="{based}"/><w:next w:val="BodyText"/><w:qFormat/>{extra}'
            f'<w:pPr>{ppr}</w:pPr><w:rPr>{rpr}</w:rPr></w:style>')


def bold(sz):
    return f'<w:b/><w:bCs/><w:color w:val="000000"/><w:sz w:val="{sz}"/><w:szCs w:val="{sz}"/>'


STYLES = {
    "Title": style("Title", "Title", "Normal", '<w:spacing w:before="240" w:after="360"/>', bold(32)),
    "Heading1": style("Heading1", "heading 1", "Normal",
                      '<w:keepNext/><w:keepLines/><w:spacing w:before="360" w:after="160"/><w:outlineLvl w:val="0"/>', bold(28)),
    "Heading2": style("Heading2", "heading 2", "Normal",
                      '<w:keepNext/><w:keepLines/><w:spacing w:before="280" w:after="120"/><w:outlineLvl w:val="1"/>', bold(22)),
    "Heading3": style("Heading3", "heading 3", "Normal",
                      '<w:keepNext/><w:keepLines/><w:spacing w:before="280" w:after="100"/><w:outlineLvl w:val="2"/>', bold(20)),
    "TOCHeading": style("TOCHeading", "TOC Heading", "Heading1",
                        '<w:spacing w:before="0" w:after="240"/><w:outlineLvl w:val="9"/>', bold(28)),
    "Table": ('<w:style w:type="table" w:default="1" w:styleId="Table"><w:name w:val="Table"/>'
              '<w:basedOn w:val="TableNormal"/><w:qFormat/><w:tblPr><w:tblInd w:w="0" w:type="dxa"/>'
              f'<w:tblBorders>{BLACK_BORDERS}</w:tblBorders><w:tblCellMar><w:top w:w="40" w:type="dxa"/>'
              '<w:left w:w="100" w:type="dxa"/><w:bottom w:w="40" w:type="dxa"/><w:right w:w="100" w:type="dxa"/>'
              '</w:tblCellMar></w:tblPr></w:style>'),
}

FOOTER = (f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:ftr {W}><w:p><w:pPr>'
          '<w:pBdr><w:top w:val="single" w:sz="4" w:space="6" w:color="808080"/></w:pBdr><w:jc w:val="center"/>'
          '<w:spacing w:after="0"/></w:pPr>'
          f'<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:t xml:space="preserve">{FOOTER_TEXT}   |   Page </w:t></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="begin"/></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="separate"/></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:t>1</w:t></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="end"/></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:t xml:space="preserve"> of </w:t></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="begin"/></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:instrText xml:space="preserve"> NUMPAGES </w:instrText></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="separate"/></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:t>1</w:t></w:r>'
          '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="end"/></w:r></w:p></w:ftr>')

PAGEBREAK = '<w:p><w:r><w:br w:type="page"/></w:r></w:p>'
TOC = ('<w:p><w:pPr><w:pStyle w:val="TOCHeading"/></w:pPr><w:r><w:t>Contents</w:t></w:r></w:p>'
       '<w:p><w:r><w:fldChar w:fldCharType="begin" w:dirty="true"/></w:r>'
       '<w:r><w:instrText xml:space="preserve"> TOC \\o "1-2" \\h \\z \\u </w:instrText></w:r>'
       '<w:r><w:fldChar w:fldCharType="separate"/></w:r>'
       '<w:r><w:t>Right-click here and choose Update Field to build the table of contents.</w:t></w:r>'
       '<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>')

# Mirrors the .tex: cover table, \clearpage, contents, \clearpage, and \clearpage before these sections.
LUA = r'''
local done_cover = false
local breaks = { ["test-scripts"] = true, ["test-summary"] = true }
local PB = pandoc.RawBlock("openxml", [==[PAGEBREAK_XML]==])
local TOC = pandoc.RawBlock("openxml", [==[TOC_XML]==])
function Pandoc(doc)
  local out = {}
  for _, b in ipairs(doc.blocks) do
    if b.t == "Header" and b.level == 1 and breaks[b.identifier] then table.insert(out, PB) end
    table.insert(out, b)
    if b.t == "Table" and not done_cover then
      done_cover = true
      table.insert(out, PB); table.insert(out, TOC); table.insert(out, PB)
    end
  end
  doc.blocks = out
  return doc
end
'''.replace("PAGEBREAK_XML", PAGEBREAK).replace("TOC_XML", TOC)


def read_zip(path):
    with zipfile.ZipFile(path) as z:
        return {n: z.read(n) for n in z.namelist()}


def write_zip(path, items):
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        for n, b in items.items():
            z.writestr(n, b)


def build_reference(path):
    items = read_zip(path + ".default")
    s = items["word/styles.xml"].decode()
    s = re.sub(r'<w:rFonts w:asciiTheme[^>]*/>', f'<w:rFonts w:ascii="{FONT}" w:hAnsi="{FONT}" w:eastAsia="{FONT}" w:cs="{FONT}"/>', s)
    s = re.sub(r'<w:rFonts w:eastAsiaTheme[^>]*/>', f'<w:rFonts w:ascii="{FONT}" w:hAnsi="{FONT}" w:eastAsia="{FONT}" w:cs="{FONT}"/>', s)
    s = re.sub(r'(<w:rPrDefault>.*?<w:sz w:val=")24(" />\s*<w:szCs w:val=")24', r'\g<1>20\g<2>20', s, flags=re.S)
    for sid, xml in STYLES.items():
        s, n = re.subn(r'<w:style [^>]*w:styleId="%s">.*?</w:style>' % sid, xml, s, flags=re.S)
        assert n == 1, sid
    items["word/styles.xml"] = s.encode()

    t = items["word/theme/theme1.xml"].decode()
    items["word/theme/theme1.xml"] = re.sub(r'(<a:(?:major|minor)Font><a:latin typeface=")[^"]*', rf'\g<1>{FONT}', t).encode()

    items["word/footer1.xml"] = FOOTER.encode()
    rels = items["word/_rels/document.xml.rels"].decode()
    items["word/_rels/document.xml.rels"] = rels.replace("</Relationships>",
        '<Relationship Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" '
        'Id="rIdFooter1" Target="footer1.xml"/></Relationships>').encode()
    ct = items["[Content_Types].xml"].decode()
    items["[Content_Types].xml"] = ct.replace("</Types>",
        '<Override PartName="/word/footer1.xml" '
        'ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/></Types>').encode()

    d = items["word/document.xml"].decode()
    d = d.replace("<w:sectPr>",
        f'<w:sectPr><w:footerReference w:type="default" r:id="rIdFooter1"/>'
        f'<w:pgSz w:w="{PAGE_W}" w:h="16838"/><w:pgMar w:top="{MARGIN}" w:right="{MARGIN}" '
        f'w:bottom="{MARGIN}" w:left="{MARGIN}" w:header="567" w:footer="567" w:gutter="0"/>', 1)
    items["word/document.xml"] = d.encode()

    st = items["word/settings.xml"].decode()  # Word offers to fill the Contents field on open
    items["word/settings.xml"] = re.sub(r"(<w:settings[^>]*>)", r'\1<w:updateFields w:val="true"/>', st, 1).encode()
    write_zip(path, items)


CHECKBOX = ('<w:sdt><w:sdtPr><w14:checkbox><w14:checked w14:val="{on}"/>'
            '<w14:checkedState w14:val="2612" w14:font="MS Gothic"/>'
            '<w14:uncheckedState w14:val="2610" w14:font="MS Gothic"/></w14:checkbox></w:sdtPr>'
            '<w:sdtContent><w:r><w:rPr><w:rFonts w:ascii="MS Gothic" w:eastAsia="MS Gothic" w:hAnsi="MS Gothic"/>'
            '</w:rPr><w:t>{glyph}</w:t></w:r></w:sdtContent></w:sdt>')


def clickable_boxes(m):
    """Turn a '☐ Pass ☐ Conditional Pass ☐ Fail' run into Word checkbox content controls (■ = pre-ticked)."""
    out = []
    for part in re.split(r"([☐■])", m.group(1)):
        if part in ("☐", "■"):
            out.append(CHECKBOX.format(on=int(part == "■"), glyph="☒" if part == "■" else "☐"))
        elif part.strip():
            out.append(f'<w:r><w:t xml:space="preserve"> {part.strip()}      </w:t></w:r>')
    return "".join(out)


def keep_together(m):
    """Stop rows splitting and chain every row but the last to the next, so a card never breaks across pages."""
    rows = re.split(r"(?=<w:tr>)", m.group(0))
    for i in range(1, len(rows) - 1):
        rows[i] = rows[i].replace('<w:pStyle w:val="Compact" />', '<w:pStyle w:val="Compact" /><w:keepNext/>')
    t = "".join(rows).replace("<w:trPr>", "<w:trPr><w:cantSplit/>")
    return re.sub(r"<w:tr>(?!\s*<w:trPr>)", "<w:tr><w:trPr><w:cantSplit/></w:trPr>", t)


def main():
    with tempfile.TemporaryDirectory() as tmp:
        ref, lua = f"{tmp}/reference.docx", f"{tmp}/breaks.lua"
        subprocess.run([PANDOC, "-o", ref + ".default", "--print-default-data-file", "reference.docx"], check=True)
        build_reference(ref)
        open(lua, "w").write(LUA)
        subprocess.run([PANDOC, SRC, "--number-sections", f"--reference-doc={ref}",
                        f"--lua-filter={lua}", "-o", OUT], check=True)

    items = read_zip(OUT)
    d = items["word/document.xml"].decode()
    k = (PAGE_W - 2 * MARGIN) / 7920  # pandoc sizes tables for its own 6-inch text width
    d = re.sub(r'(<w:gridCol w:w=")(\d+)', lambda m: m.group(1) + str(int(int(m.group(2)) * k)), d)
    d = re.sub(r"<w:tbl>.*?</w:tbl>", keep_together, d, flags=re.S)
    d = re.sub(r'<w:r><w:t xml:space="preserve">([^<]*[☐■][^<]*)</w:t></w:r>', clickable_boxes, d)
    d = d.replace("<w:document ", '<w:document xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" '
                  'xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="w14" ', 1)
    items["word/document.xml"] = d.encode()
    write_zip(OUT, items)
    print("wrote", OUT)


if __name__ == "__main__":
    main()
