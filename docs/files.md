# Files

Most mail clients show you attachments: a list of arrivals, one per message,
so the contract that went back and forth six times appears six times. InboxQL
shows you **files**. A file is its bytes, so the same document sent to five
people is one file that arrived five times — and "which version is this?" and
"who else has it?" become questions you can ask.

## The File tab

Click any attachment and it opens on its own, in the File tab (or inline in
the message — Settings → General → **Attachment Previews** chooses which, or
both). The File tab shows:

- a preview, for PDFs, images and text;
- its size, type and every name it arrived under;
- **every message that carried it**, newest first, each a link — or, when it
  arrived only once, the one message it came on;
- the text InboxQL has read out of it, if any.

The rail's **Files** entry lists them all.

## Asking about files

`in:attachments` makes a query about files rather than mail:

```
in:attachments filetype:pdf larger:1mb
in:attachments content:invoice from:*@acme.com
in:attachments is:shared | count by from
in:attachments messages>1
```

| Term | Matches |
|---|---|
| `filename:` `name:` | a name it arrived under — any of them |
| `filetype:` `type:` | `pdf` `image` `photo` `audio` `video` `doc` `sheet` `slides`, or a MIME type such as `image/` |
| `content:` | words **inside** the file, once it has been read |
| `size:` `larger:` `smaller:` | the file's own size |
| `messages:` | how many messages carried it — `messages>1` is a document that went round |
| `is:` | `shared` `inline` `attached` `stored` `missing` `read` `unread` `scanned` `searchable` |
| `has:text` | something was read out of it |
| `similar:<id>` | files close to it in meaning, once embedded |

Any message field works too, and asks about the mail the file came on:
`in:attachments from:*@acme.com after:7d` is files that came from Acme this
week. A file that arrived from two people is from both.

A bare word searches the file's name, its text, and the message it came on.

### The other way round: mail by its files

`attached:` is an ordinary mail term — which mail carried this file:

```
attached:contract -from:me()      somebody sent me the contract
attached:*.pdf after:7d           mail with a PDF on it, this week
attached:128072f0de5e             mail that carried this exact file
```

A value of eight or more hexadecimal characters is matched against the file's
fingerprint, so it finds that exact file whatever it was called; anything else
is a name.

## Three silences that mean different things

A search of file contents that finds nothing can mean three things, and InboxQL
keeps them apart:

| | |
|---|---|
| `is:unread` | nobody has read inside this file yet |
| `is:scanned` | it was read and holds no text — usually a scan or a photo of a page |
| `is:missing` | it is recorded, but its bytes are not on this disk |

So on a mailbox where no files have been read, `content:invoice` returning
nothing means *nobody has looked*, not *no file says invoice*. `iql doctor`
says which situation you are in.

## Reading files

Files are stored and read in steps you can run when it suits you:

```sh
iql maintenance attachments   # store the files that are only recorded
iql maintenance text          # read the text out of PDFs and plain formats
iql ocr                       # read scans, with a vision model
```

Text is read from PDFs and plain-text formats. A scan has no text layer, so it
needs `iql ocr`, which uses a vision model through your AI provider. Some
formats have no reader at all yet; those stay `is:unread`.

Extractors read files too: on a mailbox of receipts, the amounts are usually
in the attached PDF rather than the message, and `receipts` finds them there
once the text has been read.

## Where files are kept

In your mailbox folder, under `attachments/`, each stored once by its
fingerprint. They are not inside the database, so **a plain `iql backup` does
not include them**; `iql backup --include-attachments` archives them beside the
database copy, and says so when it is leaving them out.

Imports leave attachments off unless you ask for them, because they can
multiply the size of a mailbox. See [Importing](accounts-and-import.md).
