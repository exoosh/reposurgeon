// repocutter is a tool for analyzing and diddecting Subversion stream files.
package main

// Copyright by Eric S. Raymond
// SPDX-License-Identifier: BSD-2-Clause

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	terminal "golang.org/x/crypto/ssh/terminal" // For GetSize()
)

const linesep = "\n"

var doc = `repocutter - stream surgery on SVN dump files
general usage: repocutter [-q] [-r SELECTION] SUBCOMMAND

In all commands, the -r (or --range) option limits the selection of revisions
over which an operation will be performed. A selection consists of one or more
comma-separated ranges. A range may consist of an integer revision number or
the special name HEAD for the head revision. Or it may be a colon-separated
pair of integers, or an integer followed by a colon followed by HEAD.

Normally, each subcommand produces a progress spinner on standard error; each
turn means another revision has been filtered. The -q (or --quiet) option
suppresses this.

Type 'repocutter help <subcommand>' for help on a specific subcommand.

Available subcommands and help topics:
   deselect
   expunge
   log
   obscure
   pathrename
   pop
   propdel
   proprename
   propset
   reduce
   renumber
   replace
   see
   select
   setlog
   sift
   strip
   swap
   testify
   version
`

// Translated from the 2017-12-13 version of repocutter,
// which began life as 'svncutter' in 2009.  The obsolete
// 'squash' command has been omitted.

var debug = false

var oneliners = map[string]string{
	"deselect":   "Deselecting revisions",
	"expunge":    "Expunge operations by Node-path header",
	"log":        "Extracting log entries",
	"obscure":    "Obscure pathnames",
	"pathrename": "Transform path headers with a regexp replace",
	"pop":        "Pop the first segment off each path",
	"propdel":    "Deleting revision properties",
	"proprename": "Renaming revision properties",
	"propset":    "Setting revision properties",
	"reduce":     "Topologically reduce a dump.",
	"renumber":   "Renumber revisions so they're contiguous",
	"replace":    "Regexp replace in blobs",
	"see":        "Report only essential topological information",
	"select":     "Selecting revisions",
	"setlog":     "Mutating log entries",
	"sift":       "Sift for operations by Node-path header",
	"strip":      "Replace content with unique cookies, preserving structure",
	"swap":       "Swap first two components of pathnames",
	"testify":    "Massage a stream file into a neutralized test load",
	"version":    "Report repocutter's version",
}

var helpdict = map[string]string{
	"deselect": `deselect: usage: repocutter [-q] [-r SELECTION] deselect

The 'deselect' subcommand selects a range and permits only revisions NOT in
that range to pass to standard output.
`,
	"expunge": `expunge: usage: repocutter [-r SELECTION ] expunge PATTERN...

Delete all operations with Node-path headers matching specified
Golang regular expressions (opposite of 'sift').  Any revision
left with no Node records after this filtering has its Revision
record removed as well.
`,
	"log": `log: usage: repocutter [-r SELECTION] log

Generate a log report, same format as the output of svn log on a
repository, to standard output.
`,
	"obscure": `obscure: usage: repocutter [-r SELECTION] obscure

Replace path segments and committer IDs with arbitrary but consistent
names in order to obscure them. The replacement algorithm is tuned to
make the replacements readily distinguishable by eyeball.
`,
	"pathrename": `pathrename: usage: repocutter [-r SELECTION ] pathrename FROM TO

Modify Node-path headers, Node-copyfrom-path headers, and
svn:mergeinfo properties matching the specified Golang regular
expression FROM; replace with TO.  TO may contain Golang-style
backreferences (${1}, ${2} etc - parentheses not optional) to
parenthesized portions of FROM.
`,
	"propdel": `propdel: usage: repocutter [-r SELECTION] propdel PROPNAME...

Delete the property PROPNAME. May be restricted by a revision
selection. You may specify multiple properties to be deleted.

`,
	"pop": `pop: usage: repocutter [-r SELECTION ] pop

Pop initial segment off each path. May be useful after a sift command to turn
a dump from a subproject stripped from a dump for a multiple-project repository
into the normal form with trunk/tags/branches at the top level.
`,
	"proprename": `proprename: usage: repocutter [-r SELECTION] proprename OLDNAME->NEWNAME...

Rename the property OLDNAME to NEWNAME. May be restricted by a
revision selection. You may specify multiple properties to be renamed.
`,
	"propset": `propset: usage: repocutter [-r SELECTION] propset PROPNAME=PROPVAL...

Set the property PROPNAME to PROPVAL. May be restricted by a revision
selection. You may specify multiple property settings.
`,
	"renumber": `renumber: usage: repocutter renumber

Renumber all revisions, patching Node-copyfrom headers as required.
Any selection option is ignored. Takes no arguments.  The -b option 
1can be used to set the base to renumber from, defaulting to 0.
`,
	"reduce": `reduce: usage: repocutter reduce INPUT-FILE

Strip revisions out of a dump so the only parts left those likely to
be relevant to a conversion problem. A revision is interesting if it
either (a) contains any operation that is not a plain file
modification - any directory operation, or any add, or any delete, or
any copy, or any operation on properties - or (b) it is referenced by
a later copy operation. Any commit that is neither interesting nor
has interesting neighbors is dropped.

Because the 'interesting' status of a commit is not known for sure
until all future commits have been checked for copy operations, this
command requires an input file.  It cannot operate on standard input.
The reduced dump is emitted to standard output.
`,
	"replace": `replace: usage: repocutter replace /REGEXP/REPLACE/

Perform a regular expression search/replace on blog content. The first
character of the argument (normally /) is treated as the end delimiter 
for the regular-expression and replacement parts.

`,
	"see": `see: usage: repocutter [-r SELECTION] see

Render a very condensed report on the repository node structure, mainly
useful for examining strange and pathological repositories.  File content
is ignored.  You get one line per repository operation, reporting the
revision, operation type, file path, and the copy source (if any).
Directory paths are distinguished by a trailing slash.  The 'copy'
operation is really an 'add' with a directory source and target;
the display name is changed to make them easier to see.
`,
	"select": `select: usage: repocutter [-q] [-r SELECTION] select

The 'select' subcommand selects a range and permits only revisions in
that range to pass to standard output.  A range beginning with 0
includes the dumpfile header.
`,
	"setlog": `setlog: usage: repocutter [-r SELECTION] -logentries=LOGFILE setlog

Replace the log entries in the input dumpfile with the corresponding entries
in the LOGFILE, which should be in the format of an svn log output.
Replacements may be restricted to a specified range.
`,
	"sift": `sift: usage: repocutter [-r SELECTION ] sift PATTERN...

Delete all operations with Node-path headers *not* matching specified
Golang regular expressions (opposite of 'expunge').  Any revision left
with no Node records after this filtering has its Revision record
removed as well.
`,
	"strip": `strip: usage: repocutter [-r SELECTION] strip PATTERN...

Replace content with unique generated cookies on all node paths
matching the specified regular expressions; if no expressions are
given, match all paths.  Useful when you need to examine a
particularly complex node structure.
`,
	"swap": `swap: usage: repocutter [-r SELECTION] swap

Swap the top two elements of each pathname in every revision in the
selection set. Useful following a sift operation for straightening out
a common form of multi-project repository.
`,
	"testify": `testify: usage: repocutter [-r SELECTION] testify

Replace commit timestamps with a monotonically increasing clock tick
starting at the Unix epoch and advancing by 10 seconds per commit.
Replace all attributions with 'fred'.  Discard the repository UUID.
Use this to neutralize procedurally-generated streams so they can be
compared.
`,
	"version": `report major and minor repocutter version
`,
}

var base int

// Baton - ship progress indications to stderr
type Baton struct {
	stream *os.File
	count  int
	endmsg string
	time   time.Time
}

