#!/bin/sh
## Test comparing master between svn and git repo

# Results should be independent of what file stem this is, as
# long as it's an svn dump.
stem=vanilla

# No user-serviceable parts below this line

# shellcheck disable=SC1091
. ./common-setup.sh

need svn git

trap 'rm -rf /tmp/test-repo$$-svn /tmp/test-repo$$-git /tmp/test-repo$$-svn-checkout /tmp/out$$' EXIT HUP INT QUIT TERM

./svn-to-svn -q -c /tmp/test-repo$$-svn /tmp/test-repo$$-svn-checkout <${stem}.svn
reposurgeon "read <${stem}.svn" "prefer git" "rebuild /tmp/test-repo$$-git" >/tmp/out$$ 2>&1
${REPOTOOL:-repotool} compare /tmp/test-repo$$-svn-checkout /tmp/test-repo$$-git | sed -e "s/$$/\$\$/"g >>/tmp/out$$ 2>&1

toolmeta "$1" /tmp/out$$
	      
# end
