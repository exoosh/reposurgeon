#
# makefile for reposurgeon
#
INSTALL=install
prefix?=/usr/local
mandir?=share/man
target=$(DESTDIR)$(prefix)

VERS=$(shell sed <surgeon/reposurgeon.go -n -e '/const *version *= *\"\(.*\)\"/s//\1/p')
SOURCES = \
	reposurgeon.adoc \
	repotool repotool.adoc \
	cutter/repocutter.go \
	mapper/repomapper.go \
	surgeon/reposurgeon.go \
	surgeon/reposurgeon_test.go \
	surgeon/baton.go \
	surgeon/extractor.go \
	surgeon/hgclient.go \
	surgeon/reposurgeon.go \
	surgeon/reposurgeon_test.go \
	surgeon/selection.go \
	surgeon/svnread.go \
	repobench repobench.adoc \
	repomapper.adoc repocutter.adoc \
	reporting-bugs.adoc repository-editing.adoc oops.svg dvcs-migration-guide.adoc \
	reposurgeon-mode.el \
	go.mod go.sum
SOURCES += Makefile control lintfilter reposturgeon.png reposurgeon-git-aliases
SOURCES += Dockerfile ci/prepare.sh .gitlab-ci.yml
DOCS = README.adoc INSTALL.adoc NEWS AUTHORS COPYING

STOPOUT=1

.PHONY: all install clean uninstall version check release refresh \
    docker-build docker-check docker-check-noscm \
    vet test fmt lint

BINARIES = reposurgeon repotool repomapper repocutter repobench
MANPAGES = reposurgeon.1 repotool.1 repomapper.1 repocutter.1 repobench.1
HTMLFILES = $(MANPAGES:.1=.html) \
            repository-editing.html dvcs-migration-guide.html reporting-bugs.html
SHARED    = $(DOCS) reposurgeon-git-aliases $(HTMLFILES)

# The following would produce reproducible builds, but it breaks Gitlab CI.
#GOFLAGS=-gcflags 'all=-N -l -trimpath $(GOPATH)/src' -asmflags 'all=-trimpath $(GOPATH)/src'

GOFLAGS=-gcflags '-N -l'
build:  $(MANPAGES) $(HTMLFILES)
	go build $(GOFLAGS) -o repocutter ./cutter
	go build $(GOFLAGS) -o repomapper ./mapper
	go build $(GOFLAGS) -o reposurgeon ./surgeon

# Requires asciidoctor and xsltproc/docbook stylesheets.
# Note: to suppress the footers with timestamps being generated in HTML,
# we use "-a nofooter".
# To debug asciidoc problems, you may need to run "xmllint" --nonet --noout --valid"
# on the intermediate XML.
.SUFFIXES: .html .adoc .1

.adoc.1:
	asciidoctor -a nofooter -b manpage $<
.adoc.html:
	asciidoctor -a webfonts! $<

#
# Auxilary Go tooling productions
#

get:
	go get -u ./...	# go get -u=patch for patch releases

test:
	go test $(TESTOPTS) ./surgeon
	go test $(TESTOPTS) ./cutter

PYLINTOPTS = --rcfile=/dev/null --reports=n \
	--msg-template="{path}:{line}: [{msg_id}({symbol}), {obj}] {msg}" \
	--dummy-variables-rgx='^_'
# W0612 is regrettable, but plying doesn't count %-substitutions
PYSUPPRESSIONS = --disable="C0103,C0111,C0301,C0410,C1801,R0911,R0911,R0912,R0914,R0915,W0511,W0612,W0622"
lint:
	golint ./... | ./lintfilter 2>&1
	-shellcheck -f gcc repobench lintfilter test/fi-to-fi test/liftcheck test/singlelift test/svn-to-git test/svn-to-svn test/*.sh test/*test
	-pylint $(PYLINTOPTS) $(PYSUPPRESSIONS) repotool

fmt:
	gofmt -w .

#
# Installation
#

install: all
	$(INSTALL) -d "$(target)/bin"
	$(INSTALL) -d "$(target)/share/doc/reposurgeon"
	$(INSTALL) -d "$(target)/$(mandir)/man1"
	$(INSTALL) -m 755 $(BINARIES) "$(target)/bin"
	$(INSTALL) -m 644 $(SHARED) "$(target)/share/doc/reposurgeon"
	$(INSTALL) -m 644 $(MANPAGES) "$(target)/$(mandir)/man1"

clean:
	rm -fr reposurgeon repocutter repomapper
	rm -fr  *~ *.1 *.html *.tar.xz MANIFEST *.md5
	rm -fr .rs .rs* test/.rs test/.rs*
	rm -f typescript test/typescript *.pyc

# Uninstallation
INSTALLED_BINARIES := $(BINARIES:%="$(target)/bin/%")
INSTALLED_SHARED   := $(SHARED:%="$(target)/share/doc/reposurgeon/%")
INSTALLED_MANPAGES := $(MANPAGES:%="$(target)/$(mandir)/man1/%")

uninstall:
	rm -f $(INSTALLED_BINARIES)
	rm -f $(INSTALLED_MANPAGES)
	rm -f $(INSTALLED_SHARED)
	rmdir "$(target)/share/doc/reposurgeon"

version:
	@echo $(VERS)

#
# Code validation
#

check: lint build test
	cd test; $(MAKE) --quiet check

#
# Continuous integration.  More specifics are in the ci/ directory
#

docker-build: $(SOURCES)
	docker build -t reposurgeon .

docker-check: docker-build
	docker run --rm -i -e "MAKEFLAGS=$(MAKEFLAGS)" -e "MAKEOVERRIDES=$(MAKEOVERRIDES)" reposurgeon make check

docker-check-only-%: docker-build
	docker run --rm -i -e "MAKEFLAGS=$(MAKEFLAGS)" -e "MAKEOVERRIDES=$(MAKEOVERRIDES)" reposurgeon bash -c "make -C ci install-only-$(*) && make check"

docker-check-no-%: docker-build
	docker run --rm -i -e "MAKEFLAGS=$(MAKEFLAGS)" -e "MAKEOVERRIDES=$(MAKEOVERRIDES)" reposurgeon bash -c "make -C ci install-no-$(*) && make check"

# Test that support for each VCS stands on its own and test without legacy
# VCS installed
docker-check-noscm: docker-check-only-bzr docker-check-only-cvs \
    docker-check-only-git docker-check-only-mercurial \
    docker-check-only-subversion docker-check-no-cvs 
# Due to many tests depending on git, docker-check-only-mercurial is a very poor
# test of Mercurial

#
# Release shipping.
#

reposurgeon-$(VERS).tar.xz: $(SOURCES) $(DOCS)
	tar --transform='s:^:reposurgeon-$(VERS)/:' --show-transformed-names -cJf reposurgeon-$(VERS).tar.xz $(SOURCES) $(DOCS) test

dist: reposurgeon-$(VERS).tar.xz reposurgeon.1 repocutter.1 repotool.1 repobench.1 repomapper.1

reposurgeon-$(VERS).md5: reposurgeon-$(VERS).tar.xz
	@md5sum reposurgeon-$(VERS).tar.xz >reposurgeon-$(VERS).md5

release: reposurgeon-$(VERS).tar.xz reposurgeon-$(VERS).md5 reposurgeon.html repocutter.html repomapper.html reporting-bugs.html repository-editing.html dvcs-migration-guide.html
	shipper version=$(VERS) | sh -e -x

refresh: reposurgeon.html repocutter.html repomapper.html reporting-bugs.html repository-editing.html dvcs-migration-guide.html
	shipper -N -w version=$(VERS) | sh -e -x