// NewBaton - create a new Baton object with specified start and end messages
func NewBaton(prompt string, endmsg string) *Baton {
	baton := Baton{
		stream: os.Stderr,
		endmsg: endmsg,
		time:   time.Now(),
	}
	baton.stream.WriteString(prompt + "...")
	if terminal.IsTerminal(int(baton.stream.Fd())) {
		baton.stream.WriteString(" \b")
	}
	//baton.stream.Flush()
	return &baton
}

// Twirl - twirl the baton indicating progress
func (baton *Baton) Twirl(ch string) {
	if baton.stream == nil {
		return
	}
	if terminal.IsTerminal(int(baton.stream.Fd())) {
		if ch != "" {
			baton.stream.WriteString(ch)
		} else {
			baton.stream.Write([]byte{"-/|\\"[baton.count%4]})
			baton.stream.WriteString("\b")
		}
	}
	baton.count++
}

// End - operation is done
func (baton *Baton) End(msg string) {
	if msg == "" {
		msg = baton.endmsg
	}
	fmt.Fprintf(baton.stream, "...(%s) %s.\n", time.Since(baton.time), msg)
}

func croak(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "repocutter: croaking, "+msg+"\n", args...)
	os.Exit(1)
}

// LineBufferedSource - Generic class for line-buffered input with pushback.
type LineBufferedSource struct {
	Linebuffer []byte
	source     io.Reader
	reader     *bufio.Reader
	stream     *os.File
	linenumber int
}

// NewLineBufferedSource - create a new source
func NewLineBufferedSource(source io.Reader) LineBufferedSource {
	if debug {
		fmt.Fprintf(os.Stderr, "<setting up NewLineBufferedSource>\n")
	}
	lbs := LineBufferedSource{
		source: source,
		reader: bufio.NewReader(source),
	}
	fd, ok := lbs.source.(*os.File)
	if ok {
		lbs.stream = fd
	}
	return lbs
}

// Rewind - reset source to its beginning, only works when seekable
func (lbs *LineBufferedSource) Rewind() {
	lbs.reader.Reset(lbs.source)
	if lbs.stream != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "<Rewind>\n")
		}
		lbs.stream.Seek(0, 0)
	}
}

// Readline - line-buffered readline.  Return "" on EOF.
func (lbs *LineBufferedSource) Readline() (line []byte) {
	if len(lbs.Linebuffer) != 0 {
		line = lbs.Linebuffer
		if debug {
			fmt.Fprintf(os.Stderr, "<Readline: popping %q>\n", line)
		}
		lbs.Linebuffer = []byte{}
		return
	}
	line, err := lbs.reader.ReadBytes('\n')
	lbs.linenumber++
	if debug {
		fmt.Fprintf(os.Stderr, "<Readline %d: read %q>\n", lbs.linenumber, line)
	}
	if err == io.EOF {
		return []byte{}
	}
	if err != nil {
		croak("I/O error in Readline of LineBufferedSource")
	}
	return
}

// Require - read a line, requiring it to have a specified prefix.
func (lbs *LineBufferedSource) Require(prefix string) []byte {
	line := lbs.Readline()
	if !strings.HasPrefix(string(line), prefix) {
		fmt.Printf("repocutter: required prefix '%s' not seen after line %d\n", prefix, lbs.linenumber)
		os.Exit(1)
	}
	//if debug {
	//	fmt.Fprintf(os.Stderr, "<Require %s -> %q>\n", strconv.Quote(prefix), viline)
	//}
	return line
}

// Straight read from underlying file, no buffering.
func (lbs *LineBufferedSource) Read(rlen int) []byte {
	if len(lbs.Linebuffer) != 0 {
		croak(fmt.Sprintf("repocutter: line buffer unexpectedly nonempty after line %d", lbs.linenumber))
	}
	text := make([]byte, 0, rlen)
	chunk := make([]byte, rlen)
	for {
		n, err := lbs.reader.Read(chunk)
		if err != nil && err != io.EOF {
			croak("I/O error in Read of LineBufferedSource")
		}
		text = append(text, chunk[0:n]...)
		if n == rlen {
			break
		}
		rlen -= n
		chunk = chunk[:rlen]
	}
	lbs.linenumber += strings.Count(string(text), "\n")
	return text
}

// Peek at the next line in the source.
func (lbs *LineBufferedSource) Peek() []byte {
	//assert(lbs.Linebuffer is None)
	nxtline, err := lbs.reader.ReadBytes('\n')
	lbs.linenumber++
	if err != nil && err != io.EOF {
		croak(fmt.Sprintf("repocutter: I/O error in Peek of LineBufferedSource: %s", err))
	}
	if debug {
		fmt.Fprintf(os.Stderr, "<Peek %d: buffer=%q + next=%q>\n",
			lbs.linenumber, lbs.Linebuffer, nxtline)
	}
	lbs.Linebuffer = nxtline
	return lbs.Linebuffer
}

// Flush - get the contents of the line buffer, clearing it.
func (lbs *LineBufferedSource) Flush() []byte {
	//assert(lbs.Linebuffer is not None)
	line := lbs.Linebuffer
	lbs.Linebuffer = []byte{}
	return line
}

// Push a line back to the line buffer.
func (lbs *LineBufferedSource) Push(line []byte) {
	//assert(lbs.linebuffer is None)
	if debug {
		fmt.Fprintf(os.Stderr, "<Push: pushing %q>\n", line)
	}
	lbs.Linebuffer = line
}

// HasLineBuffered - do we have one ready to go?
func (lbs *LineBufferedSource) HasLineBuffered() bool {
	return len(lbs.Linebuffer) != 0
}

// Properties -- represent revision or node properties
type Properties struct {
	properties  map[string]string
	propkeys    []string
	propdelkeys []string
}

// NewProperties - create a new Properties object for a revision or node
func NewProperties(source *DumpfileSource) Properties {
	var props Properties
	newprops := make(map[string]string)
	props.properties = newprops
	for {
		currentline := source.Lbs.Peek()
		if bytes.HasPrefix(currentline, []byte("PROPS-END")) {
			break
		}

		if bytes.HasPrefix(currentline, []byte("D ")) {
			source.Lbs.Require("D")
			keyhd := string(source.Lbs.Readline())
			key := strings.TrimRight(keyhd, linesep)
			props.propdelkeys = append(props.propdelkeys, key)
			continue
		}
		source.Lbs.Require("K")
		keyhd := string(source.Lbs.Readline())
		key := strings.TrimRight(keyhd, linesep)
		valhd := source.Lbs.Require("V")
		vlen, _ := strconv.Atoi(string(bytes.Fields(valhd)[1]))
		value := string(source.Lbs.Read(vlen))
		source.Lbs.Require(linesep)
		props.properties[key] = value
		props.propkeys = append(props.propkeys, key)
	}
	source.Lbs.Flush()
	return props
}

// Stringer - return a representation of properties that can round-trip
func (props *Properties) Stringer() string {
	var b strings.Builder
	for _, key := range props.propkeys {
		fmt.Fprintf(&b, "K %d%s", len(key), linesep)
		fmt.Fprintf(&b, "%s%s", key, linesep)
		fmt.Fprintf(&b, "V %d%s", len(props.properties[key]), linesep)
		fmt.Fprintf(&b, "%s%s", props.properties[key], linesep)
	}
	for _, key := range props.propdelkeys {
		fmt.Fprintf(&b, "D %d%s", len(key), linesep)
		fmt.Fprintf(&b, "%s%s", key, linesep)
	}
	b.WriteString("PROPS-END\n")
	return b.String()
}

// Contains - does a Properties object contain a specified key?
func (props *Properties) Contains(key string) bool {
	_, ok := props.properties[key]
	return ok
}

// Dumpfile parsing machinery goes here

var revisionLine *regexp.Regexp
var textContentLength *regexp.Regexp
var nodeCopyfrom *regexp.Regexp

func init() {
	revisionLine = regexp.MustCompile("Revision-number: ([0-9])")
	textContentLength = regexp.MustCompile("Text-content-length: ([1-9][0-9]*)")
	nodeCopyfrom = regexp.MustCompile("Node-copyfrom-rev: ([1-9][0-9]*)")
}

