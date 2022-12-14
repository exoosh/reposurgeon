// vcs ncapsulates of VCS capabilities

// Copyright by Eric S. Raymond
// SPDX-License-Identifier: BSD-2-Clause

package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
)

// Most knowledge about specific version-control systems lives in the
// following class list. Exception; there's a git-specific hook in the
// repo reader; also see the extractor classes; also see the dump method
// in the Blob() class.
//
// Import/export style flags are as follows:
//     "no-nl-after-commit" = no extra NL after each commit
//     "nl-after-comment" = inserts an extra NL after each comment
//     "export-progress" = exporter generates its own progress messages,
//                         no need for baton prompt.
//     "import-defaults" = Import sets default ignores
//
// Preserve and prenuke parts can be directories.
//
// Note that some of the commands used here are plugins or extensions
// that are not part of the basic VCS. Thus these may fail when called;
// we need to be prepared to cope with that.
//
// ${pwd} is replaced with the name of the present working directory.

// VCS is a class representing a version-control system.
type VCS struct {
	name         string           // Name of the VCS
	subdirectory string           // Name of its metadata subdirectory
	exporter     string           // Import/export style flags.
	quieter      string           // How to make exporter quieter
	styleflags   orderedStringSet // fast-export style flags
	extensions   orderedStringSet // Format extension flags
	initializer  string           // Command to initualize a repo
	pathlister   string           // Command to list registered files
	taglister    string           // Command to list tag names
	branchlister string           // Command to list branch names
	importer     string           // Command to import from stream format
	checkout     string           // Command to check out working copy
	preserve     orderedStringSet // Config and hook stuff to be preserved
	prenuke      orderedStringSet // Things to be removed from staging
	authormap    string           // Where importer might drop an authormap
	ignorename   string           // Where the ignore patterns live
	dfltignores  string           // Default ignore patterns
	cookies      []regexp.Regexp  // How to recogbnize a commit reference
	project      string           // VCS project URL
	notes        string           // Notes and caveats
	// Hidden members
	checkignore string // how to tell if directory is a checkout
}

// Constants needed in VCS class methods
const suffixNumeric = `[0-9]+(\s|[.]\n)`
const tokenNumeric = `\s` + suffixNumeric
const dottedNumeric = `\s[0-9]+(\.[0-9]+)`

// manages tells us if a directory might be managed by this VCS
func (vcs VCS) manages(dirname string) bool {
	if vcs.subdirectory != "" {
		subdir := filepath.Join(dirname, vcs.subdirectory)
		subdir = filepath.FromSlash(subdir)
		if exists(subdir) && isdir(subdir) {
			return true
		}
	}
	// Could be a CVS repository without CVSROOT
	if vcs.name == "cvs" {
		files, err := ioutil.ReadDir(dirname)
		if err == nil {
			for _, p := range files {
				if strings.HasSuffix(p.Name(), ",v") {
					return true
				}
			}
		}
	}
	return false
}

func (vcs VCS) String() string {
	realignores := newOrderedStringSet()
	scanner := bufio.NewScanner(strings.NewReader(vcs.dfltignores))
	for scanner.Scan() {
		item := scanner.Text()
		if len(item) > 0 && !strings.HasPrefix(item, "# ") {
			realignores.Add(item)
		}
	}
	notes := strings.Trim(vcs.notes, "\t ")

	return fmt.Sprintf("         Name: %s\n", vcs.name) +
		fmt.Sprintf(" Subdirectory: %s\n", vcs.subdirectory) +
		fmt.Sprintf("     Exporter: %s\n", vcs.exporter) +
		fmt.Sprintf(" Export-Style: %s\n", vcs.styleflags.String()) +
		fmt.Sprintf("   Extensions: %s\n", vcs.extensions.String()) +
		fmt.Sprintf("  Initializer: %s\n", vcs.initializer) +
		fmt.Sprintf("   Pathlister: %s\n", vcs.pathlister) +
		fmt.Sprintf("    Taglister: %s\n", vcs.taglister) +
		fmt.Sprintf(" Branchlister: %s\n", vcs.branchlister) +
		fmt.Sprintf("     Importer: %s\n", vcs.importer) +
		fmt.Sprintf("     Checkout: %s\n", vcs.checkout) +
		fmt.Sprintf("      Prenuke: %s\n", vcs.prenuke.String()) +
		fmt.Sprintf("     Preserve: %s\n", vcs.preserve.String()) +
		fmt.Sprintf("    Authormap: %s\n", vcs.authormap) +
		fmt.Sprintf("   Ignorename: %s\n", vcs.ignorename) +
		fmt.Sprintf("      Ignores: %s\n", realignores.String()) +
		fmt.Sprintf("      Project: %s\n", vcs.project) +
		fmt.Sprintf("        Notes: %s\n", notes)
}

