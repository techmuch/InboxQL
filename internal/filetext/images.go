package filetext

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PageImage is one image lifted out of a document.
type PageImage struct {
	// Page is the 1-based page it was found on, or 0 when the page could not
	// be determined.
	Page int
	// MIME is what the bytes are: image/jpeg today, since that is what scanners
	// produce and what can be handed straight to a decoder.
	MIME string
	Data []byte
	// Width and Height as the document declares them, for deciding whether an
	// image is a page or a logo.
	Width, Height int
}

// PageImages returns the images a PDF's pages are made of.
//
// # What this is for
//
// A scanned document has no text to extract — [Extract] says so and stops.
// The words are still there, as pixels, and getting at them means handing a
// picture of the page to something that can read it. This produces those
// pictures without rendering anything: a scanned page *is* one large image,
// stored in the file as a JPEG, so it can be lifted out whole.
//
// # Why only JPEG
//
// A DCTDecode stream is a complete JPEG file and needs no work at all; every
// consumer of an image already reads JPEG. The other scanner codecs —
// CCITTFaxDecode for fax-style bitonal scans, JBIG2, JPX — each need a decoder
// and an encoder to become anything usable, which is a large amount of code in
// service of a format that is disappearing. A page in one of those is reported
// as unavailable rather than silently skipped, so the caller can say which
// files it could not look at.
func PageImages(data []byte, minPixels int) ([]PageImage, error) {
	doc, err := parsePDF(data)
	if err != nil {
		return nil, err
	}
	if doc.encrypted {
		return nil, fmt.Errorf("the document is encrypted")
	}

	var out []PageImage
	pages := doc.pageObjects()

	for i, num := range pages {
		page := doc.objects[num]
		if page == nil {
			continue
		}
		for _, obj := range doc.pageXObjects(page.dict) {
			img, ok := doc.imageFrom(obj)
			if !ok {
				continue
			}
			if minPixels > 0 && img.Width*img.Height < minPixels {
				// A signature logo or a spacer. Passing those to an OCR engine
				// costs a request per image and returns nothing.
				continue
			}
			img.Page = i + 1
			out = append(out, img)
			if len(out) >= maxPagesToExtract {
				return out, nil
			}
		}
	}

	// No page tree this scanner could follow: take every image in the file and
	// leave the page unattributed rather than reporting none.
	if len(out) == 0 {
		for num := range doc.objects {
			img, ok := doc.imageFrom(num)
			if !ok || (minPixels > 0 && img.Width*img.Height < minPixels) {
				continue
			}
			out = append(out, img)
			if len(out) >= maxPagesToExtract {
				break
			}
		}
	}
	return out, nil
}

var xobjectRef = regexp.MustCompile(`/([A-Za-z0-9]+)\s+(\d+)\s+\d+\s+R`)

// pageXObjects lists the object numbers in a page's /XObject resources.
func (d *pdfDoc) pageXObjects(dict string) []int {
	resources := d.resolveDict(dict, "/Resources")
	if resources == "" {
		return nil
	}
	xobjects := d.resolveDict(resources, "/XObject")
	if xobjects == "" {
		return nil
	}

	var out []int
	for _, m := range xobjectRef.FindAllStringSubmatch(xobjects, 64) {
		if n, err := strconv.Atoi(m[2]); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// imageFrom returns an object as an image, when it is one this can hand over.
func (d *pdfDoc) imageFrom(num int) (PageImage, bool) {
	obj := d.objects[num]
	if obj == nil || len(obj.stream) == 0 {
		return PageImage{}, false
	}
	if !strings.Contains(obj.dict, "/Image") {
		return PageImage{}, false
	}
	// DCTDecode means the stream is a JPEG already. Anything else would need a
	// decoder this package deliberately does not carry.
	if !strings.Contains(obj.dict, "/DCTDecode") {
		return PageImage{}, false
	}

	img := PageImage{
		MIME:   "image/jpeg",
		Data:   obj.stream,
		Width:  dictInt(obj.dict, "/Width"),
		Height: dictInt(obj.dict, "/Height"),
	}

	// A JPEG starts with SOI. Checking it catches a stream whose filter chain
	// left something else in front, which would otherwise be sent onward as a
	// picture and rejected by whatever received it.
	if !bytes.HasPrefix(img.Data, []byte{0xFF, 0xD8}) {
		if i := bytes.Index(img.Data, []byte{0xFF, 0xD8, 0xFF}); i >= 0 && i < 64 {
			img.Data = img.Data[i:]
		} else {
			return PageImage{}, false
		}
	}
	return img, true
}

// HasImagesOnly reports whether a PDF's pages carry images this cannot read.
//
// Used to tell "a scan in a format we can lift out" from "a scan in a format
// we cannot", so a file that OCR will never reach says so once rather than
// being retried.
func HasUnreadableImages(data []byte) bool {
	return bytes.Contains(data, []byte("/CCITTFaxDecode")) ||
		bytes.Contains(data, []byte("/JBIG2Decode")) ||
		bytes.Contains(data, []byte("/JPXDecode"))
}