// DumpfileSource - this class knows about Subversion dumpfile format.
type DumpfileSource struct {
	Lbs              LineBufferedSource
	Baton            *Baton
	Revision         int
	Index            int
	EmittedRevisions map[string]bool
}

// NewDumpfileSource - declare a new dumpfile source object with implied parsing
func NewDumpfileSource(rd io.Reader, baton *Baton) DumpfileSource {
	return DumpfileSource{
		Lbs:              NewLineBufferedSource(rd),
		Baton:            baton,
		Revision:         0,
		EmittedRevisions: make(map[string]bool),
	}
	//runtime.SetFinalizer(&ds, func (s DumpfileSource) {s.Baton.End("")})
}

// SetLength - alter the length field of a specified header
func SetLength(header string, data []byte, val int) []byte {
	re := regexp.MustCompile("(" + header + "-length:) ([0-9]+)")
	return re.ReplaceAll(data, []byte("$1 "+strconv.Itoa(val)))
}

// stripChecksums - remove chweckauums from a blob header
func stripChecksums(header []byte) []byte {
	r1 := regexp.MustCompile("Text-content-md5:.*\n")
	header = r1.ReplaceAll(header, []byte{})
	r2 := regexp.MustCompile("Text-content-sha1:.*\n")
	header = r2.ReplaceAll(header, []byte{})
	r3 := regexp.MustCompile("Text-copy-source-md5:.*\n")
	header = r3.ReplaceAll(header, []byte{})
	r4 := regexp.MustCompile("Text-copy-source-sha1:.*\n")
	header = r4.ReplaceAll(header, []byte{})
	return header
}

// ReadRevisionHeader - read a revision header, parsing its properties.
func (ds *DumpfileSource) ReadRevisionHeader(PropertyHook func(*Properties)) ([]byte, map[string]string) {
	stash := ds.Lbs.Require("Revision-number:")
	rev := string(bytes.Fields(stash)[1])
	rval, err := strconv.Atoi(rev)
	if err != nil {
		fmt.Printf("repocutter: invalid revision number %s at line %d\n", rev, ds.Lbs.linenumber)
		os.Exit(1)
	}
	ds.Revision = rval
	ds.Index = 0
	stash = append(stash, ds.Lbs.Require("Prop-content-length:")...)
	stash = append(stash, ds.Lbs.Require("Content-length:")...)
	stash = append(stash, ds.Lbs.Require(linesep)...)
	props := NewProperties(ds)
	if PropertyHook != nil {
		PropertyHook(&props)
		stash = SetLength("Prop-content", stash, len(props.Stringer()))
		stash = SetLength("Content", stash, len(props.Stringer()))
	}
	stash = append(stash, []byte(props.Stringer())...)
	if debug {
		fmt.Fprintf(os.Stderr, "<after append: %d>\n", ds.Lbs.linenumber)
	}
	for string(ds.Lbs.Peek()) == linesep {
		stash = append(stash, ds.Lbs.Readline()...)
	}
	if ds.Baton != nil {
		ds.Baton.Twirl("")
	}
	if debug {
		fmt.Fprintf(os.Stderr, "<ReadRevisionHeader %d: returns stash=%q>\n",
			ds.Lbs.linenumber, stash)
	}
	return stash, props.properties
}

// ReadNode - read a node header and body.
func (ds *DumpfileSource) ReadNode(PropertyHook func(*Properties)) ([]byte, []byte, []byte) {
	if debug {
		fmt.Fprintf(os.Stderr, "<READ NODE BEGINS>\n")
	}
	header := ds.Lbs.Require("Node-")
	for {
		line := ds.Lbs.Readline()
		if len(line) == 0 {
			fmt.Fprintf(os.Stderr, "repocutter: unexpected EOF in node header\n")
			os.Exit(1)
		}
		m := nodeCopyfrom.FindSubmatch(line)
		if m != nil {
			r := string(m[1])
			if !ds.EmittedRevisions[r] {
				header = append(header, line...)
				header = append(header, ds.Lbs.Require("Node-copyfrom-path")...)
				continue
			}
		}
		header = append(header, line...)
		if string(line) == linesep {
			break
		}
	}
	properties := ""
	if bytes.Contains(header, []byte("Prop-content-length")) {
		props := NewProperties(ds)
		if PropertyHook != nil {
			PropertyHook(&props)
		}
		properties = props.Stringer()
	}
	// Using a read() here allows us to handle binary content
	content := []byte{}
	cl := textContentLength.FindSubmatch(header)
	if len(cl) > 1 {
		n, _ := strconv.Atoi(string(cl[1]))
		content = append(content, ds.Lbs.Read(n)...)
	}
	if debug {
		fmt.Fprintf(os.Stderr, "<READ NODE ENDS>\n")
	}
	if PropertyHook != nil {
		header = SetLength("Prop-content", header, len(properties))
		header = SetLength("Content", header, len(properties)+len(content))
	}
	return header, []byte(properties), content
}

// ReadUntilNextRevision - Must only be called from renumber, as it
// doesn't apply the revmap.  On the other hand, it can't be confused
// by content resembling dumpfile headers.
func (ds *DumpfileSource) ReadUntilNextRevision(contentLength int) []byte {
	stash := []byte{}
	for {
		line := ds.Lbs.Readline()
		if len(line) == 0 {
			return stash
		}
		if string(line) == "\n" {
			if contentLength > 0 {
				stash = append(stash, ds.Lbs.Read(contentLength)...)
				contentLength = 0
			}
		} else if strings.HasPrefix(string(line), "Revision-number:") {
			ds.Lbs.Push(line)
			return stash
		}

		stash = append(stash, line...)
		if strings.HasPrefix(string(line), "Content-length:") {
			contentLength, _ = strconv.Atoi(string(bytes.Fields(line)[1]))
		}
	}
}

// ReadUntilNext - accumulate lines until the next matches a specified prefix.
func (ds *DumpfileSource) ReadUntilNext(prefix string, revmap map[int]int) []byte {
	if debug {
		fmt.Fprintf(os.Stderr, "<ReadUntilNext: until %s>\n", prefix)
	}
	stash := []byte{}
	for {
		line := ds.Lbs.Readline()
		if debug {
			fmt.Fprintf(os.Stderr, "<ReadUntilNext: sees %q>\n", line)
		}
		if len(line) == 0 {
			return stash
		}
		if strings.HasPrefix(string(line), prefix) {
			ds.Lbs.Push(line)
			if debug {
				fmt.Fprintf(os.Stderr, "<ReadUntilNext pushes: %q>\n", line)
			}
			return stash
		}
		// Hack the revision levels in copy-from headers.
		// We're actually modifying the dumpfile contents
		// (rather than selectively omitting parts of it).
		// Note: this will break on a dumpfile that has dumpfiles
		// in its nodes!
		if revmap != nil && strings.HasPrefix(string(line), "Node-copyfrom-rev:") {
			old := bytes.Fields(line)[1]
			oldi, err := strconv.Atoi(string(old))
			if err != nil {
				newrev := []byte(strconv.Itoa(revmap[oldi]))
				line = bytes.Replace(line, old, newrev, 1)
			}
		}
		stash = append(stash, line...)
		if debug {
			fmt.Fprintf(os.Stderr, "<ReadUntilNext: appends %q>\n", line)
		}

	}
}

func (ds *DumpfileSource) say(text []byte) {
	matches := revisionLine.FindSubmatch(text)
	if len(matches) > 1 {
		ds.EmittedRevisions[string(matches[1])] = true
	}
	os.Stdout.Write(text)
}

// SubversionRange - represent a polyrange of Subversion commit numbers
type SubversionRange struct {
	intervals [][2]int
}