// Used for pre-compiling regular expressions at module load time
func reMake(patterns ...string) []regexp.Regexp {
	regexps := make([]regexp.Regexp, 0)
	for _, item := range patterns {
		regexps = append(regexps, *regexp.MustCompile(item))
	}
	return regexps
}

func (vcs VCS) hasReference(comment []byte) bool {
	for i := range vcs.cookies {
		if vcs.cookies[i].Find(comment) != nil {
			return true
		}
	}
	return false
}

var vcstypes []VCS
var svntype *VCS

// This one is special because it's used directly in the Subversion
// dump parser, as well as in the VCS capability table.
const subversionDefaultIgnores = `# A simulation of Subversion default ignores, generated by reposurgeon.
*.o
*.lo
*.la
*.al
*.libs
*.so
*.so.[0-9]*
*.a
*.pyc
*.pyo
*.rej
*~
*.#*
.*.swp
.DS_store
# Simulated Subversion default ignores end here
`

func vcsInit() {
	vcstypes = []VCS{
		{
			name:         "git",
			subdirectory: ".git",
			// Requires git 2.19.2 or later for --show-original-ids
			exporter:     "git fast-export --show-original-ids --signed-tags=verbatim --tag-of-filtered-object=drop --use-done-feature --all",
			quieter:      "",
			styleflags:   newOrderedStringSet(),
			extensions:   newOrderedStringSet(),
			initializer:  "git init --quiet",
			importer:     "git fast-import --quiet --export-marks=.git/marks",
			checkout:     "git checkout",
			pathlister:   "git ls-files",
			taglister:    "git tag -l",
			branchlister: "git branch -q --list 2>&1 | cut -c 3- | egrep -v 'detached|^master$' || exit 0",
			prenuke:      newOrderedStringSet(".git/config", ".git/hooks"),
			preserve:     newOrderedStringSet(".git/config", ".git/hooks"),
			authormap:    ".git/cvs-authors",
			ignorename:   ".gitignore",
			dfltignores:  "",
			cookies:      reMake(`\b[0-9a-f]{6}\b`, `\b[0-9a-f]{40}\b`),
			project:      "http://git-scm.com/",
			notes:        "The authormap is not required, but will be used if present.",
		},
		{
			name:         "bzr",
			subdirectory: ".bzr",
			exporter:     "bzr fast-export --no-plain .",
			quieter:      "",
			styleflags: newOrderedStringSet(
				"export-progress",
				"no-nl-after-commit",
				"nl-after-comment"),
			extensions: newOrderedStringSet(
				"empty-directories",
				"multiple-authors", "commit-properties"),
			initializer:  "",
			pathlister:   "",
			taglister:    "bzr tags",
			branchlister: "bzr branches | cut -c 3-",
			importer:     "bzr fast-import -",
			checkout:     "bzr checkout",
			prenuke:      newOrderedStringSet(".bzr/plugins"),
			preserve:     newOrderedStringSet(),
			authormap:    "",
			project:      "http://bazaar.canonical.com/en/",
			ignorename:   ".bzrignore",
			dfltignores: `
# A simulation of bzr default ignores, generated by reposurgeon.
*.a
*.o
*.py[co]
*.so
*.sw[nop]
*~
.#*
[#]*#
__pycache__
bzr-orphans
# Simulated bzr default ignores end here
`,
			cookies: reMake(tokenNumeric),
			notes:   "Requires the bzr-fast-import plugin.",
		},
		{
			name:         "hg",
			subdirectory: ".hg",
			exporter:     "",
			styleflags: newOrderedStringSet(
				"import-defaults",
				"nl-after-comment",
				"export-progress"),
			extensions:   newOrderedStringSet(),
			initializer:  "hg init",
			pathlister:   "hg status -macn",
			taglister:    "hg tags --quiet",
			branchlister: "hg branches --closed --template '{branch}\n' | grep -v '^default$'",
			importer:     "hg-git-fast-import",
			checkout:     "hg checkout",
			prenuke:      newOrderedStringSet(".hg/hgrc"),
			preserve:     newOrderedStringSet(".hg/hgrc"),
			authormap:    "",
			ignorename:   ".hgignore",
			dfltignores:  "",
			cookies:      reMake(`\b[0-9a-f]{40}\b`, `\b[0-9a-f]{12}\b`),
			project:      "https://github.com/kilork/hg-git-fast-import",
			notes: `The hg-git-fast-import method is not part of stock Mercurial.

If there is no branch named 'master' in a repo when it is read, the hg 'default'
branch is renamed to 'master'.
`,
		},
		{
			// Styleflags may need tweaking for round-tripping
			name:         "darcs",
			subdirectory: "_darcs",
			exporter:     "darcs fastconvert export",
			quieter:      "",
			styleflags:   newOrderedStringSet(),
			extensions:   newOrderedStringSet(),
			initializer:  "",
			pathlister:   "darcs show files",
			taglister:    "darcs show tags",
			branchlister: "",
			importer:     "darcs fastconvert import",
			checkout:     "",
			prenuke:      newOrderedStringSet(),
			preserve:     newOrderedStringSet(),
			authormap:    "",
			ignorename:   "_darcs/prefs/boring",
			dfltignores: `
# A simulation of darcs default ignores, generated by reposurgeon.
# haskell (ghc) interfaces
*.hi
*.hi-boot
*.o-boot
# object files
*.o
*.o.cmd
# profiling haskell
*.p_hi
*.p_o
# haskell program coverage resp. profiling info
*.tix
*.prof
# fortran module files
*.mod
# linux kernel
*.ko.cmd
*.mod.c
*.tmp_versions
# *.ko files aren't boring by default because they might
# be Korean translations rather than kernel modules
# *.ko
# python, emacs, java byte code
*.py[co]
*.elc
*.class
# objects and libraries; lo and la are libtool things
*.obj
*.a
*.exe
*.so
*.lo
*.la
# compiled zsh configuration files
*.zwc
# Common LISP output files for CLISP and CMUCL
*.fas
*.fasl
*.sparcf
*.x86f
### build and packaging systems
# cabal intermediates
*.installed-pkg-config
*.setup-config
# standard cabal build dir, might not be boring for everybody
# dist
# autotools
autom4te.cache
config.log
config.status
# microsoft web expression, visual studio metadata directories
*.\\_vti_cnf
*.\\_vti_pvt
# gentoo tools
*.revdep-rebuild.*
# generated dependencies
.depend
### version control
# darcs
_darcs
.darcsrepo
*.darcs-temp-mail
-darcs-backup[[:digit:]]+
# gnu arch
+
,
vssver.scc
*.swp
MT
{arch}
*.arch-ids
# bitkeeper
BitKeeper
ChangeSet
### miscellaneous
# backup files
*~
*.bak
*.BAK
# patch originals and rejects
*.orig
*.rej
# X server
..serverauth.*
# image spam
\\#
Thumbs.db
# vi, emacs tags
tags
TAGS
# core dumps
core
# partial broken files (KIO copy operations)
*.part
# mac os finder
.DS_Store
# Simulated darcs default ignores end here
`,
			cookies: reMake(),
			project: "http://darcs.net/",
			notes:   "Assumes no boringfile preference has been set.",
		},
		{
			name:         "mtn",
			subdirectory: "_MTN",
			exporter:     "mtn git_export",
			quieter:      "",
			styleflags:   newOrderedStringSet(),
			extensions:   newOrderedStringSet(),
			initializer:  "",
			pathlister:   "mtn list known",
			taglister:    "",
			branchlister: "",
			importer:     "",
			checkout:     "",
			prenuke:      newOrderedStringSet(),
			preserve:     newOrderedStringSet(),
			authormap:    "",
			ignorename:   ".mtn_ignore", // Assumes default hooks
			dfltignores: `
*.a
*.so
*.o
*.la
*.lo
^core
*.class
*.pyc
*.pyo
*.g?mo
*.intltool*-merge*-cache
*.aux
*.bak
*.orig
*.rej
%~
*.[^/]**.swp
*#[^/]*%#
*.scc
^*.DS_Store
/*.DS_Store
^desktop*.ini
/desktop*.ini
autom4te*.cache
*.deps
*.libs
*.consign
*.sconsign
CVS
*.svn
SCCS
_darcs
*.cdv
*.git
*.bzr
*.hg
`,
			cookies: reMake(),
			project: "http://www.monotone.ca/",
			notes:   "Exporter is buggy, occasionally emitting negative timestamps.",
		},
		{
			name:         "svn",
			subdirectory: "locks",
			exporter:     "svnadmin dump  .",
			quieter:      "--quiet",
			styleflags:   newOrderedStringSet("import-defaults", "export-progress"),
			extensions:   newOrderedStringSet(),
			initializer:  "svnadmin create .",
			importer:     "",
			checkout:     "",
			pathlister:   "",
			taglister:    "svn ls 'file://${pwd}/tags' | sed 's|/$||'",
			branchlister: "svn ls 'file://${pwd}/branches' | sed 's|/$||'",
			prenuke:      newOrderedStringSet(),
			preserve:     newOrderedStringSet("hooks"),
			authormap:    "",
			ignorename:   "",
			dfltignores:  subversionDefaultIgnores,
			cookies:      reMake(`\sr?\d+([.])?\s`),
			project:      "http://subversion.apache.org/",
			notes:        "Run from the repository, not a checkout directory.",
			checkignore:  ".svn",
		},
		{
			name:         "cvs",
			subdirectory: "CVSROOT", // Can't be Attic, that doesn't always exist.
			exporter:     "find . -name '*,v' -print | cvs-fast-export --reposurgeon",
			quieter:      "",
			styleflags:   newOrderedStringSet("import-defaults", "export-progress"),
			extensions:   newOrderedStringSet(),
			initializer:  "",
			importer:     "",
			checkout:     "",
			pathlister:   "",
			// CVS code will screw up if any tag is not common to all files
			// Hacks at https://stackoverflow.com/questions/6174742/how-to-get-a-list-of-tags-created-in-cvs-repository
			// would be better (fewer dependencies) but they seem to be for running in a checkout directory.
			taglister:    "module=`ls -1 | grep -v CVSROOT`; cvs -Q -d:local:${pwd} rlog -h $module 2>&1 | awk -F'[.:]' '/^\t/&&$(NF-1)!=0{print $1}' |awk '{print $1}' | sort -u",
			branchlister: "module=`ls -1 | grep -v CVSROOT`; cvs -Q -d:local:${pwd} rlog -h $module 2>&1 | awk -F'[.:]' '/^\t/&&$(NF-1)==0{print $1}' |awk '{print $1}' | sort -u",
			prenuke:      newOrderedStringSet(),
			preserve:     newOrderedStringSet(),
			authormap:    "",
			ignorename:   "",
			dfltignores: `
# A simulation of cvs default ignores, generated by reposurgeon.
tags
TAGS
.make.state
.nse_depinfo
*~
#*
.#*
,*
_$*
*$
*.old
*.bak
*.BAK
*.orig
*.rej
.del-*
*.a
*.olb
*.o
*.obj
*.so
*.exe
*.Z
*.elc
*.ln
core
# Simulated cvs default ignores end here
`,
			cookies:     reMake(dottedNumeric, dottedNumeric+`\w`),
			project:     "http://www.catb.org/~esr/cvs-fast-export",
			notes:       "Requires cvs-fast-export.",
			checkignore: "CVS",
		},
		{
			name:         "rcs",
			subdirectory: "RCS",
			exporter:     "find . -name '*,v' -print | cvs-fast-export --reposurgeon",
			quieter:      "",
			styleflags:   newOrderedStringSet("export-progress"),
			extensions:   newOrderedStringSet(),
			initializer:  "",
			importer:     "",
			checkout:     "",
			pathlister:   "",
			preserve:     newOrderedStringSet(),
			authormap:    "",
			ignorename:   "",
			dfltignores:  "", // Has none
			cookies:      reMake(dottedNumeric),
			project:      "http://www.catb.org/~esr/cvs-fast-export",
			notes:        "Requires cvs-fast-export.",
		},
		{
			name:         "src",
			subdirectory: ".src",
			exporter:     "src fast-export",
			quieter:      "",
			styleflags:   newOrderedStringSet(),
			extensions:   newOrderedStringSet(),
			initializer:  "src init",
			importer:     "",
			checkout:     "",
			pathlister:   "src ls",
			prenuke:      newOrderedStringSet(),
			preserve:     newOrderedStringSet(),
			authormap:    "",
			ignorename:   "",
			dfltignores:  "", // Has none
			cookies:      reMake(tokenNumeric),
			project:      "http://catb.org/~esr/src",
			notes:        "",
		},
		{
			// Styleflags may need tweaking for round-tripping
			name:         "bk",
			subdirectory: ".bk",
			exporter:     "bk fast-export --no-bk-keys",
			quieter:      "-q",
			styleflags:   newOrderedStringSet(),
			extensions:   newOrderedStringSet(),
			initializer:  "", // bk setup doesn't work here
			pathlister:   "bk gfiles -U",
			taglister:    "bk tags | sed -n 's/ *TAG: *//p'",
			branchlister: "",
			importer:     "bk fast-import -q",
			checkout:     "",
			prenuke:      newOrderedStringSet(),
			preserve:     newOrderedStringSet(),
			authormap:    "",
			ignorename:   "BitKeeper/etc/ignore",
			dfltignores:  "",                    // Has none
			cookies:      reMake(dottedNumeric), // Same as SCCS/CVS
			project:      "https://www.bitkeeper.com/",
			// No tag support, and a tendency to core-dump
			notes: "Bitkeeper's importer is flaky and incomplete as of 7.3.1ce.",
		},
	}

	svntype = &vcstypes[5]
}

// Import and export filter methods for VCSes that use magic files rather
// than magic directories. So far there is only one of these.
//
// Each entry maps a read/write option to an (importer, exporter) pair.
// The input filter must be an *exporter from* that takes an alien file
// and emits a fast-import stream on standard output.  The exporter
// must be an *importer to* that takes an import stream on standard input
// and produces a named alien file.
//
var fileFilters = map[string]struct {
	importer string
	exporter string
}{
	"fossil": {"fossil export --git %s", "fossil import --git %s"},
}

// findVCS finds a VCS by name
func findVCS(name string) *VCS {
	for _, vcs := range vcstypes {
		if vcs.name == name {
			return &vcs
		}
	}
	panic(fmt.Sprintf("reposurgeon: failed to find '%s' in VCS types (len %d)", name, len(vcstypes)))
}

// identifyRepo finds what type of repo we're looking at.
func identifyRepo(dirname string) *VCS {
	for _, vcs := range vcstypes {
		if vcs.manages(dirname) {
			return &vcs
		}
	}
	return nil
}

// end
