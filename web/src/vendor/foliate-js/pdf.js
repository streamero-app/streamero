// Streamero: this file replaces foliate-js's PDF adapter. PDFs open in Streamero's
// own PDF viewer (pdfjs-dist), so the PDF.js build vendored by foliate-js is not
// shipped. Everything else in this folder is foliate-js as published (see LICENSE
// and THIRD_PARTY.md for the commit).
export const makePDF = async () => {
  throw new Error("PDF files are opened in the PDF viewer, not through foliate-js")
}