// NewSubversionRange - create a new polyrange object
func NewSubversionRange(txt string) SubversionRange {
	var s SubversionRange
	s.intervals = make([][2]int, 0)
	var upperbound int
	for _, item := range strings.Split(txt, ",") {
		var parts [2]int
		if strings.Contains(item, "-") {
			croak("use ':' for version ranges instead of '-'")
		}

		if strings.Contains(item, ":") {
			fields := strings.Split(item, ":")
			if fields[0] == "HEAD" {
				croak("can't accept HEAD as lower bound of a range.")
			}
			parts[0], _ = strconv.Atoi(fields[0])
			if fields[1] == "HEAD" {
				// Be on safe side - could be a 32-bit machine
				parts[1] = math.MaxInt32
			} else {
				parts[1], _ = strconv.Atoi(fields[1])
			}
		} else {
			parts[0], _ = strconv.Atoi(item)
			parts[1], _ = strconv.Atoi(item)
		}
		if parts[0] >= upperbound {
			upperbound = parts[0]
		} else {
			croak("ill-formed range specification")
		}
		s.intervals = append(s.intervals, parts)
	}
	return s
}

// Contains - does this range contain a specified revision?
func (s *SubversionRange) Contains(rev int) bool {
	for _, interval := range s.intervals {
		if rev >= interval[0] && rev <= interval[1] {
			return true
		}
	}
	return false
}

// Upperbound - what is the uppermost revision in the spec?
func (s *SubversionRange) Upperbound() int {
	return s.intervals[len(s.intervals)-1][1]
}

