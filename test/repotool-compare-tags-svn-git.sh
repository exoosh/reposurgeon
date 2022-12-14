#!/bin/sh
## Test comparing tags between svn and git repo

# Should be a multiple-tag repository
stem=simpletag

# No user-serviceable parts below this line

# shellcheck disable=SC1091
. ./common-setup.sh

need svn git

trap 'rm -rf /tmp/test-repo$$-svn /tmp/test-repo$$-git /tmp/out$$' EXIT HUP INT QUIT TERM

./svn-to-svn -q -n /tmp/test-repo$$-svn <"$stem.svn"
reposurgeon "read <${stem}.svn" "prefer git" "rebuild /tmp/test-repo$$-git" >/tmp/out$$ 2>&1
${REPOTOOL:-repotool} compare-tags /tmp/test-repo$$-svn /tmp/test-repo$$-git | sed -e "s/$$/\$\$/"g >/tmp/out$$ 2>&1

toolmeta "$1" /tmp/out$$
	      
# end
