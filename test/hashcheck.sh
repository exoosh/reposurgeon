#!/bin/sh
## Verify git hash computation

set -e
repo=${PWD}/hashcheck-git-$$

trap 'rm -fr ${repo}' EXIT HUP INT QUIT TERM

command -v git >/dev/null 2>&1 || ( echo "not ok - $0: git is not installed # SKIP"; exit 1 )

version_gt() { test "$(printf '%s\n' "$@" | sort -V | head -n 1)" != "$1"; }

# shellcheck disable=SC2046
set -- $(git --version)
version="$3"
if version_gt "2.25.1" "$version";
then
    # This script will have mysterious failures if the local git chokes
    # on --show-original-ids.
    echo "not ok - $0: sensitive to Git version skew, seeing unsupported $version # SKIP"
    exit 0
fi

#  Elijah Newren <newren@gmail.com>> Most of this is explained at
# > https://git-scm.com/book/en/v2/Git-Internals-Git-Objects, though they
# > don't really cover the internals of tree objects.  You can learn
# > most of this stuff as well by playing around with the hash-object,
# > mktree, commit-tree, write-tree, and cat-file subcommands of git, but
# > it might be easiest to just demonstrate how to construct a full commit
# > hash using shell commands and comparing to what git reports, and doing
# > so a couple different ways for each step:

git init --quiet "${repo}"
cd "${repo}" >/dev/null
# Required for CI
git config user.email jrh@random.net
git config user.name "J. Random Hacker"
mkdir some
echo 'Hello, World!' >some/file.txt
git add some/file.txt

# shellcheck disable=SC2034
GIT_AUTHOR_NAME="John Doe" GIT_AUTHOR_EMAIL="john@doe.com" GIT_AUTHOR_DATE="1234567890" 
# shellcheck disable=SC2034
GIT_COMMITTER_NAME="John Doe" GIT_COMMITTER_EMAIL="john@doe.com" GIT_COMMITTER_DATE="1234567890"
# shellcheck disable=SC2034
TZ="America/Los_Angeles"
export GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_AUTHOR_DATE GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL GIT_COMMITTER_DATE TZ

git commit --quiet  -a -m "Lovely commit message"

# Verify blob hash computation
filehash=$(git hash-object some/file.txt)
# shellcheck disable=SC2046
set -- $(reposurgeon 'read .' ':1 hash')

if [ "${filehash}" != "$2" ]
then
    echo "not ok - $0: file and synthetic hash for some/file.txt do not match ($filehash != $2)."
    exit 1
fi

# > $ # Ask git for the inner tree hash, two different ways, then duplicate
# > $ git rev-parse $(git write-tree):some
# > 08687c1be8a39bde242c31d308baa4aba277dc02
# > $ git rev-parse HEAD:some
# > 08687c1be8a39bde242c31d308baa4aba277dc02
# > $ printf "100644 blob
# > 8ab686eafeb1f44702738c8b0f24f2567c36da6d\tfile.txt" | git mktree
# > 08687c1be8a39bde242c31d308baa4aba277dc02
# > $ (printf "tree 36\0"; printf "100644 file.txt\0"; echo -n
# > 8ab686eafeb1f44702738c8b0f24f2567c36da6d | xxd -p -r) | sha1sum
# > 08687c1be8a39bde242c31d308baa4aba277dc02  -
# > 
# > $ # Ask git for the outer tree hash, two different ways, then duplicate
# > $ git write-tree
# > 62296ca7563b5d575acb0a914442f78f3a76db4d
# > $ git rev-parse HEAD^{tree}
# > 62296ca7563b5d575acb0a914442f78f3a76db4d
# > $ printf "040000 tree 08687c1be8a39bde242c31d308baa4aba277dc02\tsome"
# > | git mktree
# > 62296ca7563b5d575acb0a914442f78f3a76db4d
# > $ (printf "tree 31\0"; printf "40000 some\0"; printf
# > 08687c1be8a39bde242c31d308baa4aba277dc02 | xxd -p -r) | sha1sum
# > 62296ca7563b5d575acb0a914442f78f3a76db4d

treehash=$(git rev-parse "HEAD^{tree}")
# shellcheck disable=SC2046
set -- $(reposurgeon 'read .' ':2 hash --tree')

if [ "${treehash}" != "$2" ]
then
    echo "not ok - $0: tree and synthetic hashes do not match."
    exit 1
fi

# > $ # Ask git for the commit hash, then recreate it two different ways
# > $ git rev-parse HEAD
# > bfce3b33968e8735e722754ceb89c8756454df1a
# > $ GIT_AUTHOR_NAME="John Doe" GIT_AUTHOR_EMAIL="john@doe.com"
# > GIT_AUTHOR_DATE="1234567890" GIT_COMMITTER_NAME="John Doe"
# > GIT_COMMITTER_EMAIL="john@doe.com" GIT_COMMITTER_DATE="1234567890" git
# > commit-tree -m "Lovely commit message"
# > 62296ca7563b5d575acb0a914442f78f3a76db4
# > bfce3b33968e8735e722754ceb89c8756454df1a
# > $ printf "commit 168\0tree
# > 62296ca7563b5d575acb0a914442f78f3a76db4d\nauthor John Doe
# > <john@doe.com> 1234567890 -0800\ncommitter John Doe <john@doe.com>
# > 1234567890 -0800\n\nLovely commit message\n" | sha1sum
# > bfce3b33968e8735e722754ceb89c8756454df1a  -

commithash=$(git rev-parse "HEAD")
# shellcheck disable=SC2046
set -- $(reposurgeon 'read .' ':2 hash')

if [ "${commithash}" != "$2" ]
then
    echo "not ok - $0: commit ${commithash} at :2 and synthetic ${2} hashes do not match."
    exit 1
fi

echo "New content." >some/file.txt
git commit --quiet -a -m "A commit through porcelain."

commithash=$(git rev-parse "HEAD")
# shellcheck disable=SC2046
set -- $(reposurgeon 'read .' ':4 hash')

if [ "${commithash}" != "$2" ]
then
    echo "not ok - $0: commit ${commithash} at :4 and synthetic ${2} hashes do not match."
    exit 1
fi

echo "ok - $0: succeeded"; exit 0

# > Some notes:
# > 
# >   * The number before the first null byte is the number of characters
# >     after the first null byte.  Yes, it's redundant information but
# >     allows git to quickly determine the size of objects.
# >   * No newlines or anything separating multiple files in a tree object;
# >     the space character, nul character, and known length of a sha1sum
# >     are all that are needed.
# >   * Although ls-tree output always shows six characters for object modes,
# >     leading zeros are stripped when writing into a tree object.
# >   * Any parents to record in commit objects are listed on separate lines
# >     between the tree and the author.  Use 'git cat-file -p' on any commit
# >     in a repo of your choosing to see an example.

# No output is good news
 
# end