// Report a filtered portion of content.
func (ds *DumpfileSource) Report(selection SubversionRange,
	nodehook func(header []byte, properties []byte, content []byte) []byte,
	prophook func(properties *Properties),
	passthrough bool, passempty bool) {
	emit := passthrough && selection.intervals[0][0] == 0
	stash := ds.ReadUntilNextRevision(0)
	if emit {
		if debug {
			fmt.Fprintf(os.Stderr, "<early stash dump: %q>\n", stash)
		}
		os.Stdout.Write(stash)
	}
	if !ds.Lbs.HasLineBuffered() {
		return
	}
	var nodecount int
	var line []byte
	for {
		ds.Index = 0
		nodecount = 0
		stash, _ := ds.ReadRevisionHeader(prophook)
		if !selection.Contains(ds.Revision) {
			if ds.Revision > selection.Upperbound() {
				return
			}
			ds.ReadUntilNextRevision(0)
			ds.Index = 0
			continue
		}
		for {
			line = ds.Lbs.Readline()
			if len(line) == 0 {
				return
			}
			if string(line) == "\n" {
				if passthrough && emit {
					if debug {
						fmt.Fprintf(os.Stderr, "<passthrough dump: %q>\n", line)
					}
					os.Stdout.Write(line)
				}
				continue
			}
			if strings.HasPrefix(string(line), "Revision-number:") {
				ds.Lbs.Push(line)
				if len(stash) != 0 && nodecount == 0 && passempty {
					if passthrough {
						if debug {
							fmt.Fprintf(os.Stderr, "<revision stash dump: %q>\n", stash)
						}
						ds.say(stash)
					}
				}
				break
			}
			if strings.HasPrefix(string(line), "Node-") {
				nodecount++
				if strings.HasPrefix(string(line), "Node-path: ") {
					ds.Index++
				}
				ds.Lbs.Push(line)
				header, properties, content := ds.ReadNode(prophook)
				if debug {
					fmt.Fprintf(os.Stderr, "<header: %q>\n", header)
					fmt.Fprintf(os.Stderr, "<properties: %q>\n", properties)
					fmt.Fprintf(os.Stderr, "<content: %q>\n", content)
				}
				var nodetxt []byte
				if nodehook != nil {
					nodetxt = nodehook(header, properties, content)
				}
				if debug {
					fmt.Fprintf(os.Stderr, "<nodetxt: %q>\n", nodetxt)
				}
				emit = len(nodetxt) > 0
				if emit && len(stash) > 0 {
					if debug {
						fmt.Fprintf(os.Stderr, "<appending to: %q>\n", stash)
					}
					nodetxt = append(stash, nodetxt...)
					stash = []byte{}
				}
				if passthrough && len(nodetxt) > 0 {
					if debug {
						fmt.Fprintf(os.Stderr, "<node dump: %q>\n", nodetxt)
					}
					ds.say(nodetxt)
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "repocutter: parse at %d doesn't look right (%q), aborting!\n", ds.Revision, line)
			os.Exit(1)
		}
	}
}

// Logentry - parsed form of a Subversion log entry for a revision
type Logentry struct {
	author []byte
	date   []byte
	text   []byte
}

// Logfile represents the state of a logfile
type Logfile struct {
	comments map[int]Logentry
	source   LineBufferedSource
}

// Contains - Does the logfile contain an entry for a specified revision
func (lf *Logfile) Contains(revision int) bool {
	_, ok := lf.comments[revision]
	return ok
}

const delim = "------------------------------------------------------------------------"

// NewLogfile - initialize a new logfile object from an input source
func NewLogfile(readable io.Reader, restrict *SubversionRange) *Logfile {
	lf := Logfile{
		comments: make(map[int]Logentry),
		source:   NewLineBufferedSource(readable),
	}
	type LogState int
	const (
		awaitingHeader LogState = iota
		inLogEntry
	)
	state := awaitingHeader
	author := []byte{}
	date := []byte{}
	logentry := []byte{}
	lineno := 0
	rev := -1
	re := regexp.MustCompile("^r[0-9]+")
	var line []byte
	for {
		lineno++
		line = lf.source.Readline()
		if state == inLogEntry {
			if len(line) == 0 || bytes.HasPrefix(line, []byte(delim)) {
				if rev > -1 {
					logentry = bytes.TrimSpace(logentry)
					if restrict == nil || restrict.Contains(rev) {
						lf.comments[rev] = Logentry{author, date, logentry}
					}
					rev = -1
					logentry = []byte{}
				}
				if len(line) == 0 {
					break
				}
				state = awaitingHeader
			} else {
				logentry = append(logentry, line...)
			}
		}
		if state == awaitingHeader {
			if len(line) == 0 {
				break
			}
			if bytes.HasPrefix(line, []byte("-----------")) {
				continue
			}
			if !re.Match(line) {
				fmt.Fprintf(os.Stderr, "line %d: repocutter did not see a comment header where one was expected\n", lineno)
				os.Exit(1)
			}
			fields := bytes.Split(line, []byte("|"))
			revstr := bytes.TrimSpace(fields[0])
			author = bytes.TrimSpace(fields[1])
			date = bytes.TrimSpace(fields[2])
			//lc := bytes.TrimSpace(fields[3])
			revstr = revstr[1:] // strip off leading 'r'
			rev, _ = strconv.Atoi(string(revstr))
			state = inLogEntry
		}
	}
	return &lf
}

// Extract content of  a specified header
func payload(hd string, header []byte) []byte {
	offs := bytes.Index(header, []byte(hd+": "))
	if offs == -1 {
		return nil
	}
	offs += len(hd) + 2
	end := bytes.Index(header[offs:], []byte("\n"))
	return header[offs : offs+end]
}

// Subcommand implememtations begin here

func doSelect(source DumpfileSource, selection SubversionRange, invert bool) {
	if debug {
		fmt.Fprintf(os.Stderr, "<entering select>")
	}
	emit := (selection.Contains(0) != invert)
	for {
		stash := source.ReadUntilNext("Revision-number:", nil)
		if debug {
			fmt.Fprintf(os.Stderr, "<stash: %q>\n", stash)
		}
		if emit {
			os.Stdout.Write(stash)
		}
		if !source.Lbs.HasLineBuffered() {
			return
		}
		fields := bytes.Fields(source.Lbs.Linebuffer)
		// Error already checked during source parsing
		revision, _ := strconv.Atoi(string(fields[1]))
		emit = (selection.Contains(revision) != invert)
		if debug {
			fmt.Fprintf(os.Stderr, "<%d:%t>\n", revision, emit)
		}
		if emit {
			os.Stdout.Write(source.Lbs.Flush())
		}
		if !invert && revision > selection.Upperbound() {
			return
		}
		source.Lbs.Flush()
	}
}

// Select a portion of the dump file defined by a revision selection.
func deselect(source DumpfileSource, selection SubversionRange) {
	doSelect(source, selection, true)
}

// Strip out ops defined by a revision selection and a path regexp.
func expunge(source DumpfileSource, selection SubversionRange, patterns []string) {
	expungehook := func(header []byte, properties []byte, content []byte) []byte {
		matched := false
		nodepath := payload("Node-path", header)
		if nodepath != nil {
			for _, pattern := range patterns {
				r := regexp.MustCompile(pattern)
				if r.Match(nodepath) {
					matched = true
					break
				}
			}
		}
		if !matched {
			all := make([]byte, 0)
			all = append(all, header...)
			all = append(all, properties...)
			all = append(all, content...)
			return all
		}
		return []byte{}
	}
	source.Report(selection, expungehook, nil, true, true)
}

func dumpall(header []byte, properties []byte, content []byte) []byte {
	all := make([]byte, 0)
	all = append(all, header...)
	all = append(all, properties...)
	all = append(all, content...)
	return all
}

// Hack pathnames to obscure them.
func obscure(seq NameSequence, source DumpfileSource, selection SubversionRange) {
	pathMutator := func(s []byte) []byte {
		parts := strings.Split(filepath.ToSlash(string(s)), "/")
		for i := range parts {
			if parts[i] != "trunk" && parts[i] != "tags" && parts[i] != "branches" && parts[i] != "" {
				parts[i] = seq.obscureString(parts[i])
			}
		}
		return []byte(filepath.FromSlash(strings.Join(parts, "/")))
	}

	nameMutator := func(s string) string {
		return strings.ToLower(seq.obscureString(s))
	}

	min := func(a, b int) int {
		if a < b {
			return a
		}
		return b
	}

	contentMutator := func(s []byte) []byte {
		// Won't bijectively map link target names longer or
		// shorter than the generated fancyname.  The problem
		// here is that we can't change the length of this
		// content - no way to patch the length headers. Note:
		// ideally we'd also remove the content hashes, they
		// become invalid after this transformation.
		if bytes.HasPrefix(s, []byte("link ")) {
			t := pathMutator(s[5:])
			c := min(len(s)-5, len(t))
			for i := 0; i < c; i++ {
				s[5+i] = t[i]
			}
		}
		return s
	}

	mutatePaths(source, selection, pathMutator, nameMutator, contentMutator)
}

// Pop the top segment off each pathname in an input dump
func pop(source DumpfileSource, selection SubversionRange) {
	popSegment := func(ins string) string {
		if strings.Contains(ins, "/") {
			return ins[strings.Index(ins, "/")+1:]
		}
		return ""
	}
	revhook := func(props *Properties) {
		if _, present := props.properties["svn:mergeinfo"]; present {
			props.properties["svn:mergeinfo"] = popSegment(props.properties["svn:mergeinfo"])
		}
	}
	nodehook := func(header []byte, properties []byte, content []byte) []byte {
		for _, htype := range []string{"Node-path: ", "Node-copyfrom-path: "} {
			offs := bytes.Index(header, []byte(htype))
			if offs > -1 {
				offs += len(htype)
				endoffs := offs + bytes.Index(header[offs:], []byte("\n"))
				before := header[:offs]
				pathline := header[offs:endoffs]
				after := make([]byte, len(header)-endoffs)
				copy(after, header[endoffs:])
				pathline = []byte(popSegment(string(pathline)))
				header = before
				header = append(header, pathline...)
				header = append(header, after...)
			}
		}
		all := make([]byte, 0)
		all = append(all, header...)
		all = append(all, properties...)
		all = append(all, content...)
		return all
	}
	source.Report(selection, nodehook, revhook, true, false)
}

// propdel - Delete properties
func propdel(source DumpfileSource, propnames []string, selection SubversionRange) {
	revhook := func(props *Properties) {
		for _, propname := range propnames {
			delete(props.properties, propname)
			for delindex, item := range props.propkeys {
				if item == propname {
					props.propkeys = append(props.propkeys[:delindex], props.propkeys[delindex+1:]...)
					break
				}
			}
			for delindex, item := range props.propdelkeys {
				if item == propname {
					props.propdelkeys = append(props.propdelkeys[:delindex], props.propdelkeys[delindex+1:]...)
					break
				}
			}
		}
	}
	source.Report(selection, dumpall, revhook, true, true)
}

// Set properties.
func propset(source DumpfileSource, propnames []string, selection SubversionRange) {
	revhook := func(props *Properties) {
		for _, propname := range propnames {
			fields := strings.Split(propname, "=")
			if _, present := props.properties[fields[0]]; !present {
				props.propkeys = append(props.propkeys, fields[0])
			}
			props.properties[fields[0]] = fields[1]
		}
	}
	source.Report(selection, dumpall, revhook, true, true)
}

// Rename properties.
func proprename(source DumpfileSource, propnames []string, selection SubversionRange) {
	revhook := func(props *Properties) {
		for _, propname := range propnames {
			fields := strings.Split(propname, "->")
			if _, present := props.properties[fields[0]]; present {
				props.properties[fields[1]] = props.properties[fields[0]]
				props.properties[fields[0]] = ""
				for i, item := range props.propkeys {
					if item == fields[0] {
						props.propkeys[i] = fields[1]
					}
				}
				for i, item := range props.propdelkeys {
					if item == fields[0] {
						props.propdelkeys[i] = fields[1]
					}
				}
			}
		}
	}
	source.Report(selection, dumpall, revhook, true, true)
}

func getAuthor(props map[string]string) string {
	if author, ok := props["svn:author"]; ok {
		return author
	}
	return "(no author)"
}

// getHeader - extract a given field from a header string
func getHeader(txt []byte, hdr string) []byte {
	for _, line := range bytes.Split(txt, []byte("\n")) {
		if bytes.HasPrefix(line, []byte(hdr+": ")) {
			return line[len(hdr)+2:]
		}
	}
	return nil
}

// SVNTimeParse - parse a date in the Subversion variant of RFC3339 format
func SVNTimeParse(rdate string) time.Time {
	// An example date in SVN format is '2011-11-30T16:40:02.180831Z'
	date, ok := time.Parse(time.RFC3339Nano, rdate)
	if ok != nil {
		fmt.Fprintf(os.Stderr, "ill-formed date '%s': %v\n", rdate, ok)
		os.Exit(1)
	}
	return date
}

// Extract log entries
func log(source DumpfileSource, selection SubversionRange) {
	prophook := func(prop *Properties) {
		props := prop.properties
		logentry := props["svn:log"]
		// This test implicitly excludes r0 metadata from being dumped.
		// It is not certain this is the right thing.
		if logentry == "" {
			return
		}
		os.Stdout.Write([]byte(delim + "\n"))
		author := getAuthor(props)
		date := SVNTimeParse(props["svn:date"])
		drep := date.Format("2006-01-02 15:04:05 +0000 (Mon, 02 Jan 2006)")
		fmt.Printf("r%d | %s | %s | %d lines\n",
			source.Revision,
			author,
			drep,
			strings.Count(logentry, "\n"))
		os.Stdout.WriteString("\n" + logentry + "\n")
	}
	source.Report(selection, nil, prophook, false, true)
}

// Hack paths by applying a specified transformation.
func mutatePaths(source DumpfileSource, selection SubversionRange, pathMutator func([]byte) []byte, nameMutator func(string) string, contentMutator func([]byte) []byte) {
	revhook := func(props *Properties) {
		if _, present := props.properties["svn:mergeinfo"]; present {
			mergeinfo := string(props.properties["svn:mergeinfo"])
			var buffer bytes.Buffer
			if len(mergeinfo) != 0 {
				for _, line := range strings.Split(strings.TrimSuffix(mergeinfo, "\n"), "\n") {
					if strings.Contains(line, ":") {
						lastidx := strings.LastIndex(line, ":")
						path, revrange := line[:lastidx], line[lastidx+1:]
						buffer.Write(pathMutator([]byte(path)))
						buffer.WriteString(":")
						buffer.WriteString(revrange)
					} else {
						buffer.WriteString(line)
					}
					buffer.WriteString("\n")
				}
			}
			props.properties["svn:mergeinfo"] = buffer.String()
		}
		if userid, present := props.properties["svn:author"]; present && nameMutator != nil {
			props.properties["svn:author"] = nameMutator(userid)
		}
	}
	nodehook := func(header []byte, properties []byte, content []byte) []byte {
		for _, htype := range []string{"Node-path: ", "Node-copyfrom-path: "} {
			offs := bytes.Index(header, []byte(htype))
			if offs > -1 {
				offs += len(htype)
				endoffs := offs + bytes.Index(header[offs:], []byte("\n"))
				before := header[:offs]
				pathline := header[offs:endoffs]
				after := make([]byte, len(header)-endoffs)
				copy(after, header[endoffs:])
				pathline = pathMutator(pathline)
				header = before
				header = append(header, pathline...)
				header = append(header, after...)
			}
		}
		if contentMutator != nil {
			content = contentMutator(content)
		}
		all := make([]byte, 0)
		all = append(all, header...)
		all = append(all, properties...)
		all = append(all, content...)
		return all
	}
	source.Report(selection, nodehook, revhook, true, true)
}

// Hack paths by applying a regexp transformation.
func pathrename(source DumpfileSource, selection SubversionRange, patterns []string) {
	r := regexp.MustCompile(patterns[0])
	mutator := func(s []byte) []byte {
		return r.ReplaceAll(s, []byte(patterns[1]))
	}

	mutatePaths(source, selection, mutator, nil, nil)
}

// Topologically reduce a dump, removing spans of plain file modifications.
func reduce(source DumpfileSource) {
	maxRev := 0
	interesting := make(map[int]bool)
	interesting[0] = true
	reducehook := func(header []byte, properties []byte, _ []byte) []byte {
		if !(string(getHeader(header, "Node-kind")) == "file" && string(getHeader(header, "Node-action")) == "change") || len(properties) > 0 { //len([]nil == 0)
			interesting[source.Revision-1] = true
			interesting[source.Revision] = true
			interesting[source.Revision+1] = true
			//fmt.Fprintf(os.Stderr, "Principal interest: %d %d %d\n", source.Revision-1, source.Revision, source.Revision+1)
		}
		copysource := getHeader(header, "Node-copyfrom-rev")
		if copysource != nil {
			n, err := strconv.Atoi(string(copysource))
			if err == nil {
				interesting[n-1] = true
				interesting[n] = true
				interesting[n+1] = true
				//fmt.Fprintf(os.Stderr, "Copy-derived interest: %d %d %d\n", n-1, n, n+1)
			}
		}
		maxRev = source.Revision
		return nil
	}
	source.Report(NewSubversionRange("0:HEAD"), reducehook, nil, false, true)
	var selection string
	for i := 0; i <= maxRev; i++ {
		if interesting[i] {
			selection += fmt.Sprintf("%d,", i)
		}
	}
	source.Lbs.Rewind()
	// -1 is to trim off trailing comma
	sselect(source, NewSubversionRange(selection[0:len(selection)-1]))
}

func renumberMergeInfo(lines []byte, renumbering map[string]int) []byte {
	modifiedLines := make([]byte, 0)
	for _, line := range bytes.Split(lines, []byte("\n")) {
		line = append(line, '\n')
		out := make([]byte, 0)
		fields := bytes.Split(line, []byte(":"))

		if len(fields) == 1 {
			modifiedLines = append(modifiedLines, fields[0]...)
			continue
		}
		fields[0] = append(fields[0], []byte(":")...)
		digits := make([]byte, 0)
		for _, c := range fields[1] {
			if bytes.ContainsAny([]byte{c}, "0123456789") {
				digits = append(digits, c)
			} else {
				if len(digits) > 0 {
					d := fmt.Sprintf("%d", renumbering[string(digits)])
					out = append(out, []byte(d)...)
					digits = make([]byte, 0)
				}
				out = append(out, c)
			}
		}

		modifiedLines = append(modifiedLines, append(fields[0], out...)...)
	}

	return modifiedLines
}

// Renumber all revisions.
func renumber(source DumpfileSource) {
	renumbering := make(map[string]int)
	counter := base
	var p []byte
	type HeaderState int
	const (
		AwaitingHeader HeaderState = iota
		InHeader
		InProps
		InText
	)

	type TextLengthState int
	const (
		awaitingTextLength TextLengthState = iota
	)

	type RenumberState int
	const (
		awaitingRevisionNumber RenumberState = iota
		awaitingContentLength
		awaitingMergeInfoKey
	)

	type PropParserState int
	const (
		awaitingNext PropParserState = iota
		awaitingPropDelete
		awaitingKeyValue
		awaitingValueLength
		awaitingMergeInfoValueLength
		readingValue
		readingMergeInfo
		propsEnd
	)

	var propParserState = awaitingNext

	var headerState = AwaitingHeader
	var textContentLength int
	var propContentLength int

	for {
		line := source.Lbs.Readline()
		if len(line) == 0 {
			break
		}

		if string(line) == "\n" {
			if headerState == InHeader {
				if propContentLength > 0 {
					headerState = InProps
				} else if textContentLength > 0 {
					headerState = InText
				} else {
					//os.Stdout.WriteString("Awaiting header, no props or content\n")
					headerState = AwaitingHeader
				}
			} else if headerState == InProps && propParserState == propsEnd {
				if textContentLength > 0 {
					headerState = InText
				} else {
					//os.Stdout.WriteString("Awaiting header, props done, no content\n")
					headerState = AwaitingHeader
				}
			} else if headerState == InProps {
				croak("empty lines inside Props-Section should be processed directly in properties parser!")
			}
			os.Stdout.Write(line)

			if headerState == InText {
				os.Stdout.Write(source.Lbs.Read(textContentLength))
				os.Stdout.Write(source.Lbs.Readline())
				//os.Stdout.WriteString("Awaiting header after content\n")
				headerState = AwaitingHeader
			}
			continue
		}

		if p = payload("Revision-number", line); p != nil {
			if headerState != AwaitingHeader {
				croak("headerState should be in InHeader, was: " + fmt.Sprint(headerState))
			}
			headerState = InHeader
			propContentLength = 0
			textContentLength = 0
			propParserState = awaitingNext

			fmt.Printf("Revision-number: %d\n", counter)
			renumbering[string(p)] = counter
			counter++
		} else if p = payload("Node-path", line); p != nil {
			if headerState != AwaitingHeader {
				croak("headerState should be in InHeader, was: " + fmt.Sprint(headerState))
			}
			headerState = InHeader
			propContentLength = 0
			textContentLength = 0
			propParserState = awaitingNext

			os.Stdout.Write(line)
		} else if p = payload("Text-content-length", line); p != nil {
			textContentLength, _ = strconv.Atoi(string(p))
			os.Stdout.Write(line)
		} else if p = payload("SVN-fs-dump-format-version", line); p != nil {
			os.Stdout.Write(line)
		} else if p = payload("UUID", line); p != nil {
			os.Stdout.Write(line)
		} else if p = payload("Prop-content-length", line); p != nil {
			propContentLength, _ = strconv.Atoi(string(p))
			os.Stdout.Write(line)
			continue
		} else if p = payload("Node-copyfrom-rev", line); p != nil {
			fmt.Printf("Node-copyfrom-rev: %d\n", renumbering[string(p)])
		} else {
			if headerState == AwaitingHeader {
				os.Stdout.Write(line)
				continue
			}

			// A typical mergeinfo entry looks like this:
			// K 13
			// svn:mergeinfo
			// V 18
			// /branches/v1.0:4-6
			//                        <- Optional empty line
			// PROPS-END
			needsWrite := true

			if headerState == InProps {
				if propParserState == awaitingNext {
					if bytes.HasPrefix(line, []byte("K ")) {
						propParserState = awaitingKeyValue
					} else if bytes.HasPrefix(line, []byte("D ")) {
						propParserState = awaitingPropDelete
					} else if bytes.HasPrefix(line, []byte("PROPS-END")) {
						needsWrite = false
						propParserState = propsEnd
						os.Stdout.Write(line)
						if textContentLength > 0 {
							os.Stdout.Write(source.Lbs.Read(textContentLength))
							os.Stdout.Write(source.Lbs.Readline())
							//os.Stdout.WriteString("Awaiting header after content after PROPS-END\n")
							headerState = AwaitingHeader
						}
					} else {
						croak("unkown property entry begin: " + string(line))
					}
				} else if propParserState == awaitingPropDelete {
					propParserState = awaitingNext
				} else if propParserState == awaitingKeyValue {
					needsWrite = false
					if bytes.HasPrefix(line, []byte("svn:mergeinfo")) {
						os.Stdout.Write(line)
						lengthline := source.Lbs.Readline()
						os.Stdout.Write(lengthline)
						mergeinfolength, _ := strconv.Atoi(string(bytes.Fields(lengthline)[1]))
						os.Stdout.Write(renumberMergeInfo(source.Lbs.Read(mergeinfolength), renumbering))
						// ignore trailing newline, already artifically appended in renumberMergeInfo
						source.Lbs.Readline()
						propParserState = awaitingNext
					} else {
						os.Stdout.Write(line)
						lengthline := source.Lbs.Readline()
						os.Stdout.Write(lengthline)
						mergeinfolength, _ := strconv.Atoi(string(bytes.Fields(lengthline)[1]))
						os.Stdout.Write(source.Lbs.Read(mergeinfolength))
						os.Stdout.Write(source.Lbs.Readline()) // trailing newline
						propParserState = awaitingNext
					}
				}
			}

			if needsWrite {
				os.Stdout.Write(line)
			}
		}
	}
}

func replace(source DumpfileSource, selection SubversionRange, transform string) {
	patternParts := strings.Split(transform[1:], transform[0:1])
	if len(patternParts) != 3 || patternParts[2] != "" {
		croak("ill-formed transform specification")
	}
	tre, err := regexp.Compile(patternParts[0])
	if err != nil {
		croak("illegal regular expression: %v", err)
	}

	innerreplace := func(header []byte, properties []byte, content []byte) []byte {
		newcontent := tre.ReplaceAll(content, []byte(patternParts[1]))
		if string(content) != string(newcontent) {
			header = []byte(SetLength("Text-content", header, len(newcontent)))
			header = []byte(SetLength("Content", header, len(properties)+len(newcontent)))
			header = stripChecksums(header)
		}

		all := make([]byte, 0)
		all = append(all, header...)
		all = append(all, properties...)
		all = append(all, newcontent...)
		return all
	}
	source.Report(selection, innerreplace, nil, true, true)
}

// Strip out ops defined by a revision selection and a path regexp.
func see(source DumpfileSource, selection SubversionRange) {
	seenode := func(header []byte, _, _ []byte) []byte {
		if debug {
			fmt.Fprintf(os.Stderr, "<header: %q>\n", header)
		}
		path := payload("Node-path", header)
		kind := payload("Node-kind", header)
		if string(kind) == "dir" {
			path = append(path, os.PathSeparator)
		}
		frompath := payload("Node-copyfrom-path", header)
		fromrev := payload("Node-copyfrom-rev", header)
		action := payload("Node-action", header)
		if frompath != nil && fromrev != nil {
			if string(kind) == "dir" {
				frompath = append(frompath, os.PathSeparator)
			}
			path = append(path, []byte(fmt.Sprintf(" from %s:%s", fromrev, frompath))...)
			action = []byte("copy")
		}
		leader := fmt.Sprintf("%d-%d", source.Revision, source.Index)
		fmt.Printf("%-5s %-8s %s\n", leader, action, path)
		return nil
	}
	source.Report(selection, seenode, nil, false, true)
}

// Mutate log entries.
func setlog(source DumpfileSource, logpath string, selection SubversionRange) {
	fd, ok := os.Open(logpath)
	if ok != nil {
		croak("couldn't open " + logpath)
	}
	logpatch := NewLogfile(fd, &selection)
	loghook := func(prop *Properties) {
		_, haslog := prop.properties["svn:log"]
		if haslog && logpatch.Contains(source.Revision) {
			logentry := logpatch.comments[source.Revision]
			if string(logentry.author) != getAuthor(prop.properties) {
				fmt.Fprintf(os.Stderr, "repocutter: author of revision %d doesn't look right, aborting!\n", source.Revision)
				os.Exit(1)
			}
			prop.properties["svn:log"] = string(logentry.text)
		}
	}
	source.Report(selection, dumpall, loghook, true, true)
}

// Strip a portion of the dump file defined by a revision selection.
// Sift for ops defined by a revision selection and a path regexp.
func sift(source DumpfileSource, selection SubversionRange, patterns []string) {
	sifthook := func(header []byte, properties []byte, content []byte) []byte {
		matched := false
		nodepath := payload("Node-path", header)
		if nodepath != nil {
			for _, pattern := range patterns {
				r := regexp.MustCompile(pattern)
				if r.Match(nodepath) {
					matched = true
					break
				}
			}
		}
		if matched {
			all := make([]byte, 0)
			all = append(all, header...)
			all = append(all, properties...)
			all = append(all, content...)
			return all
		}
		return []byte{}
	}
	source.Report(selection, sifthook, nil, true, true)
}

func strip(source DumpfileSource, selection SubversionRange, patterns []string) {
	innerstrip := func(header []byte, properties []byte, content []byte) []byte {
		// first check against the patterns, if any are given
		ok := true
		nodepath := payload("Node-path", header)
		if nodepath != nil {
			for _, pattern := range patterns {
				ok = false
				re := regexp.MustCompile(pattern)
				if re.Match(nodepath) {
					//os.Stderr.Write("strip skipping: %s\n", nodepath)
					ok = true
					break
				}
			}
		}
		if ok {
			if len(content) > 0 { //len([]nil == 0)
				// Avoid replacing symlinks, a reposurgeon sanity check barfs.
				if !bytes.HasPrefix(content, []byte("link ")) {
					tell := fmt.Sprintf("Revision is %d, file path is %s.\n",
						source.Revision, getHeader(header, "Node-path"))
					content = []byte(tell)
					header = SetLength("Text-content", header, len(content))
					header = SetLength("Content", header, len(properties)+len(content))
					header = stripChecksums(header)
				}
			}
		}

		all := make([]byte, 0)
		all = append(all, header...)
		all = append(all, properties...)
		all = append(all, content...)
		return all
	}
	source.Report(selection, innerstrip, nil, true, true)
}

// Select a portion of the dump file not defined by a revision selection.
func sselect(source DumpfileSource, selection SubversionRange) {
	doSelect(source, selection, false)
}

// Hack paths by swapping the top two components.
func swap(source DumpfileSource, selection SubversionRange) {
	mutator := func(path []byte) []byte {
		parts := bytes.Split(path, []byte("/"))
		if len(parts) < 2 {
			// Single-component directory creations should be
			// skipped; each such operation for directory 'foo' is
			// replaced by the creation of the swapped directory
			// trunk/foo.  Works because both reposurgeon and
			// Subversion's stream dump reader don't mind if no
			// explicit trunk/ directory creation is ever done as
			// long as some trunk subdirectory *is* created.
			return nil
		}
		tp := parts[0]
		parts[0] = parts[1]
		parts[1] = tp
		return bytes.Join(parts, []byte("/"))
	}
	revhook := func(props *Properties) {
		var mergepath []byte
		if m, ok := props.properties["svn:mergeinfo"]; ok {
			mergepath = mutator([]byte(m))
		}
		if mergepath != nil {
			props.properties["svn:mergeinfo"] = string(mergepath)
		}
		if source.Revision == 1 && props.Contains("svn:log") {
			props.properties["svn:log"] = "Synthetic branch-structure creation.\n"
		}
		// We leave author and date alone.  This will get
		// dropped in the git version; it's only being
		// generated so reposurgeon doesn't get confused about
		// the branch structure.
	}
	var swaplatch bool // Ugh, but less than global
	nodehook := func(header []byte, properties []byte, content []byte) []byte {
		// This is dodgy.  The assumption here is that the first node
		// of r1 is the directory creation for the first project.
		// Replace it with synthetic nodes that create normal directory
		// structure.
		const swapHeader = `Node-path: branches
Node-kind: dir
Node-action: add
Prop-content-length: 10
Content-length: 10

PROPS-END


Node-path: tags
Node-kind: dir
Node-action: add
Prop-content-length: 10
Content-length: 10

PROPS-END


Node-path: trunk
Node-kind: dir
Node-action: add
Prop-content-length: 10
Content-length: 10

PROPS-END

`
		if source.Revision == 1 && !swaplatch {
			swaplatch = true
			return []byte(swapHeader)
		}
		for _, htype := range []string{"Node-path: ", "Node-copyfrom-path: "} {
			offs := bytes.Index(header, []byte(htype))
			if offs > -1 {
				offs += len(htype)
				endoffs := offs + bytes.Index(header[offs:], []byte("\n"))
				before := header[:offs]
				pathline := header[offs:endoffs]
				after := header[endoffs:]
				pathline = mutator(pathline)
				if pathline == nil {
					return nil
				}
				header = []byte(string(before) + string(pathline) + string(after))
			}
		}
		all := make([]byte, 0)
		all = append(all, header...)
		all = append(all, properties...)
		all = append(all, content...)
		return all
	}
	source.Report(selection, nodehook, revhook, true, true)
}

// Neutralize the input test load
func testify(source DumpfileSource) {
	const NeutralUser = "fred"
	counter := base
	var p []byte
	var state int
	for {
		line := source.Lbs.Readline()
		if len(line) == 0 {
			break
		}
		if p = payload("UUID", line); p != nil && source.Lbs.linenumber <= 10 {
			continue
		} else if p = payload("Revision-number", line); p != nil {
			counter++
		} else if bytes.HasPrefix(line, []byte("svn:author")) {
			state = 1
		} else if state == 1 && bytes.HasPrefix(line, []byte("V ")) {
			line = []byte(fmt.Sprintf("V %d\n", len(NeutralUser)))
			state = 2
		} else if bytes.HasPrefix(line, []byte("svn:date")) {
			state = 4
		} else if bytes.HasPrefix(line, []byte("PROPS-END")) {
			state = 0
		}
		if state == 3 {
			line = []byte(NeutralUser + "\n")
			state = 0
		} else if state == 6 {
			t := time.Unix(int64((counter-1)*10), 0).UTC().Format(time.RFC3339)
			t2 := t[:19] + ".000000Z"
			line = []byte(t2 + "\n")
			state = 0
		}
		os.Stdout.Write(line)
		if state >= 2 {
			state++
		}
	}
}

func main() {
	selection := NewSubversionRange("0:HEAD")
	var quiet bool
	var logentries string
	var rangestr string
	var infile string
	input := os.Stdin
	flag.BoolVar(&debug, "d", false, "enable debug messages")
	flag.BoolVar(&debug, "debug", false, "enable debug messages")
	flag.StringVar(&infile, "i", "", "set input file")
	flag.StringVar(&infile, "infile", "", "set input file")
	flag.StringVar(&logentries, "l", "", "pass in log patch")
	flag.StringVar(&logentries, "logentries", "", "pass in log patch")
	flag.BoolVar(&quiet, "q", false, "disable progress messages")
	flag.BoolVar(&quiet, "quiet", false, "disable progress messages")
	flag.StringVar(&rangestr, "r", "", "set selection range")
	flag.StringVar(&rangestr, "range", "", "set selection range")
	flag.IntVar(&base, "b", 0, "base value to renumber from")
	flag.IntVar(&base, "base", 0, "base value to renumber from")
	flag.Parse()
	if rangestr != "" {
		selection = NewSubversionRange(rangestr)
	}
	if infile != "" {
		var err error
		input, err = os.Open(infile)
		if err != nil {
			fmt.Fprint(os.Stderr, "Input file open failed.\n")
			os.Exit(1)
		}
	}
	if debug {
		fmt.Fprintf(os.Stderr, "<selection: %v>\n", selection)
	}

	if flag.NArg() == 0 {
		fmt.Fprint(os.Stderr, "Type 'repocutter help' for usage.\n")
		os.Exit(1)
	} else if debug {
		fmt.Fprintf(os.Stderr, "<command=%s>\n", flag.Arg(0))
	}
	var baton *Baton
	if flag.Arg(0) != "help" && flag.Arg(0) != "version" {
		if !quiet {
			baton = NewBaton(oneliners[flag.Arg(0)], "done")
		} else {
			baton = nil
		}
	}

	switch flag.Arg(0) {
	case "deselect":
		deselect(NewDumpfileSource(input, baton), selection)
	case "expunge":
		expunge(NewDumpfileSource(input, baton), selection, flag.Args()[1:])
	case "help":
		if len(flag.Args()) == 1 {
			os.Stdout.WriteString(doc)
			break
		}
		if cdoc, ok := helpdict[flag.Arg(1)]; ok {
			os.Stdout.WriteString(cdoc)
			break
		}
		fmt.Fprintf(os.Stderr, "repocutter: no such command\n")
		os.Exit(1)
	case "log":
		log(NewDumpfileSource(input, baton), selection)
	case "obscure":
		obscure(NewNameSequence(), NewDumpfileSource(input, baton), selection)
	case "pathrename":
		pathrename(NewDumpfileSource(input, baton), selection, flag.Args()[1:])
	case "pop":
		pop(NewDumpfileSource(input, baton), selection)
	case "propdel":
		propdel(NewDumpfileSource(input, baton), flag.Args()[1:], selection)
	case "propset":
		propset(NewDumpfileSource(input, baton), flag.Args()[1:], selection)
	case "proprename":
		proprename(NewDumpfileSource(input, baton), flag.Args()[1:], selection)
	case "reduce":
		if len(flag.Args()) < 2 {
			fmt.Fprintf(os.Stderr, "repocutter: reduce requires a file argument.\n")
			os.Exit(1)
		}
		f, err := os.Open(flag.Args()[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "repocutter: can't open stream to reduce.\n")
			os.Exit(1)
		}
		reduce(NewDumpfileSource(f, baton))
	case "renumber":
		renumber(NewDumpfileSource(input, baton))
	case "replace":
		replace(NewDumpfileSource(input, baton), selection, flag.Args()[1])
	case "see":
		see(NewDumpfileSource(input, baton), selection)
	case "select":
		sselect(NewDumpfileSource(input, baton), selection)
	case "setlog":
		if logentries == "" {
			fmt.Fprintf(os.Stderr, "repocutter: setlog requires a log entries file.\n")
			os.Exit(1)
		}
		setlog(NewDumpfileSource(input, baton), logentries, selection)
	case "sift":
		sift(NewDumpfileSource(input, baton), selection, flag.Args()[1:])
	case "strip":
		strip(NewDumpfileSource(input, baton), selection, flag.Args()[1:])
	case "swap":
		swap(NewDumpfileSource(input, baton), selection)
	case "testify":
		testify(NewDumpfileSource(input, baton))
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprintf(os.Stderr, "repocutter: \"%s\": unknown subcommand\n", flag.Arg(0))
		os.Exit(1)
	}
	if baton != nil {
		baton.End("")
	}
}
